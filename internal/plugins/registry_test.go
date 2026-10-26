package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/internal/step"
	"github.com/Orfeo42/ai-orchestration/internal/workflow"
	"github.com/Orfeo42/ai-orchestration/pluginapi"
)

type registryFixture struct {
	registry  *Registry
	store     *MockStore
	workflows *MockWorkflowSource
	upserted  []Record
	stored    map[string]Record
}

func registryNew(t *testing.T, tokens TokenSource, existing ...Record) *registryFixture {
	t.Helper()
	f := &registryFixture{stored: map[string]Record{}, workflows: &MockWorkflowSource{}}
	for _, r := range existing {
		f.stored[r.Name] = r
	}
	f.store = &MockStore{
		UpsertPluginFunc: func(_ context.Context, r Record) error {
			f.upserted = append(f.upserted, r)
			f.stored[r.Name] = r
			return nil
		},
		GetPluginFunc: func(_ context.Context, name string) (Record, error) {
			r, ok := f.stored[name]
			if !ok {
				return Record{}, ErrNotFound
			}
			return r, nil
		},
		ListPluginsFunc: func(context.Context) ([]Record, error) {
			records := make([]Record, 0, len(f.stored))
			for _, r := range f.stored {
				records = append(records, r)
			}
			return records, nil
		},
		DeletePluginFunc: func(_ context.Context, name string) (bool, error) {
			_, ok := f.stored[name]
			delete(f.stored, name)
			return ok, nil
		},
	}
	clients := NewClients(tokens, nil)
	catalog := NewCatalog(step.NewRegistry(), f.store, clients, NewSigner([]byte("k"), "http://core"))
	clients.Bind(catalog)
	f.registry = NewRegistry(f.store, clients, catalog, f.workflows)
	return f
}

func registryWorkflow(t *testing.T, def workflow.Definition) process.WorkflowVersion {
	t.Helper()
	raw, err := json.Marshal(def)
	require.NoError(t, err)
	return process.WorkflowVersion{Name: def.Name, Definition: raw}
}

func registryServe(t *testing.T) (string, pluginapi.Manifest) {
	t.Helper()
	ts, m := catServe(t, "alpha", catSyncAction("act", stepOKOutput(), nil))
	return ts.URL, m
}

func TestRegistryList(t *testing.T) {
	t.Run("returns stored plugins", func(t *testing.T) {
		f := registryNew(t, catTokens, Record{Name: "alpha"})
		records, err := f.registry.List(context.Background())
		require.NoError(t, err)
		require.Len(t, records, 1)
		assert.Equal(t, "alpha", records[0].Name)
	})

	t.Run("store error is returned", func(t *testing.T) {
		f := registryNew(t, catTokens)
		boom := errors.New("boom")
		f.store.ListPluginsFunc = func(context.Context) ([]Record, error) { return nil, boom }
		_, err := f.registry.List(context.Background())
		require.ErrorIs(t, err, boom)
	})
}

func TestRegistryRegister(t *testing.T) {
	t.Run("registers a healthy plugin", func(t *testing.T) {
		url, m := registryServe(t)
		f := registryNew(t, catTokens)
		rec, err := f.registry.Register(context.Background(), "alpha", url)
		require.NoError(t, err)
		assert.Equal(t, "alpha", rec.Name)
		assert.Equal(t, url, rec.BaseURL)
		assert.Equal(t, StatusOK, rec.Status)
		assert.Empty(t, rec.Error)
		assert.False(t, rec.CheckedAt.IsZero())
		assert.Equal(t, m.Name, rec.Manifest.Name)
		require.Len(t, f.upserted, 1)
	})

	t.Run("refreshes the catalog", func(t *testing.T) {
		url, _ := registryServe(t)
		f := registryNew(t, catTokens)
		_, err := f.registry.Register(context.Background(), "alpha", url)
		require.NoError(t, err)
		_, ok := f.registry.catalog.Plugin("alpha")
		assert.True(t, ok)
	})

	t.Run("name mismatch", func(t *testing.T) {
		url, _ := registryServe(t)
		f := registryNew(t, catTokens)
		_, err := f.registry.Register(context.Background(), "other", url)
		require.ErrorIs(t, err, ErrNameMismatch)
		assert.Empty(t, f.upserted)
	})

	t.Run("invalid manifest", func(t *testing.T) {
		ts := catStub(t, 200, `{"protocol":"1","name":"alpha"}`)
		f := registryNew(t, catTokens)
		_, err := f.registry.Register(context.Background(), "alpha", ts.URL)
		require.ErrorIs(t, err, pluginapi.ErrInvalidManifest)
		assert.Empty(t, f.upserted)
	})

	t.Run("unreachable plugin", func(t *testing.T) {
		f := registryNew(t, catTokens)
		_, err := f.registry.Register(context.Background(), "alpha", catClosedURL(t))
		require.ErrorIs(t, err, ErrUnreachable)
		assert.Empty(t, f.upserted)
	})

	t.Run("missing token", func(t *testing.T) {
		url, _ := registryServe(t)
		f := registryNew(t, catNoTokens)
		_, err := f.registry.Register(context.Background(), "alpha", url)
		require.ErrorIs(t, err, ErrNoToken)
	})

	t.Run("store error is returned", func(t *testing.T) {
		url, _ := registryServe(t)
		f := registryNew(t, catTokens)
		boom := errors.New("boom")
		f.store.UpsertPluginFunc = func(context.Context, Record) error { return boom }
		_, err := f.registry.Register(context.Background(), "alpha", url)
		require.ErrorIs(t, err, boom)
	})
}

