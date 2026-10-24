package plugins

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/internal/step"
	"github.com/Orfeo42/ai-orchestration/pluginapi"
)

const cbPluginName = "demo"

type cbPlugin struct {
	server    *httptest.Server
	state     pluginapi.JobState
	getStatus int
	cancels   atomic.Int32
}

func cbStartPlugin(t *testing.T) *cbPlugin {
	t.Helper()
	p := &cbPlugin{getStatus: http.StatusOK}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/cancel") {
			p.cancels.Add(1)
			require.NoError(t, json.NewEncoder(w).Encode(pluginapi.JobState{Status: pluginapi.JobCancelled}))
			return
		}
		if p.getStatus != http.StatusOK {
			w.WriteHeader(p.getStatus)
			require.NoError(t, json.NewEncoder(w).Encode(pluginapi.APIError{Code: pluginapi.CodeJobNotFound, Message: "no such job"}))
			return
		}
		require.NoError(t, json.NewEncoder(w).Encode(p.state))
	}))
	t.Cleanup(p.server.Close)
	return p
}

func cbRecord(baseURL string) Record {
	return Record{
		Name:    cbPluginName,
		BaseURL: baseURL,
		Manifest: pluginapi.Manifest{
			Name: cbPluginName,
			Actions: []pluginapi.ActionSpec{{
				Name:         "echo",
				Mode:         pluginapi.ModeAsync,
				InputSchema:  json.RawMessage(`{"type":"object"}`),
				OutputSchema: json.RawMessage(`{"type":"object","required":["v"],"properties":{"v":{"type":"integer"}}}`),
				Outcomes:     []pluginapi.Outcome{pluginapi.OutcomeOK, "other"},
			}},
		},
	}
}

func cbNewCatalog(t *testing.T, records ...Record) (*Catalog, *Clients) {
	t.Helper()
	store := &MockStore{ListPluginsFunc: func(context.Context) ([]Record, error) { return records, nil }}
	clients := NewClients(func(string) (string, bool) { return "tok", true }, nil)
	catalog := NewCatalog(step.NewRegistry(), store, clients, NewSigner([]byte("secret"), "http://core"))
	require.NoError(t, catalog.Refresh(context.Background()))
	clients.Bind(catalog)
	return catalog, clients
}
