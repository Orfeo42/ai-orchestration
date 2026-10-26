package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/internal/events"
	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/internal/step"
)

type cancelCall struct {
	plugin string
	jobID  string
}

type fakeJobs struct {
	calls []cancelCall
	err   error
}

func (f *fakeJobs) CancelJob(_ context.Context, plugin, jobID string) error {
	f.calls = append(f.calls, cancelCall{plugin: plugin, jobID: jobID})
	return f.err
}

func jobWaitingProcess(attempt int) process.Process {
	return process.Process{
		ID:                "p1",
		WorkflowVersionID: 1,
		Status:            process.StatusWaiting,
		WaitingFor: &process.Wait{
			Kind: process.WaitJob,
			Step: "a",
			Job:  &process.JobWait{StepRunID: "sr1", Plugin: "pl", Attempt: attempt},
		},
	}
}

func (h *harness) jobRun(uses string) {
	h.tx.GetStepRunFunc = func(_ context.Context, id string) (process.StepRun, error) {
		return process.StepRun{ID: id, ProcessID: "p1", Uses: uses}, nil
	}
}

func jobWaitRun(job *step.JobWait) func(context.Context, step.Context) (step.Result, error) {
	return func(context.Context, step.Context) (step.Result, error) {
		return step.Result{
			Vars: process.Vars{"k": []byte(`1`)},
			Wait: &step.WaitRequest{Kind: process.WaitJob, Job: job},
		}, nil
	}
}

func TestWorkJobWait(t *testing.T) {
	deadline := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("settles waiting without finishing the step run", func(t *testing.T) {
		var got step.Context
		h := newHarness(t, testDef(), func(ctx context.Context, sc step.Context) (step.Result, error) {
			got = sc
			return jobWaitRun(&step.JobWait{Plugin: "pl", HeartbeatSeconds: 30, Deadline: deadline})(ctx, sc)
		})
		h.store.GetProcessFunc = func(context.Context, string) (process.Process, error) { return runningProcess(7), nil }
		job := newJob(7, 1, 1, "a")
		job.Args.AttemptBase = 1
		job.Args.TimeoutSeconds = 45

		err := h.worker(Hooks{}).Work(context.Background(), job)

		require.NoError(t, err)
		assert.Empty(t, h.stepRuns)
		assert.Equal(t, []process.Vars{{"k": []byte(`1`)}}, h.merged)
		require.Len(t, h.waits, 1)
		w := h.waits[0]
		assert.Equal(t, process.WaitJob, w.Kind)
		assert.Equal(t, "a", w.Step)
		assert.False(t, w.Since.IsZero())
		require.NotNil(t, w.Job)
		assert.Equal(t, h.started[0].ID, w.Job.StepRunID)
		assert.Equal(t, "pl", w.Job.Plugin)
		assert.Equal(t, 2, w.Job.Attempt)
		assert.Equal(t, 30, w.Job.HeartbeatSeconds)
		assert.Equal(t, deadline, w.Job.Deadline)
		assert.Equal(t, 1, h.completed)
		assert.Equal(t, 2, h.started[0].Attempt)
		assert.Equal(t, "a", got.StepID)
		assert.Equal(t, 2, got.Attempt)
		assert.Equal(t, 45*time.Second, got.Timeout)
		assert.Equal(t, h.started[0].ID, got.StepRunID)
	})

	t.Run("missing job details fails the process", func(t *testing.T) {
		h := newHarness(t, testDef(), jobWaitRun(nil))
		h.store.GetProcessFunc = func(context.Context, string) (process.Process, error) { return runningProcess(7), nil }

		err := h.worker(Hooks{}).Work(context.Background(), newJob(7, 1, 3, "a"))

		var cancelErr *rivertype.JobCancelError
		require.ErrorAs(t, err, &cancelErr)
		require.ErrorIs(t, err, ErrMissingJobWait)
		require.Len(t, h.finishes, 1)
		assert.Equal(t, process.ErrorKindInternal, h.finishes[0].ErrorKind)
		assert.Empty(t, h.waits)
	})

	t.Run("superseded process cancels the step run and the plugin job", func(t *testing.T) {
		h := newHarness(t, testDef(), jobWaitRun(&step.JobWait{Plugin: "pl"}))
		jobs := &fakeJobs{}
		h.engine.cfg.Jobs = jobs
		h.store.GetProcessFunc = func(context.Context, string) (process.Process, error) { return runningProcess(7), nil }
		h.tx.SetWaitingFunc = func(context.Context, string, process.Wait) (bool, error) { return false, nil }

		err := h.worker(Hooks{}).Work(context.Background(), newJob(7, 1, 3, "a"))

		require.NoError(t, err)
		assert.Equal(t, 1, h.completed)
		require.Len(t, h.stepRuns, 1)
		assert.Equal(t, process.StepCancelled, h.stepRuns[0].Status)
		assert.Equal(t, []cancelCall{{plugin: "pl", jobID: h.started[0].ID}}, jobs.calls)
	})

	t.Run("superseded process without job canceller still settles", func(t *testing.T) {
		h := newHarness(t, testDef(), jobWaitRun(&step.JobWait{Plugin: "pl"}))
		h.store.GetProcessFunc = func(context.Context, string) (process.Process, error) { return runningProcess(7), nil }
		h.tx.SetWaitingFunc = func(context.Context, string, process.Wait) (bool, error) { return false, nil }

		err := h.worker(Hooks{}).Work(context.Background(), newJob(7, 1, 3, "a"))

		require.NoError(t, err)
		assert.Equal(t, 1, h.completed)
	})
}

