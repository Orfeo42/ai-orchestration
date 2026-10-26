package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/internal/events"
	"github.com/Orfeo42/ai-orchestration/internal/process"
)

func TestStartStepRun(t *testing.T) {
	t.Run("matching job inserts run and marks process started", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		p := e.seedRunning(t, "wf", "s1", 7)
		run := newStepRun(p.ID, 7)
		run.Output = process.Vars{"k": []byte(`"v"`)}
		run.Plugin = "github"

		require.NoError(t, e.store.StartStepRun(ctx, run))

		got, err := e.store.GetStepRun(ctx, run.ID)
		require.NoError(t, err)
		assert.Equal(t, process.StepRunning, got.Status)
		assert.Equal(t, "agent", got.Uses)
		assert.Equal(t, "github", got.Plugin)
		assert.Nil(t, got.HeartbeatAt)
		assert.False(t, got.StartedAt.IsZero())
		assert.JSONEq(t, `"v"`, string(got.Output["k"]))
		proc, err := e.store.GetProcess(ctx, p.ID)
		require.NoError(t, err)
		assert.NotNil(t, proc.StartedAt)
	})

	t.Run("mismatched job id returns not active", func(t *testing.T) {
		e := newEnv(t)
		p := e.seedRunning(t, "wf", "s1", 7)

		err := e.store.StartStepRun(context.Background(), newStepRun(p.ID, 8))

		require.ErrorIs(t, err, process.ErrNotActive)
	})

	t.Run("non running process returns not active", func(t *testing.T) {
		e := newEnv(t)
		p := e.seedProcess(t, "wf", "s1")

		err := e.store.StartStepRun(context.Background(), newStepRun(p.ID, 7))

		require.ErrorIs(t, err, process.ErrNotActive)
	})
}

func TestFinishStepRun(t *testing.T) {
	t.Run("running run is finished", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		p := e.seedRunning(t, "wf", "s1", 7)
		run := newStepRun(p.ID, 7)
		require.NoError(t, e.store.StartStepRun(ctx, run))

		ok, err := e.store.FinishStepRun(ctx, process.StepResult{
			StepRunID: run.ID, Status: process.StepSucceeded, Outcome: "done", Output: process.Vars{"r": []byte(`1`)},
		})

		require.NoError(t, err)
		assert.True(t, ok)
		got, err := e.store.GetStepRun(ctx, run.ID)
		require.NoError(t, err)
		assert.Equal(t, process.StepSucceeded, got.Status)
		assert.Equal(t, "done", got.Outcome)
		assert.NotNil(t, got.EndedAt)
		assert.JSONEq(t, `1`, string(got.Output["r"]))
	})

	t.Run("nil output is stored as empty object", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		p := e.seedRunning(t, "wf", "s1", 7)
		run := newStepRun(p.ID, 7)
		require.NoError(t, e.store.StartStepRun(ctx, run))

		ok, err := e.store.FinishStepRun(ctx, process.StepResult{StepRunID: run.ID, Status: process.StepSucceeded, Output: nil})

		require.NoError(t, err)
		assert.True(t, ok)
		got, err := e.store.GetStepRun(ctx, run.ID)
		require.NoError(t, err)
		assert.Empty(t, got.Output)
	})

	t.Run("already finished run returns false", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		p := e.seedRunning(t, "wf", "s1", 7)
		run := newStepRun(p.ID, 7)
		require.NoError(t, e.store.StartStepRun(ctx, run))
		_, err := e.store.FinishStepRun(ctx, process.StepResult{StepRunID: run.ID, Status: process.StepSucceeded})
		require.NoError(t, err)

		ok, err := e.store.FinishStepRun(ctx, process.StepResult{StepRunID: run.ID, Status: process.StepFailed, Error: "late"})

		require.NoError(t, err)
		assert.False(t, ok)
		got, err := e.store.GetStepRun(ctx, run.ID)
		require.NoError(t, err)
		assert.Equal(t, process.StepSucceeded, got.Status)
	})
}

func TestGetStepRun(t *testing.T) {
	t.Run("unknown id returns not found", func(t *testing.T) {
		e := newEnv(t)

		_, err := e.store.GetStepRun(context.Background(), "00000000-0000-0000-0000-000000000001")

		require.ErrorIs(t, err, process.ErrNotFound)
	})
}

