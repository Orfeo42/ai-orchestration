package models

import (
	"strings"
	"time"
)

type ForgejoIssue struct {
	ID        int      `json:"id"`
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	RepoOwner string   `json:"repo_owner"`
	RepoName  string   `json:"repo_name"`
	Labels    []string `json:"labels"`
}

type IssueComment struct {
	ID        int64     `json:"id"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ForgejoPR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`

	RepoOwner string `json:"repo_owner,omitempty"`
	RepoName  string `json:"repo_name,omitempty"`
}

type AssessJob struct {
	RepoOwner   string   `json:"repo_owner"`
	RepoName    string   `json:"repo_name"`
	IssueNumber int      `json:"issue_number"`
	IssueTitle  string   `json:"issue_title"`
	IssueBody   string   `json:"issue_body"`
	Project     string   `json:"project"`
	Labels      []string `json:"labels,omitempty"`
}

type ImplJob struct {
	RepoOwner   string `json:"repo_owner"`
	RepoName    string `json:"repo_name"`
	IssueNumber int    `json:"issue_number"`
	IssueTitle  string `json:"issue_title"`
	IssueBody   string `json:"issue_body"`
	Project     string `json:"project"`
}

type ReviewJob struct {
	RepoOwner   string `json:"repo_owner"`
	RepoName    string `json:"repo_name"`
	PRNumber    int    `json:"pr_number"`
	PRTitle     string `json:"pr_title"`
	PRBranch    string `json:"pr_branch"`
	Project     string `json:"project"`
	IssueNumber int    `json:"issue_number"`
	Diff        string `json:"diff"`
}

type ReviewResult struct {
	Summary     string   `json:"summary"`
	Issues      []string `json:"issues"`
	Suggestions []string `json:"suggestions"`
	Approved    bool     `json:"approved"`
}

func ParseReviewText(text string) ReviewResult {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	result := ReviewResult{
		Issues:      []string{},
		Suggestions: []string{},
	}
	var section string

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "SUMMARY:"):
			result.Summary = strings.TrimSpace(strings.TrimPrefix(line, "SUMMARY:"))
			section = ""
		case strings.HasPrefix(line, "APPROVED:"):
			result.Approved = strings.Contains(strings.ToLower(line), "yes")
			section = ""
		case strings.HasPrefix(line, "ISSUES:"):
			section = "issues"
		case strings.HasPrefix(line, "SUGGESTIONS:"):
			section = "suggestions"
		case strings.HasPrefix(line, "- "):
			item := line[2:]
			switch section {
			case "issues":
				result.Issues = append(result.Issues, item)
			case "suggestions":
				result.Suggestions = append(result.Suggestions, item)
			}
		}
	}
	return result
}
