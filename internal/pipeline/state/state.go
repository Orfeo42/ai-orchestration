package state

import (
	"github.com/Orfeo42/ai-orchestration/internal/agent"
	"github.com/Orfeo42/ai-orchestration/internal/gitops"
	"github.com/Orfeo42/ai-orchestration/internal/models"
	"github.com/Orfeo42/ai-orchestration/internal/projects"
	"github.com/Orfeo42/ai-orchestration/internal/runstore"
)

type Assess struct {
	Job         models.AssessJob
	Proj        projects.Config
	Handle      *runstore.RunHandle
	Issue       models.ForgejoIssue
	Comments    []models.IssueComment
	Result      agent.AssessResult
	ProjectPath string
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
	ProjectPath   string
}

type Review struct {
	Job         models.ReviewJob
	Handle      *runstore.RunHandle
	Result      models.ReviewResult
	Git         gitops.Git
	ProjectPath string
	Proj        projects.Config
}