func TestListStepRuns(t *testing.T) {
	t.Run("returns runs ordered by start", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		p := e.seedRunning(t, "wf", "s1", 7)
		first := newStepRun(p.ID, 7)
		second := newStepRun(p.ID, 7)
		require.NoError(t, e.store.StartStepRun(ctx, first))
		require.NoError(t, e.store.StartStepRun(ctx, second))

		got, err := e.store.ListStepRuns(ctx, p.ID)

		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, first.ID, got[0].ID)
		assert.Equal(t, second.ID, got[1].ID)
	})

	t.Run("no runs returns empty", func(t *testing.T) {
		e := newEnv(t)
		p := e.seedProcess(t, "wf", "s1")

		got, err := e.store.ListStepRuns(context.Background(), p.ID)

		require.NoError(t, err)
		assert.Empty(t, got)
	})
}

func TestActiveStepRun(t *testing.T) {
	t.Run("none running returns false", func(t *testing.T) {
		e := newEnv(t)
		p := e.seedProcess(t, "wf", "s1")

		_, found, err := e.store.ActiveStepRun(context.Background(), p.ID)

		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("returns the running run", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		p := e.seedRunning(t, "wf", "s1", 7)
		done := newStepRun(p.ID, 7)
		active := newStepRun(p.ID, 7)
		require.NoError(t, e.store.StartStepRun(ctx, done))
		_, err := e.store.FinishStepRun(ctx, process.StepResult{StepRunID: done.ID, Status: process.StepSucceeded})
		require.NoError(t, err)
		require.NoError(t, e.store.StartStepRun(ctx, active))

		got, found, err := e.store.ActiveStepRun(ctx, p.ID)

		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, active.ID, got.ID)
	})
}

func TestSetStepFields(t *testing.T) {
	t.Run("phase iteration and session are updated", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		p := e.seedRunning(t, "wf", "s1", 7)
		run := newStepRun(p.ID, 7)
		require.NoError(t, e.store.StartStepRun(ctx, run))

		require.NoError(t, e.store.SetStepPhase(ctx, run.ID, "testing"))
		require.NoError(t, e.store.SetStepIteration(ctx, run.ID, 3))
		require.NoError(t, e.store.SetStepSession(ctx, run.ID, "sess-1"))

		got, err := e.store.GetStepRun(ctx, run.ID)
		require.NoError(t, err)
		assert.Equal(t, "testing", got.Phase)
		assert.Equal(t, 3, got.Iteration)
		assert.Equal(t, "sess-1", got.ClaudeSession)
	})
}

func TestAddStepUsage(t *testing.T) {
	t.Run("accumulates across calls", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		p := e.seedRunning(t, "wf", "s1", 7)
		run := newStepRun(p.ID, 7)
		require.NoError(t, e.store.StartStepRun(ctx, run))

		require.NoError(t, e.store.AddStepUsage(ctx, run.ID, events.TokenUsage{Input: 1, Output: 2, CacheCreationInput: 3, CacheReadInput: 4}))
		require.NoError(t, e.store.AddStepUsage(ctx, run.ID, events.TokenUsage{Input: 1, Output: 1, CacheCreationInput: 1, CacheReadInput: 1}))

		got, err := e.store.GetStepRun(ctx, run.ID)
		require.NoError(t, err)
		assert.Equal(t, events.TokenUsage{Input: 2, Output: 3, CacheCreationInput: 4, CacheReadInput: 5}, got.Usage)
	})
}

func TestTouchStepHeartbeat(t *testing.T) {
	t.Run("running run gets heartbeat", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		p := e.seedRunning(t, "wf", "s1", 7)
		run := newStepRun(p.ID, 7)
		require.NoError(t, e.store.StartStepRun(ctx, run))

		ok, err := e.store.TouchStepHeartbeat(ctx, run.ID)

		require.NoError(t, err)
		assert.True(t, ok)
		got, err := e.store.GetStepRun(ctx, run.ID)
		require.NoError(t, err)
		require.NotNil(t, got.HeartbeatAt)
		assert.WithinDuration(t, time.Now(), *got.HeartbeatAt, time.Minute)
	})

	t.Run("finished run returns false", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		p := e.seedRunning(t, "wf", "s1", 7)
		run := newStepRun(p.ID, 7)
		require.NoError(t, e.store.StartStepRun(ctx, run))
		_, err := e.store.FinishStepRun(ctx, process.StepResult{StepRunID: run.ID, Status: process.StepSucceeded})
		require.NoError(t, err)

		ok, err := e.store.TouchStepHeartbeat(ctx, run.ID)

		require.NoError(t, err)
		assert.False(t, ok)
		got, err := e.store.GetStepRun(ctx, run.ID)
		require.NoError(t, err)
		assert.Nil(t, got.HeartbeatAt)
	})
}

type backdateParams struct {
	ID          string     `db:"id"`
	StartedAt   time.Time  `db:"started_at"`
	HeartbeatAt *time.Time `db:"heartbeat_at"`
}

