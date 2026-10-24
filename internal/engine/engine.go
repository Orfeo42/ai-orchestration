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

	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/internal/runstore"
	"github.com/Orfeo42/ai-orchestration/internal/step"
	"github.com/Orfeo42/ai-orchestration/internal/workflow"
)

//go:generate moq -rm -out engine_mock.go . Queue:MockQueue Completer:MockCompleter Control:MockControl

type engineError string

func (e engineError) Error() string { return string(e) }

const ErrStepRunClosed engineError = "step run already closed"

type StepArgs struct {
	ProcessID      string `json:"process_id"`
	StepID         string `json:"step_id"`
	TimeoutSeconds int    `json:"timeout_seconds"`
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

type HandleFactory func(ctx context.Context, stepRunID string) *runstore.RunHandle

type Config struct {
	WorkerID       string
	DefaultTimeout time.Duration
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
	reg   *step.Registry
	q     Queue
	ctrl  Control
	cfg   Config
}

func New(store process.Store, reg *step.Registry, q Queue, ctrl Control, cfg Config) *Engine {
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

func (e *Engine) scheduleStep(ctx context.Context, tx process.Tx, processID string, def workflow.Definition, stepID string, from []process.Status) error {
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
	}
	jobID, err := e.q.InsertStepTx(ctx, tx.SQL(), args, StepOpts{Queue: d.Queue, MaxAttempts: sd.Retry + 1})
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
	return e.scheduleStep(ctx, tx, processID, def, target, from)
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
		return e.scheduleStep(ctx, tx, p.ID, def, def.Start, []process.Status{process.StatusPending})
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
		return e.scheduleStep(ctx, tx, processID, def, target, process.RetryableStatuses())
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
