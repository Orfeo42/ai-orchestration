package plugins

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/internal/step"
	"github.com/Orfeo42/ai-orchestration/pluginapi"
)

func catTestCatalog(t *testing.T) *Catalog {
	t.Helper()
	m := catManifest("alpha", []pluginapi.ActionSpec{catSpec("act", pluginapi.ModeSync)}, catTrigger("issue.labeled"))
	catalog, _, _ := catFixture(t, catTokens, catRecord("alpha", "http://alpha", m))
	return catalog
}

func TestCatalogLookup(t *testing.T) {
	t.Run("plugin action returns def fields", func(t *testing.T) {
		def, ok := catTestCatalog(t).Lookup("alpha/act")
		require.True(t, ok)
		assert.Equal(t, "alpha/act", def.Name)
		assert.Equal(t, "alpha", def.Plugin)
		assert.Equal(t, StepQueue, def.Queue)
		assert.Equal(t, "test action", def.Description)
		assert.Equal(t, []step.Outcome{step.OutcomeOK, "other"}, def.Outcomes)
		assert.JSONEq(t, string(catSpec("act", pluginapi.ModeSync).InputSchema), string(def.InputSchema))
	})

	t.Run("unknown plugin action not found", func(t *testing.T) {
		_, ok := catTestCatalog(t).Lookup("alpha/missing")
		assert.False(t, ok)
	})

	t.Run("static step falls through to registry", func(t *testing.T) {
		reg := step.NewRegistry()
		require.NoError(t, step.Register(reg, step.Def{Name: "static", Queue: StepQueue, Outcomes: []step.Outcome{step.OutcomeOK}}, func(step.NoParams) step.Step { return stepNoop{} }))
		catalog := NewCatalog(reg, &MockStore{}, NewClients(catTokens, nil), NewSigner([]byte("k"), "http://core"))
		def, ok := catalog.Lookup("static")
		require.True(t, ok)
		assert.Equal(t, "static", def.Name)
		assert.Empty(t, def.Plugin)
	})
}

func TestCatalogBuild(t *testing.T) {
	t.Run("plugin action builds a step", func(t *testing.T) {
		s, err := catTestCatalog(t).Build("alpha/act", json.RawMessage(`{"n":1}`))
		require.NoError(t, err)
		assert.NotNil(t, s)
	})

	t.Run("unknown plugin action returns ErrUnknownStep", func(t *testing.T) {
		_, err := catTestCatalog(t).Build("alpha/missing", nil)
		require.ErrorIs(t, err, step.ErrUnknownStep)
	})

	t.Run("unknown static step returns ErrUnknownStep", func(t *testing.T) {
		_, err := catTestCatalog(t).Build("nope", nil)
		require.ErrorIs(t, err, step.ErrUnknownStep)
	})

	t.Run("static step falls through to registry", func(t *testing.T) {
		reg := step.NewRegistry()
		require.NoError(t, step.Register(reg, step.Def{Name: "static", Queue: StepQueue, Outcomes: []step.Outcome{step.OutcomeOK}}, func(step.NoParams) step.Step { return stepNoop{} }))
		catalog := NewCatalog(reg, &MockStore{}, NewClients(catTokens, nil), NewSigner([]byte("k"), "http://core"))
		s, err := catalog.Build("static", nil)
		require.NoError(t, err)
		assert.Equal(t, stepNoop{}, s)
	})
}

func TestCatalogDefs(t *testing.T) {
	t.Run("merges static and plugin defs sorted by name", func(t *testing.T) {
		reg := step.NewRegistry()
		require.NoError(t, step.Register(reg, step.Def{Name: "zeta", Queue: StepQueue, Outcomes: []step.Outcome{step.OutcomeOK}}, func(step.NoParams) step.Step { return stepNoop{} }))
		m := catManifest("alpha", []pluginapi.ActionSpec{catSpec("b", pluginapi.ModeSync), catSpec("a", pluginapi.ModeSync)})
		store := &MockStore{ListPluginsFunc: func(context.Context) ([]Record, error) {
			return []Record{catRecord("alpha", "http://alpha", m)}, nil
		}}
		catalog := NewCatalog(reg, store, NewClients(catTokens, nil), NewSigner([]byte("k"), "http://core"))
		require.NoError(t, catalog.Refresh(context.Background()))

		defs := catalog.Defs()
		names := make([]string, len(defs))
		for i, d := range defs {
			names[i] = d.Name
		}
		assert.Equal(t, []string{"alpha/a", "alpha/b", "zeta"}, names)
	})
}