func TestWorkPermanentError(t *testing.T) {
	t.Run("fails immediately with attempts left", func(t *testing.T) {
		h := newHarness(t, testDef(), func(context.Context, step.Context) (step.Result, error) {
			return step.Result{}, errors.Join(step.ErrPermanent, step.ErrUpstream)
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
	})
}

func TestScheduleStepAttemptBase(t *testing.T) {
	schedule := func(t *testing.T, base int) *harness {
		t.Helper()
		h := newHarness(t, testDef(), okRun)
		h.lock(waitingProcess(process.WaitSignal, "a", ""))
		err := h.engine.scheduleStep(context.Background(), h.tx, "p1", h.def, "c", []process.Status{process.StatusWaiting}, base)
		require.NoError(t, err)
		require.Len(t, h.inserts, 1)
		return h
	}

	t.Run("zero base keeps full budget", func(t *testing.T) {
		h := schedule(t, 0)

		assert.Equal(t, 3, h.inserts[0].Opts.MaxAttempts)
		assert.Equal(t, 0, h.inserts[0].Args.AttemptBase)
	})

	t.Run("base reduces budget", func(t *testing.T) {
		h := schedule(t, 2)

		assert.Equal(t, 1, h.inserts[0].Opts.MaxAttempts)
		assert.Equal(t, 2, h.inserts[0].Args.AttemptBase)
	})

	t.Run("budget never drops below one", func(t *testing.T) {
		h := schedule(t, 9)

		assert.Equal(t, 1, h.inserts[0].Opts.MaxAttempts)
	})
}

func TestCompleteJob(t *testing.T) {
	t.Run("success stores output under step id and advances", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(jobWaitingProcess(1))
		h.jobRun("fake")

		err := h.engine.CompleteJob(context.Background(), JobResult{StepRunID: "sr1", Outcome: "ok", Output: []byte(`{"x":1}`)})

		require.NoError(t, err)
		vars := process.Vars{"a": []byte(`{"x":1}`)}
		assert.Equal(t, []process.StepResult{{StepRunID: "sr1", Status: process.StepSucceeded, Outcome: "ok", Output: vars}}, h.stepRuns)
		assert.Equal(t, []process.Vars{vars}, h.merged)
		require.Len(t, h.inserts, 1)
		assert.Equal(t, "c", h.inserts[0].Args.StepID)
		assert.Equal(t, []process.Next{{From: []process.Status{process.StatusWaiting}, Step: "c", JobID: nextJobID}}, h.nexts)
	})

	t.Run("result before the job wait is recorded is pending", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(process.Process{ID: "p1", WorkflowVersionID: 1, Status: process.StatusRunning, CurrentStep: "a"})
		h.tx.GetStepRunFunc = func(_ context.Context, id string) (process.StepRun, error) {
			return process.StepRun{ID: id, ProcessID: "p1", StepID: "a", Uses: "fake", Status: process.StepRunning}, nil
		}

		err := h.engine.CompleteJob(context.Background(), JobResult{StepRunID: "sr1", Outcome: "ok"})

		require.ErrorIs(t, err, ErrJobPending)
		assert.Empty(t, h.stepRuns)
		assert.Empty(t, h.merged)
		assert.Empty(t, h.inserts)
	})

	t.Run("running process on another step is not active", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(process.Process{ID: "p1", WorkflowVersionID: 1, Status: process.StatusRunning, CurrentStep: "c"})
		h.tx.GetStepRunFunc = func(_ context.Context, id string) (process.StepRun, error) {
			return process.StepRun{ID: id, ProcessID: "p1", StepID: "a", Uses: "fake", Status: process.StepRunning}, nil
		}

		err := h.engine.CompleteJob(context.Background(), JobResult{StepRunID: "sr1", Outcome: "ok"})

		require.ErrorIs(t, err, process.ErrNotActive)
		assert.NotErrorIs(t, err, ErrJobPending)
	})

	t.Run("closed step run of a running process is not active", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(process.Process{ID: "p1", WorkflowVersionID: 1, Status: process.StatusRunning, CurrentStep: "a"})
		h.tx.GetStepRunFunc = func(_ context.Context, id string) (process.StepRun, error) {
			return process.StepRun{ID: id, ProcessID: "p1", StepID: "a", Uses: "fake", Status: process.StepFailed}, nil
		}

		err := h.engine.CompleteJob(context.Background(), JobResult{StepRunID: "sr1", Outcome: "ok"})

		require.ErrorIs(t, err, process.ErrNotActive)
	})

	t.Run("success with empty output omits the key", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(jobWaitingProcess(1))
		h.jobRun("fake")

		err := h.engine.CompleteJob(context.Background(), JobResult{StepRunID: "sr1", Outcome: "done"})

		require.NoError(t, err)
		require.Len(t, h.merged, 1)
		assert.Empty(t, h.merged[0])
		assert.Equal(t, []process.Finish{{Status: process.StatusSucceeded}}, h.finishes)
	})

	t.Run("undeclared outcome fails step run and process without retry", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(jobWaitingProcess(1))
		h.jobRun("fake")

		err := h.engine.CompleteJob(context.Background(), JobResult{StepRunID: "sr1", Outcome: "weird"})

		require.NoError(t, err)
		require.Len(t, h.stepRuns, 1)
		assert.Equal(t, process.StepFailed, h.stepRuns[0].Status)
		assert.Equal(t, process.ErrorKindInternal, h.stepRuns[0].ErrorKind)
		assert.Contains(t, h.stepRuns[0].Error, string(step.ErrUndeclaredResult))
		require.Len(t, h.finishes, 1)
		assert.Equal(t, process.StatusFailed, h.finishes[0].Status)
		assert.Equal(t, process.ErrorKindInternal, h.finishes[0].ErrorKind)
		assert.Empty(t, h.inserts)
	})

	t.Run("unknown step type fails process", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(jobWaitingProcess(1))
		h.jobRun("nope")

		err := h.engine.CompleteJob(context.Background(), JobResult{StepRunID: "sr1", Outcome: "ok"})

		require.NoError(t, err)
		require.Len(t, h.finishes, 1)
		assert.Equal(t, process.StatusFailed, h.finishes[0].Status)
	})

	t.Run("error with attempts left schedules a retry with attempt base", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(jobWaitingProcess(1))
		h.jobRun("fake")

		err := h.engine.CompleteJob(context.Background(), JobResult{StepRunID: "sr1", Error: "boom", ErrorKind: process.ErrorKindUpstream})

		require.NoError(t, err)
		assert.Equal(t, []process.StepResult{{StepRunID: "sr1", Status: process.StepFailed, Error: "boom", ErrorKind: process.ErrorKindUpstream}}, h.stepRuns)
		require.Len(t, h.inserts, 1)
		assert.Equal(t, StepArgs{ProcessID: "p1", StepID: "a", AttemptBase: 1}, h.inserts[0].Args)
		assert.Equal(t, 1, h.inserts[0].Opts.MaxAttempts)
		assert.Equal(t, []process.Next{{From: []process.Status{process.StatusWaiting}, Step: "a", JobID: nextJobID}}, h.nexts)
		assert.Empty(t, h.finishes)
	})

	t.Run("error on last attempt fails the process with the kind", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(jobWaitingProcess(2))
		h.jobRun("fake")

		err := h.engine.CompleteJob(context.Background(), JobResult{StepRunID: "sr1", Error: "boom", ErrorKind: process.ErrorKindUpstream})

		require.NoError(t, err)
		assert.Equal(t, []process.Finish{{Status: process.StatusFailed, Error: "boom", ErrorKind: process.ErrorKindUpstream}}, h.finishes)
		assert.Empty(t, h.inserts)
	})

	t.Run("error without kind defaults to internal", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(jobWaitingProcess(2))
		h.jobRun("fake")

		err := h.engine.CompleteJob(context.Background(), JobResult{StepRunID: "sr1", Error: "boom"})

		require.NoError(t, err)
		assert.Equal(t, process.ErrorKindInternal, h.stepRuns[0].ErrorKind)
		assert.Equal(t, process.ErrorKindInternal, h.finishes[0].ErrorKind)
	})

	t.Run("usage is recorded when non zero", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(jobWaitingProcess(1))
		h.jobRun("fake")
		var got []events.TokenUsage
		h.tx.AddStepUsageFunc = func(_ context.Context, _ string, u events.TokenUsage) error {
			got = append(got, u)
			return nil
		}

		err := h.engine.CompleteJob(context.Background(), JobResult{StepRunID: "sr1", Outcome: "done", Usage: events.TokenUsage{Input: 3, Output: 4}})

		require.NoError(t, err)
		assert.Equal(t, []events.TokenUsage{{Input: 3, Output: 4}}, got)
	})

	t.Run("zero usage is not recorded", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(jobWaitingProcess(1))
		h.jobRun("fake")

		err := h.engine.CompleteJob(context.Background(), JobResult{StepRunID: "sr1", Outcome: "done"})

		require.NoError(t, err)
		assert.Empty(t, h.tx.AddStepUsageCalls())
	})

	t.Run("process not waiting is not active", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(runningProcess(7))
		h.jobRun("fake")

		err := h.engine.CompleteJob(context.Background(), JobResult{StepRunID: "sr1", Outcome: "ok"})

		require.ErrorIs(t, err, process.ErrNotActive)
		assert.Empty(t, h.stepRuns)
	})

	t.Run("different step run id is not active", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(jobWaitingProcess(1))
		h.jobRun("fake")

		err := h.engine.CompleteJob(context.Background(), JobResult{StepRunID: "other", Outcome: "ok"})

		require.ErrorIs(t, err, process.ErrNotActive)
		assert.Empty(t, h.stepRuns)
	})

	t.Run("signal wait is not active", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(waitingProcess(process.WaitSignal, "a", ""))
		h.jobRun("fake")

		err := h.engine.CompleteJob(context.Background(), JobResult{StepRunID: "sr1", Outcome: "ok"})

		require.ErrorIs(t, err, process.ErrNotActive)
	})

	t.Run("unknown step run returns the load error", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.tx.GetStepRunFunc = func(context.Context, string) (process.StepRun, error) {
			return process.StepRun{}, process.ErrNotFound
		}

		err := h.engine.CompleteJob(context.Background(), JobResult{StepRunID: "sr1", Outcome: "ok"})

		require.ErrorIs(t, err, process.ErrNotFound)
	})
}

