package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"ai-orchestration/internal/models"
)

const claudePRDescPrompt = `/pr-description

Issue #%d: %s

%s

The diff for this branch is already pushed. Use the skill to produce the PR description markdown.
Output only the final markdown body, no preamble.`

type ClaudePRDescWriter struct {
	timeout time.Duration
}

func NewPRDescWriter(timeout time.Duration) *ClaudePRDescWriter {
	return &ClaudePRDescWriter{timeout: timeout}
}

func (c *ClaudePRDescWriter) Generate(ctx context.Context, issue models.ForgejoIssue, projectPath, _ string) (string, error) {
	slog.InfoContext(ctx, "generating PR description via claude",
		"issue", issue.Number,
		"path", projectPath,
	)

	cctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	prompt := fmt.Sprintf(claudePRDescPrompt, issue.Number, issue.Title, issue.Body)

	out, err := runClaudeStream(cctx, projectPath, prompt, []string{
		"--print",
		"--dangerously-skip-permissions",
		"--output-format", "stream-json",
		"--verbose",
		"--allowedTools", "Bash,Read,Glob,Grep",
	})
	if err != nil {
		return "", fmt.Errorf("claude pr desc: %w", err)
	}
	if out == "" {
		return "", fmt.Errorf("claude pr desc: empty output")
	}
	return out, nil
}
