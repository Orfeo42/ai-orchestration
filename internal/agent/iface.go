package agent

import (
	"context"

	"ai-orchestration/internal/events"
	"ai-orchestration/internal/models"
)

//go:generate moq -rm -out mock.go . Agent:MockAgent

type Assessment struct {
	Clear     bool
	Questions string
}

type AssessResult struct {
	Assessment Assessment
	SessionID  string
	Usage      events.TokenUsage
}

type ImplementResult struct {
	Output    string
	SessionID string
	Usage     events.TokenUsage
}

type Agent interface {
	AssessIssue(ctx context.Context, issue models.ForgejoIssue, comments []models.IssueComment, projectPath string, sink events.Sink) (AssessResult, error)
	ImplementIssue(ctx context.Context, issue models.ForgejoIssue, comments []models.IssueComment, projectPath, sessionID string, testError *string, sink events.Sink) (ImplementResult, error)
}
