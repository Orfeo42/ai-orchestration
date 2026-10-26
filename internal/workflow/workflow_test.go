package workflow

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/internal/step"
)

func TestDurationUnmarshalJSON(t *testing.T) {
	t.Run("valid string", func(t *testing.T) {
		var d Duration
		require.NoError(t, json.Unmarshal([]byte(`"90s"`), &d))
		assert.Equal(t, Duration(90*time.Second), d)
	})

	t.Run("non string rejected", func(t *testing.T) {
		var d Duration
		require.Error(t, json.Unmarshal([]byte(`30`), &d))
	})

	t.Run("bad duration rejected", func(t *testing.T) {
		var d Duration
		require.Error(t, json.Unmarshal([]byte(`"soon"`), &d))
	})
}

func TestNext(t *testing.T) {
	def := parseExample(t)

	t.Run("known outcome", func(t *testing.T) {
		got, err := def.Next("assess", "clear")
		require.NoError(t, err)
		assert.Equal(t, "implement", got)
	})

	t.Run("unknown step", func(t *testing.T) {
		_, err := def.Next("nope", "ok")
		require.ErrorIs(t, err, ErrUnknownStep)
	})

	t.Run("unhandled outcome", func(t *testing.T) {
		_, err := def.Next("assess", "ok")
		require.ErrorIs(t, err, ErrUnhandledResult)
	})
}

func TestParse(t *testing.T) {
	t.Run("example parses", func(t *testing.T) {
		def, err := Parse([]byte(exampleYAML))
		require.NoError(t, err)
		assert.Equal(t, "implement-issue", def.Name)
		assert.Equal(t, "assess", def.Start)
		require.NotNil(t, def.Trigger.Forgejo)
		assert.Equal(t, []string{"ionstream/handbook"}, def.Trigger.Forgejo.Repos)
		assert.Equal(t, "To-do", def.Trigger.Forgejo.Label)
		assert.Len(t, def.Steps, 7)
		assert.Equal(t, Duration(30*time.Minute), def.Steps["assess"].Timeout)
		assert.Equal(t, 2, def.Steps["assess"].Retry)
		assert.Equal(t, step.Outcome("implement"), step.Outcome(def.Steps["assess"].On["clear"]))
		assert.JSONEq(t, `{"max_attempts":3}`, string(def.Steps["implement"].With))
	})

	t.Run("unknown top level field rejected", func(t *testing.T) {
		_, err := Parse([]byte("name: x\nbogus: 1\n"))
		require.ErrorIs(t, err, ErrInvalid)
	})

	t.Run("unknown step field rejected", func(t *testing.T) {
		_, err := Parse([]byte("name: x\nsteps:\n  a: { uses: u, bogus: 1, on: {} }\n"))
		require.ErrorIs(t, err, ErrInvalid)
	})

	t.Run("bad duration rejected", func(t *testing.T) {
		_, err := Parse([]byte("name: x\nsteps:\n  a: { uses: u, timeout: soon, on: {} }\n"))
		require.ErrorIs(t, err, ErrInvalid)
	})

	t.Run("malformed yaml rejected", func(t *testing.T) {
		_, err := Parse([]byte("name: [unclosed\n"))
		require.ErrorIs(t, err, ErrInvalid)
	})
}

