package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/internal/step"
	"github.com/Orfeo42/ai-orchestration/internal/workflow"
)

func waitingProcess(kind process.WaitKind, stepID, resume string) process.Process {
	return process.Process{
		ID:                "p1",
		WorkflowVersionID: 1,
		Status:            process.StatusWaiting,
		WaitingFor:        &process.Wait{Kind: kind, Step: stepID, ResumeOutcome: resume},
	}
}

func TestStart(t *testing.T) {
	req := StartRequest{
		WorkflowVersionID: 1,
		Subject:           process.Subject{Kind: process.SubjectIssue, Key: "r#1"},
		Title:             "t",
		Project:           "proj",
		Vars:              process.Vars{"k": []byte(`1`)},
	}

	t.Run("creates process and schedules start step", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.store.GetProcessFunc = func(_ context.Context, id string) (process.Process, error) {
			return process.Process{ID: id, Status: process.StatusRunning}, nil
		}

		res, err := h.engine.Start(context.Background(), req)

		require.NoError(t, err)
		assert.True(t, res.Created)
		require.Len(t, h.created, 1)
		created := h.created[0]
		assert.NotEmpty(t, created.ID)
		assert.Equal(t, res.Process.ID, created.ID)
		assert.Equal(t, process.StatusPending, created.Status)
		assert.Equal(t, "wf", created.WorkflowName)
		assert.Equal(t, int64(1), created.WorkflowVersionID)
		assert.Equal(t, req.Subject, created.Subject)
		assert.Equal(t, "t", created.Title)
		assert.Equal(t, "proj", created.Project)
		assert.Equal(t, req.Vars, created.Vars)
		require.Len(t, h.inserts, 1)
		assert.Equal(t, StepArgs{ProcessID: created.ID, StepID: "a"}, h.inserts[0].Args)
		assert.Equal(t, StepOpts{Queue: step.QueueDefault, MaxAttempts: 2}, h.inserts[0].Opts)
		assert.Equal(t, []process.Next{{From: []process.Status{process.StatusPending}, Step: "a", JobID: nextJobID}}, h.nexts)
	})

	t.Run("existing process is not created and no job inserted", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.tx.CreateProcessFunc = func(context.Context, process.Process) (bool, error) { return false, nil }

		res, err := h.engine.Start(context.Background(), req)

		require.NoError(t, err)
		assert.False(t, res.Created)
		assert.Empty(t, h.inserts)
		assert.Empty(t, h.nexts)
	})

	t.Run("workflow version load error is returned", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.store.GetWorkflowVersionFunc = func(context.Context, int64) (process.WorkflowVersion, error) {
			return process.WorkflowVersion{}, process.ErrNotFound
		}

		_, err := h.engine.Start(context.Background(), req)

		require.ErrorIs(t, err, process.ErrNotFound)
	})

	t.Run("start step using unknown step type fails", func(t *testing.T) {
		def := testDef()
		def.Steps["a"] = workflow.StepDef{Uses: "nope"}
		h := newHarness(t, def, okRun)

		_, err := h.engine.Start(context.Background(), req)

		require.ErrorIs(t, err, step.ErrUnknownStep)
		assert.Empty(t, h.inserts)
	})

	t.Run("set next refused returns not active", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.tx.SetNextFunc = func(context.Context, string, process.Next) (bool, error) { return false, nil }

		_, err := h.engine.Start(context.Background(), req)

		require.ErrorIs(t, err, process.ErrNotActive)
	})
}

func TestCancel(t *testing.T) {
	t.Run("cancels process job and running step", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(runningProcess(7))
		h.store.ActiveStepRunFunc = func(context.Context, string) (process.StepRun, bool, error) {
			return process.StepRun{ID: "sr1"}, true, nil
		}
		var cancelledRuns []string
		h.control.CancelFunc = func(_ context.Context, id string) error {
			cancelledRuns = append(cancelledRuns, id)
			return nil
		}

		err := h.engine.Cancel(context.Background(), "p1")

		require.NoError(t, err)
		assert.Equal(t, []process.Finish{{Status: process.StatusCancelled}}, h.finishes)
		assert.Equal(t, []int64{7}, h.cancelled)
		assert.Equal(t, []string{"sr1"}, cancelledRuns)
	})

	t.Run("no current job skips job cancel", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		p := waitingProcess(process.WaitSignal, "a", "")
		h.lock(p)
		h.store.ActiveStepRunFunc = func(context.Context, string) (process.StepRun, bool, error) {
			return process.StepRun{}, false, nil
		}

		err := h.engine.Cancel(context.Background(), "p1")

		require.NoError(t, err)
		assert.Empty(t, h.cancelled)
		assert.Empty(t, h.control.CancelCalls())
	})

	t.Run("terminal process is not active", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		p := runningProcess(7)
		p.Status = process.StatusSucceeded
		h.lock(p)

		err := h.engine.Cancel(context.Background(), "p1")

		require.ErrorIs(t, err, process.ErrNotActive)
		assert.Empty(t, h.finishes)
	})

	t.Run("unknown process returns not found", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.tx.LockProcessFunc = func(context.Context, string) (process.Process, error) {
			return process.Process{}, process.ErrNotFound
		}

		err := h.engine.Cancel(context.Background(), "p1")

		require.ErrorIs(t, err, process.ErrNotFound)
	})

	t.Run("control publish error is ignored", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(runningProcess(7))
		h.store.ActiveStepRunFunc = func(context.Context, string) (process.StepRun, bool, error) {
			return process.StepRun{ID: "sr1"}, true, nil
		}
		h.control.CancelFunc = func(context.Context, string) error { return errors.New("redis down") }

		err := h.engine.Cancel(context.Background(), "p1")

		require.NoError(t, err)
	})
}

