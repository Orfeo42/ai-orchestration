package gemini

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"ai-orchestration/internal/events"
	"ai-orchestration/internal/models"

	"github.com/google/generative-ai-go/genai"
)

const docsPrompt = `Update the README.md for this project based on the changes in a merged PR.

PR: %s
Summary of changes: %s

Current README:
%s

Diff of changes (truncated):
` + "```diff\n%s\n```" + `

Return ONLY the updated README content, no preamble.
If no meaningful update is needed, return the original content unchanged.`

type GeminiDocsWriter struct {
	client *genai.Client
	model  *genai.GenerativeModel
}

func NewDocsWriter(ctx context.Context, apiKey string) (*GeminiDocsWriter, error) {
	client, err := newClient(ctx, apiKey)
	if err != nil {
		return nil, fmt.Errorf("gemini client: %w", err)
	}
	return &GeminiDocsWriter{
		client: client,
		model:  client.GenerativeModel("gemini-2.0-flash"),
	}, nil
}

func (d *GeminiDocsWriter) UpdateReadme(ctx context.Context, job models.ReviewJob, projectPath string, review models.ReviewResult, sink events.Sink) (bool, error) {
	readmePath := filepath.Join(projectPath, "README.md")
	existing := ""
	if data, err := os.ReadFile(readmePath); err == nil {
		existing = string(data)
	}

	slog.InfoContext(ctx, "updating README",
		"path", projectPath,
	)
	diff := job.Diff
	if len(diff) > 4000 {
		diff = diff[:4000]
	}
	prompt := fmt.Sprintf(docsPrompt, job.PRTitle, review.Summary, existing, diff)

	resp, err := d.model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return false, fmt.Errorf("generate content: %w", err)
	}
	emitUsage(ctx, sink, resp)
	newContent, err := responseText(resp)
	if err != nil {
		return false, err
	}

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

func (d *GeminiDocsWriter) Close() {
	d.client.Close()
}
