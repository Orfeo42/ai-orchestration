package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"ai-orchestration/internal/events"
	"ai-orchestration/internal/models"
)

var errEmptyDocs = errors.New("empty docs output")

const claudeDocsPrompt = `Update the README.md for this project based on the changes in a merged PR.

PR: %s
Summary of changes: %s

Current README:
%s

Diff of changes (truncated):
` + "```diff\n%s\n```" + `

Return ONLY the updated README content, no preamble.
If no meaningful update is needed, return the original content unchanged.`

type ClaudeDocsWriter struct {
	rt Runtime
}

func NewDocsWriter(rt Runtime) *ClaudeDocsWriter {
	return &ClaudeDocsWriter{rt: rt}
}

func (d *ClaudeDocsWriter) UpdateReadme(ctx context.Context, job models.ReviewJob, projectPath string, review models.ReviewResult, sink events.Sink) (bool, error) {
	readmePath := filepath.Join(projectPath, "README.md")
	existing := ""
	if data, err := os.ReadFile(readmePath); err == nil {
		existing = string(data)
	}

	slog.InfoContext(ctx, "updating README via claude",
		"path", projectPath,
	)

	diff := job.Diff
	if len(diff) > 4000 {
		diff = diff[:4000]
	}
	prompt := fmt.Sprintf(claudeDocsPrompt, job.PRTitle, review.Summary, existing, diff)

	cctx, cancel := context.WithTimeout(ctx, d.rt.Timeout)
	defer cancel()

	out, err := runClaudeStream(cctx, projectPath, prompt, baseArgs(d.rt, "Bash,Read,Glob,Grep"), sink)
	if err != nil {
		return false, fmt.Errorf("claude docs: %w", err)
	}
	if out.Result == "" {
		return false, errEmptyDocs
	}

	newContent := out.Result
	if strings.TrimSpace(newContent) == strings.TrimSpace(existing) {
		slog.InfoContext(ctx, "README unchanged")
		return false, nil
	}

	if err := os.WriteFile(readmePath, []byte(newContent), 0644); err != nil {
		return false, fmt.Errorf("write README: %w", err)
	}
	slog.InfoContext(ctx, "README updated")
	return true, nil
}
