package steps

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ai-orchestration/internal/agent"
	"ai-orchestration/internal/events"
	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/pipeline/impl"
	"ai-orchestration/internal/step"

	"ai-orchestration/internal/gitops"
	"ai-orchestration/internal/process"
	"ai-orchestration/internal/projects"
)

func implementDeps(g gitops.Git) Deps {
	return Deps{
		GitFactory: factoryFor(g),
		Projects:   testProjects(),
		TestRunner: func(string) (bool, string) { return true, "" },
	}
}

func implementHarness(hasChanges, testsPass bool) (Deps, *agent.MockAgent) {
	g := okGit()
	g.HasChangesFunc = func() (bool, error) { return hasChanges, nil }
	ag := &agent.MockAgent{
		ImplementIssueFunc: func(context.Context, models.ForgejoIssue, []models.IssueComment, string, string, *string, events.Sink) (agent.ImplementResult, error) {
			return agent.ImplementResult{Output: "ok"}, nil
		},
	}
	d := implementDeps(g)
	d.Claude = ag
	d.Forgejo = &forgejo.MockForgejo{
		GetCommentsFunc: func(string, string, int) ([]models.IssueComment, error) { return nil, nil },
		CreatePRFunc: func(string, string, string, string, string, string) (models.ForgejoPR, error) {
			return models.ForgejoPR{Number: 11, Title: "feat: Add search"}, nil
		},
	}
	d.TestRunner = func(string) (bool, string) { return testsPass, "red" }
	return d, ag
}

func TestImplementStep(t *testing.T) {
	t.Run("success returns ok with branch and pr vars", func(t *testing.T) {
		d, _ := implementHarness(true, true)
		res, err := implementStep{d: d}.Run(context.Background(), newScope(t, baseVars(t, "api")))
		require.NoError(t, err)
		assert.Equal(t, step.OutcomeOK, res.Outcome)
		branch, ok, err := process.Get(res.Vars, KeyBranch)
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, "feature/7-add-search", branch)
		pr, ok, err := process.Get(res.Vars, KeyPR)
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, 11, pr.Number)
	})

	t.Run("no changes returns no_changes", func(t *testing.T) {
		d, _ := implementHarness(false, true)
		res, err := implementStep{d: d}.Run(context.Background(), newScope(t, baseVars(t, "api")))
		require.NoError(t, err)
		assert.Equal(t, OutcomeNoChanges, res.Outcome)
		assert.Empty(t, res.Vars)
	})

	t.Run("failing tests exhaust max_attempts", func(t *testing.T) {
		d, ag := implementHarness(true, false)
		res, err := implementStep{d: d, params: implementParams{MaxAttempts: 2}}.Run(context.Background(), newScope(t, baseVars(t, "api")))
		require.NoError(t, err)
		assert.Equal(t, OutcomeTestsFailed, res.Outcome)
		assert.Len(t, ag.ImplementIssueCalls(), 2)
	})

	t.Run("default max attempts applies when param unset", func(t *testing.T) {
		d, ag := implementHarness(true, false)
		res, err := implementStep{d: d}.Run(context.Background(), newScope(t, baseVars(t, "api")))
		require.NoError(t, err)
		assert.Equal(t, OutcomeTestsFailed, res.Outcome)
		assert.Len(t, ag.ImplementIssueCalls(), impl.MaxAttempts)
	})

	t.Run("claude failure is classified upstream", func(t *testing.T) {
		d, ag := implementHarness(true, true)
		ag.ImplementIssueFunc = func(context.Context, models.ForgejoIssue, []models.IssueComment, string, string, *string, events.Sink) (agent.ImplementResult, error) {
			return agent.ImplementResult{}, errors.New("boom")
		}
		_, err := implementStep{d: d}.Run(context.Background(), newScope(t, baseVars(t, "api")))
		require.ErrorIs(t, err, step.ErrUpstream)
	})

	t.Run("missing issue var errors", func(t *testing.T) {
		vars := process.Vars{}
		setVar(t, vars, KeyProject, "api")
		_, err := implementStep{d: implementDeps(okGit())}.Run(context.Background(), newScope(t, vars))
		require.ErrorIs(t, err, ErrMissingVar)
	})

	t.Run("unknown project errors", func(t *testing.T) {
		_, err := implementStep{d: implementDeps(okGit())}.Run(context.Background(), newScope(t, baseVars(t, "nope")))
		require.ErrorIs(t, err, ErrUnknownProject)
	})

	t.Run("invalid project repo errors", func(t *testing.T) {
		_, err := implementStep{d: implementDeps(okGit())}.Run(context.Background(), newScope(t, baseVars(t, "bad")))
		require.ErrorIs(t, err, projects.ErrInvalidRepo)
	})

	t.Run("checkout failure before the loop is returned", func(t *testing.T) {
		g := okGit()
		g.EnsureRepoFunc = func() error { return errors.New("boom") }
		_, err := implementStep{d: implementDeps(g)}.Run(context.Background(), newScope(t, baseVars(t, "api")))
		require.Error(t, err)
	})
}
