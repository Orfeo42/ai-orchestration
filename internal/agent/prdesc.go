package agent

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
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

	cmd := exec.CommandContext(cctx, "claude",
		"--print",
		"--dangerously-skip-permissions",
		"--allowedTools", "Bash,Read,Glob,Grep",
	)
	cmd.Dir = projectPath
	cmd.Stdin = strings.NewReader(prompt)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude pr desc: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}

	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return "", fmt.Errorf("claude pr desc: empty output")
	}
	return out, nil
}
