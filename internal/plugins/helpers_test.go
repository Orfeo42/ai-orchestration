package plugins

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/internal/step"
	"github.com/Orfeo42/ai-orchestration/pluginapi"
	"github.com/Orfeo42/ai-orchestration/pluginapi/server"
)

const catToken = "tok"

func catTokens(string) (string, bool) { return catToken, true }

func catNoTokens(string) (string, bool) { return "", false }

type stepNoop struct{}

func (stepNoop) Run(context.Context, step.Context) (step.Result, error) {
	return step.Result{Outcome: step.OutcomeOK}, nil
}

type stepAction struct {
	spec pluginapi.ActionSpec
	run  func(ctx context.Context, job pluginapi.Job) (pluginapi.Output, error)
}

func (a stepAction) Spec() pluginapi.ActionSpec { return a.spec }

func (a stepAction) Run(ctx context.Context, job pluginapi.Job, _ pluginapi.Reporter) (pluginapi.Output, error) {
	return a.run(ctx, job)
}

func catSpec(name string, mode pluginapi.Mode) pluginapi.ActionSpec {
	return pluginapi.ActionSpec{
		Name:                  name,
		Description:           "test action",
		Mode:                  mode,
		InputSchema:           json.RawMessage(`{"type":"object","required":["n"],"properties":{"n":{"type":"integer"}}}`),
		OutputSchema:          json.RawMessage(`{"type":"object","required":["v"],"properties":{"v":{"type":"integer"}}}`),
		Outcomes:              []pluginapi.Outcome{pluginapi.OutcomeOK, "other"},
		DefaultTimeoutSeconds: 60,
	}
}

func catTrigger(name string) pluginapi.TriggerSpec {
	return pluginapi.TriggerSpec{
		Name:          name,
		Description:   "test trigger",
		PayloadSchema: json.RawMessage(`{"type":"object","required":["id"],"properties":{"id":{"type":"integer"}}}`),
	}
}

func catManifest(name string, specs []pluginapi.ActionSpec, triggers ...pluginapi.TriggerSpec) pluginapi.Manifest {
	return pluginapi.Manifest{
		Protocol:    pluginapi.ProtocolVersion,
		Name:        name,
		Version:     "1.0.0",
		Description: "test plugin",
		Actions:     specs,
		Triggers:    triggers,
	}
}

func catRecord(name, url string, m pluginapi.Manifest) Record {
	return Record{Name: name, BaseURL: url, Manifest: m, Status: StatusOK}
}

func catSyncAction(name string, out pluginapi.Output, err error) stepAction {
	return stepAction{
		spec: catSpec(name, pluginapi.ModeSync),
		run: func(context.Context, pluginapi.Job) (pluginapi.Output, error) {
			return out, err
		},
	}
}

func catBlockingAction(name string, release <-chan struct{}) stepAction {
	return stepAction{
		spec: catSpec(name, pluginapi.ModeAsync),
		run: func(ctx context.Context, _ pluginapi.Job) (pluginapi.Output, error) {
			select {
			case <-release:
				return pluginapi.Output{Outcome: pluginapi.OutcomeOK, Data: json.RawMessage(`{"v":1}`)}, nil
			case <-ctx.Done():
				return pluginapi.Output{}, ctx.Err()
			}
		},
	}
}

func catServe(t *testing.T, name string, actions ...pluginapi.Action) (*httptest.Server, pluginapi.Manifest) {
	t.Helper()
	srv, err := server.New(server.Config{Token: catToken}, pluginapi.Info{Name: name, Version: "1.0.0", Description: "test plugin"}, actions...)
	require.NoError(t, err)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, srv.Shutdown(ctx))
	})
	return ts, srv.Manifest()
}

func catStub(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, err := w.Write([]byte(body))
		require.NoError(t, err)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func catClosedURL(t *testing.T) string {
	t.Helper()
	ts := httptest.NewServer(http.NotFoundHandler())
	url := ts.URL
	ts.Close()
	return url
}

func catFixture(t *testing.T, tokens TokenSource, records ...Record) (*Catalog, *Clients, *MockStore) {
	t.Helper()
	store := &MockStore{
		ListPluginsFunc: func(context.Context) ([]Record, error) { return records, nil },
	}
	clients := NewClients(tokens, nil)
	catalog := NewCatalog(step.NewRegistry(), store, clients, NewSigner([]byte("k"), "http://core"))
	clients.Bind(catalog)
	require.NoError(t, catalog.Refresh(context.Background()))
	return catalog, clients, store
}
