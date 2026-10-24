package steps

import (
	"context"
	"fmt"

	"github.com/Orfeo42/ai-orchestration/internal/pipeline"
	"github.com/Orfeo42/ai-orchestration/internal/pipeline/checkout"
	"github.com/Orfeo42/ai-orchestration/internal/pipeline/review"
	"github.com/Orfeo42/ai-orchestration/internal/pipeline/state"
	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/internal/projects"
	"github.com/Orfeo42/ai-orchestration/internal/step"
)

const (
	OutcomeChangesRequested step.Outcome = "changes_requested"
	providerGemini                       = "gemini"
)

type reviewParams struct {
	Provider string `json:"provider" validate:"omitempty,oneof=gemini claude"`
}

type reviewStep struct {
	d      Deps
	params reviewParams
}

func (r reviewStep) Run(ctx context.Context, sc step.Context) (step.Result, error) {
	provider := r.params.Provider
	if provider == "" {
		provider = providerGemini
	}
	reviewer, ok := r.d.Reviewers[provider]
	if !ok || reviewer == nil {
		return step.Result{}, fmt.Errorf("%w: %s", ErrNoReviewer, provider)
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
	code, err := projects.SplitRepo(proj.Config.Repo)
	if err != nil {
		return step.Result{}, err
	}
	job, err := newReviewJob(r.d.Forgejo, issue, proj.Key, code, pr.PR, pr.Branch)
	if err != nil {
		return step.Result{}, err
	}
	s := state.Review{Job: job, Handle: sc.Handle, Proj: proj.Config}
	chain := []pipeline.Processor[state.Review]{
		checkout.ReviewCheckout{
			GitFactory:   r.d.GitFactory,
			ForgejoURL:   r.d.ForgejoURL,
			ForgejoToken: r.d.ForgejoToken,
			ProjectsRoot: r.d.ProjectsRoot,
			WorkerID:     sc.WorkerID,
		},
		review.ReviewPR{Reviewer: reviewer},
		review.PostReview{Fj: r.d.Forgejo},
	}
	if err := pipeline.Execute(ctx, &s, chain); err != nil {
		return step.Result{}, err
	}
	vars := process.Vars{}
	if err := process.Set(vars, KeyReview, s.Result); err != nil {
		return step.Result{}, err
	}
	if !s.Result.Approved {
		return step.Result{Outcome: OutcomeChangesRequested, Vars: vars}, nil
	}
	return step.Result{Outcome: OutcomeApproved, Vars: vars}, nil
}
