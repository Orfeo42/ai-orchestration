package impl

import (
	"context"
	"log/slog"

	"ai-orchestration/internal/events"
	"ai-orchestration/internal/models"
)

type FallbackPRWriter struct {
	Primary  PRDescWriter
	Fallback PRDescWriter
}

func (f *FallbackPRWriter) Generate(ctx context.Context, issue models.ForgejoIssue, projectPath, diff string, sink events.Sink) (string, error) {
	desc, err := f.Primary.Generate(ctx, issue, projectPath, diff, sink)
	if err == nil {
		return desc, nil
	}
	if f.Fallback == nil {
		return "", err
	}
	slog.WarnContext(ctx, "primary pr writer failed, using fallback",
		slog.Any("error", err),
	)
	return f.Fallback.Generate(ctx, issue, projectPath, diff, sink)
}
