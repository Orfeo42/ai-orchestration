package plugins

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/internal/engine"
	"github.com/Orfeo42/ai-orchestration/internal/events"
	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/pluginapi"
)

type cbHarness struct {
	callbacks *Callbacks
	sink      *MockJobSink
	got       []engine.JobResult
	progress  []engine.JobProgress
}

func cbNewHarness(t *testing.T, run process.StepRun, runErr error, records ...Record) *cbHarness {
	t.Helper()
	catalog, _ := cbNewCatalog(t, records...)
	h := &cbHarness{}
	runs := &MockStepRunReader{GetStepRunFunc: func(context.Context, string) (process.StepRun, error) { return run, runErr }}
	h.sink = &MockJobSink{
		CompleteJobFunc: func(_ context.Context, res engine.JobResult) error {
			h.got = append(h.got, res)
			return nil
		},
		JobProgressFunc: func(_ context.Context, p engine.JobProgress) error {
			h.progress = append(h.progress, p)
			return nil
		},
	}
	h.callbacks = NewCallbacks(runs, catalog, h.sink)
	return h
}

func cbPluginRun() process.StepRun {
	return process.StepRun{ID: "job-1", Uses: cbPluginName + "/echo", Plugin: cbPluginName}
}

func TestCallbacksResult(t *testing.T) {
	ctx := context.Background()
	rec := cbRecord("http://unused")

	t.Run("success maps outcome output and usage", func(t *testing.T) {
		h := cbNewHarness(t, cbPluginRun(), nil, rec)
		res := pluginapi.Result{
			JobID:   "job-1",
			Outcome: pluginapi.OutcomeOK,
			Output:  json.RawMessage(`{"v":1}`),
			Usage:   pluginapi.Usage{Input: 1, Output: 2, CacheCreationInput: 3, CacheReadInput: 4},
		}
		require.NoError(t, h.callbacks.Result(ctx, "job-1", res))
		require.Len(t, h.got, 1)
		assert.Equal(t, "job-1", h.got[0].StepRunID)
		assert.Equal(t, "ok", string(h.got[0].Outcome))
		assert.JSONEq(t, `{"v":1}`, string(h.got[0].Output))
		assert.Empty(t, h.got[0].Error)
		assert.Equal(t, events.TokenUsage{Input: 1, Output: 2, CacheCreationInput: 3, CacheReadInput: 4}, h.got[0].Usage)
	})

	t.Run("empty job id in result is accepted", func(t *testing.T) {
		h := cbNewHarness(t, cbPluginRun(), nil, rec)
		res := pluginapi.Result{Outcome: pluginapi.OutcomeOK, Output: json.RawMessage(`{"v":1}`)}
		require.NoError(t, h.callbacks.Result(ctx, "job-1", res))
		require.Len(t, h.got, 1)
	})

	t.Run("upstream error result maps kind", func(t *testing.T) {
		h := cbNewHarness(t, cbPluginRun(), nil, rec)
		res := pluginapi.Result{JobID: "job-1", Error: &pluginapi.JobError{Kind: pluginapi.ErrorUpstream, Message: "boom"}}
		require.NoError(t, h.callbacks.Result(ctx, "job-1", res))
		require.Len(t, h.got, 1)
		assert.Equal(t, "boom", h.got[0].Error)
		assert.Equal(t, process.ErrorKindUpstream, h.got[0].ErrorKind)
		assert.Empty(t, h.got[0].Outcome)
	})

	t.Run("internal error result maps kind", func(t *testing.T) {
		h := cbNewHarness(t, cbPluginRun(), nil, rec)
		res := pluginapi.Result{JobID: "job-1", Error: &pluginapi.JobError{Kind: pluginapi.ErrorInternal, Message: "bad"}}
		require.NoError(t, h.callbacks.Result(ctx, "job-1", res))
		assert.Equal(t, process.ErrorKindInternal, h.got[0].ErrorKind)
	})

	t.Run("undeclared outcome becomes internal error", func(t *testing.T) {
		h := cbNewHarness(t, cbPluginRun(), nil, rec)
		res := pluginapi.Result{JobID: "job-1", Outcome: "nope", Output: json.RawMessage(`{"v":1}`)}
		require.NoError(t, h.callbacks.Result(ctx, "job-1", res))
		assert.Equal(t, process.ErrorKindInternal, h.got[0].ErrorKind)
		assert.Contains(t, h.got[0].Error, "nope")
		assert.Empty(t, h.got[0].Outcome)
	})

	t.Run("output schema violation becomes internal error", func(t *testing.T) {
		h := cbNewHarness(t, cbPluginRun(), nil, rec)
		res := pluginapi.Result{JobID: "job-1", Outcome: pluginapi.OutcomeOK, Output: json.RawMessage(`{"v":"x"}`)}
		require.NoError(t, h.callbacks.Result(ctx, "job-1", res))
		assert.Equal(t, process.ErrorKindInternal, h.got[0].ErrorKind)
		assert.NotEmpty(t, h.got[0].Error)
		assert.Empty(t, h.got[0].Output)
	})

	t.Run("unregistered action becomes internal error", func(t *testing.T) {
		h := cbNewHarness(t, cbPluginRun(), nil)
		res := pluginapi.Result{JobID: "job-1", Outcome: pluginapi.OutcomeOK, Output: json.RawMessage(`{"v":1}`)}
		require.NoError(t, h.callbacks.Result(ctx, "job-1", res))
		assert.Equal(t, process.ErrorKindInternal, h.got[0].ErrorKind)
		assert.Equal(t, "action demo/echo no longer registered", h.got[0].Error)
	})

	t.Run("job id mismatch is invalid input", func(t *testing.T) {
		h := cbNewHarness(t, cbPluginRun(), nil, rec)
		err := h.callbacks.Result(ctx, "job-1", pluginapi.Result{JobID: "job-2"})
		require.ErrorIs(t, err, pluginapi.ErrInvalidInput)
		assert.Empty(t, h.got)
	})

	t.Run("non plugin step run", func(t *testing.T) {
		h := cbNewHarness(t, process.StepRun{ID: "job-1", Uses: "claude"}, nil, rec)
		err := h.callbacks.Result(ctx, "job-1", pluginapi.Result{JobID: "job-1"})
		require.ErrorIs(t, err, ErrNotPluginStep)
		assert.Empty(t, h.got)
	})

	t.Run("step run lookup error is propagated", func(t *testing.T) {
		h := cbNewHarness(t, process.StepRun{}, process.ErrNotFound, rec)
		err := h.callbacks.Result(ctx, "job-1", pluginapi.Result{JobID: "job-1"})
		require.ErrorIs(t, err, process.ErrNotFound)
	})

	t.Run("sink not active error is passed through", func(t *testing.T) {
		h := cbNewHarness(t, cbPluginRun(), nil, rec)
		h.sink.CompleteJobFunc = func(context.Context, engine.JobResult) error { return process.ErrNotActive }
		res := pluginapi.Result{JobID: "job-1", Outcome: pluginapi.OutcomeOK, Output: json.RawMessage(`{"v":1}`)}
		require.ErrorIs(t, h.callbacks.Result(ctx, "job-1", res), process.ErrNotActive)
	})
}

