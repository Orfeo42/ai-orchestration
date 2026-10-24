package gemini

import (
	"context"
	"log/slog"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"ai-orchestration/internal/events"
	"ai-orchestration/internal/models"
)

type FallbackReviewer struct {
	Primary   Reviewer
	Secondary Reviewer
}

func (r FallbackReviewer) Review(ctx context.Context, job models.ReviewJob, sink events.Sink) (models.ReviewResult, error) {
	res, err := r.Primary.Review(ctx, job, sink)
	if err == nil || !isQuotaErr(err) {
		return res, err
	}
	slog.WarnContext(ctx, "primary reviewer quota exhausted, using secondary",
		slog.Any("error", err),
	)
	return r.Secondary.Review(ctx, job, sink)
}

type FallbackDocsWriter struct {
	Primary   DocsWriter
	Secondary DocsWriter
}

func (d FallbackDocsWriter) UpdateReadme(ctx context.Context, job models.ReviewJob, projectPath string, review models.ReviewResult, sink events.Sink) (bool, error) {
	updated, err := d.Primary.UpdateReadme(ctx, job, projectPath, review, sink)
	if err == nil || !isQuotaErr(err) {
		return updated, err
	}
	slog.WarnContext(ctx, "primary docs writer quota exhausted, using secondary",
		slog.Any("error", err),
	)
	return d.Secondary.UpdateReadme(ctx, job, projectPath, review, sink)
}

func isQuotaErr(err error) bool {
	if s, ok := status.FromError(err); ok && s.Code() == codes.ResourceExhausted {
		return true
	}
	msg := err.Error()
	if strings.Contains(msg, "RESOURCE_EXHAUSTED") {
		return true
	}
	if strings.Contains(strings.ToLower(msg), "quota") {
		return true
	}
	return strings.Contains(msg, " 429") || strings.Contains(msg, "(429)")
}
