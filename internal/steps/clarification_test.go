package steps

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/internal/forgejo"
	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/internal/step"
)

func clarificationVars(t *testing.T) process.Vars {
	t.Helper()
	vars := baseVars(t, "api")
	setVar(t, vars, KeyQuestions, "which db?")
	return vars
}

func TestClarificationStep(t *testing.T) {
	t.Run("posts comment, labels issue and waits for comment", func(t *testing.T) {
		var body string
		var labelled int
		fj := &forgejo.MockForgejo{
			CreateCommentFunc: func(owner, repo string, n int, b string) error {
				body = b
				return nil
			},
			GetLabelIDFunc: func(owner, repo, name string) (*int, error) {
				assert.Equal(t, forgejo.LabelNeedsInfo, name)
				id := 9
				return &id, nil
			},
			AddLabelFunc: func(owner, repo string, n, id int) error {
				labelled = id
				return nil
			},
		}
		res, err := clarificationStep{d: Deps{Forgejo: fj}}.Run(context.Background(), newScope(t, clarificationVars(t)))
		require.NoError(t, err)
		assert.Equal(t, "Requirements unclear, need clarification before implementation:\n\nwhich db?", body)
		assert.Equal(t, 9, labelled)
		assert.Empty(t, res.Outcome)
		require.NotNil(t, res.Wait)
		assert.Equal(t, process.WaitComment, res.Wait.Kind)
		assert.Equal(t, OutcomeAnswered, res.Wait.ResumeOutcome)
	})

	t.Run("missing label is skipped", func(t *testing.T) {
		fj := &forgejo.MockForgejo{
			CreateCommentFunc: func(string, string, int, string) error { return nil },
			GetLabelIDFunc:    func(string, string, string) (*int, error) { return nil, nil },
		}
		res, err := clarificationStep{d: Deps{Forgejo: fj}}.Run(context.Background(), newScope(t, clarificationVars(t)))
		require.NoError(t, err)
		assert.NotNil(t, res.Wait)
		assert.Empty(t, fj.AddLabelCalls())
	})

	t.Run("add label failure does not fail the step", func(t *testing.T) {
		fj := &forgejo.MockForgejo{
			CreateCommentFunc: func(string, string, int, string) error { return nil },
			GetLabelIDFunc: func(string, string, string) (*int, error) {
				id := 1
				return &id, nil
			},
			AddLabelFunc: func(string, string, int, int) error { return errors.New("boom") },
		}
		_, err := clarificationStep{d: Deps{Forgejo: fj}}.Run(context.Background(), newScope(t, clarificationVars(t)))
		require.NoError(t, err)
	})

	t.Run("comment failure is classified upstream", func(t *testing.T) {
		fj := &forgejo.MockForgejo{
			CreateCommentFunc: func(string, string, int, string) error { return errors.New("boom") },
		}
		_, err := clarificationStep{d: Deps{Forgejo: fj}}.Run(context.Background(), newScope(t, clarificationVars(t)))
		require.ErrorIs(t, err, step.ErrUpstream)
	})

	t.Run("missing questions var errors", func(t *testing.T) {
		_, err := clarificationStep{d: Deps{Forgejo: &forgejo.MockForgejo{}}}.Run(context.Background(), newScope(t, baseVars(t, "api")))
		require.ErrorIs(t, err, ErrMissingVar)
	})

	t.Run("missing issue var errors", func(t *testing.T) {
		_, err := clarificationStep{d: Deps{Forgejo: &forgejo.MockForgejo{}}}.Run(context.Background(), newScope(t, process.Vars{}))
		require.ErrorIs(t, err, ErrMissingVar)
	})
}
