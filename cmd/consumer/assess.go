package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/pipeline"
	"ai-orchestration/internal/pipeline/assess"
	"ai-orchestration/internal/pipeline/state"
	"ai-orchestration/internal/queue"
	"ai-orchestration/internal/runstore"
)

func runAssessConsumer(ctx context.Context, deps consumerDeps) {
	steps := []pipeline.Processor[state.Assess]{
		assess.HydrateIssue{},
		assess.FetchComments{Fj: deps.fj},
		assess.AssessClarity{Claude: deps.claude, ProjectsRoot: deps.cfg.projectsRoot},
		assess.EnqueueImpl{Rq: deps.rq},
	}

	slog.InfoContext(ctx, "assess consumer started")
	for {
		if ctx.Err() != nil {
			slog.InfoContext(ctx, "assess consumer shutting down")
			return
		}
		raw, err := deps.rq.Pop(ctx, queue.QueueAssess, 10*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.ErrorContext(ctx, "pop assess job failed",
				"error", err,
			)
			continue
		}
		if raw == nil {
			continue
		}

		var job models.AssessJob
		if err := json.Unmarshal(raw, &job); err != nil {
			slog.ErrorContext(ctx, "unmarshal assess job failed",
				"error", err,
			)
			continue
		}

		handleAssessJob(ctx, deps, steps, job)
	}
}

func handleAssessJob(ctx context.Context, deps consumerDeps, steps []pipeline.Processor[state.Assess], job models.AssessJob) {
	proj, ok := deps.cfg.projects[projectKey(job.Project)]
	if !ok {
		slog.ErrorContext(ctx, "unknown project in assess job",
			"project", job.Project,
			"issue", job.IssueNumber,
		)
		return
	}

	handle := runstore.NewRunHandle(ctx, deps.store, deps.ctrl, runstore.Run{
		Service:     runstore.ServiceAssess,
		RepoOwner:   job.RepoOwner,
		RepoName:    job.RepoName,
		IssueNumber: job.IssueNumber,
		Title:       job.IssueTitle,
		Project:     job.Project,
		Phase:       runstore.PhaseAssessing,
	})
	defer handle.Cancel()

	s := state.Assess{Job: job, Proj: proj, Handle: handle}
	err := pipeline.Execute(handle.Ctx(), &s, steps)
	key := fmt.Sprintf("%s/%s#%d", job.RepoOwner, job.RepoName, job.IssueNumber)

	switch {
	case errors.Is(err, pipeline.ErrIssueUnclear):
		if uerr := postClarification(ctx, deps.fj, job, s.Result.Assessment.Questions); uerr != nil {
			slog.WarnContext(ctx, "post clarification failed",
				"issue", job.IssueNumber,
				"error", uerr,
			)
		}
		handle.Finalize(ctx, runstore.PhaseDone, nil)
		if err := deps.rq.ClearInProgress(ctx, key); err != nil {
			slog.WarnContext(ctx, "clear in-progress failed",
				"key", key,
				"error", err,
			)
		}
	case err != nil:
		slog.ErrorContext(ctx, "assess pipeline failed",
			"issue", job.IssueNumber,
			"error", err,
		)
		handle.Finalize(ctx, runstore.PhaseFailed, err)
		if err := deps.rq.ClearInProgress(ctx, key); err != nil {
			slog.WarnContext(ctx, "clear in-progress failed",
				"key", key,
				"error", err,
			)
		}
	default:
		handle.Finalize(ctx, runstore.PhaseDone, nil)
	}
}

func postClarification(ctx context.Context, fj forgejo.Forgejo, job models.AssessJob, questions string) error {
	body := "Requirements unclear, need clarification before implementation:\n\n" + questions
	if err := fj.CreateComment(job.RepoOwner, job.RepoName, job.IssueNumber, body); err != nil {
		return fmt.Errorf("create clarification comment: %w", err)
	}
	if labelID, err := fj.GetLabelID(job.RepoOwner, job.RepoName, forgejo.LabelNeedsInfo); err == nil && labelID != nil {
		if err := fj.AddLabel(job.RepoOwner, job.RepoName, job.IssueNumber, *labelID); err != nil {
			slog.WarnContext(ctx, "add needs-info label failed",
				"error", err,
			)
		}
	}
	return nil
}

func projectKey(repo string) string {
	for i := 0; i < len(repo); i++ {
		if repo[i] == '/' {
			return repo[i+1:]
		}
	}
	return repo
}
