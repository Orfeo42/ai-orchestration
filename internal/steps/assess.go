package steps

import (
	"context"
	"errors"

	"ai-orchestration/internal/models"
	"ai-orchestration/internal/pipeline"
	"ai-orchestration/internal/pipeline/assess"
	"ai-orchestration/internal/pipeline/checkout"
	"ai-orchestration/internal/pipeline/state"
	"ai-orchestration/internal/process"
	"ai-orchestration/internal/step"
)

const (
	OutcomeClear   step.Outcome = "clear"
	OutcomeUnclear step.Outcome = "unclear"
)

type assessStep struct {
	d Deps
}

func (a assessStep) Run(ctx context.Context, sc step.Context) (step.Result, error) {
	issue, err := loadIssue(sc)
	if err != nil {
		return step.Result{}, err
	}
	proj, err := loadProject(sc, a.d.Projects)
	if err != nil {
		return step.Result{}, err
	}
	s := state.Assess{
		Job: models.AssessJob{
			RepoOwner:   issue.RepoOwner,
			RepoName:    issue.RepoName,
			IssueNumber: issue.Number,
			IssueTitle:  issue.Title,
			IssueBody:   issue.Body,
			Project:     proj.Key,
			Labels:      issue.Labels,
		},
		Proj:   proj.Config,
		Handle: sc.Handle,
	}
	chain := []pipeline.Processor[state.Assess]{
		checkout.AssessCheckout{
			GitFactory:   a.d.GitFactory,
			ForgejoURL:   a.d.ForgejoURL,
			ForgejoToken: a.d.ForgejoToken,
			ProjectsRoot: a.d.ProjectsRoot,
			WorkerID:     sc.WorkerID,
		},
		assess.HydrateIssue{},
		assess.FetchComments{Fj: a.d.Forgejo},
		assess.AssessClarity{Claude: a.d.Claude, ProjectsRoot: a.d.ProjectsRoot, WorkerID: sc.WorkerID},
	}
	err = pipeline.Execute(ctx, &s, chain)
	if errors.Is(err, pipeline.ErrIssueUnclear) {
		vars := process.Vars{}
		if err := process.Set(vars, KeyQuestions, s.Result.Assessment.Questions); err != nil {
			return step.Result{}, err
		}
		return step.Result{Outcome: OutcomeUnclear, Vars: vars}, nil
	}
	if err != nil {
		return step.Result{}, err
	}
	return step.Result{Outcome: OutcomeClear}, nil
}
