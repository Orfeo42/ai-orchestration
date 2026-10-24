package steps

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/internal/agent"
	"github.com/Orfeo42/ai-orchestration/internal/events"
	"github.com/Orfeo42/ai-orchestration/internal/forgejo"
	"github.com/Orfeo42/ai-orchestration/internal/models"
	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/internal/step"
)

func assessDeps(result agent.AssessResult, assessErr error) Deps {
	return Deps{
		Forgejo: &forgejo.MockForgejo{
			GetCommentsFunc: func(string, string, int) ([]models.IssueComment, error) { return nil, nil },
		},
		Claude: &agent.MockAgent{
			AssessIssueFunc: func(context.Context, models.ForgejoIssue, []models.IssueComment, string, events.Sink) (agent.AssessResult, error) {
				return result, assessErr
			},
		},
		GitFactory: factoryFor(okGit()),
		Projects:   testProjects(),
	}
}

func TestAssessStep(t *testing.T) {
	t.Run("clear issue returns clear outcome", func(t *testing.T) {
		d := assessDeps(agent.AssessResult{Assessment: agent.Assessment{Clear: true}}, nil)
		res, err := assessStep{d: d}.Run(context.Background(), newScope(t, baseVars(t, "api")))
		require.NoError(t, err)
		assert.Equal(t, OutcomeClear, res.Outcome)
		assert.Empty(t, res.Vars)
	})

	t.Run("unclear issue returns unclear outcome with questions", func(t *testing.T) {
		d := assessDeps(agent.AssessResult{Assessment: agent.Assessment{Clear: false, Questions: "which db?"}}, nil)
		res, err := assessStep{d: d}.Run(context.Background(), newScope(t, baseVars(t, "api")))
		require.NoError(t, err)
		assert.Equal(t, OutcomeUnclear, res.Outcome)
		questions, ok, err := process.Get(res.Vars, KeyQuestions)
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, "which db?", questions)
	})

	t.Run("agent failure is classified upstream", func(t *testing.T) {
		d := assessDeps(agent.AssessResult{}, errors.New("boom"))
		_, err := assessStep{d: d}.Run(context.Background(), newScope(t, baseVars(t, "api")))
		require.ErrorIs(t, err, step.ErrUpstream)
	})

	t.Run("missing issue var errors", func(t *testing.T) {
		vars := process.Vars{}
		setVar(t, vars, KeyProject, "api")
		_, err := assessStep{d: assessDeps(agent.AssessResult{}, nil)}.Run(context.Background(), newScope(t, vars))
		require.ErrorIs(t, err, ErrMissingVar)
	})

	t.Run("missing project var errors", func(t *testing.T) {
		vars := process.Vars{}
		setVar(t, vars, KeyIssue, testIssue)
		_, err := assessStep{d: assessDeps(agent.AssessResult{}, nil)}.Run(context.Background(), newScope(t, vars))
		require.ErrorIs(t, err, ErrMissingVar)
	})

	t.Run("unknown project errors", func(t *testing.T) {
		_, err := assessStep{d: assessDeps(agent.AssessResult{}, nil)}.Run(context.Background(), newScope(t, baseVars(t, "nope")))
		require.ErrorIs(t, err, ErrUnknownProject)
	})
}