func TestRegistryRefresh(t *testing.T) {
	t.Run("success replaces manifest and status", func(t *testing.T) {
		url, m := registryServe(t)
		old := catRecord("alpha", url, catManifest("alpha", []pluginapi.ActionSpec{catSpec("old", pluginapi.ModeSync)}))
		old.Status = StatusUnreachable
		old.Error = "earlier failure"
		f := registryNew(t, catTokens, old)

		rec, err := f.registry.Refresh(context.Background(), "alpha")
		require.NoError(t, err)
		assert.Equal(t, StatusOK, rec.Status)
		assert.Empty(t, rec.Error)
		assert.Equal(t, m.Actions[0].Name, rec.Manifest.Actions[0].Name)
		assert.False(t, rec.CheckedAt.IsZero())
	})

	t.Run("failure keeps the old manifest and marks unreachable", func(t *testing.T) {
		oldManifest := catManifest("alpha", []pluginapi.ActionSpec{catSpec("old", pluginapi.ModeSync)})
		old := catRecord("alpha", catClosedURL(t), oldManifest)
		f := registryNew(t, catTokens, old)

		rec, err := f.registry.Refresh(context.Background(), "alpha")
		require.NoError(t, err)
		assert.Equal(t, StatusUnreachable, rec.Status)
		assert.NotEmpty(t, rec.Error)
		assert.Equal(t, "old", rec.Manifest.Actions[0].Name)
	})

	t.Run("name mismatch keeps the old manifest", func(t *testing.T) {
		url, _ := registryServe(t)
		oldManifest := catManifest("beta", []pluginapi.ActionSpec{catSpec("old", pluginapi.ModeSync)})
		f := registryNew(t, catTokens, catRecord("beta", url, oldManifest))

		rec, err := f.registry.Refresh(context.Background(), "beta")
		require.NoError(t, err)
		assert.Equal(t, StatusUnreachable, rec.Status)
		assert.Equal(t, "old", rec.Manifest.Actions[0].Name)
	})

	t.Run("unknown plugin returns ErrNotFound", func(t *testing.T) {
		f := registryNew(t, catTokens)
		_, err := f.registry.Refresh(context.Background(), "alpha")
		require.ErrorIs(t, err, ErrNotFound)
	})
}

func TestRegistryRefreshAll(t *testing.T) {
	t.Run("refreshes every plugin", func(t *testing.T) {
		url, _ := registryServe(t)
		f := registryNew(t, catTokens,
			catRecord("alpha", url, catManifest("alpha", []pluginapi.ActionSpec{catSpec("old", pluginapi.ModeSync)})),
			catRecord("beta", catClosedURL(t), catManifest("beta", []pluginapi.ActionSpec{catSpec("old", pluginapi.ModeSync)})),
		)
		require.NoError(t, f.registry.RefreshAll(context.Background()))
		assert.Len(t, f.upserted, 2)
		assert.Equal(t, StatusOK, f.stored["alpha"].Status)
		assert.Equal(t, StatusUnreachable, f.stored["beta"].Status)
	})

	t.Run("joins store errors", func(t *testing.T) {
		f := registryNew(t, catTokens, Record{Name: "alpha"}, Record{Name: "beta"})
		boom := errors.New("boom")
		f.store.UpsertPluginFunc = func(context.Context, Record) error { return boom }
		err := f.registry.RefreshAll(context.Background())
		require.ErrorIs(t, err, boom)
		assert.Len(t, f.store.UpsertPluginCalls(), 2)
	})

	t.Run("list error is returned", func(t *testing.T) {
		f := registryNew(t, catTokens)
		boom := errors.New("boom")
		f.store.ListPluginsFunc = func(context.Context) ([]Record, error) { return nil, boom }
		require.ErrorIs(t, f.registry.RefreshAll(context.Background()), boom)
	})
}