func TestValidate(t *testing.T) {
	reg := newTestCatalog(t)

	t.Run("example is valid", func(t *testing.T) {
		require.NoError(t, parseExample(t).Validate(reg))
	})

	t.Run("empty name", func(t *testing.T) {
		def := parseExample(t)
		def.Name = ""
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "name is required")
	})

	t.Run("empty start", func(t *testing.T) {
		def := parseExample(t)
		def.Start = ""
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "start")
	})

	t.Run("start not a step", func(t *testing.T) {
		def := parseExample(t)
		def.Start = "ghost"
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "ghost")
	})

	t.Run("zero trigger is valid", func(t *testing.T) {
		def := parseExample(t)
		def.Trigger = Trigger{}
		require.NoError(t, def.Validate(reg))
	})

	t.Run("no repos", func(t *testing.T) {
		def := parseExample(t)
		def.Trigger.Forgejo.Repos = nil
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "repos")
	})

	t.Run("repo without slash", func(t *testing.T) {
		def := parseExample(t)
		def.Trigger.Forgejo.Repos = []string{"handbook"}
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "handbook")
	})

	t.Run("repo with two slashes", func(t *testing.T) {
		def := parseExample(t)
		def.Trigger.Forgejo.Repos = []string{"a/b/c"}
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "a/b/c")
	})

	t.Run("repo with empty part", func(t *testing.T) {
		def := parseExample(t)
		def.Trigger.Forgejo.Repos = []string{"owner/"}
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "owner/")
	})

	t.Run("empty label", func(t *testing.T) {
		def := parseExample(t)
		def.Trigger.Forgejo.Label = ""
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "label")
	})

	t.Run("plugin trigger is valid", func(t *testing.T) {
		def := parseExample(t)
		def.Trigger = Trigger{Plugin: "git", Event: "push", Match: map[string]json.RawMessage{"branch": json.RawMessage(`"main"`)}}
		require.NoError(t, def.Validate(reg))
	})

	t.Run("forgejo with plugin", func(t *testing.T) {
		def := parseExample(t)
		def.Trigger.Plugin = "git"
		def.Trigger.Event = "push"
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "mutually exclusive")
	})

	t.Run("event without plugin", func(t *testing.T) {
		def := parseExample(t)
		def.Trigger = Trigger{Event: "push"}
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "trigger.event requires trigger.plugin")
	})

	t.Run("match without plugin", func(t *testing.T) {
		def := parseExample(t)
		def.Trigger = Trigger{Match: map[string]json.RawMessage{"a": json.RawMessage(`1`)}}
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "trigger.match requires trigger.plugin")
	})

	t.Run("plugin without event", func(t *testing.T) {
		def := parseExample(t)
		def.Trigger = Trigger{Plugin: "git"}
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "trigger.event is required")
	})

	t.Run("unknown plugin trigger", func(t *testing.T) {
		def := parseExample(t)
		def.Trigger = Trigger{Plugin: "git", Event: "tag"}
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "git/tag")
	})

	t.Run("empty match key", func(t *testing.T) {
		def := parseExample(t)
		def.Trigger = Trigger{Plugin: "git", Event: "push", Match: map[string]json.RawMessage{"": json.RawMessage(`1`)}}
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "match keys must not be empty")
	})

	t.Run("no steps", func(t *testing.T) {
		def := parseExample(t)
		def.Steps = nil
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "at least one step")
	})

	t.Run("reserved step id", func(t *testing.T) {
		def := parseExample(t)
		def.Steps[TargetEnd] = def.Steps["docs"]
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), `"end" is reserved`)
	})

	t.Run("unregistered uses", func(t *testing.T) {
		def := parseExample(t)
		s := def.Steps["docs"]
		s.Uses = "nope.nothing"
		def.Steps["docs"] = s
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "step docs")
		assert.Contains(t, err.Error(), "nope.nothing")
	})

	t.Run("invalid params value", func(t *testing.T) {
		def := parseExample(t)
		s := def.Steps["review"]
		s.With = json.RawMessage(`{"provider":"openai"}`)
		def.Steps["review"] = s
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		require.ErrorIs(t, err, step.ErrInvalidParams)
		assert.Contains(t, err.Error(), "step review")
	})

	t.Run("unknown params field", func(t *testing.T) {
		def := parseExample(t)
		s := def.Steps["implement"]
		s.With = json.RawMessage(`{"bogus":1}`)
		def.Steps["implement"] = s
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		require.ErrorIs(t, err, step.ErrInvalidParams)
		assert.Contains(t, err.Error(), "step implement")
	})

	t.Run("negative retry", func(t *testing.T) {
		def := parseExample(t)
		s := def.Steps["assess"]
		s.Retry = -1
		def.Steps["assess"] = s
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "step assess: retry")
	})

	t.Run("negative timeout", func(t *testing.T) {
		def := parseExample(t)
		s := def.Steps["assess"]
		s.Timeout = Duration(-time.Second)
		def.Steps["assess"] = s
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "step assess: timeout")
	})

	t.Run("declared outcome without on entry", func(t *testing.T) {
		def := parseExample(t)
		delete(def.Steps["assess"].On, "unclear")
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "step assess")
		assert.Contains(t, err.Error(), "unclear")
	})

	t.Run("on key not declared", func(t *testing.T) {
		def := parseExample(t)
		def.Steps["docs"].On["weird"] = TargetEnd
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "step docs")
		assert.Contains(t, err.Error(), "weird")
	})

	t.Run("on target unknown", func(t *testing.T) {
		def := parseExample(t)
		def.Steps["docs"].On["ok"] = "ghost"
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "step docs")
		assert.Contains(t, err.Error(), "ghost")
	})

	t.Run("unreachable step", func(t *testing.T) {
		def := parseExample(t)
		def.Steps["orphan"] = StepDef{Uses: "docs.readme", On: map[step.Outcome]string{"ok": TargetEnd}}
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "step orphan is unreachable")
	})

	t.Run("multiple violations all reported", func(t *testing.T) {
		def := parseExample(t)
		def.Name = ""
		def.Trigger.Forgejo.Label = ""
		def.Steps["docs"].On["ok"] = "ghost"
		s := def.Steps["assess"]
		s.Retry = -1
		def.Steps["assess"] = s
		err := def.Validate(reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "name is required")
		assert.Contains(t, err.Error(), "label is required")
		assert.Contains(t, err.Error(), "ghost")
		assert.Contains(t, err.Error(), "step assess: retry")
	})
}

