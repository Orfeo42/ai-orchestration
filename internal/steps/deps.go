package steps

import (
	"errors"

	"ai-orchestration/internal/agent"
	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/gemini"
	"ai-orchestration/internal/gitops"
	"ai-orchestration/internal/pipeline/impl"
	"ai-orchestration/internal/projects"
	"ai-orchestration/internal/step"
)

type stepsError string

func (e stepsError) Error() string { return string(e) }

const (
	ErrMissingVar     stepsError = "missing process var"
	ErrUnknownProject stepsError = "unknown project"
	ErrNoReviewer     stepsError = "reviewer provider not configured"
	ErrNoPRWriter     stepsError = "pr description writer not configured"
	ErrNoDocsWriter   stepsError = "docs writer not configured"
)

type Deps struct {
	Forgejo      forgejo.Forgejo
	Claude       agent.Agent
	PRWriter     impl.PRDescWriter
	Reviewers    map[string]gemini.Reviewer
	DocsWriter   gemini.DocsWriter
	GitFactory   gitops.Factory
	ForgejoURL   string
	ForgejoToken string
	Projects     map[string]projects.Config
	ProjectsRoot string
	TestRunner   func(path string) (bool, string)
}

func RegisterAll(r *step.Registry, d Deps) error {
	return errors.Join(
		step.Register(r, step.Def{Name: "claude.assess", Queue: step.QueueClaude, Outcomes: []step.Outcome{OutcomeClear, OutcomeUnclear}},
			func(step.NoParams) step.Step { return assessStep{d: d} }),
		step.Register(r, step.Def{Name: "tracker.ask_clarification", Queue: step.QueueDefault, Outcomes: []step.Outcome{OutcomeAnswered}},
			func(step.NoParams) step.Step { return clarificationStep{d: d} }),
		step.Register(r, step.Def{Name: "claude.implement", Queue: step.QueueClaude, Outcomes: []step.Outcome{step.OutcomeOK, OutcomeNoChanges, OutcomeTestsFailed}},
			func(p implementParams) step.Step { return implementStep{d: d, params: p} }),
		step.Register(r, step.Def{Name: "pr.describe", Queue: step.QueueClaude, Outcomes: []step.Outcome{step.OutcomeOK}},
			func(step.NoParams) step.Step { return describeStep{d: d} }),
		step.Register(r, step.Def{Name: "ai.review", Queue: step.QueueClaude, Outcomes: []step.Outcome{OutcomeApproved, OutcomeChangesRequested}},
			func(p reviewParams) step.Step { return reviewStep{d: d, params: p} }),
		step.Register(r, step.Def{Name: "docs.readme", Queue: step.QueueClaude, Outcomes: []step.Outcome{step.OutcomeOK}},
			func(step.NoParams) step.Step { return readmeStep{d: d} }),
		step.Register(r, step.Def{Name: "tracker.labels", Queue: step.QueueDefault, Outcomes: []step.Outcome{step.OutcomeOK}},
			func(p labelsParams) step.Step { return labelsStep{d: d, params: p} }),
		step.Register(r, step.Def{Name: "human.approve", Queue: step.QueueDefault, Outcomes: []step.Outcome{OutcomeApproved, OutcomeRejected}},
			func(step.NoParams) step.Step { return approveStep{} }),
	)
}
