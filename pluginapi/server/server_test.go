package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/pluginapi"
	"github.com/Orfeo42/ai-orchestration/pluginapi/client"
)

func TestNew(t *testing.T) {
	info := pluginapi.Info{Name: "tester", Version: "1.0.0", Description: "test plugin"}

	t.Run("empty token rejected", func(t *testing.T) {
		_, err := New(Config{}, info, okAction("a", pluginapi.ModeSync))
		require.ErrorIs(t, err, ErrMissingToken)
	})

	t.Run("invalid manifest rejected", func(t *testing.T) {
		_, err := New(Config{Token: "t"}, pluginapi.Info{Name: "Bad Name"}, okAction("a", pluginapi.ModeSync))
		require.ErrorIs(t, err, pluginapi.ErrInvalidManifest)
	})

	t.Run("no actions rejected", func(t *testing.T) {
		_, err := New(Config{Token: "t"}, info)
		require.ErrorIs(t, err, pluginapi.ErrInvalidManifest)
	})

	t.Run("defaults applied", func(t *testing.T) {
		s, err := New(Config{Token: "t"}, info, okAction("a", pluginapi.ModeSync))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, s.Shutdown(context.Background())) })
		assert.Equal(t, pluginapi.DefaultMaxBodyBytes, s.cfg.MaxBodyBytes)
		assert.Equal(t, 30*time.Second, s.cfg.ShutdownTimeout)
		assert.Equal(t, 24*time.Hour, s.cfg.JobRetention)
		assert.Equal(t, time.Second, s.cfg.RetryBackoff)
		assert.Equal(t, http.DefaultClient, s.cfg.HTTPClient)
		assert.Equal(t, slog.Default(), s.cfg.Logger)
		assert.Equal(t, pluginapi.HealthOK, s.cfg.HealthCheck(context.Background()).Status)
	})

	t.Run("manifest built from info and actions", func(t *testing.T) {
		s, err := New(Config{Token: "t"}, info, okAction("a", pluginapi.ModeSync))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, s.Shutdown(context.Background())) })
		m := s.Manifest()
		assert.Equal(t, pluginapi.ProtocolVersion, m.Protocol)
		assert.Equal(t, "tester", m.Name)
		require.Len(t, m.Actions, 1)
		assert.Equal(t, "a", m.Actions[0].Name)
	})
}

func TestAuth(t *testing.T) {
	h := newHarness(t, Config{}, okAction("a", pluginapi.ModeSync))

	t.Run("missing token rejected", func(t *testing.T) {
		status, body := h.rawPost(t, "/v1/actions/a/jobs", "", []byte(`{}`))
		assert.Equal(t, http.StatusUnauthorized, status)
		var apiErr pluginapi.APIError
		require.NoError(t, json.Unmarshal(body, &apiErr))
		assert.Equal(t, pluginapi.CodeUnauthorized, apiErr.Code)
	})

	t.Run("wrong token rejected", func(t *testing.T) {
		_, err := client.NewPlugin(h.ts.URL, "wrong", nil).Manifest(context.Background())
		require.ErrorIs(t, err, pluginapi.ErrUnauthorized)
	})

	t.Run("right token accepted", func(t *testing.T) {
		_, err := h.plugin.Manifest(context.Background())
		require.NoError(t, err)
	})
}

func TestManifest(t *testing.T) {
	h := newHarness(t, Config{}, okAction("a", pluginapi.ModeSync))

	t.Run("served and valid", func(t *testing.T) {
		m, err := h.plugin.Manifest(context.Background())
		require.NoError(t, err)
		require.NoError(t, m.Validate())
		assert.Equal(t, "tester", m.Name)
	})
}

func TestHealth(t *testing.T) {
	t.Run("default is ok", func(t *testing.T) {
		h := newHarness(t, Config{}, okAction("a", pluginapi.ModeSync))
		health, err := h.plugin.Health(context.Background())
		require.NoError(t, err)
		assert.Equal(t, pluginapi.HealthOK, health.Status)
	})

	t.Run("custom check used", func(t *testing.T) {
		cfg := Config{HealthCheck: func(context.Context) pluginapi.Health {
			return pluginapi.Health{Status: pluginapi.HealthDegraded, Checks: map[string]pluginapi.Check{"db": {Status: pluginapi.HealthDown}}}
		}}
		h := newHarness(t, cfg, okAction("a", pluginapi.ModeSync))
		health, err := h.plugin.Health(context.Background())
		require.NoError(t, err)
		assert.Equal(t, pluginapi.HealthDegraded, health.Status)
		assert.Equal(t, pluginapi.HealthDown, health.Checks["db"].Status)
	})
}

