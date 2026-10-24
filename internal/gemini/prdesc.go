package gemini

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/Orfeo42/ai-orchestration/internal/events"
	"github.com/Orfeo42/ai-orchestration/internal/models"

	"google.golang.org/genai"
)

const prDescPrompt = `Write a pull request description for the following implementation.

Issue #%d: %s

%s

Diff:
` + "```diff\n%s\n```" + `

Write a concise PR description in markdown. Include:
- What was implemented
- Key technical decisions if non-obvious
- No fluff, no summaries of the diff line by line`

type GeminiPRDescWriter struct {
	client *genai.Client
}

func NewPRDescWriter(ctx context.Context, apiKey string) (*GeminiPRDescWriter, error) {
	client, err := newClient(ctx, apiKey)
	if err != nil {
		return nil, fmt.Errorf("gemini client: %w", err)
	}
	return &GeminiPRDescWriter{
		client: client,
	}, nil
}

func (p *GeminiPRDescWriter) Generate(ctx context.Context, issue models.ForgejoIssue, _, diff string, sink events.Sink) (string, error) {
	slog.InfoContext(ctx, "generating PR description",
		"issue", issue.Number,
	)
	if len(diff) > 50000 {
		diff = diff[:50000]
	}
	prompt := fmt.Sprintf(prDescPrompt, issue.Number, issue.Title, issue.Body, diff)
	resp, err := p.client.Models.GenerateContent(ctx, modelName, genai.Text(prompt), nil)
	if err != nil {
		return "", fmt.Errorf("generate content: %w", err)
	}
	emitUsage(ctx, sink, resp)
	text, err := responseText(resp)
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(text)
	emitText(ctx, sink, out)
	return out, nil
}

func (p *GeminiPRDescWriter) Close() {}
