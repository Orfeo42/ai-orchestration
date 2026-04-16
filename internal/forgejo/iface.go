package forgejo

import "ai-orchestration/internal/models"

//go:generate moq -out mock.go . Forgejo:MockForgejo

type Forgejo interface {
	GetIssues(owner, repo, label string, assignee *string) ([]models.ForgejoIssue, error)
	GetComments(owner, repo string, issueNumber int) ([]models.IssueComment, error)
	GetLabelID(owner, repo, name string) (*int, error)
	AddLabel(owner, repo string, issueNumber, labelID int) error
	RemoveLabel(owner, repo string, issueNumber, labelID int) error
	CreateComment(owner, repo string, issueNumber int, body string) error
	CreatePR(owner, repo, title, body, head, base string) (models.ForgejoPR, error)
	UpdatePR(owner, repo string, prNumber int, body string) error
	GetPRDiff(owner, repo string, prNumber int) (string, error)
	CreatePRReview(owner, repo string, prNumber int, body string, approved bool) error
}
