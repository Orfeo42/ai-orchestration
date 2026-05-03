package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"ai-orchestration/internal/agent"
	"ai-orchestration/internal/events"
	"ai-orchestration/internal/gemini"
	"ai-orchestration/internal/gitops"
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/pipeline"
	"ai-orchestration/internal/pipeline/checkout"
	"ai-orchestration/internal/pipeline/review"
	"ai-orchestration/internal/pipeline/state"
	"ai-orchestration/internal/queue"
	"ai-orchestration/internal/runstore"
	"ai-orchestration/internal/runtime"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func runReviewConsumer(ctx context.Context, deps consumerDeps) {
	claudeReviewer := agent.NewReviewer(deps.cfg.claudeTimeout, deps.cfg.projectsRoot)
	claudeDocs := agent.NewDocsWriter(deps.cfg.claudeTimeout)

	var reviewer gemini.Reviewer = claudeReviewer
	var docsWriter gemini.DocsWriter = claudeDocs

	if deps.cfg.geminiAPIKey != nil {
		gemReviewer, err := gemini.NewReviewer(ctx, *deps.cfg.geminiAPIKey)
		if err != nil {
			runtime.Die("reviewer", err)
		}
		defer gemReviewer.Close()

		gemDocs, err := gemini.NewDocsWriter(ctx, *deps.cfg.geminiAPIKey)
		if err != nil {
			runtime.Die("docs-writer", err)
		}
		defer gemDocs.Close()

		reviewer = fallbackReviewer{primary: gemReviewer, secondary: claudeReviewer}
		docsWriter = fallbackDocsWriter{primary: gemDocs, secondary: claudeDocs}
	} else {
		slog.InfoContext(ctx, "GEMINI_API_KEY not set, using claude for review pipeline")
	}

	steps := []pipeline.Processor[state.Review]{
		checkout.ReviewCheckout{
			GitFactory:   gitops.DefaultFactory,
			ForgejoURL:   deps.cfg.forgejoURL,
			ForgejoToken: deps.cfg.forgejoToken,
			ProjectsRoot: deps.cfg.projectsRoot,
			WorkerID:     deps.cfg.workerID,
		},
		review.ReviewPR{Reviewer: reviewer},
		review.CheckoutPRBranch{
			GitFactory:   gitops.DefaultFactory,
			ForgejoURL:   deps.cfg.forgejoURL,
			ForgejoToken: deps.cfg.forgejoToken,
		},
		review.UpdateReadme{DocsWriter: docsWriter},
		review.PostReview{Fj: deps.fj},
	}

	slog.InfoContext(ctx, "review consumer started")
	for {
		if ctx.Err() != nil {
			slog.InfoContext(ctx, "review consumer shutting down")
			return
		}
		raw, err := deps.rq.Pop(ctx, queue.QueueReview, 10*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.ErrorContext(ctx, "pop review job failed",
				"error", err,
			)
			continue
		}
		if raw == nil {
			continue
		}

		var job models.ReviewJob
		if err := json.Unmarshal(raw, &job); err != nil {
			slog.ErrorContext(ctx, "unmarshal review job failed",
				"error", err,
			)
			continue
		}
		handleReviewJob(ctx, deps, steps, job)
	}
}

func handleReviewJob(ctx context.Context, deps consumerDeps, steps []pipeline.Processor[state.Review], job models.ReviewJob) {
	proj, ok := deps.cfg.projects[projectKey(job.Project)]
	if !ok {
		slog.ErrorContext(ctx, "unknown project in review job",
			"project", job.Project,
			"pr", job.PRNumber,
		)
		return
	}

	payload, err := json.Marshal(job)
	if err != nil {
		slog.ErrorContext(ctx, "marshal review job payload failed",
			"pr", job.PRNumber,
			"error", err,
		)
		return
	}
	handle := runstore.NewRunHandle(ctx, deps.store, deps.ctrl, runstore.Run{
		Service:    runstore.ServiceReview,
		RepoOwner:  job.RepoOwner,
		RepoName:   job.RepoName,
		PRNumber:   job.PRNumber,
		Title:      job.PRTitle,
		Phase:      runstore.PhaseReviewing,
		JobPayload: payload,
	})
	defer handle.Cancel()

	s := state.Review{Job: job, Proj: proj, Handle: handle}
	err = pipeline.Execute(handle.Ctx(), &s, steps)
	switch {
	case errors.Is(err, context.Canceled):
		handle.Finalize(ctx, runstore.PhaseCancelled, nil)
	case err != nil:
		slog.ErrorContext(ctx, "review pipeline failed",
			"pr", job.PRNumber,
			"error", err,
		)
		handle.Finalize(ctx, runstore.PhaseFailed, err)
	default:
		handle.Finalize(ctx, runstore.PhaseDone, nil)
	}
}

type fallbackReviewer struct {
	primary   gemini.Reviewer
	secondary gemini.Reviewer
}

func (r fallbackReviewer) Review(ctx context.Context, job models.ReviewJob, sink events.Sink) (models.ReviewResult, error) {
	res, err := r.primary.Review(ctx, job, sink)
	if err == nil || !isQuotaErr(err) {
		return res, err
	}
	slog.WarnContext(ctx, "gemini reviewer quota exhausted, falling back to claude",
		"error", err,
	)
	return r.secondary.Review(ctx, job, sink)
}

type fallbackDocsWriter struct {
	primary   gemini.DocsWriter
	secondary gemini.DocsWriter
}

func (d fallbackDocsWriter) UpdateReadme(ctx context.Context, job models.ReviewJob, projectPath string, review models.ReviewResult, sink events.Sink) (bool, error) {
	updated, err := d.primary.UpdateReadme(ctx, job, projectPath, review, sink)
	if err == nil || !isQuotaErr(err) {
		return updated, err
	}
	slog.WarnContext(ctx, "gemini docs writer quota exhausted, falling back to claude",
		"error", err,
	)
	return d.secondary.UpdateReadme(ctx, job, projectPath, review, sink)
}

func isQuotaErr(err error) bool {
	if err == nil {
		return false
	}
	if s, ok := status.FromError(err); ok && s.Code() == codes.ResourceExhausted {
		return true
	}
	msg := err.Error()
	if strings.Contains(msg, "RESOURCE_EXHAUSTED") {
		return true
	}
	if strings.Contains(strings.ToLower(msg), "quota") {
		return true
	}
	if strings.Contains(msg, " 429") || strings.Contains(msg, "(429)") {
		return true
	}
	return false
}
