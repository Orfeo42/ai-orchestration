package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ai-orchestration/internal/events"
	"ai-orchestration/internal/process"
)

func TestStartStepRun(t *testing.T) {
	t.Run("matching job inserts run and marks process started", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		p := e.seedRunning(t, "wf", "s1", 7)
		run := newStepRun(p.ID, 7)
		run.Output = process.Vars{"k": []byte(`"v"`)}

		require.NoError(t, e.store.StartStepRun(ctx, run))

		got, err := e.store.GetStepRun(ctx, run.ID)
		require.NoError(t, err)
		assert.Equal(t, process.StepRunning, got.Status)
		assert.Equal(t, "agent", got.Uses)
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