func TestRegistryRemove(t *testing.T) {
	t.Run("in use by a step", func(t *testing.T) {
		f := registryNew(t, catTokens, Record{Name: "alpha"})
		f.workflows.LatestWorkflowVersionsFunc = func(context.Context) ([]process.WorkflowVersion, error) {
			return []process.WorkflowVersion{registryWorkflow(t, workflow.Definition{
				Name:  "wf",
				Start: "a",
				Steps: map[string]workflow.StepDef{"a": {Uses: "alpha/act"}},
			})}, nil
		}
		err := f.registry.Remove(context.Background(), "alpha")
		require.ErrorIs(t, err, ErrInUse)
		assert.ErrorContains(t, err, "wf")
		assert.Empty(t, f.store.DeletePluginCalls())
	})

	t.Run("in use by a trigger", func(t *testing.T) {
		f := registryNew(t, catTokens, Record{Name: "alpha"})
		f.workflows.LatestWorkflowVersionsFunc = func(context.Context) ([]process.WorkflowVersion, error) {
			return []process.WorkflowVersion{registryWorkflow(t, workflow.Definition{
				Name:    "wf",
				Trigger: workflow.Trigger{Plugin: "alpha", Event: "issue.labeled"},
				Start:   "a",
				Steps:   map[string]workflow.StepDef{"a": {Uses: "other/act"}},
			})}, nil
		}
		err := f.registry.Remove(context.Background(), "alpha")
		require.ErrorIs(t, err, ErrInUse)
		assert.Empty(t, f.store.DeletePluginCalls())
	})

	t.Run("plugin with a longer name sharing the prefix is not in use", func(t *testing.T) {
		f := registryNew(t, catTokens, Record{Name: "alpha"})
		f.workflows.LatestWorkflowVersionsFunc = func(context.Context) ([]process.WorkflowVersion, error) {
			return []process.WorkflowVersion{registryWorkflow(t, workflow.Definition{
				Name:  "wf",
				Start: "a",
				Steps: map[string]workflow.StepDef{"a": {Uses: "alpha-two/act"}},
			})}, nil
		}
		require.NoError(t, f.registry.Remove(context.Background(), "alpha"))
	})

	t.Run("not found", func(t *testing.T) {
		f := registryNew(t, catTokens)
		f.workflows.LatestWorkflowVersionsFunc = func(context.Context) ([]process.WorkflowVersion, error) { return nil, nil }
		require.ErrorIs(t, f.registry.Remove(context.Background(), "alpha"), ErrNotFound)
	})

	t.Run("removes an unused plugin and refreshes the catalog", func(t *testing.T) {
		f := registryNew(t, catTokens, Record{Name: "alpha"})
		f.workflows.LatestWorkflowVersionsFunc = func(context.Context) ([]process.WorkflowVersion, error) {
			return []process.WorkflowVersion{registryWorkflow(t, workflow.Definition{
				Name:  "wf",
				Start: "a",
				Steps: map[string]workflow.StepDef{"a": {Uses: "other/act"}},
			})}, nil
		}
		require.NoError(t, f.registry.Remove(context.Background(), "alpha"))
		assert.Empty(t, f.stored)
		assert.NotEmpty(t, f.store.ListPluginsCalls())
	})

	t.Run("workflow source error is returned", func(t *testing.T) {
		f := registryNew(t, catTokens)
		boom := errors.New("boom")
		f.workflows.LatestWorkflowVersionsFunc = func(context.Context) ([]process.WorkflowVersion, error) { return nil, boom }
		require.ErrorIs(t, f.registry.Remove(context.Background(), "alpha"), boom)
	})
}