func TestRetry(t *testing.T) {
	failed := func() process.Process {
		p := runningProcess(7)
		p.Status = process.StatusFailed
		p.CurrentStep = "c"
		return p
	}

	t.Run("empty step id retries current step", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(failed())

		err := h.engine.Retry(context.Background(), "p1", "")

		require.NoError(t, err)
		require.Len(t, h.inserts, 1)
		assert.Equal(t, "c", h.inserts[0].Args.StepID)
		assert.Equal(t, []process.Next{{From: process.RetryableStatuses(), Step: "c", JobID: nextJobID}}, h.nexts)
	})

	t.Run("explicit step id overrides current step", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		p := failed()
		p.Status = process.StatusCancelled
		h.lock(p)

		err := h.engine.Retry(context.Background(), "p1", "a")

		require.NoError(t, err)
		assert.Equal(t, "a", h.inserts[0].Args.StepID)
		assert.Equal(t, step.QueueDefault, h.inserts[0].Opts.Queue)
	})

	t.Run("running process is not active", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(runningProcess(7))

		err := h.engine.Retry(context.Background(), "p1", "")

		require.ErrorIs(t, err, process.ErrNotActive)
		assert.Empty(t, h.inserts)
	})

	t.Run("step missing from definition returns unknown step", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(failed())

		err := h.engine.Retry(context.Background(), "p1", "zzz")

		require.ErrorIs(t, err, workflow.ErrUnknownStep)
		assert.Empty(t, h.inserts)
	})
}

func TestSignal(t *testing.T) {
	t.Run("handled outcome advances to next step", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(waitingProcess(process.WaitSignal, "a", ""))

		err := h.engine.Signal(context.Background(), "p1", "ok")

		require.NoError(t, err)
		require.Len(t, h.inserts, 1)
		assert.Equal(t, "c", h.inserts[0].Args.StepID)
		assert.Equal(t, []process.Next{{From: []process.Status{process.StatusWaiting}, Step: "c", JobID: nextJobID}}, h.nexts)
	})

	t.Run("end target finishes process", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(waitingProcess(process.WaitSignal, "a", ""))

		err := h.engine.Signal(context.Background(), "p1", "done")

		require.NoError(t, err)
		assert.Equal(t, []process.Finish{{Status: process.StatusSucceeded}}, h.finishes)
	})

	t.Run("unhandled outcome returns unhandled result", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(waitingProcess(process.WaitSignal, "a", ""))

		err := h.engine.Signal(context.Background(), "p1", "nope")

		require.ErrorIs(t, err, workflow.ErrUnhandledResult)
		assert.Empty(t, h.inserts)
	})

	t.Run("comment wait is not active for signal", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(waitingProcess(process.WaitComment, "a", "ok"))

		err := h.engine.Signal(context.Background(), "p1", "ok")

		require.ErrorIs(t, err, process.ErrNotActive)
	})

	t.Run("non waiting process is not active", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(runningProcess(7))

		err := h.engine.Signal(context.Background(), "p1", "ok")

		require.ErrorIs(t, err, process.ErrNotActive)
	})
}

func TestResume(t *testing.T) {
	t.Run("uses resume outcome of the wait", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(waitingProcess(process.WaitComment, "a", "done"))

		err := h.engine.Resume(context.Background(), "p1")

		require.NoError(t, err)
		assert.Equal(t, []process.Finish{{Status: process.StatusSucceeded}}, h.finishes)
	})

	t.Run("schedules next step for resume outcome", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(waitingProcess(process.WaitComment, "a", "ok"))

		err := h.engine.Resume(context.Background(), "p1")

		require.NoError(t, err)
		require.Len(t, h.inserts, 1)
		assert.Equal(t, "c", h.inserts[0].Args.StepID)
	})

	t.Run("signal wait is not active for resume", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(waitingProcess(process.WaitSignal, "a", ""))

		err := h.engine.Resume(context.Background(), "p1")

		require.ErrorIs(t, err, process.ErrNotActive)
	})
}