func TestCatalogRefresh(t *testing.T) {
	t.Run("store error is returned", func(t *testing.T) {
		store := &MockStore{ListPluginsFunc: func(context.Context) ([]Record, error) { return nil, ErrNotFound }}
		catalog := NewCatalog(step.NewRegistry(), store, NewClients(catTokens, nil), NewSigner([]byte("k"), "http://core"))
		require.ErrorIs(t, catalog.Refresh(context.Background()), ErrNotFound)
	})
}

func TestCatalogHasTrigger(t *testing.T) {
	catalog := catTestCatalog(t)

	t.Run("declared trigger", func(t *testing.T) {
		assert.True(t, catalog.HasTrigger("alpha", "issue.labeled"))
	})

	t.Run("unknown trigger", func(t *testing.T) {
		assert.False(t, catalog.HasTrigger("alpha", "other"))
	})

	t.Run("unknown plugin", func(t *testing.T) {
		assert.False(t, catalog.HasTrigger("beta", "issue.labeled"))
	})
}

func TestCatalogTriggers(t *testing.T) {
	t.Run("lists triggers sorted", func(t *testing.T) {
		ma := catManifest("beta", []pluginapi.ActionSpec{catSpec("act", pluginapi.ModeSync)}, catTrigger("z"), catTrigger("a"))
		mb := catManifest("alpha", []pluginapi.ActionSpec{catSpec("act", pluginapi.ModeSync)}, catTrigger("m"))
		catalog, _, _ := catFixture(t, catTokens, catRecord("beta", "http://b", ma), catRecord("alpha", "http://a", mb))

		defs := catalog.Triggers()
		require.Len(t, defs, 3)
		assert.Equal(t, "alpha", defs[0].Plugin)
		assert.Equal(t, "m", defs[0].Name)
		assert.Equal(t, "beta", defs[1].Plugin)
		assert.Equal(t, "a", defs[1].Name)
		assert.Equal(t, "z", defs[2].Name)
		assert.Equal(t, "test trigger", defs[0].Description)
		assert.NotEmpty(t, defs[0].PayloadSchema)
	})
}

func TestCatalogValidateTrigger(t *testing.T) {
	catalog := catTestCatalog(t)

	t.Run("valid payload", func(t *testing.T) {
		err := catalog.ValidateTrigger(pluginapi.TriggerEvent{Plugin: "alpha", Trigger: "issue.labeled", Payload: json.RawMessage(`{"id":1}`)})
		require.NoError(t, err)
	})

	t.Run("unknown plugin", func(t *testing.T) {
		err := catalog.ValidateTrigger(pluginapi.TriggerEvent{Plugin: "beta", Trigger: "issue.labeled"})
		require.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("unknown trigger", func(t *testing.T) {
		err := catalog.ValidateTrigger(pluginapi.TriggerEvent{Plugin: "alpha", Trigger: "nope"})
		require.ErrorIs(t, err, ErrUnknownTrigger)
	})

	t.Run("payload violating schema", func(t *testing.T) {
		err := catalog.ValidateTrigger(pluginapi.TriggerEvent{Plugin: "alpha", Trigger: "issue.labeled", Payload: json.RawMessage(`{"id":"x"}`)})
		require.ErrorIs(t, err, ErrInvalidPayload)
	})

	t.Run("empty payload treated as empty object", func(t *testing.T) {
		err := catalog.ValidateTrigger(pluginapi.TriggerEvent{Plugin: "alpha", Trigger: "issue.labeled"})
		require.ErrorIs(t, err, ErrInvalidPayload)
	})
}
