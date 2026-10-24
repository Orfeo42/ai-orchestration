package impl

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/internal/agent"
	"github.com/Orfeo42/ai-orchestration/internal/events"
	"github.com/Orfeo42/ai-orchestration/internal/models"
	"github.com/Orfeo42/ai-orchestration/internal/step"
)

func TestImplementAttempt(t *testing.T) {
	t.Run("success stores output", func(t *testing.T) {
		ag := &agent.MockAgent{
			ImplementIssueFunc: func(context.Context, models.ForgejoIssue, []models.IssueComment, string, string, *string, events.Sink) (agent.ImplementResult, error) {
				return agent.ImplementResult{Output: "done"}, nil
			},
		}
		s := newState(okGit())
		require.NoError(t, ImplementAttempt{Claude: ag}.Run(context.Background(), s))
		assert.Equal(t, "done", s.ImplOutput)
	})

	t.Run("claude error without notes is upstream", func(t *testing.T) {
		ag := &agent.MockAgent{
			ImplementIssueFunc: func(context.Context, models.ForgejoIssue, []models.IssueComment, string, string, *string, events.Sink) (agent.ImplementResult, error) {
				return agent.ImplementResult{}, errors.New("boom")
			},
		}
		s := newState(okGit())
		err := ImplementAttempt{Claude: ag}.Run(context.Background(), s)
		require.ErrorIs(t, err, step.ErrUpstream)
	})

	t.Run("cancelled run context returns ctx error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		ag := &agent.MockAgent{
			ImplementIssueFunc: func(context.Context, models.ForgejoIssue, []models.IssueComment, string, string, *string, events.Sink) (agent.ImplementResult, error) {
				cancel()
				return agent.ImplementResult{}, errors.New("killed")
			},
		}
		s := newState(okGit())
		err := ImplementAttempt{Claude: ag}.Run(ctx, s)
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("injected note re-attempts with note kept", func(t *testing.T) {
		s := newState(okGit())
		s.Attempt = 2
		ag := &agent.MockAgent{
			ImplementIssueFunc: func(context.Context, models.ForgejoIssue, []models.IssueComment, string, string, *string, events.Sink) (agent.ImplementResult, error) {
				s.Handle.AppendNote("n")
				return agent.ImplementResult{}, errors.New("interrupted")
			},
		}
		require.NoError(t, ImplementAttempt{Claude: ag}.Run(context.Background(), s))
		assert.Equal(t, 1, s.Attempt)
		assert.Equal(t, []string{"n"}, s.Handle.PendingNotes())
	})

	t.Run("pending notes are passed to claude", func(t *testing.T) {
		s := newState(okGit())
		s.Handle.AppendNote("use sqlite")
		var got *string
		ag := &agent.MockAgent{
			ImplementIssueFunc: func(_ context.Context, _ models.ForgejoIssue, _ []models.IssueComment, _, _ string, testError *string, _ events.Sink) (agent.ImplementResult, error) {
				got = testError
				return agent.ImplementResult{}, nil
			},
		}
		require.NoError(t, ImplementAttempt{Claude: ag}.Run(context.Background(), s))
		require.NotNil(t, got)
		assert.Contains(t, *got, "use sqlite")
	})
}
