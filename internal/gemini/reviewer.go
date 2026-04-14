package gemini

import (
	"context"
	"fmt"
	"log/slog"

	"ai-orchestration/internal/models"

	"github.com/google/generative-ai-go/genai"
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
	model  *genai.GenerativeModel
}

func NewReviewer(ctx context.Context, apiKey string) (*GeminiReviewer, error) {
	client, err := newClient(ctx, apiKey)
	if err != nil {
		return nil, fmt.Errorf("gemini client: %w", err)
	}
	return &GeminiReviewer{
		client: client,
		model:  client.GenerativeModel("gemini-2.0-flash"),
	}, nil
}

func (r *GeminiReviewer) Review(ctx context.Context, job models.ReviewJob) (models.ReviewResult, error) {
	slog.InfoContext(ctx, "requesting gemini review",
		"pr_number", job.PRNumber,
	)
	prompt := fmt.Sprintf(reviewPrompt, job.PRTitle, job.IssueNumber, job.Diff)
	resp, err := r.model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return models.ReviewResult{}, fmt.Errorf("generate content: %w", err)
	}
	text, err := responseText(resp)
	if err != nil {
		return models.ReviewResult{}, err
	}
	slog.DebugContext(ctx, "gemini review received",
		"text", text,
	)
	return models.ParseReviewText(text), nil
}

func (r *GeminiReviewer) Close() {
	r.client.Close()
}
