package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/Orfeo42/ai-orchestration/internal/events"
	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/internal/step"
	"github.com/Orfeo42/ai-orchestration/internal/workflow"
)

const waitingOutcome = "waiting"

type Hooks struct {
	BeforeStep func(ctx context.Context, def step.Def)
}

type StepWorker struct {
	river.WorkerDefaults[StepArgs]
	engine    *Engine
	completer Completer
	handles   HandleFactory
	hooks     Hooks
}

func NewStepWorker(e *Engine, c Completer, handles HandleFactory, hooks Hooks) *StepWorker {
	return &StepWorker{engine: e, completer: c, handles: handles, hooks: hooks}
}

func (w *StepWorker) Timeout(job *river.Job[StepArgs]) time.Duration {
	if job.Args.TimeoutSeconds > 0 {
		return time.Duration(job.Args.TimeoutSeconds) * time.Second
	}
	return w.engine.cfg.DefaultTimeout
}

type prepared struct {
	def     workflow.Definition
	stepDef workflow.StepDef
	decl    step.Def
	runner  step.Step
}

type execution struct {
	job   *river.Job[StepArgs]
	proc  process.Process
	pr    prepared
	runID string
}

func (w *StepWorker) prepare(ctx context.Context, p process.Process, stepID string) (prepared, error) {
	e := w.engine
	def, err := e.definition(ctx, e.store, p.WorkflowVersionID)
	if err != nil {
		return prepared{}, err
	}
	sd, err := def.Step(stepID)
	if err != nil {
		return prepared{}, err
	}
	d, ok := e.reg.Lookup(sd.Uses)
	if !ok {
		return prepared{}, fmt.Errorf("%w: %s", step.ErrUnknownStep, sd.Uses)
	}
	s, err := e.reg.Build(sd.Uses, sd.With)
	if err != nil {
		return prepared{}, err
	}
	return prepared{def: def, stepDef: sd, decl: d, runner: s}, nil
}

func (w *StepWorker) complete(ctx context.Context, job *river.Job[StepArgs]) error {
	err := w.engine.store.InTx(ctx, func(ctx context.Context, tx process.Tx) error {
		return w.completer.CompleteTx(ctx, tx.SQL(), job)
	})
	if err != nil {
		return fmt.Errorf("complete job %d: %w", job.ID, err)
	}
	return nil
}

func (w *StepWorker) Work(ctx context.Context, job *river.Job[StepArgs]) error {
	e := w.engine
	p, err := e.store.GetProcess(ctx, job.Args.ProcessID)
	if err != nil {
		return fmt.Errorf("get process: %w", err)
	}
	if p.Status != process.StatusRunning || p.CurrentJobID == nil || *p.CurrentJobID != job.ID {
		return w.complete(ctx, job)
	}
	bg := context.WithoutCancel(ctx)
	x := execution{job: job, proc: p}
	x.pr, err = w.prepare(ctx, p, job.Args.StepID)
	if err != nil {
		return w.finalFail(bg, x, err, process.ErrorKindInternal)
	}
	x.runID = uuid.NewString()
	err = e.store.InTx(ctx, func(ctx context.Context, tx process.Tx) error {
		return tx.StartStepRun(ctx, process.StepRun{
			ID:         x.runID,
			ProcessID:  p.ID,
			StepID:     job.Args.StepID,
			Uses:       x.pr.stepDef.Uses,
			Plugin:     x.pr.decl.Plugin,
			Attempt:    job.Args.AttemptBase + job.Attempt,
			RiverJobID: job.ID,
			WorkerID:   e.cfg.WorkerID,
			Status:     process.StepRunning,
			StartedAt:  time.Now().UTC(),
		})
	})
	if errors.Is(err, process.ErrNotActive) {
		return w.complete(ctx, job)
	}
	if err != nil {
		return fmt.Errorf("start step run: %w", err)
	}
	if w.hooks.BeforeStep != nil {
		w.hooks.BeforeStep(ctx, x.pr.decl)
	}
	h := w.handles(ctx, x.runID)
	defer h.Cancel()
	res, runErr := x.pr.runner.Run(h.Ctx(), step.Context{
		Process:   p,
		StepID:    job.Args.StepID,
		StepRunID: x.runID,
		Attempt:   job.Args.AttemptBase + job.Attempt,
		Timeout:   time.Duration(job.Args.TimeoutSeconds) * time.Second,
		WorkerID:  e.cfg.WorkerID,
		Handle:    h,
	})
	remoteCancel := errors.Is(context.Cause(ctx), river.ErrJobCancelledRemotely)
	controlCancel := errors.Is(runErr, context.Canceled) && (ctx.Err() == nil || remoteCancel)
	phase := process.StepSucceeded
	switch {
	case controlCancel:
		phase = process.StepCancelled
	case runErr != nil:
		phase = process.StepFailed
	}
	jobWait := runErr == nil && res.Wait != nil && res.Wait.Kind == process.WaitJob
	if !jobWait {
		h.Emit(bg, events.Event{Kind: events.KindRunEnd, Phase: string(phase)})
	}
	switch {
	case runErr == nil && res.Wait != nil:
		return w.wait(bg, x, res)
	case runErr == nil:
		return w.succeed(bg, x, res)
	case controlCancel:
		return w.cancelled(bg, x, runErr, remoteCancel)
	case ctx.Err() != nil:
		return w.interrupted(bg, x, runErr)
	}
	return w.failed(bg, x, runErr)
}

