package gemini

import (
	"context"

	"github.com/Orfeo42/ai-orchestration/internal/events"
	"github.com/Orfeo42/ai-orchestration/internal/models"
)

//go:generate moq -rm -out mock.go . Reviewer:MockReviewer DocsWriter:MockDocsWriter PRDescWriter:MockPRDescWriter

type Reviewer interface {
	Review(ctx context.Context, job models.ReviewJob, sink events.Sink) (models.ReviewResult, error)
}

type DocsWriter interface {
	UpdateReadme(ctx context.Context, job models.ReviewJob, projectPath string, review models.ReviewResult, sink events.Sink) (bool, error)
}

type PRDescWriter interface {
	Generate(ctx context.Context, issue models.ForgejoIssue, projectPath, diff string, sink events.Sink) (string, error)
}
