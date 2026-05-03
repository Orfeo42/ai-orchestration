package impl

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"ai-orchestration/internal/agent"
	"ai-orchestration/internal/events"
	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/gitops"
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/pipeline"
	"ai-orchestration/internal/pipeline/state"
	"ai-orchestration/internal/queue"
	"ai-orchestration/internal/runstore"
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
	impl, err := p.Claude.ImplementIssue(ctx, s.Issue, s.FreshComments, s.ProjectPath, "", withNotes(s.TestError, notes), s.Handle.Sink())
	if err != nil {
		if pending := s.Handle.DrainNotes(); len(pending) > 0 || ctx.Err() != nil {
			s.Handle.PrependNotes(pending)
			s.Handle.AddUsage(ctx, impl.Usage)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			s.Attempt--
			return nil
		}
		return fmt.Errorf("claude impl: %w", err)
	}
	s.Handle.AddUsage(ctx, impl.Usage)
	s.ImplOutput = impl.Output
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

type OpenPR struct {
	Fj           forgejo.Forgejo
	Rq           queue.Queue
	PrWriter     PRDescWriter
	ForgejoURL   string
	CodeOwner    string
	CodeName     string
	BaseBranch   string
}

func (p OpenPR) Run(ctx context.Context, s *state.Impl) error {
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
		return fmt.Errorf("create pr: %w", err)
	}
	s.PR = &pr
	if s.Handle.Store() != nil {
		if uerr := s.Handle.Store().Update(ctx, s.Handle.ID(), func(r *runstore.Run) { r.PRNumber = pr.Number }); uerr != nil {
			slog.WarnContext(ctx, "update run pr number failed",
				"run", s.Handle.ID(),
				"error", uerr,
			)
		}
	}

	if p.PrWriter != nil {
		p.writePRDescription(ctx, s, pr)
	}
	p.finalizeLabels(ctx, s)
	return p.queueReview(ctx, s, pr)
}

func (p OpenPR) writePRDescription(ctx context.Context, s *state.Impl, pr models.ForgejoPR) {
	diff, err := p.Fj.GetPRDiff(p.CodeOwner, p.CodeName, pr.Number)
	if err != nil {
		slog.WarnContext(ctx, "get pr diff failed",
			"error", err,
		)
		return
	}
	if diff == "" {
		return
	}
	s.Handle.SetPhase(ctx, runstore.PhasePRDesc)
	desc, err := p.PrWriter.Generate(ctx, s.Issue, s.ProjectPath, diff, s.Handle.Sink())
	if err != nil {
		slog.WarnContext(ctx, "pr description generation failed",
			"error", err,
		)
		return
	}
	body := fmt.Sprintf("relates to: %s/%s#%d\n\n%s", s.Job.RepoOwner, s.Job.RepoName, s.Job.IssueNumber, desc)
	if err := p.Fj.UpdatePR(p.CodeOwner, p.CodeName, pr.Number, body); err != nil {
		slog.WarnContext(ctx, "update pr body failed",
			"error", err,
		)
	}
}

func (p OpenPR) finalizeLabels(ctx context.Context, s *state.Impl) {
	if doneID, err := p.Fj.GetLabelID(s.Job.RepoOwner, s.Job.RepoName, forgejo.LabelDone); err == nil && doneID != nil {
		if err := p.Fj.AddLabel(s.Job.RepoOwner, s.Job.RepoName, s.Job.IssueNumber, *doneID); err != nil {
			slog.WarnContext(ctx, "add done label failed",
				"error", err,
			)
		}
	}
	if ipID, err := p.Fj.GetLabelID(s.Job.RepoOwner, s.Job.RepoName, forgejo.LabelInProgress); err == nil && ipID != nil {
		if err := p.Fj.RemoveLabel(s.Job.RepoOwner, s.Job.RepoName, s.Job.IssueNumber, *ipID); err != nil {
			slog.WarnContext(ctx, "remove in-progress label failed",
				"error", err,
			)
		}
	}
}

func (p OpenPR) queueReview(ctx context.Context, s *state.Impl, pr models.ForgejoPR) error {
	diff, _ := p.Fj.GetPRDiff(p.CodeOwner, p.CodeName, pr.Number)
	job := models.ReviewJob{
		RepoOwner:   p.CodeOwner,
		RepoName:    p.CodeName,
		PRNumber:    pr.Number,
		PRTitle:     "feat: " + s.Job.IssueTitle,
		PRBranch:    s.Branch,
		Project:     s.Job.Project,
		IssueNumber: s.Job.IssueNumber,
		Diff:        diff,
	}
	if err := p.Rq.Push(ctx, queue.QueueReview, job); err != nil {
		return fmt.Errorf("push review job: %w", err)
	}
	if err := s.Git.Checkout(p.BaseBranch); err != nil {
		slog.WarnContext(ctx, "checkout base after PR failed",
			"error", err,
		)
	}
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

// Ensure loop terminates after maxAttempts with a meaningful error if tests never pass.
var _ = errors.New
