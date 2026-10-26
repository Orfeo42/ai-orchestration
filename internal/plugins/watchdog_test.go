package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/internal/engine"
	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/pluginapi"
)

type cbWatchdogHarness struct {
	watchdog *Watchdog
	stale    *MockStaleLister
	sink     *MockJobSink
	plugin   *cbPlugin
	got      []engine.JobResult
}

func cbNewWatchdog(t *testing.T, jobs ...process.StaleJob) *cbWatchdogHarness {
	t.Helper()
	h := &cbWatchdogHarness{plugin: cbStartPlugin(t)}
	catalog, clients := cbNewCatalog(t, cbRecord(h.plugin.server.URL))
	h.stale = &MockStaleLister{
		ListStaleJobsFunc:      func(context.Context, time.Time) ([]process.StaleJob, error) { return jobs, nil },
		TouchStepHeartbeatFunc: func(context.Context, string) (bool, error) { return true, nil },
	}
	h.sink = &MockJobSink{CompleteJobFunc: func(_ context.Context, res engine.JobResult) error {
		h.got = append(h.got, res)
		return nil
	}}
	runs := &MockStepRunReader{GetStepRunFunc: func(_ context.Context, id string) (process.StepRun, error) {
		return process.StepRun{ID: id, Uses: cbPluginName + "/echo", Plugin: cbPluginName}, nil
	}}
	h.watchdog = NewWatchdog(h.stale, catalog, clients, NewCallbacks(runs, catalog, h.sink))
	return h
}

func cbStaleJob(plugin string, expired bool) process.StaleJob {
	return process.StaleJob{ProcessID: "p-1", StepRunID: "job-1", Plugin: plugin, Expired: expired}
}

func TestWatchdogCheck(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	t.Run("expired job is cancelled and fails with deadline error", func(t *testing.T) {
		h := cbNewWatchdog(t, cbStaleJob(cbPluginName, true))
		require.NoError(t, h.watchdog.Check(ctx, now))
		assert.Equal(t, int32(1), h.plugin.cancels.Load())
		require.Len(t, h.got, 1)
		assert.Equal(t, "job-1", h.got[0].StepRunID)
		assert.Equal(t, "deadline exceeded", h.got[0].Error)
		assert.Equal(t, process.ErrorKindInternal, h.got[0].ErrorKind)
	})

	t.Run("running job gets heartbeat touched and no delivery", func(t *testing.T) {
		h := cbNewWatchdog(t, cbStaleJob(cbPluginName, false))
		h.plugin.state = pluginapi.JobState{JobID: "job-1", Status: pluginapi.JobRunning}
		var touched []string
		h.stale.TouchStepHeartbeatFunc = func(_ context.Context, id string) (bool, error) {
			touched = append(touched, id)
			return true, nil
		}
		require.NoError(t, h.watchdog.Check(ctx, now))
		assert.Equal(t, []string{"job-1"}, touched)
		assert.Empty(t, h.got)
	})

	t.Run("terminal job delivers the plugin result", func(t *testing.T) {
		h := cbNewWatchdog(t, cbStaleJob(cbPluginName, false))
		h.plugin.state = pluginapi.JobState{
			JobID:  "job-1",
			Status: pluginapi.JobSucceeded,
			Result: &pluginapi.Result{JobID: "job-1", Outcome: pluginapi.OutcomeOK, Output: json.RawMessage(`{"v":7}`)},
		}
		require.NoError(t, h.watchdog.Check(ctx, now))
		require.Len(t, h.got, 1)
		assert.Equal(t, "ok", string(h.got[0].Outcome))
		assert.JSONEq(t, `{"v":7}`, string(h.got[0].Output))
	})

	t.Run("terminal job without result delivers upstream error", func(t *testing.T) {
		h := cbNewWatchdog(t, cbStaleJob(cbPluginName, false))
		h.plugin.state = pluginapi.JobState{JobID: "job-1", Status: pluginapi.JobFailed}
		require.NoError(t, h.watchdog.Check(ctx, now))
		require.Len(t, h.got, 1)
		assert.Equal(t, "missing result", h.got[0].Error)
		assert.Equal(t, process.ErrorKindUpstream, h.got[0].ErrorKind)
	})

	t.Run("job not found on plugin delivers upstream lost", func(t *testing.T) {
		h := cbNewWatchdog(t, cbStaleJob(cbPluginName, false))
		h.plugin.getStatus = http.StatusNotFound
		require.NoError(t, h.watchdog.Check(ctx, now))
		require.Len(t, h.got, 1)
		assert.Contains(t, h.got[0].Error, "job lost")
		assert.Equal(t, process.ErrorKindUpstream, h.got[0].ErrorKind)
	})

	t.Run("unreachable plugin delivers upstream error", func(t *testing.T) {
		h := cbNewWatchdog(t, cbStaleJob(cbPluginName, false))
		h.plugin.server.Close()
		require.NoError(t, h.watchdog.Check(ctx, now))
		require.Len(t, h.got, 1)
		assert.Contains(t, h.got[0].Error, "job lost")
		assert.Equal(t, process.ErrorKindUpstream, h.got[0].ErrorKind)
	})

	t.Run("unknown plugin delivers upstream error", func(t *testing.T) {
		h := cbNewWatchdog(t, cbStaleJob("ghost", false))
		require.NoError(t, h.watchdog.Check(ctx, now))
		require.Len(t, h.got, 1)
		assert.Equal(t, "plugin not registered", h.got[0].Error)
		assert.Equal(t, process.ErrorKindUpstream, h.got[0].ErrorKind)
	})

	t.Run("delivery not active is ignored and processing continues", func(t *testing.T) {
		h := cbNewWatchdog(t, cbStaleJob("ghost", false), cbStaleJob("ghost", false))
		calls := 0
		h.sink.CompleteJobFunc = func(context.Context, engine.JobResult) error {
			calls++
			return process.ErrNotActive
		}
		require.NoError(t, h.watchdog.Check(ctx, now))
		assert.Equal(t, 2, calls)
	})

	t.Run("delivery failure does not stop other jobs", func(t *testing.T) {
		h := cbNewWatchdog(t, cbStaleJob("ghost", false), cbStaleJob("ghost", false))
		calls := 0
		h.sink.CompleteJobFunc = func(context.Context, engine.JobResult) error {
			calls++
			return errors.New("db down")
		}
		require.NoError(t, h.watchdog.Check(ctx, now))
		assert.Equal(t, 2, calls)
	})

	t.Run("list error is returned", func(t *testing.T) {
		h := cbNewWatchdog(t)
		boom := errors.New("boom")
		h.stale.ListStaleJobsFunc = func(context.Context, time.Time) ([]process.StaleJob, error) { return nil, boom }
		require.ErrorIs(t, h.watchdog.Check(ctx, now), boom)
	})
}
