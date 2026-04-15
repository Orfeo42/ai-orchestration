package gemini

import (
	"context"

	"ai-orchestration/internal/models"
)

//go:generate moq -out mock.go . Reviewer:MockReviewer DocsWriter:MockDocsWriter PRDescWriter:MockPRDescWriter

type Reviewer interface {
	Review(ctx context.Context, job models.ReviewJob) (models.ReviewResult, error)
}

type DocsWriter interface {
	UpdateReadme(ctx context.Context, job models.ReviewJob, review models.ReviewResult) (bool, error)
}

type PRDescWriter interface {
	Generate(ctx context.Context, issue models.ForgejoIssue, projectPath, diff string) (string, error)
}
