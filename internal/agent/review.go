package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Orfeo42/ai-orchestration/internal/events"
	"github.com/Orfeo42/ai-orchestration/internal/models"
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
	rt  Runtime
	cwd string
}

func NewReviewer(rt Runtime, cwd string) *ClaudeReviewer {
	return &ClaudeReviewer{rt: rt, cwd: cwd}
}

func (c *ClaudeReviewer) Review(ctx context.Context, job models.ReviewJob, sink events.Sink) (models.ReviewResult, error) {
	slog.InfoContext(ctx, "requesting claude review",
		"pr_number", job.PRNumber,
	)

	cctx, cancel := context.WithTimeout(ctx, c.rt.Timeout)
	defer cancel()

	prompt := fmt.Sprintf(claudeReviewPrompt, job.PRTitle, job.IssueNumber, job.Diff)
	out, err := runClaudeStream(cctx, c.cwd, prompt, baseArgs(c.rt, "Bash,Read,Glob,Grep"), sink)
	if err != nil {
		return models.ReviewResult{}, fmt.Errorf("claude review: %w", err)
	}
	if out.Result == "" {
		return models.ReviewResult{}, errEmptyReview
	}
	return models.ParseReviewText(out.Result), nil
}