func TestJobProgress(t *testing.T) {
	t.Run("touches heartbeat only when nothing else is set", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.store.TouchStepHeartbeatFunc = func(context.Context, string) (bool, error) { return true, nil }

		err := h.engine.JobProgress(context.Background(), JobProgress{StepRunID: "sr1"})

		require.NoError(t, err)
		assert.Len(t, h.store.TouchStepHeartbeatCalls(), 1)
		assert.Empty(t, h.store.SetStepPhaseCalls())
		assert.Empty(t, h.store.AddStepUsageCalls())
	})

	t.Run("not running step run is not active", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.store.TouchStepHeartbeatFunc = func(context.Context, string) (bool, error) { return false, nil }

		err := h.engine.JobProgress(context.Background(), JobProgress{StepRunID: "sr1", Phase: "x"})

		require.ErrorIs(t, err, process.ErrNotActive)
		assert.Empty(t, h.store.SetStepPhaseCalls())
	})

	t.Run("heartbeat error is returned", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.store.TouchStepHeartbeatFunc = func(context.Context, string) (bool, error) { return false, errors.New("db down") }

		err := h.engine.JobProgress(context.Background(), JobProgress{StepRunID: "sr1"})

		require.Error(t, err)
		assert.NotErrorIs(t, err, process.ErrNotActive)
	})

	t.Run("phase and usage are written", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.store.TouchStepHeartbeatFunc = func(context.Context, string) (bool, error) { return true, nil }
		var phases []string
		var usages []events.TokenUsage
		h.store.SetStepPhaseFunc = func(_ context.Context, id, phase string) error {
			phases = append(phases, id+":"+phase)
			return nil
		}
		h.store.AddStepUsageFunc = func(_ context.Context, _ string, u events.TokenUsage) error {
			usages = append(usages, u)
			return nil
		}

		err := h.engine.JobProgress(context.Background(), JobProgress{StepRunID: "sr1", Phase: "thinking", Usage: &events.TokenUsage{Input: 5}})

		require.NoError(t, err)
		assert.Equal(t, []string{"sr1:thinking"}, phases)
		assert.Equal(t, []events.TokenUsage{{Input: 5}}, usages)
	})
}

