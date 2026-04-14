package agent

import "ai-orchestration/internal/models"

//go:generate moq -out mock.go . Agent:MockAgent

type Agent interface {
	ImplementIssue(issue models.ForgejoIssue, projectPath string, testError *string, resume bool) (string, error)
}
