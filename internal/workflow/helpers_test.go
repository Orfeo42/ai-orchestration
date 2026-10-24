package workflow

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"ai-orchestration/internal/step"
)

const exampleYAML = `name: implement-issue
trigger:
  forgejo: { repos: [ionstream/handbook], label: To-do, assignee: "" }
start: assess
steps:
  assess:      { uses: claude.assess, timeout: 30m, retry: 2, on: { clear: implement, unclear: ask } }
  ask:         { uses: tracker.ask_clarification, on: { answered: assess } }
  implement:   { uses: claude.implement, with: { max_attempts: 3 }, on: { ok: describe, no_changes: fail, tests_failed: fail } }
  describe:    { uses: pr.describe, on: { ok: review } }
  review:      { uses: ai.review, with: { provider: gemini }, on: { approved: docs, changes_requested: docs } }
  docs:        { uses: docs.readme, on: { ok: label_done } }
  label_done:  { uses: tracker.labels, with: { add: [done], remove: [in-progress] }, on: { ok: end } }
`

type fakeStep struct{}

func (fakeStep) Run(_ context.Context, _ step.Context) (step.Result, error) {
	return step.Result{}, nil
}

type implementParams struct {
	MaxAttempts int `json:"max_attempts" validate:"omitempty,min=1,max=10"`
}

type reviewParams struct {
	Provider string `json:"provider" validate:"omitempty,oneof=gemini claude"`
}

type labelParams struct {
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
}

func newTestRegistry(t *testing.T) *step.Registry {
	t.Helper()
	reg := step.NewRegistry()
	require.NoError(t, step.Register(reg, step.Def{Name: "claude.assess", Queue: step.QueueClaude, Outcomes: []step.Outcome{"clear", "unclear"}}, func(step.NoParams) step.Step { return fakeStep{} }))
	require.NoError(t, step.Register(reg, step.Def{Name: "tracker.ask_clarification", Queue: step.QueueDefault, Outcomes: []step.Outcome{"answered"}}, func(step.NoParams) step.Step { return fakeStep{} }))
	require.NoError(t, step.Register(reg, step.Def{Name: "claude.implement", Queue: step.QueueClaude, Outcomes: []step.Outcome{"ok", "no_changes", "tests_failed"}}, func(implementParams) step.Step { return fakeStep{} }))
	require.NoError(t, step.Register(reg, step.Def{Name: "pr.describe", Queue: step.QueueClaude, Outcomes: []step.Outcome{step.OutcomeOK}}, func(step.NoParams) step.Step { return fakeStep{} }))
	require.NoError(t, step.Register(reg, step.Def{Name: "ai.review", Queue: step.QueueDefault, Outcomes: []step.Outcome{"approved", "changes_requested"}}, func(reviewParams) step.Step { return fakeStep{} }))
	require.NoError(t, step.Register(reg, step.Def{Name: "docs.readme", Queue: step.QueueClaude, Outcomes: []step.Outcome{step.OutcomeOK}}, func(step.NoParams) step.Step { return fakeStep{} }))
	require.NoError(t, step.Register(reg, step.Def{Name: "tracker.labels", Queue: step.QueueDefault, Outcomes: []step.Outcome{step.OutcomeOK}}, func(labelParams) step.Step { return fakeStep{} }))
	return reg
}

func parseExample(t *testing.T) Definition {
	t.Helper()
	def, err := Parse([]byte(exampleYAML))
	require.NoError(t, err)
	return def
}
