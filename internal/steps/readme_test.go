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
	"ai-orchestration/internal/gitops"
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/step"
)

func readmeDeps(g gitops.Git, w *gemini.MockDocsWriter) Deps {
	return Deps{
		Forgejo:    &forgejo.MockForgejo{GetPRDiffFunc: func(string, string, int) (string, error) { return "diff", nil }},
		DocsWriter: w,
		GitFactory: factoryFor(g),
		Projects:   testProjects(),
	}
}

func docsWriterReturning(updated bool, err error) *gemini.MockDocsWriter {
	return &gemini.MockDocsWriter{
		UpdateReadmeFunc: func(context.Context, models.ReviewJob, string, models.ReviewResult, events.Sink) (bool, error) {
			return updated, err
		},
	}
}

func TestReadmeStep(t *testing.T) {
	t.Run("updated readme is committed and pushed to the pr branch", func(t *testing.T) {
		g := okGit()
		w := docsWriterReturning(true, nil)
		vars := prVars(t)
		setVar(t, vars, KeyReview, models.ReviewResult{Summary: "good", Approved: true})
		res, err := readmeStep{d: readmeDeps(g, w)}.Run(context.Background(), newScope(t, vars))
		require.NoError(t, err)
		assert.Equal(t, step.OutcomeOK, res.Outcome)
		require.Len(t, g.PushCalls(), 1)
		assert.Equal(t, "feature/7-add-search", g.PushCalls()[0].Branch)
		assert.Equal(t, "good", w.UpdateReadmeCalls()[0].Review.Summary)
	})

	t.Run("unchanged readme pushes nothing", func(t *testing.T) {
		g := okGit()
		vars := prVars(t)
		setVar(t, vars, KeyReview, models.ReviewResult{})
		res, err := readmeStep{d: readmeDeps(g, docsWriterReturning(false, nil))}.Run(context.Background(), newScope(t, vars))
		require.NoError(t, err)
		assert.Equal(t, step.OutcomeOK, res.Outcome)
		assert.Empty(t, g.PushCalls())
	})

	t.Run("docs writer failure is classified upstream", func(t *testing.T) {
		vars := prVars(t)
		setVar(t, vars, KeyReview, models.ReviewResult{})
		_, err := readmeStep{d: readmeDeps(okGit(), docsWriterReturning(false, errors.New("boom")))}.Run(context.Background(), newScope(t, vars))
		require.ErrorIs(t, err, step.ErrUpstream)
	})

	t.Run("missing review var errors", func(t *testing.T) {
		_, err := readmeStep{d: readmeDeps(okGit(), docsWriterReturning(false, nil))}.Run(context.Background(), newScope(t, prVars(t)))
		require.ErrorIs(t, err, ErrMissingVar)
	})

	t.Run("missing docs writer errors", func(t *testing.T) {
		d := readmeDeps(okGit(), nil)
		d.DocsWriter = nil
		_, err := readmeStep{d: d}.Run(context.Background(), newScope(t, prVars(t)))
		require.ErrorIs(t, err, ErrNoDocsWriter)
	})
}
