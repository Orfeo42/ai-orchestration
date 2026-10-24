package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/Orfeo42/ai-orchestration/internal/events"
	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/internal/runstore"
	"github.com/Orfeo42/ai-orchestration/internal/step"
	"github.com/Orfeo42/ai-orchestration/internal/workflow"
)

//go:generate moq -rm -out engine_mock.go . Queue:MockQueue Completer:MockCompleter Control:MockControl

type engineError string

func (e engineError) Error() string { return string(e) }

const (
	ErrStepRunClosed  engineError = "step run already closed"
	ErrMissingJobWait engineError = "job wait without job details"
)

type StepArgs struct {
	ProcessID      string `json:"process_id"`
	StepID         string `json:"step_id"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	AttemptBase    int    `json:"attempt_base,omitempty"`
}

func (StepArgs) Kind() string { return "step" }

type StepOpts struct {
	Queue       string
	MaxAttempts int
}

type Queue interface {
	InsertStepTx(ctx context.Context, tx *sql.Tx, args StepArgs, opts StepOpts) (int64, error)
	CancelJobTx(ctx context.Context, tx *sql.Tx, jobID int64) error
}

type Completer interface {
	CompleteTx(ctx context.Context, tx *sql.Tx, job *river.Job[StepArgs]) error
}

type Control interface {
	Cancel(ctx context.Context, runID string) error
}

type JobCanceller interface {
	CancelJob(ctx context.Context, plugin, jobID string) error
}

type JobResult struct {
	StepRunID string
	Outcome   step.Outcome
	Output    json.RawMessage
	Error     string
	ErrorKind process.ErrorKind
	Usage     events.TokenUsage
}

type JobProgress struct {
	StepRunID string
	Phase     string
	Usage     *events.TokenUsage
}

type HandleFactory func(ctx context.Context, stepRunID string) *runstore.RunHandle

type Config struct {
	WorkerID       string
	DefaultTimeout time.Duration
	Jobs           JobCanceller
}

type StartRequest struct {
	WorkflowVersionID int64
	Subject           process.Subject
	Title             string
	Project           string
	Vars              process.Vars
}

type StartResult struct {
	Process process.Process
	Created bool
}

type Engine struct {
	store process.Store
	reg   step.Catalog
	q     Queue
	ctrl  Control
	cfg   Config
}

func New(store process.Store, reg step.Catalog, q Queue, ctrl Control, cfg Config) *Engine {
	return &Engine{store: store, reg: reg, q: q, ctrl: ctrl, cfg: cfg}
}

func (e *Engine) definition(ctx context.Context, q process.Querier, versionID int64) (workflow.Definition, error) {
	version, err := q.GetWorkflowVersion(ctx, versionID)
	if err != nil {
		return workflow.Definition{}, fmt.Errorf("get workflow version %d: %w", versionID, err)
	}
	var def workflow.Definition
	if err := json.Unmarshal(version.Definition, &def); err != nil {
		return workflow.Definition{}, fmt.Errorf("decode workflow version %d: %w", versionID, err)
	}
	return def, nil
}

func (e *Engine) scheduleStep(ctx context.Context, tx process.Tx, processID string, def workflow.Definition, stepID string, from []process.Status, attemptBase int) error {
	sd, err := def.Step(stepID)
	if err != nil {
		return err
	}
	d, ok := e.reg.Lookup(sd.Uses)
	if !ok {
		return fmt.Errorf("%w: %s", step.ErrUnknownStep, sd.Uses)
	}
	args := StepArgs{
		ProcessID:      processID,
		StepID:         stepID,
		TimeoutSeconds: int(time.Duration(sd.Timeout).Seconds()),
		AttemptBase:    attemptBase,
	}
	jobID, err := e.q.InsertStepTx(ctx, tx.SQL(), args, StepOpts{Queue: d.Queue, MaxAttempts: max(1, sd.Retry+1-attemptBase)})
	if err != nil {
		return fmt.Errorf("insert step job %s: %w", stepID, err)
	}
	ok, err = tx.SetNext(ctx, processID, process.Next{From: from, Step: stepID, JobID: jobID})
	if err != nil {
		return fmt.Errorf("set next step %s: %w", stepID, err)
	}
	if !ok {
		return fmt.Errorf("%w: cannot schedule step %s", process.ErrNotActive, stepID)
	}
	return nil
}

func (e *Engine) finishProcess(ctx context.Context, tx process.Tx, processID string, finish process.Finish) error {
	ok, err := tx.FinishProcess(ctx, processID, finish)
	if err != nil {
		return fmt.Errorf("finish process: %w", err)
	}
	if !ok {
		return fmt.Errorf("%w: cannot finish process", process.ErrNotActive)
	}
	return nil
}

func (e *Engine) advance(ctx context.Context, tx process.Tx, processID string, def workflow.Definition, stepID string, outcome step.Outcome, from []process.Status) error {
	target, err := def.Next(stepID, outcome)
	if err != nil {
		return err
	}
	switch target {
	case workflow.TargetEnd:
		return e.finishProcess(ctx, tx, processID, process.Finish{Status: process.StatusSucceeded})
	case workflow.TargetFail:
		return e.finishProcess(ctx, tx, processID, process.Finish{
			Status:    process.StatusFailed,
			Error:     fmt.Sprintf("step %s ended with outcome %s", stepID, outcome),
			ErrorKind: process.ErrorKindBusiness,
		})
	}
	return e.scheduleStep(ctx, tx, processID, def, target, from, 0)
}

func (e *Engine) Start(ctx context.Context, req StartRequest) (StartResult, error) {
	def, err := e.definition(ctx, e.store, req.WorkflowVersionID)
	if err != nil {
		return StartResult{}, err
	}
	p := process.Process{
		ID:                uuid.NewString(),
		WorkflowVersionID: req.WorkflowVersionID,
		WorkflowName:      def.Name,
		Subject:           req.Subject,
		Title:             req.Title,
		Project:           req.Project,
		Status:            process.StatusPending,
		Vars:              req.Vars,
	}
	created := false
	err = e.store.InTx(ctx, func(ctx context.Context, tx process.Tx) error {
		ok, err := tx.CreateProcess(ctx, p)
		if err != nil {
			return fmt.Errorf("create process: %w", err)
		}
		if !ok {
			return nil
		}
		created = true
		return e.scheduleStep(ctx, tx, p.ID, def, def.Start, []process.Status{process.StatusPending}, 0)
	})
	if err != nil {
		return StartResult{}, err
	}
	if !created {
		return StartResult{}, nil
	}
	started, err := e.store.GetProcess(ctx, p.ID)
	if err != nil {
		return StartResult{}, fmt.Errorf("get process: %w", err)
	}
	return StartResult{Process: started, Created: true}, nil
}

func (e *Engine) Cancel(ctx context.Context, processID string) error {
	var jobWait *process.JobWait
	err := e.store.InTx(ctx, func(ctx context.Context, tx process.Tx) error {
		p, err := tx.LockProcess(ctx, processID)
		if err != nil {
			return fmt.Errorf("lock process: %w", err)
		}
		if p.Status.Terminal() {
			return fmt.Errorf("%w: status %s", process.ErrNotActive, p.Status)
		}
		if err := e.finishProcess(ctx, tx, processID, process.Finish{Status: process.StatusCancelled}); err != nil {
			return err
		}
		if p.WaitingFor != nil && p.WaitingFor.Kind == process.WaitJob && p.WaitingFor.Job != nil {
			jobWait = p.WaitingFor.Job
			if _, err := tx.FinishStepRun(ctx, process.StepResult{StepRunID: jobWait.StepRunID, Status: process.StepCancelled}); err != nil {
				return fmt.Errorf("finish step run: %w", err)
			}
		}
		if p.CurrentJobID == nil {
			return nil
		}
		if err := e.q.CancelJobTx(ctx, tx.SQL(), *p.CurrentJobID); err != nil {
			return fmt.Errorf("cancel job %d: %w", *p.CurrentJobID, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if jobWait != nil && e.cfg.Jobs != nil {
		if err := e.cfg.Jobs.CancelJob(ctx, jobWait.Plugin, jobWait.StepRunID); err != nil {
			slog.WarnContext(ctx, "cancel plugin job failed",
				slog.String("process_id", processID),
				slog.String("plugin", jobWait.Plugin),
				slog.String("step_run_id", jobWait.StepRunID),
				slog.Any("error", err),
			)
		}
	}
	run, ok, err := e.store.ActiveStepRun(ctx, processID)
	if err != nil {
		return fmt.Errorf("get active step run: %w", err)
	}
	if !ok {
		return nil
	}
	if err := e.ctrl.Cancel(ctx, run.ID); err != nil {
		slog.WarnContext(ctx, "publish step cancel failed",
			slog.String("process_id", processID),
			slog.String("step_run_id", run.ID),
			slog.Any("error", err),
		)
	}
	return nil
}

func (e *Engine) Retry(ctx context.Context, processID, stepID string) error {
	return e.store.InTx(ctx, func(ctx context.Context, tx process.Tx) error {
		p, err := tx.LockProcess(ctx, processID)
		if err != nil {
			return fmt.Errorf("lock process: %w", err)
		}
		if !slices.Contains(process.RetryableStatuses(), p.Status) {
			return fmt.Errorf("%w: status %s", process.ErrNotActive, p.Status)
		}
		def, err := e.definition(ctx, tx, p.WorkflowVersionID)
		if err != nil {
			return err
		}
		target := stepID
		if target == "" {
			target = p.CurrentStep
		}
		return e.scheduleStep(ctx, tx, processID, def, target, process.RetryableStatuses(), 0)
	})
}

func (e *Engine) Signal(ctx context.Context, processID, outcome string) error {
	return e.wake(ctx, processID, process.WaitSignal, outcome)
}

func (e *Engine) Resume(ctx context.Context, processID string) error {
	return e.wake(ctx, processID, process.WaitComment, "")
}

func (e *Engine) wake(ctx context.Context, processID string, kind process.WaitKind, outcome string) error {
	return e.store.InTx(ctx, func(ctx context.Context, tx process.Tx) error {
		p, err := tx.LockProcess(ctx, processID)
		if err != nil {
			return fmt.Errorf("lock process: %w", err)
		}
		if p.Status != process.StatusWaiting || p.WaitingFor == nil || p.WaitingFor.Kind != kind {
			return fmt.Errorf("%w: not waiting for %s", process.ErrNotActive, kind)
		}
		def, err := e.definition(ctx, tx, p.WorkflowVersionID)
		if err != nil {
			return err
		}
		if kind == process.WaitComment {
			outcome = p.WaitingFor.ResumeOutcome
		}
		return e.advance(ctx, tx, processID, def, p.WaitingFor.Step, step.Outcome(outcome), []process.Status{process.StatusWaiting})
	})
}

func (e *Engine) CompleteJob(ctx context.Context, res JobResult) error {
	var processID, stepID string
	var attempt int
	kind := res.ErrorKind
	if kind == "" {
		kind = process.ErrorKindInternal
	}
	err := e.store.InTx(ctx, func(ctx context.Context, tx process.Tx) error {
		run, err := tx.GetStepRun(ctx, res.StepRunID)
		if err != nil {
			return fmt.Errorf("get step run: %w", err)
		}
		p, err := tx.LockProcess(ctx, run.ProcessID)
		if err != nil {
			return fmt.Errorf("lock process: %w", err)
		}
		w := p.WaitingFor
		if p.Status != process.StatusWaiting || w == nil || w.Kind != process.WaitJob || w.Job == nil || w.Job.StepRunID != res.StepRunID {
			return fmt.Errorf("%w: not waiting for job %s", process.ErrNotActive, res.StepRunID)
		}
		processID = p.ID
		stepID = w.Step
		attempt = w.Job.Attempt
		if res.Usage != (events.TokenUsage{}) {
			if err := tx.AddStepUsage(ctx, res.StepRunID, res.Usage); err != nil {
				return fmt.Errorf("add step usage: %w", err)
			}
		}
		def, err := e.definition(ctx, tx, p.WorkflowVersionID)
		if err != nil {
			return err
		}
		sd, err := def.Step(stepID)
		if err != nil {
			return err
		}
		waiting := []process.Status{process.StatusWaiting}
		if res.Error != "" {
			return e.failJob(ctx, tx, p.ID, def, stepID, sd, res, kind, w.Job.Attempt)
		}
		decl, ok := e.reg.Lookup(run.Uses)
		if !ok || !decl.Declares(res.Outcome) {
			res.Error = fmt.Sprintf("%s: %s", step.ErrUndeclaredResult, res.Outcome)
			kind = process.ErrorKindInternal
			if _, err := tx.FinishStepRun(ctx, process.StepResult{StepRunID: res.StepRunID, Status: process.StepFailed, Error: res.Error, ErrorKind: kind}); err != nil {
				return fmt.Errorf("finish step run: %w", err)
			}
			return e.finishProcess(ctx, tx, p.ID, process.Finish{Status: process.StatusFailed, Error: res.Error, ErrorKind: kind})
		}
		vars := process.Vars{}
		if len(res.Output) > 0 {
			vars[stepID] = res.Output
		}
		if _, err := tx.FinishStepRun(ctx, process.StepResult{StepRunID: res.StepRunID, Status: process.StepSucceeded, Outcome: string(res.Outcome), Output: vars}); err != nil {
			return fmt.Errorf("finish step run: %w", err)
		}
		if err := tx.MergeVars(ctx, p.ID, vars); err != nil {
			return fmt.Errorf("merge vars: %w", err)
		}
		return e.advance(ctx, tx, p.ID, def, stepID, res.Outcome, waiting)
	})
	if err != nil {
		return err
	}
	if res.Error == "" {
		slog.InfoContext(ctx, "step finished",
			slog.String("process_id", processID),
			slog.String("step", stepID),
			slog.String("outcome", string(res.Outcome)),
		)
		return nil
	}
	attrs := []any{
		slog.String("process_id", processID),
		slog.String("step", stepID),
		slog.Int("attempt", attempt),
		slog.String("error_kind", string(kind)),
		slog.String("error", res.Error),
	}
	if kind == process.ErrorKindUpstream {
		slog.WarnContext(ctx, "step failed", attrs...)
		return nil
	}
	slog.ErrorContext(ctx, "step failed", attrs...)
	return nil
}

func (e *Engine) failJob(ctx context.Context, tx process.Tx, processID string, def workflow.Definition, stepID string, sd workflow.StepDef, res JobResult, kind process.ErrorKind, attempt int) error {
	_, err := tx.FinishStepRun(ctx, process.StepResult{StepRunID: res.StepRunID, Status: process.StepFailed, Error: res.Error, ErrorKind: kind})
	if err != nil {
		return fmt.Errorf("finish step run: %w", err)
	}
	if attempt < sd.Retry+1 {
		return e.scheduleStep(ctx, tx, processID, def, stepID, []process.Status{process.StatusWaiting}, attempt)
	}
	return e.finishProcess(ctx, tx, processID, process.Finish{Status: process.StatusFailed, Error: res.Error, ErrorKind: kind})
}

func (e *Engine) JobProgress(ctx context.Context, p JobProgress) error {
	ok, err := e.store.TouchStepHeartbeat(ctx, p.StepRunID)
	if err != nil {
		return fmt.Errorf("touch step heartbeat: %w", err)
	}
	if !ok {
		return fmt.Errorf("%w: step run %s not running", process.ErrNotActive, p.StepRunID)
	}
	if p.Phase != "" {
		if err := e.store.SetStepPhase(ctx, p.StepRunID, p.Phase); err != nil {
			return fmt.Errorf("set step phase: %w", err)
		}
	}
	if p.Usage != nil {
		if err := e.store.AddStepUsage(ctx, p.StepRunID, *p.Usage); err != nil {
			return fmt.Errorf("add step usage: %w", err)
		}
	}
	return nil
}
