package impl

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
	"ai-orchestration/internal/pipeline/state"
	"ai-orchestration/internal/runstore"
	"ai-orchestration/internal/step"
)

func newState(g gitops.Git) *state.Impl {
	return &state.Impl{
		Job: models.ImplJob{
			RepoOwner: "acme", RepoName: "tracker", IssueNumber: 7, IssueTitle: "Add search",
		},
		Handle:      runstore.NewStepHandle(context.Background(), nil, nil, nil, "r1", runstore.ServiceOrchestrator),
		Issue:       models.ForgejoIssue{Number: 7, Title: "Add search", RepoOwner: "acme", RepoName: "tracker"},
		Git:         g,
		Branch:      "feature/7-add-search",
		ProjectPath: "/p",
	}
}

func okGit() *gitops.MockGit {
	return &gitops.MockGit{
		AddAllFunc:   func() error { return nil },
		CommitFunc:   func(string) error { return nil },
		PushFunc:     func(string) error { return nil },
		CheckoutFunc: func(string) error { return nil },
	}
}

func TestPushAndCreatePR(t *testing.T) {
	proc := func(fj forgejo.Forgejo) PushAndCreatePR {
		return PushAndCreatePR{Fj: fj, ForgejoURL: "https://fj", CodeOwner: "acme", CodeName: "api", BaseBranch: "main"}
	}

	t.Run("commits, pushes, creates pr and checks out base", func(t *testing.T) {
		g := okGit()
		fj := &forgejo.MockForgejo{
			CreatePRFunc: func(owner, repo, title, body, head, base string) (models.ForgejoPR, error) {
				assert.Equal(t, "acme", owner)
				assert.Equal(t, "api", repo)
				assert.Equal(t, "feat: Add search", title)
				assert.Equal(t, "Implements [acme/tracker#7](https://fj/acme/tracker/issues/7)", body)
				assert.Equal(t, "feature/7-add-search", head)
				assert.Equal(t, "main", base)
				return models.ForgejoPR{Number: 11}, nil
			},
		}
		s := newState(g)
		require.NoError(t, proc(fj).Run(context.Background(), s))
		require.NotNil(t, s.PR)
		assert.Equal(t, 11, s.PR.Number)
		assert.Equal(t, "feat: implement issue #7 - Add search", g.CommitCalls()[0].Message)
		assert.Equal(t, "feature/7-add-search", g.PushCalls()[0].Branch)
		assert.Equal(t, "main", g.CheckoutCalls()[0].Branch)
	})

	t.Run("git add failure is returned", func(t *testing.T) {
		g := okGit()
		g.AddAllFunc = func() error { return errors.New("boom") }
		err := proc(&forgejo.MockForgejo{}).Run(context.Background(), newState(g))
		require.Error(t, err)
		assert.NotErrorIs(t, err, step.ErrUpstream)
	})

	t.Run("git commit failure is returned", func(t *testing.T) {
		g := okGit()
		g.CommitFunc = func(string) error { return errors.New("boom") }
		require.Error(t, proc(&forgejo.MockForgejo{}).Run(context.Background(), newState(g)))
	})

	t.Run("git push failure is returned", func(t *testing.T) {
		g := okGit()
		g.PushFunc = func(string) error { return errors.New("boom") }
		require.Error(t, proc(&forgejo.MockForgejo{}).Run(context.Background(), newState(g)))
	})

	t.Run("create pr failure is classified upstream", func(t *testing.T) {
		fj := &forgejo.MockForgejo{
			CreatePRFunc: func(string, string, string, string, string, string) (models.ForgejoPR, error) {
				return models.ForgejoPR{}, errors.New("boom")
			},
		}
		s := newState(okGit())
		err := proc(fj).Run(context.Background(), s)
		require.ErrorIs(t, err, step.ErrUpstream)
		assert.Nil(t, s.PR)
	})

	t.Run("base checkout failure does not fail the processor", func(t *testing.T) {
		g := okGit()
		g.CheckoutFunc = func(string) error { return errors.New("boom") }
		fj := &forgejo.MockForgejo{
			CreatePRFunc: func(string, string, string, string, string, string) (models.ForgejoPR, error) {
				return models.ForgejoPR{Number: 11}, nil
			},
		}
		s := newState(g)
		require.NoError(t, proc(fj).Run(context.Background(), s))
		assert.NotNil(t, s.PR)
	})
}

func TestPRDescription(t *testing.T) {
	proc := func(fj forgejo.Forgejo, w PRDescWriter) PRDescription {
		return PRDescription{Fj: fj, PrWriter: w, CodeOwner: "acme", CodeName: "api"}
	}
	writer := func(desc string, err error) *gemini.MockPRDescWriter {
		return &gemini.MockPRDescWriter{
			GenerateFunc: func(context.Context, models.ForgejoIssue, string, string, events.Sink) (string, error) {
				return desc, err
			},
		}
	}
	withPR := func() *state.Impl {
		s := newState(okGit())
		s.PR = &models.ForgejoPR{Number: 11}
		return s
	}

	t.Run("generates description and updates pr body", func(t *testing.T) {
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
		w := writer("desc", nil)
		require.NoError(t, proc(fj, w).Run(context.Background(), withPR()))
		assert.Equal(t, "relates to: acme/tracker#7\n\ndesc", body)
		assert.Equal(t, "diff", w.GenerateCalls()[0].Diff)
		assert.Equal(t, "/p", w.GenerateCalls()[0].ProjectPath)
	})

	t.Run("empty diff does nothing", func(t *testing.T) {
		fj := &forgejo.MockForgejo{GetPRDiffFunc: func(string, string, int) (string, error) { return "", nil }}
		w := writer("desc", nil)
		require.NoError(t, proc(fj, w).Run(context.Background(), withPR()))
		assert.Empty(t, w.GenerateCalls())
	})

	t.Run("diff failure is classified upstream", func(t *testing.T) {
		fj := &forgejo.MockForgejo{GetPRDiffFunc: func(string, string, int) (string, error) { return "", errors.New("boom") }}
		require.ErrorIs(t, proc(fj, writer("", nil)).Run(context.Background(), withPR()), step.ErrUpstream)
	})

	t.Run("generate failure is classified upstream", func(t *testing.T) {
		fj := &forgejo.MockForgejo{GetPRDiffFunc: func(string, string, int) (string, error) { return "diff", nil }}
		require.ErrorIs(t, proc(fj, writer("", errors.New("boom"))).Run(context.Background(), withPR()), step.ErrUpstream)
	})

	t.Run("update failure is classified upstream", func(t *testing.T) {
		fj := &forgejo.MockForgejo{
			GetPRDiffFunc: func(string, string, int) (string, error) { return "diff", nil },
			UpdatePRFunc:  func(string, string, int, string) error { return errors.New("boom") },
		}
		require.ErrorIs(t, proc(fj, writer("desc", nil)).Run(context.Background(), withPR()), step.ErrUpstream)
	})
}
