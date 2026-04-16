package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ai-orchestration/internal/control"
	"ai-orchestration/internal/events"
	"ai-orchestration/internal/runstore"
)

func newTestServer(t *testing.T) (*httptest.Server, *runstore.RedisStore, *control.RedisChannel) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := runstore.NewRedisFromClient(client)
	ctrl := control.NewRedisFromClient(client)
	srv, err := newServer("127.0.0.1:0", store, ctrl)
	require.NoError(t, err)
	ts := httptest.NewServer(srv.srv.Handler)
	t.Cleanup(ts.Close)
	return ts, store, ctrl
}

func TestListRunsEmpty(t *testing.T) {
	t.Run("returns empty array", func(t *testing.T) {
		ts, _, _ := newTestServer(t)
		resp, err := http.Get(ts.URL + "/api/runs")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var body struct {
			Runs []runstore.Run `json:"runs"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		assert.Empty(t, body.Runs)
	})
}

func TestListRunsAfterCreate(t *testing.T) {
	t.Run("created run appears in list", func(t *testing.T) {
		ts, store, _ := newTestServer(t)
		ctx := context.Background()
		require.NoError(t, store.Create(ctx, runstore.Run{
			ID:        "r1",
			Service:   runstore.ServiceOrchestrator,
			RepoOwner: "acme",
			RepoName:  "x",
			Phase:     runstore.PhaseAssessing,
			StartedAt: time.Now().UTC(),
		}))

		resp, err := http.Get(ts.URL + "/api/runs")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var body struct {
			Runs []runstore.Run `json:"runs"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		require.Len(t, body.Runs, 1)
		assert.Equal(t, "r1", body.Runs[0].ID)
	})
}

func TestGetRunNotFound(t *testing.T) {
	t.Run("returns 404 for missing run", func(t *testing.T) {
		ts, _, _ := newTestServer(t)
		resp, err := http.Get(ts.URL + "/api/runs/missing")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}

func TestGetRunEvents(t *testing.T) {
	t.Run("returns appended events", func(t *testing.T) {
		ts, store, _ := newTestServer(t)
		ctx := context.Background()
		require.NoError(t, store.Create(ctx, runstore.Run{ID: "r2", StartedAt: time.Now().UTC()}))
		_, err := store.Append(ctx, "r2", events.Event{Kind: events.KindAssistant, Text: "hi"})
		require.NoError(t, err)

		resp, err := http.Get(ts.URL + "/api/runs/r2/events")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var body struct {
			Events []events.Event `json:"events"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		require.Len(t, body.Events, 1)
		assert.Equal(t, "hi", body.Events[0].Text)
	})
}

func TestCancelEndpoint(t *testing.T) {
	t.Run("publishes cancel ctrl message", func(t *testing.T) {
		ts, _, ctrl := newTestServer(t)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		sub, err := ctrl.Subscribe(ctx, "r3")
		require.NoError(t, err)
		defer sub.Close()

		resp, err := http.Post(ts.URL+"/api/runs/r3/cancel", "application/json", nil)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)

		select {
		case msg := <-sub.Messages():
			assert.Equal(t, control.TypeCancel, msg.Type)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for cancel ctrl msg")
		}
	})
}

func TestInjectEndpoint(t *testing.T) {
	t.Run("publishes inject ctrl message and persists note", func(t *testing.T) {
		ts, store, ctrl := newTestServer(t)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		require.NoError(t, store.Create(ctx, runstore.Run{ID: "r4", StartedAt: time.Now().UTC()}))

		sub, err := ctrl.Subscribe(ctx, "r4")
		require.NoError(t, err)
		defer sub.Close()

		body := strings.NewReader(`{"note":"also update README"}`)
		resp, err := http.Post(ts.URL+"/api/runs/r4/inject", "application/json", body)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)

		select {
		case msg := <-sub.Messages():
			assert.Equal(t, control.TypeInject, msg.Type)
			assert.Equal(t, "also update README", msg.Note)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for inject ctrl msg")
		}

		got, err := store.Get(ctx, "r4")
		require.NoError(t, err)
		assert.Contains(t, got.InjectNotes, "also update README")
	})
}

func TestHealthz(t *testing.T) {
	t.Run("returns ok", func(t *testing.T) {
		ts, _, _ := newTestServer(t)
		resp, err := http.Get(ts.URL + "/healthz")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestStaticFallback(t *testing.T) {
	t.Run("unknown route serves index.html", func(t *testing.T) {
		ts, _, _ := newTestServer(t)
		resp, err := http.Get(ts.URL + "/some/spa/route")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}
