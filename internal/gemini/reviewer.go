package gemini

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Orfeo42/ai-orchestration/internal/events"
	"github.com/Orfeo42/ai-orchestration/internal/models"

	"google.golang.org/genai"
)

const reviewPrompt = `Review this pull request diff. Be concise and direct.

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

type GeminiReviewer struct {
	client *genai.Client
}

func NewReviewer(ctx context.Context, apiKey string) (*GeminiReviewer, error) {
	client, err := newClient(ctx, apiKey)
	if err != nil {
		return nil, fmt.Errorf("gemini client: %w", err)
	}
	return &GeminiReviewer{
		client: client,
	}, nil
}

func (r *GeminiReviewer) Review(ctx context.Context, job models.ReviewJob, sink events.Sink) (models.ReviewResult, error) {
	slog.InfoContext(ctx, "requesting gemini review",
		"pr_number", job.PRNumber,
	)
	prompt := fmt.Sprintf(reviewPrompt, job.PRTitle, job.IssueNumber, job.Diff)
	resp, err := r.client.Models.GenerateContent(ctx, modelName, genai.Text(prompt), nil)
	if err != nil {
		return models.ReviewResult{}, fmt.Errorf("generate content: %w", err)
	}
	emitUsage(ctx, sink, resp)
	text, err := responseText(resp)
	if err != nil {
		return models.ReviewResult{}, err
	}
	emitText(ctx, sink, text)
	return models.ParseReviewText(text), nil
}

func (r *GeminiReviewer) Close() {}
