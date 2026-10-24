package pluginapi

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validAction() ActionSpec {
	return ActionSpec{
		Name:                  "run.task",
		Description:           "runs a task",
		Mode:                  ModeAsync,
		InputSchema:           json.RawMessage(`{"type":"object"}`),
		OutputSchema:          json.RawMessage(`{"type":"object"}`),
		Outcomes:              []Outcome{OutcomeOK, "failed"},
		Capabilities:          []Capability{CapabilityEvents},
		DefaultTimeoutSeconds: 30,
	}
}

func validTrigger() TriggerSpec {
	return TriggerSpec{
		Name:          "task.done",
		Description:   "task finished",
		PayloadSchema: json.RawMessage(`{"type":"object"}`),
	}
}

func validManifest() Manifest {
	return Manifest{
		Protocol:    ProtocolVersion,
		Name:        "my-plugin",
		Version:     "1.0.0",
		Description: "a plugin",
		Actions:     []ActionSpec{validAction()},
		Triggers:    []TriggerSpec{validTrigger()},
	}
}

func TestManifestValidate(t *testing.T) {
	t.Run("valid manifest returns nil", func(t *testing.T) {
		require.NoError(t, validManifest().Validate())
	})

	t.Run("protocol mismatch", func(t *testing.T) {
		m := validManifest()
		m.Protocol = "2"
		require.ErrorIs(t, m.Validate(), ErrUnsupportedProtocol)
	})

	t.Run("bad plugin name", func(t *testing.T) {
		m := validManifest()
		m.Name = "Bad_Name"
		require.ErrorIs(t, m.Validate(), ErrInvalidManifest)
	})

	t.Run("empty version", func(t *testing.T) {
		m := validManifest()
		m.Version = ""
		require.ErrorIs(t, m.Validate(), ErrInvalidManifest)
	})

	t.Run("empty description", func(t *testing.T) {
		m := validManifest()
		m.Description = ""
		require.ErrorIs(t, m.Validate(), ErrInvalidManifest)
	})

	t.Run("no actions", func(t *testing.T) {
		m := validManifest()
		m.Actions = nil
		require.ErrorIs(t, m.Validate(), ErrInvalidManifest)
	})

	t.Run("bad action name", func(t *testing.T) {
		m := validManifest()
		m.Actions[0].Name = "Bad-Name"
		require.ErrorIs(t, m.Validate(), ErrInvalidManifest)
	})

	t.Run("duplicate action name", func(t *testing.T) {
		m := validManifest()
		m.Actions = append(m.Actions, validAction())
		err := m.Validate()
		require.ErrorIs(t, err, ErrInvalidManifest)
		assert.Contains(t, err.Error(), "duplicate action")
	})

	t.Run("empty action description", func(t *testing.T) {
		m := validManifest()
		m.Actions[0].Description = ""
		require.ErrorIs(t, m.Validate(), ErrInvalidManifest)
	})

	t.Run("bad mode", func(t *testing.T) {
		m := validManifest()
		m.Actions[0].Mode = "weird"
		require.ErrorIs(t, m.Validate(), ErrInvalidManifest)
	})

	t.Run("no outcomes", func(t *testing.T) {
		m := validManifest()
		m.Actions[0].Outcomes = nil
		require.ErrorIs(t, m.Validate(), ErrInvalidManifest)
	})

	t.Run("bad outcome name", func(t *testing.T) {
		m := validManifest()
		m.Actions[0].Outcomes = []Outcome{"Bad"}
		require.ErrorIs(t, m.Validate(), ErrInvalidManifest)
	})

	t.Run("duplicate outcome", func(t *testing.T) {
		m := validManifest()
		m.Actions[0].Outcomes = []Outcome{OutcomeOK, OutcomeOK}
		require.ErrorIs(t, m.Validate(), ErrInvalidManifest)
	})

	t.Run("unknown capability", func(t *testing.T) {
		m := validManifest()
		m.Actions[0].Capabilities = []Capability{"teleport"}
		require.ErrorIs(t, m.Validate(), ErrInvalidManifest)
	})

	t.Run("duplicate capability", func(t *testing.T) {
		m := validManifest()
		m.Actions[0].Capabilities = []Capability{CapabilityInject, CapabilityInject}
		require.ErrorIs(t, m.Validate(), ErrInvalidManifest)
	})

	t.Run("zero timeout", func(t *testing.T) {
		m := validManifest()
		m.Actions[0].DefaultTimeoutSeconds = 0
		require.ErrorIs(t, m.Validate(), ErrInvalidManifest)
	})

	t.Run("bad input schema", func(t *testing.T) {
		m := validManifest()
		m.Actions[0].InputSchema = json.RawMessage(`{"type":5}`)
		err := m.Validate()
		require.ErrorIs(t, err, ErrInvalidManifest)
		require.ErrorIs(t, err, ErrInvalidSchema)
	})

	t.Run("missing output schema", func(t *testing.T) {
		m := validManifest()
		m.Actions[0].OutputSchema = nil
		require.ErrorIs(t, m.Validate(), ErrInvalidManifest)
	})

	t.Run("bad trigger name", func(t *testing.T) {
		m := validManifest()
		m.Triggers[0].Name = "Bad"
		require.ErrorIs(t, m.Validate(), ErrInvalidManifest)
	})

	t.Run("duplicate trigger name", func(t *testing.T) {
		m := validManifest()
		m.Triggers = append(m.Triggers, validTrigger())
		err := m.Validate()
		require.ErrorIs(t, err, ErrInvalidManifest)
		assert.Contains(t, err.Error(), "duplicate trigger")
	})

	t.Run("empty trigger description", func(t *testing.T) {
		m := validManifest()
		m.Triggers[0].Description = ""
		require.ErrorIs(t, m.Validate(), ErrInvalidManifest)
	})

	t.Run("bad trigger payload schema", func(t *testing.T) {
		m := validManifest()
		m.Triggers[0].PayloadSchema = json.RawMessage(`{`)
		require.ErrorIs(t, m.Validate(), ErrInvalidManifest)
	})

	t.Run("multiple problems are all reported", func(t *testing.T) {
		m := validManifest()
		m.Protocol = "2"
		m.Version = ""
		err := m.Validate()
		require.ErrorIs(t, err, ErrUnsupportedProtocol)
		require.ErrorIs(t, err, ErrInvalidManifest)
		assert.Contains(t, err.Error(), "version is empty")
	})
}

func TestManifestAction(t *testing.T) {
	m := validManifest()

	t.Run("found", func(t *testing.T) {
		a, ok := m.Action("run.task")
		require.True(t, ok)
		assert.Equal(t, "run.task", a.Name)
	})

	t.Run("missing", func(t *testing.T) {
		a, ok := m.Action("nope")
		assert.False(t, ok)
		assert.Equal(t, ActionSpec{}, a)
	})
}

func TestActionSpecDeclares(t *testing.T) {
	a := validAction()

	t.Run("declared outcome", func(t *testing.T) {
		assert.True(t, a.Declares(OutcomeOK))
	})

	t.Run("undeclared outcome", func(t *testing.T) {
		assert.False(t, a.Declares("other"))
	})
}

func TestActionSpecHas(t *testing.T) {
	a := validAction()

	t.Run("present capability", func(t *testing.T) {
		assert.True(t, a.Has(CapabilityEvents))
	})

	t.Run("absent capability", func(t *testing.T) {
		assert.False(t, a.Has(CapabilityInject))
	})
}