func TestStartJob(t *testing.T) {
	t.Run("unknown action 404", func(t *testing.T) {
		h := newHarness(t, Config{}, okAction("a", pluginapi.ModeSync))
		_, err := h.plugin.StartJob(context.Background(), "nope", h.request("j1", `{}`))
		require.ErrorIs(t, err, pluginapi.ErrUnknownAction)
	})

	t.Run("bad json 422", func(t *testing.T) {
		h := newHarness(t, Config{}, okAction("a", pluginapi.ModeSync))
		status, body := h.rawPost(t, "/v1/actions/a/jobs", testToken, []byte(`{not json`))
		assert.Equal(t, http.StatusUnprocessableEntity, status)
		var apiErr pluginapi.APIError
		require.NoError(t, json.Unmarshal(body, &apiErr))
		assert.Equal(t, pluginapi.CodeInvalidInput, apiErr.Code)
	})

	t.Run("input violating schema 422", func(t *testing.T) {
		h := newHarness(t, Config{}, okAction("a", pluginapi.ModeSync))
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{"n":"x"}`))
		require.ErrorIs(t, err, pluginapi.ErrInvalidInput)
	})

	t.Run("past deadline 422", func(t *testing.T) {
		h := newHarness(t, Config{}, okAction("a", pluginapi.ModeSync))
		req := h.request("j1", `{}`)
		req.Deadline = time.Now().Add(-time.Minute)
		_, err := h.plugin.StartJob(context.Background(), "a", req)
		require.ErrorIs(t, err, pluginapi.ErrInvalidInput)
	})

	t.Run("zero deadline 422", func(t *testing.T) {
		h := newHarness(t, Config{}, okAction("a", pluginapi.ModeSync))
		req := h.request("j1", `{}`)
		req.Deadline = time.Time{}
		_, err := h.plugin.StartJob(context.Background(), "a", req)
		require.ErrorIs(t, err, pluginapi.ErrInvalidInput)
	})

	t.Run("empty job id 422", func(t *testing.T) {
		h := newHarness(t, Config{}, okAction("a", pluginapi.ModeSync))
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("", `{}`))
		require.ErrorIs(t, err, pluginapi.ErrInvalidInput)
	})

	t.Run("missing callback urls 422", func(t *testing.T) {
		h := newHarness(t, Config{}, okAction("a", pluginapi.ModeAsync))
		req := h.request("j1", `{}`)
		req.Callback.ProgressURL = ""
		_, err := h.plugin.StartJob(context.Background(), "a", req)
		require.ErrorIs(t, err, pluginapi.ErrInvalidInput)
	})

	t.Run("zero heartbeat 422", func(t *testing.T) {
		h := newHarness(t, Config{}, okAction("a", pluginapi.ModeAsync))
		req := h.request("j1", `{}`)
		req.HeartbeatSeconds = 0
		_, err := h.plugin.StartJob(context.Background(), "a", req)
		require.ErrorIs(t, err, pluginapi.ErrInvalidInput)
	})

	t.Run("oversized body 413", func(t *testing.T) {
		h := newHarness(t, Config{MaxBodyBytes: 64}, okAction("a", pluginapi.ModeSync))
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{"pad":"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}`))
		require.ErrorIs(t, err, pluginapi.ErrPayloadTooLarge)
	})

	t.Run("sync success returns result", func(t *testing.T) {
		h := newHarness(t, Config{}, okAction("a", pluginapi.ModeSync))
		status, state := h.rawStart(t, "a", h.request("j1", `{"n":1}`))
		assert.Equal(t, http.StatusOK, status)
		assert.Equal(t, pluginapi.JobSucceeded, state.Status)
		require.NotNil(t, state.Result)
		assert.Equal(t, "j1", state.Result.JobID)
		assert.Equal(t, pluginapi.OutcomeOK, state.Result.Outcome)
		assert.JSONEq(t, `{"v":1}`, string(state.Result.Output))
		assert.Equal(t, 3, state.Result.Usage.Input)
		assert.Empty(t, h.core.results)
	})

	t.Run("async start reports running even when the job ends at once", func(t *testing.T) {
		instant := fakeAction{
			spec: testSpec("a", pluginapi.ModeAsync),
			run: func(context.Context, pluginapi.Job, pluginapi.Reporter) (pluginapi.Output, error) {
				return pluginapi.Output{}, pluginapi.ErrUpstream
			},
		}
		h := newHarness(t, Config{RetryBackoff: 10 * time.Millisecond}, instant)
		status, state := h.rawStart(t, "a", h.request("j1", `{}`))
		assert.Equal(t, http.StatusAccepted, status)
		assert.Equal(t, pluginapi.JobRunning, state.Status)
	})

	t.Run("async accepted then result delivered", func(t *testing.T) {
		h := newHarness(t, Config{}, okAction("a", pluginapi.ModeAsync))
		status, state := h.rawStart(t, "a", h.request("j1", `{}`))
		assert.Equal(t, http.StatusAccepted, status)
		assert.Equal(t, pluginapi.JobRunning, state.Status)
		res := h.core.nextResult(t)
		assert.Equal(t, "j1", res.JobID)
		assert.Equal(t, pluginapi.OutcomeOK, res.Outcome)
		assert.JSONEq(t, `{"v":1}`, string(res.Output))
		assert.Equal(t, 4, res.Usage.Output)
		assert.Nil(t, res.Error)
	})

	t.Run("null input rejected by schema", func(t *testing.T) {
		h := newHarness(t, Config{}, okAction("a", pluginapi.ModeSync))
		req := h.request("j1", ``)
		req.Input = nil
		_, err := h.plugin.StartJob(context.Background(), "a", req)
		require.ErrorIs(t, err, pluginapi.ErrInvalidInput)
	})
}

