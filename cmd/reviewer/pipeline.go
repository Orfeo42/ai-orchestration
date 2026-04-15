package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/gemini"
	"ai-orchestration/internal/gitops"
	"ai-orchestration/internal/models"
)

type reviewDeps struct {
	reviewer   gemini.Reviewer
	docsWriter gemini.DocsWriter
	fj         forgejo.Forgejo
	gitFactory gitops.Factory
}

func runPipeline(ctx context.Context, job models.ReviewJob, deps reviewDeps, cfg config) error {
	result, err := deps.reviewer.Review(ctx, job)
	if err != nil {
		return fmt.Errorf("review: %w", err)
	}
	slog.InfoContext(ctx, "reviewed PR",
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

	updated, err := deps.docsWriter.UpdateReadme(ctx, job, result)
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
		slog.InfoContext(ctx, "README updated",
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
