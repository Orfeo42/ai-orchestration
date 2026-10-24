package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/internal/step"
)

func TestWork(t *testing.T) {
	t.Run("stale when process not running completes job", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		p := runningProcess(7)
		p.Status = process.StatusCancelled
		h.store.GetProcessFunc = func(context.Context, string) (process.Process, error) { return p, nil }

		err := h.worker(Hooks{}).Work(context.Background(), newJob(7, 1, 2, "a"))

		require.NoError(t, err)
		assert.Equal(t, 1, h.completed)
		assert.Empty(t, h.started)
	})

	t.Run("stale when job id differs completes job", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.store.GetProcessFunc = func(context.Context, string) (process.Process, error) { return runningProcess(8), nil }

		err := h.worker(Hooks{}).Work(context.Background(), newJob(7, 1, 2, "a"))

		require.NoError(t, err)
		assert.Equal(t, 1, h.completed)
		assert.Empty(t, h.started)
	})

	t.Run("not active at start completes job", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.store.GetProcessFunc = func(context.Context, string) (process.Process, error) { return runningProcess(7), nil }
		h.tx.StartStepRunFunc = func(context.Context, process.StepRun) error { return process.ErrNotActive }

		err := h.worker(Hooks{}).Work(context.Background(), newJob(7, 1, 2, "a"))

		require.NoError(t, err)
		assert.Equal(t, 1, h.completed)
		assert.Empty(t, h.stepRuns)
	})

	t.Run("outcome schedules next step", func(t *testing.T) {
		vars := process.Vars{"k": []byte(`"v"`)}
		h := newHarness(t, testDef(), func(context.Context, step.Context) (step.Result, error) {
			return step.Result{Outcome: step.OutcomeOK, Vars: vars}, nil
		})
		h.store.GetProcessFunc = func(context.Context, string) (process.Process, error) { return runningProcess(7), nil }

		err := h.worker(Hooks{}).Work(context.Background(), newJob(7, 2, 2, "a"))

		require.NoError(t, err)
		require.Len(t, h.started, 1)
		assert.Equal(t, "w1", h.started[0].WorkerID)
		assert.Equal(t, 2, h.started[0].Attempt)
		assert.Equal(t, int64(7), h.started[0].RiverJobID)
		assert.Equal(t, "fake", h.started[0].Uses)
		require.Len(t, h.stepRuns, 1)
		assert.Equal(t, process.StepSucceeded, h.stepRuns[0].Status)
		assert.Equal(t, "ok", h.stepRuns[0].Outcome)
		assert.Equal(t, []process.Vars{vars}, h.merged)
		require.Len(t, h.inserts, 1)
		assert.Equal(t, StepArgs{ProcessID: "p1", StepID: "c", TimeoutSeconds: 30}, h.inserts[0].Args)
		assert.Equal(t, StepOpts{Queue: step.QueueClaude, MaxAttempts: 3}, h.inserts[0].Opts)
		assert.Equal(t, []process.Next{{From: []process.Status{process.StatusRunning}, Step: "c", JobID: nextJobID}}, h.nexts)
		assert.Equal(t, 1, h.completed)
		assert.Empty(t, h.finishes)
	})

	t.Run("end outcome finishes process succeeded", func(t *testing.T) {
		h := newHarness(t, testDef(), func(context.Context, step.Context) (step.Result, error) {
			return step.Result{Outcome: "done"}, nil
		})
		h.store.GetProcessFunc = func(context.Context, string) (process.Process, error) { return runningProcess(7), nil }

		err := h.worker(Hooks{}).Work(context.Background(), newJob(7, 1, 2, "a"))

		require.NoError(t, err)
		assert.Equal(t, []process.Finish{{Status: process.StatusSucceeded}}, h.finishes)
		assert.Empty(t, h.inserts)
		assert.Equal(t, 1, h.completed)
	})

	t.Run("fail outcome finishes process failed business", func(t *testing.T) {
		h := newHarness(t, testDef(), func(context.Context, step.Context) (step.Result, error) {
			return step.Result{Outcome: "bad"}, nil
		})
		h.store.GetProcessFunc = func(context.Context, string) (process.Process, error) { return runningProcess(7), nil }

		err := h.worker(Hooks{}).Work(context.Background(), newJob(7, 1, 2, "a"))

		require.NoError(t, err)
		assert.Equal(t, []process.Finish{{
			Status:    process.StatusFailed,
			Error:     "step a ended with outcome bad",
			ErrorKind: process.ErrorKindBusiness,
		}}, h.finishes)
		assert.Equal(t, 1, h.completed)
	})

	t.Run("step already closed only completes job", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.store.GetProcessFunc = func(context.Context, string) (process.Process, error) { return runningProcess(7), nil }
		h.tx.FinishStepRunFunc = func(context.Context, process.StepResult) (bool, error) { return false, nil }

		err := h.worker(Hooks{}).Work(context.Background(), newJob(7, 1, 2, "a"))

		require.NoError(t, err)
		assert.Equal(t, 1, h.completed)
		assert.Empty(t, h.nexts)
		assert.Empty(t, h.merged)
	})

	t.Run("wait request sets process waiting", func(t *testing.T) {
		h := newHarness(t, testDef(), func(context.Context, step.Context) (step.Result, error) {
			return step.Result{Wait: &step.WaitRequest{Kind: process.WaitComment, ResumeOutcome: step.OutcomeOK}}, nil
		})
		h.store.GetProcessFunc = func(context.Context, string) (process.Process, error) { return runningProcess(7), nil }

		err := h.worker(Hooks{}).Work(context.Background(), newJob(7, 1, 2, "a"))

		require.NoError(t, err)
		require.Len(t, h.stepRuns, 1)
		assert.Equal(t, "waiting", h.stepRuns[0].Outcome)
		require.Len(t, h.waits, 1)
		assert.Equal(t, process.WaitComment, h.waits[0].Kind)
		assert.Equal(t, "a", h.waits[0].Step)
		assert.Equal(t, "ok", h.waits[0].ResumeOutcome)
		assert.False(t, h.waits[0].Since.IsZero())
		assert.Equal(t, 1, h.completed)
		assert.Empty(t, h.nexts)
	})

	t.Run("undeclared outcome fails process without retry", func(t *testing.T) {
		h := newHarness(t, testDef(), func(context.Context, step.Context) (step.Result, error) {
			return step.Result{Outcome: "weird"}, nil
		})
		h.store.GetProcessFunc = func(context.Context, string) (process.Process, error) { return runningProcess(7), nil }

		err := h.worker(Hooks{}).Work(context.Background(), newJob(7, 1, 3, "a"))

		var cancelErr *rivertype.JobCancelError
		require.ErrorAs(t, err, &cancelErr)
		require.Len(t, h.finishes, 1)
		assert.Equal(t, process.StatusFailed, h.finishes[0].Status)
		assert.Equal(t, process.ErrorKindInternal, h.finishes[0].ErrorKind)
		require.Len(t, h.stepRuns, 1)
		assert.Equal(t, process.StepFailed, h.stepRuns[0].Status)
		assert.Zero(t, h.completed)
	})

	t.Run("unknown workflow step fails process without retry", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.store.GetProcessFunc = func(context.Context, string) (process.Process, error) { return runningProcess(7), nil }

		err := h.worker(Hooks{}).Work(context.Background(), newJob(7, 1, 3, "zzz"))

		var cancelErr *rivertype.JobCancelError
		require.ErrorAs(t, err, &cancelErr)
		require.Len(t, h.finishes, 1)
		assert.Equal(t, process.ErrorKindInternal, h.finishes[0].ErrorKind)
		assert.Empty(t, h.started)
		assert.Empty(t, h.stepRuns)
	})

	t.Run("upstream error with attempts left returns error for retry", func(t *testing.T) {
		runErr := fmt.Errorf("boom: %w", step.ErrUpstream)
		h := newHarness(t, testDef(), func(context.Context, step.Context) (step.Result, error) {
			return step.Result{}, runErr
		})
		h.store.GetProcessFunc = func(context.Context, string) (process.Process, error) { return runningProcess(7), nil }

		err := h.worker(Hooks{}).Work(context.Background(), newJob(7, 1, 2, "a"))

		require.ErrorIs(t, err, runErr)
		var cancelErr *rivertype.JobCancelError
		assert.False(t, errors.As(err, &cancelErr))
		require.Len(t, h.stepRuns, 1)
		assert.Equal(t, process.StepFailed, h.stepRuns[0].Status)
		assert.Equal(t, process.ErrorKindUpstream, h.stepRuns[0].ErrorKind)
		assert.Empty(t, h.finishes)
		assert.Zero(t, h.completed)
	})

	t.Run("upstream error on last attempt fails process and cancels job", func(t *testing.T) {
		h := newHarness(t, testDef(), func(context.Context, step.Context) (step.Result, error) {
			return step.Result{}, fmt.Errorf("boom: %w", step.ErrUpstream)
		})
		h.store.GetProcessFunc = func(context.Context, string) (process.Process, error) { return runningProcess(7), nil }

		err := h.worker(Hooks{}).Work(context.Background(), newJob(7, 2, 2, "a"))

		var cancelErr *rivertype.JobCancelError
		require.ErrorAs(t, err, &cancelErr)
		require.Len(t, h.finishes, 1)
		assert.Equal(t, process.StatusFailed, h.finishes[0].Status)
		assert.Equal(t, process.ErrorKindUpstream, h.finishes[0].ErrorKind)
		assert.Contains(t, h.finishes[0].Error, "boom")
	})

	t.Run("internal error on last attempt fails process and cancels job", func(t *testing.T) {
		h := newHarness(t, testDef(), func(context.Context, step.Context) (step.Result, error) {
			return step.Result{}, errors.New("bug")
		})
		h.store.GetProcessFunc = func(context.Context, string) (process.Process, error) { return runningProcess(7), nil }

		err := h.worker(Hooks{}).Work(context.Background(), newJob(7, 1, 1, "a"))

		var cancelErr *rivertype.JobCancelError
		require.ErrorAs(t, err, &cancelErr)
		require.Len(t, h.finishes, 1)
		assert.Equal(t, process.ErrorKindInternal, h.finishes[0].ErrorKind)
		assert.Equal(t, process.ErrorKindInternal, h.stepRuns[0].ErrorKind)
	})

	t.Run("control cancel closes step run as cancelled", func(t *testing.T) {
		h := newHarness(t, testDef(), func(context.Context, step.Context) (step.Result, error) {
			return step.Result{}, context.Canceled
		})
		h.store.GetProcessFunc = func(context.Context, string) (process.Process, error) { return runningProcess(7), nil }

		err := h.worker(Hooks{}).Work(context.Background(), newJob(7, 1, 2, "a"))

		require.NoError(t, err)
		require.Len(t, h.stepRuns, 1)
		assert.Equal(t, process.StepCancelled, h.stepRuns[0].Status)
		assert.Equal(t, 1, h.completed)
		assert.Empty(t, h.finishes)
	})

	t.Run("remote cancel closes step run as cancelled without completing job", func(t *testing.T) {
		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		h := newHarness(t, testDef(), func(rctx context.Context, _ step.Context) (step.Result, error) {
			cancel(river.ErrJobCancelledRemotely)
			return step.Result{}, rctx.Err()
		})
		h.store.GetProcessFunc = func(context.Context, string) (process.Process, error) { return runningProcess(7), nil }

		err := h.worker(Hooks{}).Work(ctx, newJob(7, 1, 2, "a"))

		require.ErrorIs(t, err, context.Canceled)
		require.Len(t, h.stepRuns, 1)
		assert.Equal(t, process.StepCancelled, h.stepRuns[0].Status)
		assert.Zero(t, h.completed)
		assert.Empty(t, h.finishes)
	})

	t.Run("river context done returns error for retry", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		h := newHarness(t, testDef(), func(rctx context.Context, _ step.Context) (step.Result, error) {
			cancel()
			return step.Result{}, rctx.Err()
		})
		h.store.GetProcessFunc = func(context.Context, string) (process.Process, error) { return runningProcess(7), nil }

		err := h.worker(Hooks{}).Work(ctx, newJob(7, 1, 1, "a"))

		require.ErrorIs(t, err, context.Canceled)
		var cancelErr *rivertype.JobCancelError
		assert.False(t, errors.As(err, &cancelErr))
		require.Len(t, h.stepRuns, 1)
		assert.Equal(t, process.StepFailed, h.stepRuns[0].Status)
		assert.Equal(t, process.ErrorKindInternal, h.stepRuns[0].ErrorKind)
		assert.Empty(t, h.finishes)
		assert.Zero(t, h.completed)
	})

	t.Run("before step hook receives step definition", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.store.GetProcessFunc = func(context.Context, string) (process.Process, error) { return runningProcess(7), nil }
		var got []string
		hooks := Hooks{BeforeStep: func(_ context.Context, def step.Def) { got = append(got, def.Name) }}

		err := h.worker(hooks).Work(context.Background(), newJob(7, 1, 2, "a"))

		require.NoError(t, err)
		assert.Equal(t, []string{"fake"}, got)
	})
}

func TestTimeout(t *testing.T) {
	t.Run("uses job timeout when set", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		job := newJob(1, 1, 1, "a")
		job.Args.TimeoutSeconds = 45

		assert.Equal(t, 45*time.Second, h.worker(Hooks{}).Timeout(job))
	})

	t.Run("falls back to default timeout", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)

		assert.Equal(t, time.Minute, h.worker(Hooks{}).Timeout(newJob(1, 1, 1, "a")))
	})
}
