package steps

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/internal/events"
	"github.com/Orfeo42/ai-orchestration/internal/forgejo"
	"github.com/Orfeo42/ai-orchestration/internal/gemini"
	"github.com/Orfeo42/ai-orchestration/internal/models"
	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/internal/step"
)

func describeDeps(g *forgejo.MockForgejo, w *gemini.MockPRDescWriter) Deps {
	return Deps{
		Forgejo:    g,
		PRWriter:   w,
		GitFactory: factoryFor(okGit()),
		Projects:   testProjects(),
	}
}

func TestDescribeStep(t *testing.T) {
	t.Run("updates pr body with generated description", func(t *testing.T) {
		var body string
		fj := &forgejo.MockForgejo{
			GetPRDiffFunc: func(string, string, int) (string, error) { return "diff", nil },
			UpdatePRFunc: func(owner, repo string, n int, b string) error {
				assert.Equal(t, "acme", owner)
				assert.Equal(t, "api", repo)
				assert.Equal(t, 11, n)
				body = b
				return nil
			},
		}
		w := &gemini.MockPRDescWriter{
			GenerateFunc: func(context.Context, models.ForgejoIssue, string, string, events.Sink) (string, error) {
				return "the description", nil
			},
		}
		res, err := describeStep{d: describeDeps(fj, w)}.Run(context.Background(), newScope(t, prVars(t)))
		require.NoError(t, err)
		assert.Equal(t, step.OutcomeOK, res.Outcome)
		assert.Equal(t, "relates to: acme/tracker#7\n\nthe description", body)
	})

	t.Run("checks out the pr branch", func(t *testing.T) {
		g := okGit()
		var branch string
		g.CheckoutRemoteBranchFunc = func(b string) error {
			branch = b
			return nil
		}
		fj := &forgejo.MockForgejo{GetPRDiffFunc: func(string, string, int) (string, error) { return "", nil }}
		d := describeDeps(fj, &gemini.MockPRDescWriter{})
		d.GitFactory = factoryFor(g)
		_, err := describeStep{d: d}.Run(context.Background(), newScope(t, prVars(t)))
		require.NoError(t, err)
		assert.Equal(t, "feature/7-add-search", branch)
		assert.Len(t, g.FetchCalls(), 1)
	})

	t.Run("diff failure is classified upstream", func(t *testing.T) {
		fj := &forgejo.MockForgejo{GetPRDiffFunc: func(string, string, int) (string, error) { return "", errors.New("boom") }}
		_, err := describeStep{d: describeDeps(fj, &gemini.MockPRDescWriter{})}.Run(context.Background(), newScope(t, prVars(t)))
		require.ErrorIs(t, err, step.ErrUpstream)
	})

	t.Run("missing writer errors", func(t *testing.T) {
		d := describeDeps(&forgejo.MockForgejo{}, nil)
		d.PRWriter = nil
		_, err := describeStep{d: d}.Run(context.Background(), newScope(t, prVars(t)))
		require.ErrorIs(t, err, ErrNoPRWriter)
	})

	t.Run("missing pr var errors", func(t *testing.T) {
		_, err := describeStep{d: describeDeps(&forgejo.MockForgejo{}, &gemini.MockPRDescWriter{})}.Run(context.Background(), newScope(t, baseVars(t, "api")))
		require.ErrorIs(t, err, ErrMissingVar)
	})

	t.Run("missing branch var errors", func(t *testing.T) {
		vars := baseVars(t, "api")
		setVar(t, vars, KeyPR, testPR)
		_, err := describeStep{d: describeDeps(&forgejo.MockForgejo{}, &gemini.MockPRDescWriter{})}.Run(context.Background(), newScope(t, vars))
		require.ErrorIs(t, err, ErrMissingVar)
	})

	t.Run("unknown project errors", func(t *testing.T) {
		vars := process.Vars{}
		setVar(t, vars, KeyIssue, testIssue)
		setVar(t, vars, KeyProject, "nope")
		_, err := describeStep{d: describeDeps(&forgejo.MockForgejo{}, &gemini.MockPRDescWriter{})}.Run(context.Background(), newScope(t, vars))
		require.ErrorIs(t, err, ErrUnknownProject)
	})
}
