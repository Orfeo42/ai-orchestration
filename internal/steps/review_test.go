package steps

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ai-orchestration/internal/events"
	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/gemini"
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/process"
	"ai-orchestration/internal/step"
)

func reviewerReturning(res models.ReviewResult, err error) *gemini.MockReviewer {
	return &gemini.MockReviewer{
		ReviewFunc: func(context.Context, models.ReviewJob, events.Sink) (models.ReviewResult, error) {
			return res, err
		},
	}
}

func reviewDeps(fj *forgejo.MockForgejo, reviewers map[string]gemini.Reviewer) Deps {
	return Deps{
		Forgejo:    fj,
		Reviewers:  reviewers,
		GitFactory: factoryFor(okGit()),
		Projects:   testProjects(),
	}
}

func reviewForgejo() *forgejo.MockForgejo {
	return &forgejo.MockForgejo{
		GetPRDiffFunc:      func(string, string, int) (string, error) { return "diff", nil },
		CreatePRReviewFunc: func(string, string, int, string, bool) error { return nil },
	}
}

func TestReviewStep(t *testing.T) {
	t.Run("approved review returns approved and stores result", func(t *testing.T) {
		fj := reviewForgejo()
		rv := reviewerReturning(models.ReviewResult{Summary: "good", Approved: true}, nil)
		s := reviewStep{d: reviewDeps(fj, map[string]gemini.Reviewer{"gemini": rv})}
		res, err := s.Run(context.Background(), newScope(t, prVars(t)))
		require.NoError(t, err)
		assert.Equal(t, OutcomeApproved, res.Outcome)
		stored, ok, err := process.Get(res.Vars, KeyReview)
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, "good", stored.Summary)
		require.Len(t, fj.CreatePRReviewCalls(), 1)
		assert.True(t, fj.CreatePRReviewCalls()[0].Approved)
	})

	t.Run("builds review job from pr and issue", func(t *testing.T) {
		fj := reviewForgejo()
		rv := reviewerReturning(models.ReviewResult{}, nil)
		s := reviewStep{d: reviewDeps(fj, map[string]gemini.Reviewer{"gemini": rv})}
		_, err := s.Run(context.Background(), newScope(t, prVars(t)))
		require.NoError(t, err)
		job := rv.ReviewCalls()[0].Job
		assert.Equal(t, models.ReviewJob{
			RepoOwner: "acme", RepoName: "api", PRNumber: 11, PRTitle: "feat: Add search",
			PRBranch: "feature/7-add-search", Project: "api", IssueNumber: 7, Diff: "diff",
		}, job)
	})

	t.Run("rejected review returns changes_requested", func(t *testing.T) {
		rv := reviewerReturning(models.ReviewResult{Approved: false}, nil)
		s := reviewStep{d: reviewDeps(reviewForgejo(), map[string]gemini.Reviewer{"gemini": rv})}
		res, err := s.Run(context.Background(), newScope(t, prVars(t)))
		require.NoError(t, err)
		assert.Equal(t, OutcomeChangesRequested, res.Outcome)
	})

	t.Run("provider param selects reviewer", func(t *testing.T) {
		gem := reviewerReturning(models.ReviewResult{Approved: false}, nil)
		cl := reviewerReturning(models.ReviewResult{Approved: true}, nil)
		d := reviewDeps(reviewForgejo(), map[string]gemini.Reviewer{"gemini": gem, "claude": cl})
		res, err := reviewStep{d: d, params: reviewParams{Provider: "claude"}}.Run(context.Background(), newScope(t, prVars(t)))
		require.NoError(t, err)
		assert.Equal(t, OutcomeApproved, res.Outcome)
		assert.Empty(t, gem.ReviewCalls())
	})

	t.Run("missing provider errors", func(t *testing.T) {
		d := reviewDeps(reviewForgejo(), map[string]gemini.Reviewer{})
		_, err := reviewStep{d: d}.Run(context.Background(), newScope(t, prVars(t)))
		require.ErrorIs(t, err, ErrNoReviewer)
	})

	t.Run("reviewer failure is classified upstream", func(t *testing.T) {
		rv := reviewerReturning(models.ReviewResult{}, errors.New("boom"))
		s := reviewStep{d: reviewDeps(reviewForgejo(), map[string]gemini.Reviewer{"gemini": rv})}
		_, err := s.Run(context.Background(), newScope(t, prVars(t)))
		require.ErrorIs(t, err, step.ErrUpstream)
	})

	t.Run("diff failure is classified upstream", func(t *testing.T) {
		fj := reviewForgejo()
		fj.GetPRDiffFunc = func(string, string, int) (string, error) { return "", errors.New("boom") }
		rv := reviewerReturning(models.ReviewResult{}, nil)
		s := reviewStep{d: reviewDeps(fj, map[string]gemini.Reviewer{"gemini": rv})}
		_, err := s.Run(context.Background(), newScope(t, prVars(t)))
		require.ErrorIs(t, err, step.ErrUpstream)
	})

	t.Run("post review failure is classified upstream", func(t *testing.T) {
		fj := reviewForgejo()
		fj.CreatePRReviewFunc = func(string, string, int, string, bool) error { return errors.New("boom") }
		rv := reviewerReturning(models.ReviewResult{}, nil)
		s := reviewStep{d: reviewDeps(fj, map[string]gemini.Reviewer{"gemini": rv})}
		_, err := s.Run(context.Background(), newScope(t, prVars(t)))
		require.ErrorIs(t, err, step.ErrUpstream)
	})

	t.Run("missing pr var errors", func(t *testing.T) {
		rv := reviewerReturning(models.ReviewResult{}, nil)
		s := reviewStep{d: reviewDeps(reviewForgejo(), map[string]gemini.Reviewer{"gemini": rv})}
		_, err := s.Run(context.Background(), newScope(t, baseVars(t, "api")))
		require.ErrorIs(t, err, ErrMissingVar)
	})
}
