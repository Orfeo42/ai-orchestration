package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"ai-orchestration/internal/control"
	"ai-orchestration/internal/events"
	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/gemini"
	"ai-orchestration/internal/gitops"
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/runstore"
)

type reviewDeps struct {
	reviewer   gemini.Reviewer
	docsWriter gemini.DocsWriter
	fj         forgejo.Forgejo
	gitFactory gitops.Factory
	store      runstore.Store
	ctrl       control.Channel
}

type reviewerHandle struct {
	id     string
	sink   events.Sink
	store  runstore.Store
	cancel func()
}

func (h *reviewerHandle) emit(ctx context.Context, evt events.Event) {
	if h.sink == nil {
		return
	}
	if err := h.sink.Emit(ctx, evt); err != nil {
		slog.WarnContext(ctx, "reviewer sink emit failed",
			"error", err,
		)
	}
}

func (h *reviewerHandle) setPhase(ctx context.Context, phase runstore.Phase) {
	if h.store != nil {
		if err := h.store.Update(ctx, h.id, func(r *runstore.Run) {
			r.Phase = phase
		}); err != nil {
			slog.WarnContext(ctx, "update reviewer phase failed",
				"run", h.id,
				"error", err,
			)
		}
	}
	h.emit(ctx, events.Event{Kind: events.KindPhase, Phase: string(phase)})
}

func (h *reviewerHandle) finalize(ctx context.Context, phase runstore.Phase, finalErr error) {
	if h.store == nil {
		return
	}
	errStr := ""
	if finalErr != nil {
		errStr = finalErr.Error()
	}
	if err := h.store.Finalize(ctx, h.id, phase, errStr); err != nil {
		slog.WarnContext(ctx, "finalize reviewer run failed",
			"run", h.id,
			"error", err,
		)
	}
	h.emit(ctx, events.Event{Kind: events.KindRunEnd, Phase: string(phase)})
}

func startReviewerHandle(ctx context.Context, deps reviewDeps, job models.ReviewJob) (*reviewerHandle, context.Context, context.CancelFunc) {
	runID := uuid.NewString()
	h := &reviewerHandle{id: runID, sink: events.NoopSink{}}

	if deps.store != nil {
		run := runstore.Run{
			ID:        runID,
			Service:   runstore.ServiceReviewer,
			RepoOwner: job.RepoOwner,
			RepoName:  job.RepoName,
			PRNumber:  job.PRNumber,
			Title:     job.PRTitle,
			Phase:     runstore.PhaseReviewing,
			StartedAt: time.Now().UTC(),
		}
		if err := deps.store.Create(ctx, run); err != nil {
			slog.WarnContext(ctx, "create reviewer run failed",
				"error", err,
			)
		} else {
			h.store = deps.store
			h.sink = deps.store.AppendSink(runID)
		}
	}

	runCtx, cancel := context.WithCancel(ctx)
	h.cancel = cancel

	if deps.ctrl != nil {
		sub, err := deps.ctrl.Subscribe(runCtx, runID)
		if err != nil {
			slog.WarnContext(ctx, "reviewer ctrl subscribe failed",
				"run", runID,
				"error", err,
			)
		} else {
			go func() {
				defer func() {
					if cerr := sub.Close(); cerr != nil {
						slog.WarnContext(runCtx, "reviewer ctrl close failed",
							"run", runID,
							"error", cerr,
						)
					}
				}()
				for {
					select {
					case <-runCtx.Done():
						return
					case msg, ok := <-sub.Messages():
						if !ok {
							return
						}
						if msg.Type == control.TypeCancel {
							h.emit(runCtx, events.Event{Kind: events.KindCancel})
							cancel()
							return
						}
					}
				}
			}()
		}
	}

	h.emit(runCtx, events.Event{
		Kind:    events.KindRunStart,
		Service: string(runstore.ServiceReviewer),
	})

	return h, runCtx, cancel
}

func runPipeline(ctx context.Context, job models.ReviewJob, deps reviewDeps, cfg config) (resErr error) {
	handle, runCtx, cancel := startReviewerHandle(ctx, deps, job)
	defer cancel()
	defer func() {
		switch {
		case resErr != nil && ctx.Err() != nil:
			handle.finalize(ctx, runstore.PhaseCancelled, nil)
		case resErr != nil:
			handle.finalize(ctx, runstore.PhaseFailed, resErr)
		default:
			handle.finalize(ctx, runstore.PhaseDone, nil)
		}
	}()

	handle.setPhase(runCtx, runstore.PhaseReviewing)
	result, err := deps.reviewer.Review(runCtx, job, handle.sink)
	if err != nil {
		return fmt.Errorf("review: %w", err)
	}
	slog.InfoContext(runCtx, "reviewed PR",
		"pr", job.PRNumber,
		"approved", result.Approved,
	)

	git := deps.gitFactory(job.ProjectPath, cfg.forgejoURL, cfg.forgejoToken, job.RepoOwner, job.RepoName)
	if err := git.Fetch(); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	if err := git.CheckoutRemoteBranch(job.PRBranch); err != nil {
		return fmt.Errorf("checkout branch: %w", err)
	}

	updated, err := deps.docsWriter.UpdateReadme(runCtx, job, result, handle.sink)
	if err != nil {
		return fmt.Errorf("update readme: %w", err)
	}
	if updated {
		if err := git.Add("README.md"); err != nil {
			return fmt.Errorf("git add: %w", err)
		}
		if err := git.Commit(fmt.Sprintf("docs: update README for PR #%d", job.PRNumber)); err != nil {
			return fmt.Errorf("git commit: %w", err)
		}
		if err := git.Push(job.PRBranch); err != nil {
			return fmt.Errorf("git push: %w", err)
		}
		slog.InfoContext(runCtx, "README updated",
			"pr", job.PRNumber,
		)
	}

	if err := deps.fj.CreatePRReview(job.RepoOwner, job.RepoName, job.PRNumber, buildReviewBody(result), result.Approved); err != nil {
		return fmt.Errorf("post review: %w", err)
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
