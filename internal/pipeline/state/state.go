package state

import (
	"ai-orchestration/internal/agent"
	"ai-orchestration/internal/gitops"
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/projects"
	"ai-orchestration/internal/runstore"
)

type Assess struct {
	Job      models.AssessJob
	Proj     projects.Config
	Handle   *runstore.RunHandle
	Issue    models.ForgejoIssue
	Comments []models.IssueComment
	Result   agent.AssessResult
}

type Impl struct {
	Job           models.ImplJob
	Proj          projects.Config
	Handle        *runstore.RunHandle
	Issue         models.ForgejoIssue
	Git           gitops.Git
	Branch        string
	TestError     *string
	Attempt       int
	PR            *models.ForgejoPR
	Comments      []models.IssueComment
	FreshComments []models.IssueComment
	ImplOutput    string
}

type Review struct {
	Job    models.ReviewJob
	Handle *runstore.RunHandle
	Result models.ReviewResult
	Git    gitops.Git
}