func TestCallbacksProgress(t *testing.T) {
	ctx := context.Background()

	t.Run("without usage", func(t *testing.T) {
		h := cbNewHarness(t, cbPluginRun(), nil)
		require.NoError(t, h.callbacks.Progress(ctx, "job-1", pluginapi.Progress{JobID: "job-1", Phase: "work"}))
		require.Len(t, h.progress, 1)
		assert.Equal(t, "job-1", h.progress[0].StepRunID)
		assert.Equal(t, "work", h.progress[0].Phase)
		assert.Nil(t, h.progress[0].Usage)
	})

	t.Run("with usage", func(t *testing.T) {
		h := cbNewHarness(t, cbPluginRun(), nil)
		p := pluginapi.Progress{JobID: "job-1", Phase: "work", Usage: &pluginapi.Usage{Input: 5, Output: 6}}
		require.NoError(t, h.callbacks.Progress(ctx, "job-1", p))
		require.NotNil(t, h.progress[0].Usage)
		assert.Equal(t, events.TokenUsage{Input: 5, Output: 6}, *h.progress[0].Usage)
	})

	t.Run("sink error is passed through", func(t *testing.T) {
		h := cbNewHarness(t, cbPluginRun(), nil)
		h.sink.JobProgressFunc = func(context.Context, engine.JobProgress) error { return process.ErrNotActive }
		require.ErrorIs(t, h.callbacks.Progress(ctx, "job-1", pluginapi.Progress{}), process.ErrNotActive)
	})
}