func (w *StepWorker) waitJob(ctx context.Context, x execution, res step.Result) error {
	if res.Wait.Job == nil {
		return w.finalFail(ctx, x, ErrMissingJobWait, process.ErrorKindInternal)
	}
	superseded := false
	err := w.engine.store.InTx(ctx, func(ctx context.Context, tx process.Tx) error {
		if err := tx.MergeVars(ctx, x.proc.ID, res.Vars); err != nil {
			return fmt.Errorf("merge vars: %w", err)
		}
		ok, err := tx.SetWaiting(ctx, x.proc.ID, process.Wait{
			Kind:  process.WaitJob,
			Step:  x.job.Args.StepID,
			Since: time.Now().UTC(),
			Job: &process.JobWait{
				StepRunID:        x.runID,
				Plugin:           res.Wait.Job.Plugin,
				Attempt:          x.job.Args.AttemptBase + x.job.Attempt,
				HeartbeatSeconds: res.Wait.Job.HeartbeatSeconds,
				Deadline:         res.Wait.Job.Deadline,
			},
		})
		if err != nil {
			return fmt.Errorf("set waiting: %w", err)
		}
		if ok {
			return w.completer.CompleteTx(ctx, tx.SQL(), x.job)
		}
		superseded = true
		if _, err := tx.FinishStepRun(ctx, process.StepResult{StepRunID: x.runID, Status: process.StepCancelled}); err != nil {
			return fmt.Errorf("finish superseded step run: %w", err)
		}
		return w.completer.CompleteTx(ctx, tx.SQL(), x.job)
	})
	if err != nil {
		return fmt.Errorf("settle job wait: %w", err)
	}
	if superseded {
		w.cancelSupersededJob(ctx, x, res.Wait.Job.Plugin)
		return nil
	}
	slog.InfoContext(ctx, "step waiting for plugin job",
		slog.String("process_id", x.proc.ID),
		slog.String("step", x.job.Args.StepID),
		slog.String("plugin", res.Wait.Job.Plugin),
		slog.String("step_run_id", x.runID),
	)
	return nil
}

