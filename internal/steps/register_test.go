package steps

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/internal/step"
)

func TestRegisterAll(t *testing.T) {
	t.Run("registers every step with its queue and outcomes", func(t *testing.T) {
		r := step.NewRegistry()
		require.NoError(t, RegisterAll(r, Deps{}))

		def, ok := r.Lookup("claude.assess")
		require.True(t, ok)
		assert.Equal(t, step.QueueClaude, def.Queue)
		assert.Equal(t, []step.Outcome{OutcomeClear, OutcomeUnclear}, def.Outcomes)

		def, ok = r.Lookup("tracker.ask_clarification")
		require.True(t, ok)
		assert.Equal(t, step.QueueDefault, def.Queue)
		assert.Equal(t, []step.Outcome{OutcomeAnswered}, def.Outcomes)

		def, ok = r.Lookup("claude.implement")
		require.True(t, ok)
		assert.Equal(t, step.QueueClaude, def.Queue)
		assert.Equal(t, []step.Outcome{step.OutcomeOK, OutcomeNoChanges, OutcomeTestsFailed}, def.Outcomes)

		def, ok = r.Lookup("pr.describe")
		require.True(t, ok)
		assert.Equal(t, step.QueueClaude, def.Queue)
		assert.Equal(t, []step.Outcome{step.OutcomeOK}, def.Outcomes)

		def, ok = r.Lookup("ai.review")
		require.True(t, ok)
		assert.Equal(t, step.QueueClaude, def.Queue)
		assert.Equal(t, []step.Outcome{OutcomeApproved, OutcomeChangesRequested}, def.Outcomes)

		def, ok = r.Lookup("docs.readme")
		require.True(t, ok)
		assert.Equal(t, step.QueueClaude, def.Queue)
		assert.Equal(t, []step.Outcome{step.OutcomeOK}, def.Outcomes)

		def, ok = r.Lookup("tracker.labels")
		require.True(t, ok)
		assert.Equal(t, step.QueueDefault, def.Queue)
		assert.Equal(t, []step.Outcome{step.OutcomeOK}, def.Outcomes)

		def, ok = r.Lookup("human.approve")
		require.True(t, ok)
		assert.Equal(t, step.QueueDefault, def.Queue)
		assert.Equal(t, []step.Outcome{OutcomeApproved, OutcomeRejected}, def.Outcomes)
	})

	t.Run("registering twice fails", func(t *testing.T) {
		r := step.NewRegistry()
		require.NoError(t, RegisterAll(r, Deps{}))
		require.ErrorIs(t, RegisterAll(r, Deps{}), step.ErrDuplicateStep)
	})

	t.Run("implement accepts max_attempts within range", func(t *testing.T) {
		r := step.NewRegistry()
		require.NoError(t, RegisterAll(r, Deps{}))
		_, err := r.Build("claude.implement", json.RawMessage(`{"max_attempts": 5}`))
		require.NoError(t, err)
	})

	t.Run("implement accepts empty params", func(t *testing.T) {
		r := step.NewRegistry()
		require.NoError(t, RegisterAll(r, Deps{}))
		_, err := r.Build("claude.implement", nil)
		require.NoError(t, err)
	})

	t.Run("implement rejects max_attempts above 10", func(t *testing.T) {
		r := step.NewRegistry()
		require.NoError(t, RegisterAll(r, Deps{}))
		_, err := r.Build("claude.implement", json.RawMessage(`{"max_attempts": 11}`))
		require.ErrorIs(t, err, step.ErrInvalidParams)
	})

	t.Run("review rejects unknown provider", func(t *testing.T) {
		r := step.NewRegistry()
		require.NoError(t, RegisterAll(r, Deps{}))
		_, err := r.Build("ai.review", json.RawMessage(`{"provider": "gpt"}`))
		require.ErrorIs(t, err, step.ErrInvalidParams)
	})

	t.Run("review accepts claude provider", func(t *testing.T) {
		r := step.NewRegistry()
		require.NoError(t, RegisterAll(r, Deps{}))
		_, err := r.Build("ai.review", json.RawMessage(`{"provider": "claude"}`))
		require.NoError(t, err)
	})

	t.Run("labels rejects empty label name", func(t *testing.T) {
		r := step.NewRegistry()
		require.NoError(t, RegisterAll(r, Deps{}))
		_, err := r.Build("tracker.labels", json.RawMessage(`{"add": [""]}`))
		require.ErrorIs(t, err, step.ErrInvalidParams)
	})

	t.Run("params-less steps reject unknown fields", func(t *testing.T) {
		r := step.NewRegistry()
		require.NoError(t, RegisterAll(r, Deps{}))
		_, err := r.Build("human.approve", json.RawMessage(`{"x": 1}`))
		require.ErrorIs(t, err, step.ErrInvalidParams)
	})
}
