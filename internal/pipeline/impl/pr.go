package impl

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Orfeo42/ai-orchestration/internal/forgejo"
	"github.com/Orfeo42/ai-orchestration/internal/pipeline/state"
	"github.com/Orfeo42/ai-orchestration/internal/runstore"
	"github.com/Orfeo42/ai-orchestration/internal/step"
)

type PushAndCreatePR struct {
	Fj         forgejo.Forgejo
	ForgejoURL string
	CodeOwner  string
	CodeName   string
	BaseBranch string
}

func (p PushAndCreatePR) Run(ctx context.Context, s *state.Impl) error {
	s.Handle.SetPhase(ctx, runstore.PhasePushing)
	if err := s.Git.AddAll(); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if err := s.Git.Commit(fmt.Sprintf("feat: implement issue #%d - %s", s.Job.IssueNumber, s.Job.IssueTitle)); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	if err := s.Git.Push(s.Branch); err != nil {
		return fmt.Errorf("git push: %w", err)
	}
	prBody := fmt.Sprintf("Implements [%s/%s#%d](%s/%s/%s/issues/%d)",
		s.Job.RepoOwner, s.Job.RepoName, s.Job.IssueNumber,
		p.ForgejoURL, s.Job.RepoOwner, s.Job.RepoName, s.Job.IssueNumber)
	pr, err := p.Fj.CreatePR(p.CodeOwner, p.CodeName, "feat: "+s.Job.IssueTitle, prBody, s.Branch, p.BaseBranch)
	if err != nil {
		return fmt.Errorf("%w: create pr: %w", step.ErrUpstream, err)
	}
	s.PR = &pr
	if err := s.Git.Checkout(p.BaseBranch); err != nil {
		slog.WarnContext(ctx, "checkout base after PR failed",
			"error", err,
		)
	}
	return nil
}

type PRDescription struct {
	Fj        forgejo.Forgejo
	PrWriter  PRDescWriter
	CodeOwner string
	CodeName  string
}

func (p PRDescription) Run(ctx context.Context, s *state.Impl) error {
	diff, err := p.Fj.GetPRDiff(p.CodeOwner, p.CodeName, s.PR.Number)
	if err != nil {
		return fmt.Errorf("%w: get pr diff: %w", step.ErrUpstream, err)
	}
	if diff == "" {
		return nil
	}
	s.Handle.SetPhase(ctx, runstore.PhasePRDesc)
	desc, err := p.PrWriter.Generate(ctx, s.Issue, s.ProjectPath, diff, s.Handle.Sink())
	if err != nil {
		return fmt.Errorf("%w: generate pr description: %w", step.ErrUpstream, err)
	}
	body := fmt.Sprintf("relates to: %s/%s#%d\n\n%s", s.Job.RepoOwner, s.Job.RepoName, s.Job.IssueNumber, desc)
	if err := p.Fj.UpdatePR(p.CodeOwner, p.CodeName, s.PR.Number, body); err != nil {
		return fmt.Errorf("%w: update pr body: %w", step.ErrUpstream, err)
	}
	return nil
}