func (w *StepWorker) cancelSupersededJob(ctx context.Context, x execution, plugin string) {
	slog.InfoContext(ctx, "plugin job superseded before it was recorded, cancelling",
		slog.String("process_id", x.proc.ID),
		slog.String("step_run_id", x.runID),
		slog.String("plugin", plugin),
	)
	if w.engine.cfg.Jobs == nil {
		return
	}
	if err := w.engine.cfg.Jobs.CancelJob(ctx, plugin, x.runID); err != nil {
		slog.WarnContext(ctx, "cancel superseded plugin job failed",
			slog.String("step_run_id", x.runID),
			slog.String("plugin", plugin),
			slog.Any("error", err),
		)
	}
}

func (w *StepWorker) wait(ctx context.Context, x execution, res step.Result) error {
	if res.Wait.Kind == process.WaitJob {
		return w.waitJob(ctx, x, res)
	}
	err := w.engine.store.InTx(ctx, func(ctx context.Context, tx process.Tx) error {
		ok, err := tx.FinishStepRun(ctx, process.StepResult{
			StepRunID: x.runID,
			Status:    process.StepSucceeded,
			Outcome:   waitingOutcome,
			Output:    res.Vars,
		})
		if err != nil {
			return fmt.Errorf("finish step run: %w", err)
		}
		if !ok {
			return w.completer.CompleteTx(ctx, tx.SQL(), x.job)
		}
		if err := tx.MergeVars(ctx, x.proc.ID, res.Vars); err != nil {
			return fmt.Errorf("merge vars: %w", err)
		}
		ok, err = tx.SetWaiting(ctx, x.proc.ID, process.Wait{
			Kind:          res.Wait.Kind,
			Step:          x.job.Args.StepID,
			Since:         time.Now().UTC(),
			ResumeOutcome: string(res.Wait.ResumeOutcome),
		})
		if err != nil {
			return fmt.Errorf("set waiting: %w", err)
		}
		if !ok {
			return fmt.Errorf("%w: cannot set waiting", process.ErrNotActive)
		}
		return w.completer.CompleteTx(ctx, tx.SQL(), x.job)
	})
	if err != nil {
		return fmt.Errorf("settle waiting step: %w", err)
	}
	slog.InfoContext(ctx, "step waiting",
		slog.String("process_id", x.proc.ID),
		slog.String("step", x.job.Args.StepID),
		slog.String("outcome", waitingOutcome),
	)
	return nil
}

func (w *StepWorker) succeed(ctx context.Context, x execution, res step.Result) error {
	if !x.pr.decl.Declares(res.Outcome) {
		return w.finalFail(ctx, x, fmt.Errorf("%w: %s", step.ErrUndeclaredResult, res.Outcome), process.ErrorKindInternal)
	}
	err := w.engine.store.InTx(ctx, func(ctx context.Context, tx process.Tx) error {
		ok, err := tx.FinishStepRun(ctx, process.StepResult{
			StepRunID: x.runID,
			Status:    process.StepSucceeded,
			Outcome:   string(res.Outcome),
			Output:    res.Vars,
		})
		if err != nil {
			return fmt.Errorf("finish step run: %w", err)
		}
		if !ok {
			return w.completer.CompleteTx(ctx, tx.SQL(), x.job)
		}
		if err := tx.MergeVars(ctx, x.proc.ID, res.Vars); err != nil {
			return fmt.Errorf("merge vars: %w", err)
		}
		err = w.engine.advance(ctx, tx, x.proc.ID, x.pr.def, x.job.Args.StepID, res.Outcome, []process.Status{process.StatusRunning})
		if err != nil {
			return err
		}
		return w.completer.CompleteTx(ctx, tx.SQL(), x.job)
	})
	if err != nil {
		return fmt.Errorf("settle succeeded step: %w", err)
	}
	slog.InfoContext(ctx, "step finished",
		slog.String("process_id", x.proc.ID),
		slog.String("step", x.job.Args.StepID),
		slog.String("outcome", string(res.Outcome)),
	)
	return nil
}

