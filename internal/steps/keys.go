package steps

import (
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/process"
)

const (
	KeyIssue     process.Key[models.ForgejoIssue] = "issue"
	KeyProject   process.Key[string]              = "project"
	KeyBranch    process.Key[string]              = "branch"
	KeyPR        process.Key[models.ForgejoPR]    = "pr"
	KeyQuestions process.Key[string]              = "questions"
	KeyReview    process.Key[models.ReviewResult] = "review"
)
