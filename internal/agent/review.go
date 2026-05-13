package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"ai-orchestration/internal/events"
	"ai-orchestration/internal/models"
)

var errEmptyReview = errors.New("empty review output")

const claudeReviewPrompt = `Review this pull request diff. Be concise and direct.

PR: %s
Issue: #%d

Diff:
` + "```diff\n%s\n```" + `

Respond in this exact format:
SUMMARY: <one paragraph>
APPROVED: <yes or no>
ISSUES:
- <blocking issue, omit section if none>
SUGGESTIONS:
- <non-blocking suggestion, omit section if none>`

type ClaudeReviewer struct {
	timeout time.Duration
	cwd     string
}

func NewReviewer(timeout time.Duration, cwd string) *ClaudeReviewer {
	return &ClaudeReviewer{timeout: timeout, cwd: cwd}
}

func (c *ClaudeReviewer) Review(ctx context.Context, job models.ReviewJob, sink events.Sink) (models.ReviewResult, error) {
	slog.InfoContext(ctx, "requesting claude review",
		"pr_number", job.PRNumber,
	)

	cctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	prompt := fmt.Sprintf(claudeReviewPrompt, job.PRTitle, job.IssueNumber, job.Diff)
	out, err := runClaudeStream(cctx, c.cwd, prompt, []string{
		"--print",
		"--dangerously-skip-permissions",
		"--output-format", "stream-json",
		"--verbose",
		"--allowedTools", "Bash,Read,Glob,Grep",
	}, sink)
	if err != nil {
		return models.ReviewResult{}, fmt.Errorf("claude review: %w", err)
	}
	if out.Result == "" {
		return models.ReviewResult{}, errEmptyReview
	}
	return models.ParseReviewText(out.Result), nil
}
