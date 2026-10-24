package steps

import (
	"context"
	"fmt"

	"ai-orchestration/internal/pipeline"
	"ai-orchestration/internal/pipeline/checkout"
	"ai-orchestration/internal/pipeline/review"
	"ai-orchestration/internal/pipeline/state"
	"ai-orchestration/internal/process"
	"ai-orchestration/internal/projects"
	"ai-orchestration/internal/step"
)

type readmeStep struct {
	d Deps
}

func (r readmeStep) Run(ctx context.Context, sc step.Context) (step.Result, error) {
	if r.d.DocsWriter == nil {
		return step.Result{}, ErrNoDocsWriter
	}
	issue, err := loadIssue(sc)
	if err != nil {
		return step.Result{}, err
	}
	proj, err := loadProject(sc, r.d.Projects)
	if err != nil {
		return step.Result{}, err
	}
	pr, err := loadPR(sc)
	if err != nil {
		return step.Result{}, err
	}
	result, ok, err := process.Get(sc.Process.Vars, KeyReview)
	if err != nil {
		return step.Result{}, err
	}
	if !ok {
		return step.Result{}, fmt.Errorf("%w: %s", ErrMissingVar, KeyReview)
	}
	code, err := projects.SplitRepo(proj.Config.Repo)
	if err != nil {
		return step.Result{}, err
	}
	job, err := newReviewJob(r.d.Forgejo, issue, proj.Key, code, pr.PR, pr.Branch)
	if err != nil {
		return step.Result{}, err
	}
	s := state.Review{Job: job, Handle: sc.Handle, Proj: proj.Config, Result: result}
	chain := []pipeline.Processor[state.Review]{
		checkout.ReviewCheckout{
			GitFactory:   r.d.GitFactory,
			ForgejoURL:   r.d.ForgejoURL,
			ForgejoToken: r.d.ForgejoToken,
			ProjectsRoot: r.d.ProjectsRoot,
			WorkerID:     sc.WorkerID,
		},
		review.CheckoutPRBranch{
			GitFactory:   r.d.GitFactory,
			ForgejoURL:   r.d.ForgejoURL,
			ForgejoToken: r.d.ForgejoToken,
		},
		review.UpdateReadme{DocsWriter: r.d.DocsWriter},
	}
	if err := pipeline.Execute(ctx, &s, chain); err != nil {
		return step.Result{}, err
	}
	return step.Result{Outcome: step.OutcomeOK}, nil
}