func TestStartJobIdempotency(t *testing.T) {
	t.Run("repeat while running starts one run", func(t *testing.T) {
		runs := make(chan struct{}, 4)
		release := make(chan struct{})
		act := fakeAction{
			spec: testSpec("b", pluginapi.ModeAsync),
			run: func(ctx context.Context, _ pluginapi.Job, _ pluginapi.Reporter) (pluginapi.Output, error) {
				runs <- struct{}{}
				select {
				case <-release:
					return okOutput(), nil
				case <-ctx.Done():
					return pluginapi.Output{}, ctx.Err()
				}
			},
		}
		h := newHarness(t, Config{}, act)
		req := h.request("j1", `{ "n" : 1 }`)
		status, _ := h.rawStart(t, "b", req)
		assert.Equal(t, http.StatusAccepted, status)
		waitSignal(t, runs)

		again := h.request("j1", `{"n":1}`)
		status, state := h.rawStart(t, "b", again)
		assert.Equal(t, http.StatusAccepted, status)
		assert.Equal(t, pluginapi.JobRunning, state.Status)

		close(release)
		h.waitStatus(t, "j1", pluginapi.JobSucceeded)
		assert.Empty(t, runs)
	})

	t.Run("repeat after finish returns 200 with result", func(t *testing.T) {
		h := newHarness(t, Config{}, okAction("a", pluginapi.ModeSync))
		first, _ := h.rawStart(t, "a", h.request("j1", `{}`))
		assert.Equal(t, http.StatusOK, first)
		status, state := h.rawStart(t, "a", h.request("j1", `{}`))
		assert.Equal(t, http.StatusOK, status)
		assert.Equal(t, pluginapi.JobSucceeded, state.Status)
		require.NotNil(t, state.Result)
	})

	t.Run("different input conflicts", func(t *testing.T) {
		h := newHarness(t, Config{}, okAction("a", pluginapi.ModeSync))
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{"n":1}`))
		require.NoError(t, err)
		_, err = h.plugin.StartJob(context.Background(), "a", h.request("j1", `{"n":2}`))
		require.ErrorIs(t, err, pluginapi.ErrJobConflict)
	})

	t.Run("different action conflicts", func(t *testing.T) {
		h := newHarness(t, Config{}, okAction("a", pluginapi.ModeSync), okAction("b", pluginapi.ModeSync))
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
		require.NoError(t, err)
		_, err = h.plugin.StartJob(context.Background(), "b", h.request("j1", `{}`))
		require.ErrorIs(t, err, pluginapi.ErrJobConflict)
	})

	t.Run("run counter stays at one", func(t *testing.T) {
		counter := make(chan struct{}, 8)
		act := fakeAction{
			spec: testSpec("a", pluginapi.ModeSync),
			run: func(context.Context, pluginapi.Job, pluginapi.Reporter) (pluginapi.Output, error) {
				counter <- struct{}{}
				return okOutput(), nil
			},
		}
		h := newHarness(t, Config{}, act)
		for range 3 {
			_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
			require.NoError(t, err)
		}
		assert.Len(t, counter, 1)
	})
}

func TestRunOutcomes(t *testing.T) {
	t.Run("undeclared outcome fails internal", func(t *testing.T) {
		act := fakeAction{
			spec: testSpec("a", pluginapi.ModeSync),
			run: func(context.Context, pluginapi.Job, pluginapi.Reporter) (pluginapi.Output, error) {
				return pluginapi.Output{Outcome: "bogus", Data: json.RawMessage(`{"v":1}`)}, nil
			},
		}
		h := newHarness(t, Config{}, act)
		_, state := h.rawStart(t, "a", h.request("j1", `{}`))
		assert.Equal(t, pluginapi.JobFailed, state.Status)
		require.NotNil(t, state.Result.Error)
		assert.Equal(t, pluginapi.ErrorInternal, state.Result.Error.Kind)
		assert.Contains(t, state.Result.Error.Message, pluginapi.ErrUndeclaredOutcome.Error())
	})

	t.Run("output violating schema fails internal", func(t *testing.T) {
		act := fakeAction{
			spec: testSpec("a", pluginapi.ModeSync),
			run: func(context.Context, pluginapi.Job, pluginapi.Reporter) (pluginapi.Output, error) {
				return pluginapi.Output{Outcome: pluginapi.OutcomeOK, Data: json.RawMessage(`{"v":"x"}`)}, nil
			},
		}
		h := newHarness(t, Config{}, act)
		_, state := h.rawStart(t, "a", h.request("j1", `{}`))
		assert.Equal(t, pluginapi.JobFailed, state.Status)
		assert.Equal(t, pluginapi.ErrorInternal, state.Result.Error.Kind)
		assert.Contains(t, state.Result.Error.Message, pluginapi.ErrSchemaViolation.Error())
	})

	t.Run("empty output validated as null", func(t *testing.T) {
		act := fakeAction{
			spec: testSpec("a", pluginapi.ModeSync),
			run: func(context.Context, pluginapi.Job, pluginapi.Reporter) (pluginapi.Output, error) {
				return pluginapi.Output{Outcome: pluginapi.OutcomeOK}, nil
			},
		}
		h := newHarness(t, Config{}, act)
		_, state := h.rawStart(t, "a", h.request("j1", `{}`))
		assert.Equal(t, pluginapi.JobFailed, state.Status)
	})

	t.Run("upstream error fails upstream", func(t *testing.T) {
		act := fakeAction{
			spec: testSpec("a", pluginapi.ModeSync),
			run: func(context.Context, pluginapi.Job, pluginapi.Reporter) (pluginapi.Output, error) {
				return pluginapi.Output{}, errors.Join(pluginapi.ErrUpstream, errors.New("forgejo down"))
			},
		}
		h := newHarness(t, Config{}, act)
		_, state := h.rawStart(t, "a", h.request("j1", `{}`))
		assert.Equal(t, pluginapi.JobFailed, state.Status)
		assert.Equal(t, pluginapi.ErrorUpstream, state.Result.Error.Kind)
		assert.Contains(t, state.Result.Error.Message, "forgejo down")
	})

	t.Run("plain error fails internal", func(t *testing.T) {
		act := fakeAction{
			spec: testSpec("a", pluginapi.ModeSync),
			run: func(context.Context, pluginapi.Job, pluginapi.Reporter) (pluginapi.Output, error) {
				return pluginapi.Output{}, errors.New("boom")
			},
		}
		h := newHarness(t, Config{}, act)
		_, state := h.rawStart(t, "a", h.request("j1", `{}`))
		assert.Equal(t, pluginapi.ErrorInternal, state.Result.Error.Kind)
		assert.Equal(t, "boom", state.Result.Error.Message)
	})

	t.Run("panic fails internal", func(t *testing.T) {
		act := fakeAction{
			spec: testSpec("a", pluginapi.ModeSync),
			run: func(context.Context, pluginapi.Job, pluginapi.Reporter) (pluginapi.Output, error) {
				panic("kaput")
			},
		}
		h := newHarness(t, Config{}, act)
		_, state := h.rawStart(t, "a", h.request("j1", `{}`))
		assert.Equal(t, pluginapi.JobFailed, state.Status)
		assert.Equal(t, pluginapi.ErrorInternal, state.Result.Error.Kind)
		assert.Contains(t, state.Result.Error.Message, "panic: kaput")
	})

	t.Run("deadline exceeded fails and is delivered", func(t *testing.T) {
		h := newHarness(t, Config{}, blockingAction("a", make(chan struct{})))
		req := h.request("j1", `{}`)
		req.Deadline = time.Now().Add(150 * time.Millisecond)
		_, err := h.plugin.StartJob(context.Background(), "a", req)
		require.NoError(t, err)
		res := h.core.nextResult(t)
		require.NotNil(t, res.Error)
		assert.Equal(t, pluginapi.ErrorInternal, res.Error.Kind)
		assert.Equal(t, "deadline exceeded", res.Error.Message)
		state := h.waitStatus(t, "j1", pluginapi.JobFailed)
		assert.Equal(t, "j1", state.Result.JobID)
	})

	t.Run("sync job cancelled when request is abandoned", func(t *testing.T) {
		started := make(chan struct{})
		ended := make(chan struct{})
		act := fakeAction{
			spec: testSpec("a", pluginapi.ModeSync),
			run: func(ctx context.Context, _ pluginapi.Job, _ pluginapi.Reporter) (pluginapi.Output, error) {
				close(started)
				<-ctx.Done()
				close(ended)
				return pluginapi.Output{}, ctx.Err()
			},
		}
		h := newHarness(t, Config{}, act)
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			_, err := h.plugin.StartJob(ctx, "a", h.request("j1", `{}`))
			assert.Error(t, err)
		}()
		waitSignal(t, started)
		cancel()
		waitSignal(t, ended)
		h.waitStatus(t, "j1", pluginapi.JobCancelled)
	})
}

func TestGetJob(t *testing.T) {
	h := newHarness(t, Config{}, okAction("a", pluginapi.ModeSync))

	t.Run("unknown 404", func(t *testing.T) {
		_, err := h.plugin.GetJob(context.Background(), "nope")
		require.ErrorIs(t, err, pluginapi.ErrJobNotFound)
	})

	t.Run("known returns state", func(t *testing.T) {
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
		require.NoError(t, err)
		state, err := h.plugin.GetJob(context.Background(), "j1")
		require.NoError(t, err)
		assert.Equal(t, "a", state.Action)
		assert.Equal(t, pluginapi.JobSucceeded, state.Status)
		require.NotNil(t, state.Result)
	})
}

func TestCancelJob(t *testing.T) {
	t.Run("unknown 404", func(t *testing.T) {
		h := newHarness(t, Config{}, okAction("a", pluginapi.ModeSync))
		_, err := h.plugin.CancelJob(context.Background(), "nope")
		require.ErrorIs(t, err, pluginapi.ErrJobNotFound)
	})

	t.Run("terminal is a no-op", func(t *testing.T) {
		h := newHarness(t, Config{}, okAction("a", pluginapi.ModeSync))
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
		require.NoError(t, err)
		code, _ := h.rawPost(t, "/v1/jobs/j1/cancel", testToken, nil)
		assert.Equal(t, http.StatusOK, code)
		state, err := h.plugin.CancelJob(context.Background(), "j1")
		require.NoError(t, err)
		assert.Equal(t, pluginapi.JobSucceeded, state.Status)
	})

	t.Run("running is cancelled and result delivered", func(t *testing.T) {
		h := newHarness(t, Config{}, blockingAction("a", make(chan struct{})))
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
		require.NoError(t, err)
		code, _ := h.rawPost(t, "/v1/jobs/j1/cancel", testToken, nil)
		assert.Equal(t, http.StatusAccepted, code)
		res := h.core.nextResult(t)
		require.NotNil(t, res.Error)
		assert.Equal(t, "job cancelled", res.Error.Message)
		assert.Equal(t, pluginapi.ErrorInternal, res.Error.Kind)
		h.waitStatus(t, "j1", pluginapi.JobCancelled)
	})
}

func TestReporter(t *testing.T) {
	t.Run("progress posts phase", func(t *testing.T) {
		proceed := make(chan struct{})
		act := fakeAction{
			spec: testSpec("a", pluginapi.ModeAsync),
			run: func(ctx context.Context, _ pluginapi.Job, rep pluginapi.Reporter) (pluginapi.Output, error) {
				if err := rep.Progress(ctx, "cloning"); err != nil {
					return pluginapi.Output{}, err
				}
				<-proceed
				return okOutput(), nil
			},
		}
		h := newHarness(t, Config{}, act)
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
		require.NoError(t, err)
		p := h.core.nextProgress(t)
		assert.Equal(t, "cloning", p.Phase)
		assert.Equal(t, "j1", p.JobID)
		state, err := h.plugin.GetJob(context.Background(), "j1")
		require.NoError(t, err)
		assert.Equal(t, "cloning", state.Phase)
		close(proceed)
		h.waitStatus(t, "j1", pluginapi.JobSucceeded)
	})

	t.Run("progress gone cancels job", func(t *testing.T) {
		gotErr := make(chan error, 1)
		act := fakeAction{
			spec: testSpec("a", pluginapi.ModeAsync),
			run: func(ctx context.Context, _ pluginapi.Job, rep pluginapi.Reporter) (pluginapi.Output, error) {
				err := rep.Progress(ctx, "x")
				gotErr <- err
				<-ctx.Done()
				return pluginapi.Output{}, ctx.Err()
			},
		}
		h := newHarness(t, Config{}, act)
		h.core.replyProgress(http.StatusGone)
		h.core.replyResults(http.StatusGone)
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
		require.NoError(t, err)
		select {
		case err := <-gotErr:
			require.ErrorIs(t, err, pluginapi.ErrJobNotActive)
		case <-time.After(waitTimeout):
			require.FailNow(t, "progress did not return")
		}
		h.waitStatus(t, "j1", pluginapi.JobCancelled)
	})

	t.Run("progress on sync job only records phase", func(t *testing.T) {
		act := fakeAction{
			spec: testSpec("a", pluginapi.ModeSync),
			run: func(ctx context.Context, _ pluginapi.Job, rep pluginapi.Reporter) (pluginapi.Output, error) {
				if err := rep.Progress(ctx, "thinking"); err != nil {
					return pluginapi.Output{}, err
				}
				return okOutput(), nil
			},
		}
		h := newHarness(t, Config{}, act)
		_, state := h.rawStart(t, "a", h.request("j1", `{}`))
		assert.Equal(t, pluginapi.JobSucceeded, state.Status)
		assert.Equal(t, "thinking", state.Phase)
		assert.Empty(t, h.core.progress)
	})
}

func TestHeartbeat(t *testing.T) {
	t.Run("posts at interval with current phase", func(t *testing.T) {
		release := make(chan struct{})
		act := fakeAction{
			spec: testSpec("a", pluginapi.ModeAsync),
			run: func(ctx context.Context, _ pluginapi.Job, rep pluginapi.Reporter) (pluginapi.Output, error) {
				if err := rep.Progress(ctx, "waiting_for_input"); err != nil {
					return pluginapi.Output{}, err
				}
				<-release
				return okOutput(), nil
			},
		}
		h := newHarness(t, Config{}, act)
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
		require.NoError(t, err)
		first := h.core.nextProgress(t)
		assert.Equal(t, "waiting_for_input", first.Phase)
		beat := h.core.nextProgress(t)
		assert.Equal(t, "waiting_for_input", beat.Phase)
		assert.Equal(t, "j1", beat.JobID)
		close(release)
		h.waitStatus(t, "j1", pluginapi.JobSucceeded)
	})

	t.Run("gone reply cancels job", func(t *testing.T) {
		h := newHarness(t, Config{}, blockingAction("a", make(chan struct{})))
		h.core.replyProgress(http.StatusGone)
		h.core.replyResults(http.StatusGone)
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
		require.NoError(t, err)
		h.waitStatus(t, "j1", pluginapi.JobCancelled)
	})
}

func TestDelivery(t *testing.T) {
	t.Run("retries after server error then succeeds", func(t *testing.T) {
		h := newHarness(t, Config{}, okAction("a", pluginapi.ModeAsync))
		h.core.replyResults(http.StatusInternalServerError, http.StatusNoContent)
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
		require.NoError(t, err)
		first := h.core.nextResult(t)
		second := h.core.nextResult(t)
		assert.Equal(t, first.JobID, second.JobID)
		assert.Equal(t, pluginapi.OutcomeOK, second.Outcome)
		select {
		case <-h.core.results:
			require.FailNow(t, "unexpected third delivery")
		case <-time.After(100 * time.Millisecond):
		}
	})

	t.Run("stops on gone", func(t *testing.T) {
		h := newHarness(t, Config{}, okAction("a", pluginapi.ModeAsync))
		h.core.replyResults(http.StatusGone)
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
		require.NoError(t, err)
		h.core.nextResult(t)
		select {
		case <-h.core.results:
			require.FailNow(t, "result redelivered after 410")
		case <-time.After(150 * time.Millisecond):
		}
	})

	t.Run("stops on client error", func(t *testing.T) {
		h := newHarness(t, Config{}, okAction("a", pluginapi.ModeAsync))
		h.core.replyResults(http.StatusBadRequest)
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
		require.NoError(t, err)
		h.core.nextResult(t)
		select {
		case <-h.core.results:
			require.FailNow(t, "result redelivered after 400")
		case <-time.After(150 * time.Millisecond):
		}
	})
}

func TestEvents(t *testing.T) {
	newEventsAction := func(emitted chan struct{}, release chan struct{}) fakeAction {
		return fakeAction{
			spec: testSpec("a", pluginapi.ModeAsync, pluginapi.CapabilityEvents),
			run: func(ctx context.Context, _ pluginapi.Job, rep pluginapi.Reporter) (pluginapi.Output, error) {
				for _, kind := range []string{"one", "two"} {
					if err := rep.Event(ctx, kind, json.RawMessage(`{"k":"`+kind+`"}`)); err != nil {
						return pluginapi.Output{}, err
					}
				}
				close(emitted)
				<-release
				if err := rep.Event(ctx, "three", nil); err != nil {
					return pluginapi.Output{}, err
				}
				return okOutput(), nil
			},
		}
	}

	t.Run("replays after seq, tails live and ends at terminal", func(t *testing.T) {
		emitted := make(chan struct{})
		release := make(chan struct{})
		h := newHarness(t, Config{}, newEventsAction(emitted, release))
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
		require.NoError(t, err)
		waitSignal(t, emitted)

		got := make(chan pluginapi.Event, 8)
		streamDone := make(chan error, 1)
		go func() {
			streamDone <- h.plugin.StreamEvents(context.Background(), "j1", 1, func(ev pluginapi.Event) error {
				got <- ev
				return nil
			})
		}()
		first := <-got
		assert.Equal(t, int64(2), first.Seq)
		assert.Equal(t, "two", first.Kind)
		assert.JSONEq(t, `{"k":"two"}`, string(first.Data))

		close(release)
		third := <-got
		assert.Equal(t, int64(3), third.Seq)
		assert.Equal(t, "three", third.Kind)
		select {
		case err := <-streamDone:
			require.NoError(t, err)
		case <-time.After(waitTimeout):
			require.FailNow(t, "stream did not end at terminal")
		}
	})

	t.Run("full replay after terminal", func(t *testing.T) {
		emitted := make(chan struct{})
		release := make(chan struct{})
		close(release)
		h := newHarness(t, Config{}, newEventsAction(emitted, release))
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
		require.NoError(t, err)
		h.waitStatus(t, "j1", pluginapi.JobSucceeded)
		var seqs []int64
		err = h.plugin.StreamEvents(context.Background(), "j1", 0, func(ev pluginapi.Event) error {
			seqs = append(seqs, ev.Seq)
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, []int64{1, 2, 3}, seqs)
	})

	t.Run("without capability 409", func(t *testing.T) {
		h := newHarness(t, Config{}, okAction("a", pluginapi.ModeSync))
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
		require.NoError(t, err)
		err = h.plugin.StreamEvents(context.Background(), "j1", 0, func(pluginapi.Event) error { return nil })
		require.ErrorIs(t, err, pluginapi.ErrUnsupported)
	})

	t.Run("unknown job 404", func(t *testing.T) {
		h := newHarness(t, Config{}, okAction("a", pluginapi.ModeSync))
		err := h.plugin.StreamEvents(context.Background(), "nope", 0, func(pluginapi.Event) error { return nil })
		require.ErrorIs(t, err, pluginapi.ErrJobNotFound)
	})

	t.Run("invalid after 422", func(t *testing.T) {
		emitted := make(chan struct{})
		release := make(chan struct{})
		close(release)
		h := newHarness(t, Config{}, newEventsAction(emitted, release))
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
		require.NoError(t, err)
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, h.ts.URL+"/v1/jobs/j1/events?after=abc", nil)
		require.NoError(t, err)
		req.Header.Set(pluginapi.HeaderAuthorization, pluginapi.BearerPrefix+testToken)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	})

	t.Run("event without capability is a no-op", func(t *testing.T) {
		act := fakeAction{
			spec: testSpec("a", pluginapi.ModeSync),
			run: func(ctx context.Context, _ pluginapi.Job, rep pluginapi.Reporter) (pluginapi.Output, error) {
				if err := rep.Event(ctx, "x", nil); err != nil {
					return pluginapi.Output{}, err
				}
				return okOutput(), nil
			},
		}
		h := newHarness(t, Config{}, act)
		_, state := h.rawStart(t, "a", h.request("j1", `{}`))
		assert.Equal(t, pluginapi.JobSucceeded, state.Status)
	})
}

func TestMessages(t *testing.T) {
	t.Run("reach Messages channel", func(t *testing.T) {
		started := make(chan struct{})
		received := make(chan pluginapi.Message, 1)
		act := fakeAction{
			spec: testSpec("a", pluginapi.ModeAsync, pluginapi.CapabilityInject),
			run: func(ctx context.Context, _ pluginapi.Job, rep pluginapi.Reporter) (pluginapi.Output, error) {
				close(started)
				select {
				case msg := <-rep.Messages():
					received <- msg
					return okOutput(), nil
				case <-ctx.Done():
					return pluginapi.Output{}, ctx.Err()
				}
			},
		}
		h := newHarness(t, Config{}, act)
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
		require.NoError(t, err)
		waitSignal(t, started)
		require.NoError(t, h.plugin.SendMessage(context.Background(), "j1", pluginapi.Message{Text: "hello"}))
		select {
		case msg := <-received:
			assert.Equal(t, "hello", msg.Text)
		case <-time.After(waitTimeout):
			require.FailNow(t, "message not received")
		}
	})

	t.Run("without capability 409", func(t *testing.T) {
		h := newHarness(t, Config{}, blockingAction("a", make(chan struct{})))
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
		require.NoError(t, err)
		err = h.plugin.SendMessage(context.Background(), "j1", pluginapi.Message{Text: "x"})
		require.ErrorIs(t, err, pluginapi.ErrUnsupported)
	})

	t.Run("after terminal 409", func(t *testing.T) {
		act := fakeAction{
			spec: testSpec("a", pluginapi.ModeAsync, pluginapi.CapabilityInject),
			run: func(context.Context, pluginapi.Job, pluginapi.Reporter) (pluginapi.Output, error) {
				return okOutput(), nil
			},
		}
		h := newHarness(t, Config{}, act)
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
		require.NoError(t, err)
		h.waitStatus(t, "j1", pluginapi.JobSucceeded)
		err = h.plugin.SendMessage(context.Background(), "j1", pluginapi.Message{Text: "x"})
		require.ErrorIs(t, err, pluginapi.ErrJobNotActive)
	})

	t.Run("unknown job 404", func(t *testing.T) {
		h := newHarness(t, Config{}, okAction("a", pluginapi.ModeSync))
		err := h.plugin.SendMessage(context.Background(), "nope", pluginapi.Message{Text: "x"})
		require.ErrorIs(t, err, pluginapi.ErrJobNotFound)
	})

	t.Run("empty text 422", func(t *testing.T) {
		act := fakeAction{
			spec: testSpec("a", pluginapi.ModeAsync, pluginapi.CapabilityInject),
			run: func(ctx context.Context, _ pluginapi.Job, _ pluginapi.Reporter) (pluginapi.Output, error) {
				<-ctx.Done()
				return pluginapi.Output{}, ctx.Err()
			},
		}
		h := newHarness(t, Config{}, act)
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
		require.NoError(t, err)
		err = h.plugin.SendMessage(context.Background(), "j1", pluginapi.Message{})
		require.ErrorIs(t, err, pluginapi.ErrInvalidInput)
	})

	t.Run("bad body 422", func(t *testing.T) {
		act := fakeAction{
			spec: testSpec("a", pluginapi.ModeAsync, pluginapi.CapabilityInject),
			run: func(ctx context.Context, _ pluginapi.Job, _ pluginapi.Reporter) (pluginapi.Output, error) {
				<-ctx.Done()
				return pluginapi.Output{}, ctx.Err()
			},
		}
		h := newHarness(t, Config{}, act)
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
		require.NoError(t, err)
		status, _ := h.rawPost(t, "/v1/jobs/j1/messages", testToken, []byte(`{nope`))
		assert.Equal(t, http.StatusUnprocessableEntity, status)
	})
}

func TestJanitor(t *testing.T) {
	t.Run("removes old terminal jobs", func(t *testing.T) {
		h := newHarness(t, Config{JobRetention: 40 * time.Millisecond}, okAction("a", pluginapi.ModeSync))
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			_, err := h.plugin.GetJob(context.Background(), "j1")
			return errors.Is(err, pluginapi.ErrJobNotFound)
		}, waitTimeout, tick)
	})

	t.Run("keeps running jobs", func(t *testing.T) {
		h := newHarness(t, Config{JobRetention: 40 * time.Millisecond}, blockingAction("a", make(chan struct{})))
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
		require.NoError(t, err)
		time.Sleep(150 * time.Millisecond)
		state, err := h.plugin.GetJob(context.Background(), "j1")
		require.NoError(t, err)
		assert.Equal(t, pluginapi.JobRunning, state.Status)
	})
}

func TestShutdown(t *testing.T) {
	t.Run("cancels running jobs and returns", func(t *testing.T) {
		h := newHarness(t, Config{}, blockingAction("a", make(chan struct{})))
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
		defer cancel()
		require.NoError(t, h.srv.Shutdown(ctx))
		state, err := h.plugin.GetJob(context.Background(), "j1")
		require.NoError(t, err)
		assert.Equal(t, pluginapi.JobCancelled, state.Status)
	})

	t.Run("idempotent", func(t *testing.T) {
		h := newHarness(t, Config{}, okAction("a", pluginapi.ModeSync))
		require.NoError(t, h.srv.Shutdown(context.Background()))
		require.NoError(t, h.srv.Shutdown(context.Background()))
	})

	t.Run("rejects new jobs after shutdown", func(t *testing.T) {
		h := newHarness(t, Config{}, okAction("a", pluginapi.ModeSync))
		require.NoError(t, h.srv.Shutdown(context.Background()))
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
		require.ErrorIs(t, err, pluginapi.ErrUpstream)
	})

	t.Run("returns context error when job ignores cancellation", func(t *testing.T) {
		started := make(chan struct{})
		release := make(chan struct{})
		act := fakeAction{
			spec: testSpec("a", pluginapi.ModeAsync),
			run: func(context.Context, pluginapi.Job, pluginapi.Reporter) (pluginapi.Output, error) {
				close(started)
				<-release
				return okOutput(), nil
			},
		}
		h := newHarness(t, Config{}, act)
		_, err := h.plugin.StartJob(context.Background(), "a", h.request("j1", `{}`))
		require.NoError(t, err)
		waitSignal(t, started)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		require.ErrorIs(t, h.srv.Shutdown(ctx), context.DeadlineExceeded)
		close(release)
	})
}

func TestServe(t *testing.T) {
	info := pluginapi.Info{Name: "tester", Version: "1.0.0", Description: "test plugin"}

	freeAddr := func(t *testing.T) string {
		t.Helper()
		l, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		addr := l.Addr().String()
		require.NoError(t, l.Close())
		return addr
	}

	t.Run("serves and stops on context cancel", func(t *testing.T) {
		addr := freeAddr(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			done <- Serve(ctx, Config{Addr: addr, Token: testToken, ShutdownTimeout: waitTimeout}, info, okAction("a", pluginapi.ModeSync))
		}()
		p := client.NewPlugin("http://"+addr, testToken, nil)
		require.Eventually(t, func() bool {
			_, err := p.Health(context.Background())
			return err == nil
		}, waitTimeout, tick)
		cancel()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(waitTimeout):
			require.FailNow(t, "Serve did not return")
		}
	})

	t.Run("invalid config returned", func(t *testing.T) {
		err := Serve(context.Background(), Config{Addr: "127.0.0.1:0"}, info, okAction("a", pluginapi.ModeSync))
		require.ErrorIs(t, err, ErrMissingToken)
	})

	t.Run("listen failure returned", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		defer func() { require.NoError(t, l.Close()) }()
		err = Serve(context.Background(), Config{Addr: l.Addr().String(), Token: testToken}, info, okAction("a", pluginapi.ModeSync))
		require.Error(t, err)
	})
}
