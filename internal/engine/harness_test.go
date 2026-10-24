package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"

	"ai-orchestration/internal/process"
	"ai-orchestration/internal/runstore"
	"ai-orchestration/internal/step"
	"ai-orchestration/internal/workflow"
)

const nextJobID int64 = 101

type fakeStep struct {
	run func(ctx context.Context, sc step.Context) (step.Result, error)
}

func (f fakeStep) Run(ctx context.Context, sc step.Context) (step.Result, error) {
	return f.run(ctx, sc)
}

type inserted struct {
	Args StepArgs
	Opts StepOpts
}

type harness struct {
	store     *process.MockStore
	tx        *process.MockTx
	queue     *MockQueue
	completer *MockCompleter
	control   *MockControl
	engine    *Engine
	def       workflow.Definition

	inserts   []inserted
	cancelled []int64
	completed int
	created   []process.Process
	nexts     []process.Next
	finishes  []process.Finish
	stepRuns  []process.StepResult
	started   []process.StepRun
	waits     []process.Wait
	merged    []process.Vars
}

func testDef() workflow.Definition {
	return workflow.Definition{
		Name:  "wf",
		Start: "a",
		Steps: map[string]workflow.StepDef{
			"a": {
				Uses:  "fake",
				Retry: 1,
				On: map[step.Outcome]string{
					"ok":   "c",
					"bad":  workflow.TargetFail,
					"done": workflow.TargetEnd,
				},
			},
			"c": {
				Uses:    "claude-step",
				Retry:   2,
				Timeout: workflow.Duration(30 * time.Second),
				On:      map[step.Outcome]string{"ok": workflow.TargetEnd},
			},
		},
	}
}

func newHarness(t *testing.T, def workflow.Definition, run func(ctx context.Context, sc step.Context) (step.Result, error)) *harness {
	t.Helper()
	h := &harness{
		store:     &process.MockStore{},
		tx:        &process.MockTx{},
		queue:     &MockQueue{},
		completer: &MockCompleter{},
		control:   &MockControl{},
		def:       def,
	}

	reg := step.NewRegistry()
	build := func(step.NoParams) step.Step { return fakeStep{run: run} }
	require.NoError(t, step.Register(reg, step.Def{Name: "fake", Queue: step.QueueDefault, Outcomes: []step.Outcome{"ok", "bad", "done"}}, build))
	require.NoError(t, step.Register(reg, step.Def{Name: "claude-step", Queue: step.QueueClaude, Outcomes: []step.Outcome{"ok"}}, build))

	raw, err := json.Marshal(def)
	require.NoError(t, err)
	version := func(context.Context, int64) (process.WorkflowVersion, error) {
		return process.WorkflowVersion{ID: 1, Name: def.Name, Definition: raw}, nil
	}
	h.store.GetWorkflowVersionFunc = version
	h.tx.GetWorkflowVersionFunc = version

	h.store.InTxFunc = func(ctx context.Context, fn func(context.Context, process.Tx) error) error {
		return fn(ctx, h.tx)
	}
	h.tx.SQLFunc = func() *sql.Tx { return nil }
	h.tx.CreateProcessFunc = func(_ context.Context, p process.Process) (bool, error) {
		h.created = append(h.created, p)
		return true, nil
	}
	h.tx.SetNextFunc = func(_ context.Context, _ string, n process.Next) (bool, error) {
		h.nexts = append(h.nexts, n)
		return true, nil
	}
	h.tx.FinishProcessFunc = func(_ context.Context, _ string, f process.Finish) (bool, error) {
		h.finishes = append(h.finishes, f)
		return true, nil
	}
	h.tx.StartStepRunFunc = func(_ context.Context, r process.StepRun) error {
		h.started = append(h.started, r)
		return nil
	}
	h.tx.FinishStepRunFunc = func(_ context.Context, r process.StepResult) (bool, error) {
		h.stepRuns = append(h.stepRuns, r)
		return true, nil
	}
	h.tx.SetWaitingFunc = func(_ context.Context, _ string, w process.Wait) (bool, error) {
		h.waits = append(h.waits, w)
		return true, nil
	}
	h.tx.MergeVarsFunc = func(_ context.Context, _ string, v process.Vars) error {
		h.merged = append(h.merged, v)
		return nil
	}
	h.queue.InsertStepTxFunc = func(_ context.Context, _ *sql.Tx, a StepArgs, o StepOpts) (int64, error) {
		h.inserts = append(h.inserts, inserted{Args: a, Opts: o})
		return nextJobID, nil
	}
	h.queue.CancelJobTxFunc = func(_ context.Context, _ *sql.Tx, id int64) error {
		h.cancelled = append(h.cancelled, id)
		return nil
	}
	h.completer.CompleteTxFunc = func(context.Context, *sql.Tx, *river.Job[StepArgs]) error {
		h.completed++
		return nil
	}
	h.control.CancelFunc = func(context.Context, string) error { return nil }

	h.engine = New(h.store, reg, h.queue, h.control, Config{WorkerID: "w1", DefaultTimeout: time.Minute})
	return h
}

func (h *harness) lock(p process.Process) {
	h.tx.LockProcessFunc = func(context.Context, string) (process.Process, error) { return p, nil }
}

func (h *harness) worker(hooks Hooks) *StepWorker {
	handles := func(ctx context.Context, id string) *runstore.RunHandle {
		return runstore.NewStepHandle(ctx, nil, nil, nil, id, runstore.ServiceOrchestrator)
	}
	return NewStepWorker(h.engine, h.completer, handles, hooks)
}

func okRun(context.Context, step.Context) (step.Result, error) {
	return step.Result{Outcome: step.OutcomeOK}, nil
}

func runningProcess(jobID int64) process.Process {
	return process.Process{
		ID:                "p1",
		WorkflowVersionID: 1,
		Status:            process.StatusRunning,
		CurrentStep:       "a",
		CurrentJobID:      &jobID,
	}
}

func newJob(id int64, attempt, maxAttempts int, stepID string) *river.Job[StepArgs] {
	return &river.Job[StepArgs]{
		JobRow: &rivertype.JobRow{ID: id, Attempt: attempt, MaxAttempts: maxAttempts},
		Args:   StepArgs{ProcessID: "p1", StepID: stepID},
	}
}
