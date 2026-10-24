package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/internal/plugins"
	"github.com/Orfeo42/ai-orchestration/pluginapi"
)

func newRecord(name string) plugins.Record {
	return plugins.Record{
		Name:    name,
		BaseURL: "http://" + name + ":8080",
		Manifest: pluginapi.Manifest{
			Protocol:    pluginapi.ProtocolVersion,
			Name:        name,
			Version:     "1.0.0",
			Description: "plugin " + name,
			Actions: []pluginapi.ActionSpec{{
				Name:                  "run",
				Description:           "run it",
				Mode:                  pluginapi.ModeAsync,
				InputSchema:           json.RawMessage(`{"type":"object"}`),
				OutputSchema:          json.RawMessage(`{"type":"object"}`),
				Outcomes:              []pluginapi.Outcome{pluginapi.OutcomeOK},
				Capabilities:          []pluginapi.Capability{pluginapi.CapabilityEvents},
				DefaultTimeoutSeconds: 60,
			}},
			Triggers: []pluginapi.TriggerSpec{{
				Name:          "issue",
				Description:   "on issue",
				PayloadSchema: json.RawMessage(`{"type":"object"}`),
			}},
		},
		Status:    plugins.StatusOK,
		CheckedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
}

func manifestJSON(t *testing.T, m pluginapi.Manifest) string {
	t.Helper()
	raw, err := json.Marshal(m)
	require.NoError(t, err)
	return string(raw)
}

func TestUpsertPlugin(t *testing.T) {
	t.Run("round-trips every field through get", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		want := newRecord("alpha")
		require.NoError(t, e.store.UpsertPlugin(ctx, want))

		got, err := e.store.GetPlugin(ctx, "alpha")
		require.NoError(t, err)
		assert.Equal(t, want.Name, got.Name)
		assert.Equal(t, want.BaseURL, got.BaseURL)
		assert.Equal(t, want.Status, got.Status)
		assert.Equal(t, want.Error, got.Error)
		assert.True(t, want.CheckedAt.Equal(got.CheckedAt))
		assert.JSONEq(t, manifestJSON(t, want.Manifest), manifestJSON(t, got.Manifest))
		assert.False(t, got.CreatedAt.IsZero())
		assert.False(t, got.UpdatedAt.IsZero())
	})

	t.Run("second upsert updates base url, status, error and manifest", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		first := newRecord("alpha")
		require.NoError(t, e.store.UpsertPlugin(ctx, first))
		created, err := e.store.GetPlugin(ctx, "alpha")
		require.NoError(t, err)

		second := newRecord("alpha")
		second.BaseURL = "http://other:9090"
		second.Status = plugins.StatusDegraded
		second.Error = "slow"
		second.Manifest.Version = "2.0.0"
		second.Manifest.Triggers = nil
		second.CheckedAt = first.CheckedAt.Add(time.Minute)
		require.NoError(t, e.store.UpsertPlugin(ctx, second))

		got, err := e.store.GetPlugin(ctx, "alpha")
		require.NoError(t, err)
		assert.Equal(t, "http://other:9090", got.BaseURL)
		assert.Equal(t, plugins.StatusDegraded, got.Status)
		assert.Equal(t, "slow", got.Error)
		assert.True(t, second.CheckedAt.Equal(got.CheckedAt))
		assert.JSONEq(t, manifestJSON(t, second.Manifest), manifestJSON(t, got.Manifest))
		assert.True(t, created.CreatedAt.Equal(got.CreatedAt))
		assert.True(t, got.UpdatedAt.After(created.UpdatedAt))

		list, err := e.store.ListPlugins(ctx)
		require.NoError(t, err)
		assert.Len(t, list, 1)
	})
}

func TestGetPlugin(t *testing.T) {
	t.Run("missing plugin returns ErrNotFound", func(t *testing.T) {
		e := newEnv(t)
		_, err := e.store.GetPlugin(context.Background(), "ghost")
		require.ErrorIs(t, err, plugins.ErrNotFound)
	})
}

func TestListPlugins(t *testing.T) {
	t.Run("empty store returns no records", func(t *testing.T) {
		e := newEnv(t)
		list, err := e.store.ListPlugins(context.Background())
		require.NoError(t, err)
		assert.Empty(t, list)
	})

	t.Run("records are ordered by name", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		require.NoError(t, e.store.UpsertPlugin(ctx, newRecord("charlie")))
		require.NoError(t, e.store.UpsertPlugin(ctx, newRecord("alpha")))
		require.NoError(t, e.store.UpsertPlugin(ctx, newRecord("bravo")))

		list, err := e.store.ListPlugins(ctx)
		require.NoError(t, err)
		require.Len(t, list, 3)
		assert.Equal(t, "alpha", list[0].Name)
		assert.Equal(t, "bravo", list[1].Name)
		assert.Equal(t, "charlie", list[2].Name)
	})
}

func TestDeletePlugin(t *testing.T) {
	t.Run("existing plugin is deleted then reports false", func(t *testing.T) {
		e := newEnv(t)
		ctx := context.Background()
		require.NoError(t, e.store.UpsertPlugin(ctx, newRecord("alpha")))

		deleted, err := e.store.DeletePlugin(ctx, "alpha")
		require.NoError(t, err)
		assert.True(t, deleted)

		deleted, err = e.store.DeletePlugin(ctx, "alpha")
		require.NoError(t, err)
		assert.False(t, deleted)

		_, err = e.store.GetPlugin(ctx, "alpha")
		require.ErrorIs(t, err, plugins.ErrNotFound)
	})
}
