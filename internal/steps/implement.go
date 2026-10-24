package steps

import (
	"context"
	"errors"

	"ai-orchestration/internal/models"
	"ai-orchestration/internal/pipeline"
	"ai-orchestration/internal/pipeline/checkout"
	"ai-orchestration/internal/pipeline/impl"
	"ai-orchestration/internal/pipeline/state"
	"ai-orchestration/internal/process"
	"ai-orchestration/internal/projects"
	"ai-orchestration/internal/step"
)

const (
	OutcomeNoChanges   step.Outcome = "no_changes"
	OutcomeTestsFailed step.Outcome = "tests_failed"
)

type implementParams struct {
	MaxAttempts int `json:"max_attempts" validate:"omitempty,min=1,max=10"`
}

type implementStep struct {
	d      Deps
	params implementParams
}

func (i implementStep) Run(ctx context.Context, sc step.Context) (step.Result, error) {
	issue, err := loadIssue(sc)
	if err != nil {
		return step.Result{}, err
	}
	proj, err := loadProject(sc, i.d.Projects)
	if err != nil {
		return step.Result{}, err
	}
	code, err := projects.SplitRepo(proj.Config.Repo)
	if err != nil {
		return step.Result{}, err
	}
	maxAttempts := i.params.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = impl.MaxAttempts
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
	}
	chain := []pipeline.Processor[state.Impl]{
		checkout.ImplCheckout{
			GitFactory:   i.d.GitFactory,
			ForgejoURL:   i.d.ForgejoURL,
			ForgejoToken: i.d.ForgejoToken,
			ProjectsRoot: i.d.ProjectsRoot,
			WorkerID:     sc.WorkerID,
		},
		impl.HydrateIssue{},
		impl.FetchComments{Fj: i.d.Forgejo},
		impl.MarkCommentsSeen{},
		impl.PrepareBranch{
			GitFactory:   i.d.GitFactory,
			ForgejoURL:   i.d.ForgejoURL,
			ForgejoToken: i.d.ForgejoToken,
			RepoOwner:    code.Owner,
			RepoName:     code.Name,
			BaseBranch:   proj.Config.BaseBranch,
		},
		pipeline.LoopProcessor[state.Impl]{
			Max:   maxAttempts,
			Until: func(s *state.Impl) bool { return s.TestError == nil && s.Attempt > 0 },
			Body: []pipeline.Processor[state.Impl]{
				impl.BeginAttempt{},
				impl.RefreshFreshComments{Fj: i.d.Forgejo},
				impl.ImplementAttempt{Claude: i.d.Claude},
				impl.CheckHasChanges{},
				impl.RunTests{Runner: i.d.TestRunner},
			},
		},
		impl.PushAndCreatePR{
			Fj:         i.d.Forgejo,
			ForgejoURL: i.d.ForgejoURL,
			CodeOwner:  code.Owner,
			CodeName:   code.Name,
			BaseBranch: proj.Config.BaseBranch,
		},
	}
	err = pipeline.Execute(ctx, &s, chain)
	if errors.Is(err, pipeline.ErrLoopMax) {
		return step.Result{Outcome: OutcomeTestsFailed}, nil
	}
	if errors.Is(err, pipeline.ErrNoChanges) {
		return step.Result{Outcome: OutcomeNoChanges}, nil
	}
	if err != nil {
		return step.Result{}, err
	}
	vars := process.Vars{}
	if err := process.Set(vars, KeyBranch, s.Branch); err != nil {
		return step.Result{}, err
	}
	if err := process.Set(vars, KeyPR, *s.PR); err != nil {
		return step.Result{}, err
	}
	return step.Result{Outcome: step.OutcomeOK, Vars: vars}, nil
}
