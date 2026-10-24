package assess

import (
	"context"
	"fmt"
	"log/slog"

	"ai-orchestration/internal/agent"
	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/pipeline"
	"ai-orchestration/internal/pipeline/state"
	"ai-orchestration/internal/runstore"
	"ai-orchestration/internal/step"
)

type HydrateIssue struct{}

func (HydrateIssue) Run(_ context.Context, s *state.Assess) error {
	s.Issue = models.ForgejoIssue{
		Number:    s.Job.IssueNumber,
		Title:     s.Job.IssueTitle,
		Body:      s.Job.IssueBody,
		RepoOwner: s.Job.RepoOwner,
		RepoName:  s.Job.RepoName,
		Labels:    s.Job.Labels,
	}
	return nil
}

type FetchComments struct {
	Fj forgejo.Forgejo
}

func (p FetchComments) Run(ctx context.Context, s *state.Assess) error {
	comments, err := p.Fj.GetComments(s.Job.RepoOwner, s.Job.RepoName, s.Job.IssueNumber)
	if err != nil {
		slog.WarnContext(ctx, "fetch comments failed, assessing without them",
			"issue", s.Job.IssueNumber,
			"error", err,
		)
		s.Comments = nil
		return nil
	}
	s.Comments = comments
	return nil
}

type AssessClarity struct {
	Claude       agent.Agent
	ProjectsRoot string
	WorkerID     string
}

func (p AssessClarity) Run(ctx context.Context, s *state.Assess) error {
	s.Handle.SetPhase(ctx, runstore.PhaseAssessing)
	result, err := p.Claude.AssessIssue(ctx, s.Issue, s.Comments, s.Proj.Path(p.ProjectsRoot, p.WorkerID), s.Handle.Sink())
	if err != nil {
		return fmt.Errorf("%w: assess issue: %w", step.ErrUpstream, err)
	}
	s.Handle.AddUsage(ctx, result.Usage)
	s.Handle.SetSession(ctx, result.SessionID)
	s.Handle.MarkCommentsSeen(s.Comments)
	s.Result = result
	if !result.Assessment.Clear {
		return pipeline.ErrIssueUnclear
	}
	return nil
}
