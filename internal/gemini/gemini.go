package gemini

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/Orfeo42/ai-orchestration/internal/events"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

var errNoCandidates = errors.New("no candidates in response")

func newClient(ctx context.Context, apiKey string) (*genai.Client, error) {
	return genai.NewClient(ctx, option.WithAPIKey(apiKey))
}

func responseText(resp *genai.GenerateContentResponse) (string, error) {
	if len(resp.Candidates) == 0 {
		return "", errNoCandidates
	}
	var sb strings.Builder
	for _, part := range resp.Candidates[0].Content.Parts {
		if txt, ok := part.(genai.Text); ok {
			sb.WriteString(string(txt))
		}
	}
	return sb.String(), nil
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
