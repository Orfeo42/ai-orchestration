package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/pluginapi"
	"github.com/Orfeo42/ai-orchestration/pluginapi/client"
)

const (
	testToken   = "secret"
	waitTimeout = 5 * time.Second
	tick        = 5 * time.Millisecond
)

type fakeAction struct {
	spec pluginapi.ActionSpec
	run  func(ctx context.Context, job pluginapi.Job, rep pluginapi.Reporter) (pluginapi.Output, error)
}

func (a fakeAction) Spec() pluginapi.ActionSpec { return a.spec }

func (a fakeAction) Run(ctx context.Context, job pluginapi.Job, rep pluginapi.Reporter) (pluginapi.Output, error) {
	return a.run(ctx, job, rep)
}

func testSpec(name string, mode pluginapi.Mode, caps ...pluginapi.Capability) pluginapi.ActionSpec {
	return pluginapi.ActionSpec{
		Name:                  name,
		Description:           "test action",
		Mode:                  mode,
		InputSchema:           json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"}}}`),
		OutputSchema:          json.RawMessage(`{"type":"object","required":["v"],"properties":{"v":{"type":"integer"}}}`),
		Outcomes:              []pluginapi.Outcome{pluginapi.OutcomeOK, "other"},
		Capabilities:          caps,
		DefaultTimeoutSeconds: 5,
	}
}

func okOutput() pluginapi.Output {
	return pluginapi.Output{
		Outcome: pluginapi.OutcomeOK,
		Data:    json.RawMessage(`{"v":1}`),
		Usage:   pluginapi.Usage{Input: 3, Output: 4},
	}
}

func okAction(name string, mode pluginapi.Mode) fakeAction {
	return fakeAction{
		spec: testSpec(name, mode),
		run: func(context.Context, pluginapi.Job, pluginapi.Reporter) (pluginapi.Output, error) {
			return okOutput(), nil
		},
	}
}

func blockingAction(name string, release <-chan struct{}) fakeAction {
	return fakeAction{
		spec: testSpec(name, pluginapi.ModeAsync),
		run: func(ctx context.Context, _ pluginapi.Job, _ pluginapi.Reporter) (pluginapi.Output, error) {
			select {
			case <-release:
				return okOutput(), nil
			case <-ctx.Done():
				return pluginapi.Output{}, ctx.Err()
			}
		},
	}
}

type fakeCore struct {
	srv      *httptest.Server
	results  chan pluginapi.Result
	progress chan pluginapi.Progress

	mu             sync.Mutex
	resultReplies  []int
	progressStatus int
}

func newFakeCore(t *testing.T) *fakeCore {
	t.Helper()
	c := &fakeCore{
		results:        make(chan pluginapi.Result, 32),
		progress:       make(chan pluginapi.Progress, 64),
		progressStatus: http.StatusNoContent,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /result", func(w http.ResponseWriter, r *http.Request) {
		var res pluginapi.Result
		if err := json.NewDecoder(r.Body).Decode(&res); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		c.results <- res
		c.mu.Lock()
		status := http.StatusNoContent
		if len(c.resultReplies) > 0 {
			status = c.resultReplies[0]
			c.resultReplies = c.resultReplies[1:]
		}
		c.mu.Unlock()
		w.WriteHeader(status)
	})
	mux.HandleFunc("POST /progress", func(w http.ResponseWriter, r *http.Request) {
		var p pluginapi.Progress
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		select {
		case c.progress <- p:
		default:
		}
		c.mu.Lock()
		status := c.progressStatus
		c.mu.Unlock()
		w.WriteHeader(status)
	})
	c.srv = httptest.NewServer(mux)
	t.Cleanup(c.srv.Close)
	return c
}

func (c *fakeCore) replyResults(statuses ...int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resultReplies = statuses
}

func (c *fakeCore) replyProgress(status int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.progressStatus = status
}

func (c *fakeCore) nextResult(t *testing.T) pluginapi.Result {
	t.Helper()
	select {
	case res := <-c.results:
		return res
	case <-time.After(waitTimeout):
		require.FailNow(t, "timed out waiting for result callback")
		return pluginapi.Result{}
	}
}

func (c *fakeCore) nextProgress(t *testing.T) pluginapi.Progress {
	t.Helper()
	select {
	case p := <-c.progress:
		return p
	case <-time.After(waitTimeout):
		require.FailNow(t, "timed out waiting for progress callback")
		return pluginapi.Progress{}
	}
}

type harness struct {
	srv    *Server
	ts     *httptest.Server
	plugin *client.Plugin
	core   *fakeCore
}

func newHarness(t *testing.T, cfg Config, actions ...pluginapi.Action) *harness {
	t.Helper()
	if cfg.Token == "" {
		cfg.Token = testToken
	}
	if cfg.RetryBackoff == 0 {
		cfg.RetryBackoff = 10 * time.Millisecond
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	info := pluginapi.Info{Name: "tester", Version: "1.0.0", Description: "test plugin"}
	srv, err := New(cfg, info, actions...)
	require.NoError(t, err)
	ts := httptest.NewServer(srv)
	t.Cleanup(func() {
		ts.Close()
		ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
		defer cancel()
		require.NoError(t, srv.Shutdown(ctx))
	})
	return &harness{
		srv:    srv,
		ts:     ts,
		plugin: client.NewPlugin(ts.URL, cfg.Token, nil),
		core:   newFakeCore(t),
	}
}

func (h *harness) request(id, input string) pluginapi.JobRequest {
	return pluginapi.JobRequest{
		JobID:     id,
		ProcessID: "proc-1",
		Input:     json.RawMessage(input),
		Callback: pluginapi.Callback{
			ResultURL:   h.core.srv.URL + "/result",
			ProgressURL: h.core.srv.URL + "/progress",
		},
		Deadline:         time.Now().Add(time.Minute),
		HeartbeatSeconds: 1,
	}
}

func (h *harness) rawPost(t *testing.T, path string, token string, body []byte) (int, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, h.ts.URL+path, bytes.NewReader(body))
	require.NoError(t, err)
	if token != "" {
		req.Header.Set(pluginapi.HeaderAuthorization, pluginapi.BearerPrefix+token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return resp.StatusCode, data
}

func (h *harness) rawStart(t *testing.T, action string, req pluginapi.JobRequest) (int, pluginapi.JobState) {
	t.Helper()
	body, err := json.Marshal(req)
	require.NoError(t, err)
	status, data := h.rawPost(t, "/v1/actions/"+action+"/jobs", testToken, body)
	var state pluginapi.JobState
	require.NoError(t, json.Unmarshal(data, &state))
	return status, state
}

func (h *harness) waitStatus(t *testing.T, jobID string, want pluginapi.JobStatus) pluginapi.JobState {
	t.Helper()
	var state pluginapi.JobState
	require.Eventually(t, func() bool {
		var err error
		state, err = h.plugin.GetJob(context.Background(), jobID)
		return err == nil && state.Status == want
	}, waitTimeout, tick)
	return state
}

func waitSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(waitTimeout):
		require.FailNow(t, "timed out waiting for signal")
	}
}