func TestCancelJobWait(t *testing.T) {
	noActive := func(context.Context, string) (process.StepRun, bool, error) {
		return process.StepRun{}, false, nil
	}

	t.Run("finishes the step run cancelled and cancels the plugin job", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		jobs := &fakeJobs{}
		h.engine.cfg.Jobs = jobs
		h.lock(jobWaitingProcess(1))
		h.store.ActiveStepRunFunc = noActive

		err := h.engine.Cancel(context.Background(), "p1")

		require.NoError(t, err)
		assert.Equal(t, []process.StepResult{{StepRunID: "sr1", Status: process.StepCancelled}}, h.stepRuns)
		assert.Equal(t, []cancelCall{{plugin: "pl", jobID: "sr1"}}, jobs.calls)
		assert.Equal(t, []process.Finish{{Status: process.StatusCancelled}}, h.finishes)
	})

	t.Run("plugin cancel error is swallowed", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.engine.cfg.Jobs = &fakeJobs{err: errors.New("plugin down")}
		h.lock(jobWaitingProcess(1))
		h.store.ActiveStepRunFunc = noActive

		err := h.engine.Cancel(context.Background(), "p1")

		require.NoError(t, err)
	})

	t.Run("nil canceller still finishes the step run", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		h.lock(jobWaitingProcess(1))
		h.store.ActiveStepRunFunc = noActive

		err := h.engine.Cancel(context.Background(), "p1")

		require.NoError(t, err)
		require.Len(t, h.stepRuns, 1)
	})

	t.Run("signal wait does not touch jobs", func(t *testing.T) {
		h := newHarness(t, testDef(), okRun)
		jobs := &fakeJobs{}
		h.engine.cfg.Jobs = jobs
		h.lock(waitingProcess(process.WaitSignal, "a", ""))
		h.store.ActiveStepRunFunc = noActive

		err := h.engine.Cancel(context.Background(), "p1")

		require.NoError(t, err)
		assert.Empty(t, jobs.calls)
		assert.Empty(t, h.stepRuns)
	})
}
