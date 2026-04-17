package main

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/queue"
)

const inProgressTTL = 2 * time.Hour

type producerDeps struct {
	fj forgejo.Forgejo
	rq queue.Queue
}

func pollRepo(ctx context.Context, owner, repo string, deps producerDeps, cfg config) {
	issues, err := deps.fj.GetIssues(owner, repo, forgejo.LabelImplement, cfg.forgejoAssignee)
	if err != nil {
		slog.ErrorContext(ctx, "fetch issues failed",
			"repo", owner+"/"+repo,
			"error", err,
		)
		return
	}

	for _, issue := range issues {
		if ctx.Err() != nil {
			return
		}
		if !shouldEnqueue(ctx, issue, owner, repo, deps, cfg) {
			continue
		}

		projectName := projectLabel(issue.Labels)
		proj := cfg.projects[projectName]

		job := models.AssessJob{
			RepoOwner:   owner,
			RepoName:    repo,
			IssueNumber: issue.Number,
			IssueTitle:  issue.Title,
			IssueBody:   issue.Body,
			Project:     proj.Repo,
			Labels:      issue.Labels,
		}

		key := issueKey(owner, repo, issue.Number)
		if err := deps.rq.MarkInProgress(ctx, key, inProgressTTL); err != nil {
			slog.ErrorContext(ctx, "mark in-progress failed",
				"key", key,
				"error", err,
			)
			continue
		}
		if err := deps.rq.Push(ctx, queue.QueueAssess, job); err != nil {
			slog.ErrorContext(ctx, "enqueue assess job failed",
				"key", key,
				"error", err,
			)
			if cerr := deps.rq.ClearInProgress(ctx, key); cerr != nil {
				slog.WarnContext(ctx, "clear in-progress after push failure failed",
					"key", key,
					"error", cerr,
				)
			}
			continue
		}
		slog.InfoContext(ctx, "enqueued assess job",
			"issue", issue.Number,
			"project", proj.Repo,
		)
	}
}

func shouldEnqueue(ctx context.Context, issue models.ForgejoIssue, owner, repo string, deps producerDeps, cfg config) bool {
	key := issueKey(owner, repo, issue.Number)

	processed, err := deps.rq.IsProcessed(ctx, key)
	if err != nil {
		slog.ErrorContext(ctx, "check processed failed",
			"key", key,
			"error", err,
		)
		return false
	}
	if processed || containsLabel(issue.Labels, forgejo.LabelDone) {
		return false
	}
	if containsLabel(issue.Labels, forgejo.LabelNeedsInfo) {
		slog.InfoContext(ctx, "issue waiting on author, skipping",
			"issue", issue.Number,
		)
		return false
	}

	projectName := projectLabel(issue.Labels)
	if projectName == "" {
		slog.WarnContext(ctx, "issue has no project label, skipping",
			"issue", issue.Number,
		)
		return false
	}
	if _, ok := cfg.projects[projectName]; !ok {
		slog.ErrorContext(ctx, "unknown project",
			"project", projectName,
			"issue", issue.Number,
		)
		return false
	}

	inProgress, err := deps.rq.IsInProgress(ctx, key)
	if err != nil {
		slog.ErrorContext(ctx, "check in-progress failed",
			"key", key,
			"error", err,
		)
		return false
	}
	return !inProgress
}

func issueKey(owner, repo string, number int) string {
	return fmt.Sprintf("%s/%s#%d", owner, repo, number)
}

func containsLabel(labels []string, target string) bool {
	return slices.Contains(labels, target)
}

func projectLabel(labels []string) string {
	for _, l := range labels {
		if after, ok := strings.CutPrefix(l, forgejo.LabelProjectPrefix); ok {
			return after
		}
	}
	return ""
}
