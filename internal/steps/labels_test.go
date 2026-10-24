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

func labelIDs() *forgejo.MockForgejo {
	return &forgejo.MockForgejo{
		GetLabelIDFunc: func(owner, repo, name string) (*int, error) {
			ids := map[string]int{"done": 1, "in-progress": 2}
			id, ok := ids[name]
			if !ok {
				return nil, nil
			}
			return &id, nil
		},
		AddLabelFunc:    func(string, string, int, int) error { return nil },
		RemoveLabelFunc: func(string, string, int, int) error { return nil },
	}
}

func TestLabelsStep(t *testing.T) {
	t.Run("adds and removes labels on the issue repo", func(t *testing.T) {
		fj := labelIDs()
		s := labelsStep{d: Deps{Forgejo: fj}, params: labelsParams{Add: []string{"done"}, Remove: []string{"in-progress"}}}
		res, err := s.Run(context.Background(), newScope(t, baseVars(t, "api")))
		require.NoError(t, err)
		assert.Equal(t, step.OutcomeOK, res.Outcome)
		require.Len(t, fj.AddLabelCalls(), 1)
		assert.Equal(t, "acme", fj.AddLabelCalls()[0].Owner)
		assert.Equal(t, "tracker", fj.AddLabelCalls()[0].Repo)
		assert.Equal(t, 7, fj.AddLabelCalls()[0].IssueNumber)
		assert.Equal(t, 1, fj.AddLabelCalls()[0].LabelID)
		require.Len(t, fj.RemoveLabelCalls(), 1)
		assert.Equal(t, 2, fj.RemoveLabelCalls()[0].LabelID)
	})

	t.Run("missing label is skipped", func(t *testing.T) {
		fj := labelIDs()
		s := labelsStep{d: Deps{Forgejo: fj}, params: labelsParams{Add: []string{"ghost"}, Remove: []string{"ghost"}}}
		res, err := s.Run(context.Background(), newScope(t, baseVars(t, "api")))
		require.NoError(t, err)
		assert.Equal(t, step.OutcomeOK, res.Outcome)
		assert.Empty(t, fj.AddLabelCalls())
		assert.Empty(t, fj.RemoveLabelCalls())
	})

	t.Run("label lookup and mutation failures are tolerated", func(t *testing.T) {
		fj := labelIDs()
		fj.AddLabelFunc = func(string, string, int, int) error { return errors.New("boom") }
		fj.RemoveLabelFunc = func(string, string, int, int) error { return errors.New("boom") }
		fj.GetLabelIDFunc = func(_, _, name string) (*int, error) {
			if name == "broken" {
				return nil, errors.New("boom")
			}
			id := 3
			return &id, nil
		}
		s := labelsStep{d: Deps{Forgejo: fj}, params: labelsParams{Add: []string{"broken", "done"}, Remove: []string{"in-progress"}}}
		res, err := s.Run(context.Background(), newScope(t, baseVars(t, "api")))
		require.NoError(t, err)
		assert.Equal(t, step.OutcomeOK, res.Outcome)
	})

	t.Run("no params is a no-op", func(t *testing.T) {
		fj := labelIDs()
		res, err := labelsStep{d: Deps{Forgejo: fj}}.Run(context.Background(), newScope(t, baseVars(t, "api")))
		require.NoError(t, err)
		assert.Equal(t, step.OutcomeOK, res.Outcome)
		assert.Empty(t, fj.GetLabelIDCalls())
	})

	t.Run("missing issue var errors", func(t *testing.T) {
		_, err := labelsStep{d: Deps{Forgejo: labelIDs()}}.Run(context.Background(), newScope(t, process.Vars{}))
		require.ErrorIs(t, err, ErrMissingVar)
	})
}
