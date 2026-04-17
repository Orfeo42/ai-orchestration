package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"ai-orchestration/internal/gemini"
	"ai-orchestration/internal/gitops"
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/pipeline"
	"ai-orchestration/internal/pipeline/review"
	"ai-orchestration/internal/pipeline/state"
	"ai-orchestration/internal/queue"
	"ai-orchestration/internal/runstore"
	"ai-orchestration/internal/runtime"
)

func runReviewConsumer(ctx context.Context, deps consumerDeps) {
	if deps.cfg.geminiAPIKey == nil {
		runtime.Die("gemini", errMissingGeminiKey{})
	}

	reviewer, err := gemini.NewReviewer(ctx, *deps.cfg.geminiAPIKey)
	if err != nil {
		runtime.Die("reviewer", err)
	}
	defer reviewer.Close()

	docsWriter, err := gemini.NewDocsWriter(ctx, *deps.cfg.geminiAPIKey)
	if err != nil {
		runtime.Die("docs-writer", err)
	}
	defer docsWriter.Close()

	steps := []pipeline.Processor[state.Review]{
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
	handle := runstore.NewRunHandle(ctx, deps.store, deps.ctrl, runstore.Run{
		Service:   runstore.ServiceReview,
		RepoOwner: job.RepoOwner,
		RepoName:  job.RepoName,
		PRNumber:  job.PRNumber,
		Title:     job.PRTitle,
		Phase:     runstore.PhaseReviewing,
	})
	defer handle.Cancel()

	s := state.Review{Job: job, Handle: handle}
	err := pipeline.Execute(handle.Ctx(), &s, steps)
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

type errMissingGeminiKey struct{}

func (errMissingGeminiKey) Error() string { return "GEMINI_API_KEY required for review pipeline" }
