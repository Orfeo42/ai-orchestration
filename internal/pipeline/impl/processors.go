package impl

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/Orfeo42/ai-orchestration/internal/agent"
	"github.com/Orfeo42/ai-orchestration/internal/events"
	"github.com/Orfeo42/ai-orchestration/internal/forgejo"
	"github.com/Orfeo42/ai-orchestration/internal/gitops"
	"github.com/Orfeo42/ai-orchestration/internal/models"
	"github.com/Orfeo42/ai-orchestration/internal/pipeline"
	"github.com/Orfeo42/ai-orchestration/internal/pipeline/state"
	"github.com/Orfeo42/ai-orchestration/internal/runstore"
	"github.com/Orfeo42/ai-orchestration/internal/step"
)

const MaxAttempts = 3

type PRDescWriter interface {
	Generate(ctx context.Context, issue models.ForgejoIssue, projectPath, diff string, sink events.Sink) (string, error)
}

type HydrateIssue struct{}

func (HydrateIssue) Run(_ context.Context, s *state.Impl) error {
	s.Issue = models.ForgejoIssue{
		Number:    s.Job.IssueNumber,
		Title:     s.Job.IssueTitle,
		Body:      s.Job.IssueBody,
		RepoOwner: s.Job.RepoOwner,
		RepoName:  s.Job.RepoName,
	}
	s.Branch = fmt.Sprintf("feature/%d-%s", s.Job.IssueNumber, branchSlug(s.Job.IssueTitle))
	return nil
}

type FetchComments struct {
	Fj forgejo.Forgejo
}

func (p FetchComments) Run(ctx context.Context, s *state.Impl) error {
	comments, err := p.Fj.GetComments(s.Job.RepoOwner, s.Job.RepoName, s.Job.IssueNumber)
	if err != nil {
		slog.WarnContext(ctx, "fetch comments failed",
			"issue", s.Job.IssueNumber,
			"error", err,
		)
		s.Comments = nil
		return nil
	}
	s.Comments = comments
	return nil
}

type MarkCommentsSeen struct{}

func (MarkCommentsSeen) Run(_ context.Context, s *state.Impl) error {
	s.Handle.MarkCommentsSeen(s.Comments)
	return nil
}

type PrepareBranch struct {
	GitFactory   gitops.Factory
	ForgejoURL   string
	ForgejoToken string
	RepoOwner    string
	RepoName     string
	BaseBranch   string
}

func (p PrepareBranch) Run(_ context.Context, s *state.Impl) error {
	git := p.GitFactory(s.ProjectPath, p.ForgejoURL, p.ForgejoToken, p.RepoOwner, p.RepoName)
	s.Git = git
	if err := git.Checkout(p.BaseBranch); err != nil {
		return fmt.Errorf("checkout base: %w", err)
	}
	if err := git.CreateBranch(s.Branch); err != nil {
		return fmt.Errorf("create branch: %w", err)
	}
	return nil
}

type BeginAttempt struct{}

func (BeginAttempt) Run(ctx context.Context, s *state.Impl) error {
	s.Attempt++
	s.Handle.SetAttempt(ctx, s.Attempt)
	s.Handle.SetPhase(ctx, runstore.PhaseImplementing)
	return nil
}

type RefreshFreshComments struct {
	Fj forgejo.Forgejo
}

func (p RefreshFreshComments) Run(ctx context.Context, s *state.Impl) error {
	latest, err := p.Fj.GetComments(s.Job.RepoOwner, s.Job.RepoName, s.Job.IssueNumber)
	if err != nil {
		slog.WarnContext(ctx, "fetch comments for impl failed, skipping delta",
			"issue", s.Job.IssueNumber,
			"error", err,
		)
		s.FreshComments = nil
		return nil
	}
	s.FreshComments = s.Handle.FreshComments(latest)
	s.Comments = latest
	return nil
}

type ImplementAttempt struct {
	Claude agent.Agent
}

func (p ImplementAttempt) Run(ctx context.Context, s *state.Impl) error {
	notes := s.Handle.DrainNotes()
	ictx, cancel := s.Handle.Interruptible(ctx)
	defer cancel()
	impl, err := p.Claude.ImplementIssue(ictx, s.Issue, s.FreshComments, s.ProjectPath, "", withNotes(s.TestError, notes), s.Handle.Sink())
	s.Handle.AddUsage(ctx, impl.Usage)
	if err == nil {
		s.ImplOutput = impl.Output
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	pending := s.Handle.DrainNotes()
	if len(pending) == 0 {
		return fmt.Errorf("%w: claude impl: %w", step.ErrUpstream, err)
	}
	s.Handle.PrependNotes(pending)
	s.Attempt--
	return nil
}

type CheckHasChanges struct{}

func (CheckHasChanges) Run(_ context.Context, s *state.Impl) error {
	changed, err := s.Git.HasChanges()
	if err != nil {
		return fmt.Errorf("check changes: %w", err)
	}
	if !changed {
		return pipeline.ErrNoChanges
	}
	return nil
}

type RunTests struct {
	Runner func(projectPath string) (bool, string)
}

func (p RunTests) Run(ctx context.Context, s *state.Impl) error {
	s.Handle.SetPhase(ctx, runstore.PhaseTesting)
	ok, tErr := p.Runner(s.ProjectPath)
	if ok {
		s.TestError = nil
		return nil
	}
	truncated := tErr
	if len(truncated) > 200 {
		truncated = truncated[:200]
	}
	slog.WarnContext(ctx, "tests failed",
		"attempt", s.Attempt,
		"issue", s.Job.IssueNumber,
		"error", truncated,
	)
	s.TestError = &tErr
	return nil
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func branchSlug(title string) string {
	slug := slugRe.ReplaceAllString(strings.ToLower(title), "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 50 {
		slug = slug[:50]
	}
	return slug
}

func withNotes(testError *string, notes []string) *string {
	if len(notes) == 0 {
		return testError
	}
	combined := "Additional instructions from user:\n" + strings.Join(notes, "\n---\n")
	if testError != nil {
		combined += "\n\n" + *testError
	}
	return &combined
}
