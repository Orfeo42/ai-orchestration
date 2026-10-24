package steps

import (
	"context"
	"fmt"

	"github.com/Orfeo42/ai-orchestration/internal/models"
	"github.com/Orfeo42/ai-orchestration/internal/pipeline/checkout"
	"github.com/Orfeo42/ai-orchestration/internal/pipeline/impl"
	"github.com/Orfeo42/ai-orchestration/internal/pipeline/state"
	"github.com/Orfeo42/ai-orchestration/internal/projects"
	"github.com/Orfeo42/ai-orchestration/internal/step"
)

type describeStep struct {
	d Deps
}

func (p describeStep) Run(ctx context.Context, sc step.Context) (step.Result, error) {
	if p.d.PRWriter == nil {
		return step.Result{}, ErrNoPRWriter
	}
	issue, err := loadIssue(sc)
	if err != nil {
		return step.Result{}, err
	}
	proj, err := loadProject(sc, p.d.Projects)
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
	s := state.Impl{
		Job: models.ImplJob{
			RepoOwner:   issue.RepoOwner,
			RepoName:    issue.RepoName,
			IssueNumber: issue.Number,
			IssueTitle:  issue.Title,
			IssueBody:   issue.Body,
			Project:     proj.Key,
		},
		Proj:   proj.Config,
		Handle: sc.Handle,
		Issue:  issue,
		Branch: pr.Branch,
		PR:     &pr.PR,
	}
	checkoutStep := checkout.ImplCheckout{
		GitFactory:   p.d.GitFactory,
		ForgejoURL:   p.d.ForgejoURL,
		ForgejoToken: p.d.ForgejoToken,
		ProjectsRoot: p.d.ProjectsRoot,
		WorkerID:     sc.WorkerID,
	}
	if err := checkoutStep.Run(ctx, &s); err != nil {
		return step.Result{}, err
	}
	git := p.d.GitFactory(s.ProjectPath, p.d.ForgejoURL, p.d.ForgejoToken, code.Owner, code.Name)
	if err := git.Fetch(); err != nil {
		return step.Result{}, fmt.Errorf("fetch: %w", err)
	}
	if err := git.CheckoutRemoteBranch(pr.Branch); err != nil {
		return step.Result{}, fmt.Errorf("checkout branch: %w", err)
	}
	desc := impl.PRDescription{Fj: p.d.Forgejo, PrWriter: p.d.PRWriter, CodeOwner: code.Owner, CodeName: code.Name}
	if err := desc.Run(ctx, &s); err != nil {
		return step.Result{}, err
	}
	return step.Result{Outcome: step.OutcomeOK}, nil
}