func (e *env) seedJobWait(t *testing.T, subject string, jobID int64, heartbeatSeconds int, deadline time.Time) (process.Process, process.StepRun) {
	t.Helper()
	ctx := context.Background()
	p := e.seedRunning(t, "wf", subject, jobID)
	run := newStepRun(p.ID, jobID)
	run.Plugin = "github"
	require.NoError(t, e.store.StartStepRun(ctx, run))
	ok, err := e.store.SetWaiting(ctx, p.ID, process.Wait{
		Kind: process.WaitJob, Step: "s1", Since: time.Now().UTC(),
		Job: &process.JobWait{StepRunID: run.ID, Plugin: "github", Attempt: 1, HeartbeatSeconds: heartbeatSeconds, Deadline: deadline},
	})
	require.NoError(t, err)
	require.True(t, ok)
	return p, run
}

func (e *env) backdate(t *testing.T, id string, startedAgo time.Duration, heartbeatAgo *time.Duration) {
	t.Helper()
	params := backdateParams{ID: id, StartedAt: time.Now().Add(-startedAgo)}
	if heartbeatAgo != nil {
		at := time.Now().Add(-*heartbeatAgo)
		params.HeartbeatAt = &at
	}
	_, err := sqlx.NamedExecContext(context.Background(), e.sqlx,
		`UPDATE step_run SET started_at = :started_at, heartbeat_at = :heartbeat_at WHERE id = :id`, params)
	require.NoError(t, err)
}

func TestListStaleJobs(t *testing.T) {
	farDeadline := time.Now().Add(time.Hour).UTC()

	t.Run("fresh heartbeat is not returned", func(t *testing.T) {
		e := newEnv(t)
		_, run := e.seedJobWait(t, "s1", 7, 30, farDeadline)
		fresh := 10 * time.Second
		e.backdate(t, run.ID, time.Hour, &fresh)

		got, err := e.store.ListStaleJobs(context.Background(), time.Now())

		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("missed heartbeat is returned", func(t *testing.T) {
		e := newEnv(t)
		p, run := e.seedJobWait(t, "s1", 7, 30, farDeadline)
		stale := 100 * time.Second
		e.backdate(t, run.ID, time.Hour, &stale)

		got, err := e.store.ListStaleJobs(context.Background(), time.Now())

		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, p.ID, got[0].ProcessID)
		assert.Equal(t, run.ID, got[0].StepRunID)
		assert.Equal(t, "github", got[0].Plugin)
		assert.WithinDuration(t, farDeadline, got[0].Deadline, time.Second)
		assert.False(t, got[0].Expired)
	})

	t.Run("no heartbeat yet and old started_at is returned", func(t *testing.T) {
		e := newEnv(t)
		_, run := e.seedJobWait(t, "s1", 7, 30, farDeadline)
		e.backdate(t, run.ID, 100*time.Second, nil)

		got, err := e.store.ListStaleJobs(context.Background(), time.Now())

		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, run.ID, got[0].StepRunID)
	})

	t.Run("no heartbeat yet and recent started_at is not returned", func(t *testing.T) {
		e := newEnv(t)
		e.seedJobWait(t, "s1", 7, 30, farDeadline)

		got, err := e.store.ListStaleJobs(context.Background(), time.Now())

		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("passed deadline is returned as expired", func(t *testing.T) {
		e := newEnv(t)
		_, run := e.seedJobWait(t, "s1", 7, 30, time.Now().Add(-time.Minute).UTC())
		fresh := time.Second
		e.backdate(t, run.ID, time.Hour, &fresh)

		got, err := e.store.ListStaleJobs(context.Background(), time.Now())

		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.True(t, got[0].Expired)
	})

	t.Run("non job waits are ignored", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		p := e.seedRunning(t, "wf", "s1", 7)
		run := newStepRun(p.ID, 7)
		require.NoError(t, e.store.StartStepRun(ctx, run))
		ok, err := e.store.SetWaiting(ctx, p.ID, process.Wait{Kind: process.WaitSignal, Step: "s1", Since: time.Now().UTC()})
		require.NoError(t, err)
		require.True(t, ok)
		e.backdate(t, run.ID, time.Hour, nil)

		got, err := e.store.ListStaleJobs(ctx, time.Now())

		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("finished step run is ignored", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		_, run := e.seedJobWait(t, "s1", 7, 30, time.Now().Add(-time.Minute).UTC())
		e.backdate(t, run.ID, time.Hour, nil)
		_, err := e.store.FinishStepRun(ctx, process.StepResult{StepRunID: run.ID, Status: process.StepSucceeded})
		require.NoError(t, err)

		got, err := e.store.ListStaleJobs(ctx, time.Now())

		require.NoError(t, err)
		assert.Empty(t, got)
	})
}
