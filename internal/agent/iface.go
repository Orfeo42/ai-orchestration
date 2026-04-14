package agent

import "ai-orchestration/internal/models"

//go:generate moq -rm -out mock.go . Agent:MockAgent

type Assessment struct {
	Clear     bool
	Questions string
}

type Agent interface {
	AssessIssue(issue models.ForgejoIssue, projectPath string) (Assessment, error)
	ImplementIssue(issue models.ForgejoIssue, projectPath string, testError *string, resume bool) (string, error)
}
