package gemini

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Orfeo42/ai-orchestration/internal/events"

	"google.golang.org/genai"
)

const modelName = "gemini-2.0-flash"

var errNoCandidates = errors.New("no candidates in response")

func newClient(ctx context.Context, apiKey string) (*genai.Client, error) {
	return genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey, Backend: genai.BackendGeminiAPI})
}

func responseText(resp *genai.GenerateContentResponse) (string, error) {
	if len(resp.Candidates) == 0 {
		return "", errNoCandidates
	}
	return resp.Text(), nil
}

func emitUsage(ctx context.Context, sink events.Sink, resp *genai.GenerateContentResponse) {
	if sink == nil || resp == nil || resp.UsageMetadata == nil {
		return
	}
	usage := events.TokenUsage{
		Input:          int(resp.UsageMetadata.PromptTokenCount),
		Output:         int(resp.UsageMetadata.CandidatesTokenCount),
		CacheReadInput: int(resp.UsageMetadata.CachedContentTokenCount),
	}
	if err := sink.Emit(ctx, events.Event{
		Kind:  events.KindResult,
		Usage: &usage,
		At:    time.Now().UTC(),
	}); err != nil {
		slog.WarnContext(ctx, "gemini usage sink emit failed",
			"error", err,
		)
	}
}

func emitText(ctx context.Context, sink events.Sink, text string) {
	if sink == nil || text == "" {
		return
	}
	if err := sink.Emit(ctx, events.Event{
		Kind: events.KindAssistant,
		Text: text,
		At:   time.Now().UTC(),
	}); err != nil {
		slog.WarnContext(ctx, "gemini text sink emit failed",
			"error", err,
		)
	}
}
