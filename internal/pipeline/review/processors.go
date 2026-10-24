package review

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/gemini"
	"ai-orchestration/internal/gitops"
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/pipeline/state"
	"ai-orchestration/internal/runstore"
	"ai-orchestration/internal/step"
)

type ReviewPR struct {
	Reviewer gemini.Reviewer
}

func (p ReviewPR) Run(ctx context.Context, s *state.Review) error {
	s.Handle.SetPhase(ctx, runstore.PhaseReviewing)
	result, err := p.Reviewer.Review(ctx, s.Job, s.Handle.Sink())
	if err != nil {
		return fmt.Errorf("%w: review: %w", step.ErrUpstream, err)
	}
	s.Result = result
	slog.InfoContext(ctx, "reviewed PR",
		"pr", s.Job.PRNumber,
		"approved", result.Approved,
	)
	return nil
}

type CheckoutPRBranch struct {
	GitFactory   gitops.Factory
	ForgejoURL   string
	ForgejoToken string
}

func (p CheckoutPRBranch) Run(_ context.Context, s *state.Review) error {
	git := p.GitFactory(s.ProjectPath, p.ForgejoURL, p.ForgejoToken, s.Job.RepoOwner, s.Job.RepoName)
	s.Git = git
	if err := git.Fetch(); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	if err := git.CheckoutRemoteBranch(s.Job.PRBranch); err != nil {
		return fmt.Errorf("checkout branch: %w", err)
	}
	return nil
}

type UpdateReadme struct {
	DocsWriter gemini.DocsWriter
}

func (p UpdateReadme) Run(ctx context.Context, s *state.Review) error {
	updated, err := p.DocsWriter.UpdateReadme(ctx, s.Job, s.ProjectPath, s.Result, s.Handle.Sink())
	if err != nil {
		return fmt.Errorf("%w: update readme: %w", step.ErrUpstream, err)
	}
	if !updated {
		return nil
	}
	if err := s.Git.Add("README.md"); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if err := s.Git.Commit(fmt.Sprintf("docs: update README for PR #%d", s.Job.PRNumber)); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	if err := s.Git.Push(s.Job.PRBranch); err != nil {
		return fmt.Errorf("git push: %w", err)
	}
	slog.InfoContext(ctx, "README updated",
		"pr", s.Job.PRNumber,
	)
	return nil
}

type PostReview struct {
	Fj forgejo.Forgejo
}

func (p PostReview) Run(_ context.Context, s *state.Review) error {
	if err := p.Fj.CreatePRReview(s.Job.RepoOwner, s.Job.RepoName, s.Job.PRNumber, buildReviewBody(s.Result), s.Result.Approved); err != nil {
		return fmt.Errorf("%w: post review: %w", step.ErrUpstream, err)
	}
	return nil
}

func buildReviewBody(result models.ReviewResult) string {
	var parts []string
	parts = append(parts, "## AI Review\n\n"+result.Summary)

	if len(result.Issues) > 0 {
		items := make([]string, len(result.Issues))
		for i, issue := range result.Issues {
			items[i] = "- " + issue
		}
		parts = append(parts, "\n### Issues\n"+strings.Join(items, "\n"))
	}
	if len(result.Suggestions) > 0 {
		items := make([]string, len(result.Suggestions))
		for i, s := range result.Suggestions {
			items[i] = "- " + s
		}
		parts = append(parts, "\n### Suggestions\n"+strings.Join(items, "\n"))
	}

	verdict := "CHANGES REQUESTED"
	if result.Approved {
		verdict = "APPROVED"
	}
	parts = append(parts, "\n**Verdict:** "+verdict)
	return strings.Join(parts, "\n")
}