func (w *StepWorker) cancelled(ctx context.Context, x execution, runErr error, remote bool) error {
	err := w.engine.store.InTx(ctx, func(ctx context.Context, tx process.Tx) error {
		ok, err := tx.FinishStepRun(ctx, process.StepResult{StepRunID: x.runID, Status: process.StepCancelled})
		if err != nil {
			return fmt.Errorf("finish step run: %w", err)
		}
		if !ok {
			return ErrStepRunClosed
		}
		if remote {
			return nil
		}
		return w.completer.CompleteTx(ctx, tx.SQL(), x.job)
	})
	if err != nil {
		return fmt.Errorf("settle cancelled step: %w", err)
	}
	slog.InfoContext(ctx, "step cancelled",
		slog.String("process_id", x.proc.ID),
		slog.String("step", x.job.Args.StepID),
		slog.String("outcome", string(process.StepCancelled)),
	)
	if remote {
		return runErr
	}
	return nil
}

func (w *StepWorker) closeRun(ctx context.Context, x execution, runErr error, kind process.ErrorKind) error {
	err := w.engine.store.InTx(ctx, func(ctx context.Context, tx process.Tx) error {
		return w.failRun(ctx, tx, x, runErr, kind)
	})
	if err != nil {
		return fmt.Errorf("settle failed step: %w", err)
	}
	return nil
}

func (w *StepWorker) failRun(ctx context.Context, tx process.Tx, x execution, runErr error, kind process.ErrorKind) error {
	if x.runID == "" {
		return nil
	}
	ok, err := tx.FinishStepRun(ctx, process.StepResult{
		StepRunID: x.runID,
		Status:    process.StepFailed,
		Error:     runErr.Error(),
		ErrorKind: kind,
	})
	if err != nil {
		return fmt.Errorf("finish step run: %w", err)
	}
	if !ok {
		return ErrStepRunClosed
	}
	return nil
}

func (w *StepWorker) interrupted(ctx context.Context, x execution, runErr error) error {
	if err := w.closeRun(ctx, x, runErr, process.ErrorKindInternal); err != nil {
		return err
	}
	w.logFailure(ctx, x, runErr, process.ErrorKindInternal)
	return runErr
}

func (w *StepWorker) failed(ctx context.Context, x execution, runErr error) error {
	if errors.Is(runErr, step.ErrPermanent) {
		return w.finalFail(ctx, x, runErr, process.ErrorKindInternal)
	}
	kind := process.ErrorKindInternal
	if errors.Is(runErr, step.ErrUpstream) {
		kind = process.ErrorKindUpstream
	}
	if x.job.Attempt >= x.job.MaxAttempts {
		return w.finalFail(ctx, x, runErr, kind)
	}
	if err := w.closeRun(ctx, x, runErr, kind); err != nil {
		return err
	}
	w.logFailure(ctx, x, runErr, kind)
	return runErr
}

func (w *StepWorker) finalFail(ctx context.Context, x execution, runErr error, kind process.ErrorKind) error {
	err := w.engine.store.InTx(ctx, func(ctx context.Context, tx process.Tx) error {
		if err := w.failRun(ctx, tx, x, runErr, kind); err != nil {
			return err
		}
		return w.engine.finishProcess(ctx, tx, x.proc.ID, process.Finish{
			Status:    process.StatusFailed,
			Error:     runErr.Error(),
			ErrorKind: kind,
		})
	})
	if err != nil {
		return fmt.Errorf("settle final failure: %w", err)
	}
	w.logFailure(ctx, x, runErr, kind)
	return river.JobCancel(runErr)
}

func (w *StepWorker) logFailure(ctx context.Context, x execution, runErr error, kind process.ErrorKind) {
	attrs := []any{
		slog.String("process_id", x.proc.ID),
		slog.String("step", x.job.Args.StepID),
		slog.Int("attempt", x.job.Attempt),
		slog.String("error_kind", string(kind)),
		slog.Any("error", runErr),
	}
	if kind == process.ErrorKindUpstream {
		slog.WarnContext(ctx, "step failed", attrs...)
		return
	}
	slog.ErrorContext(ctx, "step failed", attrs...)
}