func TestParseJSON(t *testing.T) {
	t.Run("valid definition", func(t *testing.T) {
		def, err := ParseJSON([]byte(`{"name":"x","start":"a","steps":{"a":{"uses":"u","timeout":"30m","on":{"ok":"end"}}}}`))
		require.NoError(t, err)
		assert.Equal(t, "x", def.Name)
		assert.Equal(t, Duration(30*time.Minute), def.Steps["a"].Timeout)
	})

	t.Run("unknown field rejected", func(t *testing.T) {
		_, err := ParseJSON([]byte(`{"name":"x","bogus":1}`))
		require.ErrorIs(t, err, ErrInvalid)
	})

	t.Run("malformed json rejected", func(t *testing.T) {
		_, err := ParseJSON([]byte(`{"name":`))
		require.ErrorIs(t, err, ErrInvalid)
	})
}

func TestDefinitionYAML(t *testing.T) {
	t.Run("example round trips", func(t *testing.T) {
		def := parseExample(t)

		out, err := def.YAML()
		require.NoError(t, err)
		back, err := Parse(out)

		require.NoError(t, err)
		assert.Equal(t, def, back)
	})

	t.Run("full definition round trips", func(t *testing.T) {
		def := Definition{
			Name:        "full",
			Description: "all fields",
			Trigger:     Trigger{Plugin: "git", Event: "push", Match: map[string]json.RawMessage{"branch": json.RawMessage(`"main"`), "n": json.RawMessage(`3`)}},
			Start:       "a",
			Steps: map[string]StepDef{
				"a": {
					Description: "first",
					Uses:        "docs.readme",
					With:        json.RawMessage(`{"k":"${vars.x}"}`),
					Timeout:     Duration(90 * time.Second),
					Retry:       2,
					On:          map[step.Outcome]string{"ok": TargetEnd},
				},
			},
		}

		out, err := def.YAML()
		require.NoError(t, err)
		back, err := Parse(out)

		require.NoError(t, err)
		assert.Equal(t, def, back)
		assert.Contains(t, string(out), "1m30s")
	})
}
