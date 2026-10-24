package steps

import (
	"fmt"

	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/process"
	"ai-orchestration/internal/projects"
	"ai-orchestration/internal/step"
)

func loadIssue(sc step.Context) (models.ForgejoIssue, error) {
	issue, ok, err := process.Get(sc.Process.Vars, KeyIssue)
	if err != nil {
		return models.ForgejoIssue{}, err
	}
	if !ok {
		return models.ForgejoIssue{}, fmt.Errorf("%w: %s", ErrMissingVar, KeyIssue)
	}
	return issue, nil
}

type projectRef struct {
	Key    string
	Config projects.Config
}

func loadProject(sc step.Context, configs map[string]projects.Config) (projectRef, error) {
	key, ok, err := process.Get(sc.Process.Vars, KeyProject)
	if err != nil {
		return projectRef{}, err
	}
	if !ok {
		return projectRef{}, fmt.Errorf("%w: %s", ErrMissingVar, KeyProject)
	}
	cfg, ok := configs[key]
	if !ok {
		return projectRef{}, fmt.Errorf("%w: %s", ErrUnknownProject, key)
	}
	return projectRef{Key: key, Config: cfg}, nil
}

type prRef struct {
	PR     models.ForgejoPR
	Branch string
}

func loadPR(sc step.Context) (prRef, error) {
	pr, ok, err := process.Get(sc.Process.Vars, KeyPR)
	if err != nil {
		return prRef{}, err
	}
	if !ok {
		return prRef{}, fmt.Errorf("%w: %s", ErrMissingVar, KeyPR)
	}
	branch, ok, err := process.Get(sc.Process.Vars, KeyBranch)
	if err != nil {
		return prRef{}, err
	}
	if !ok {
		return prRef{}, fmt.Errorf("%w: %s", ErrMissingVar, KeyBranch)
	}
	return prRef{PR: pr, Branch: branch}, nil
}

func newReviewJob(fj forgejo.Forgejo, issue models.ForgejoIssue, projectKey string, code projects.Repo, pr models.ForgejoPR, branch string) (models.ReviewJob, error) {
	diff, err := fj.GetPRDiff(code.Owner, code.Name, pr.Number)
	if err != nil {
		return models.ReviewJob{}, fmt.Errorf("%w: get pr diff: %w", step.ErrUpstream, err)
	}
	return models.ReviewJob{
		RepoOwner:   code.Owner,
		RepoName:    code.Name,
		PRNumber:    pr.Number,
		PRTitle:     "feat: " + issue.Title,
		PRBranch:    branch,
		Project:     projectKey,
		IssueNumber: issue.Number,
		Diff:        diff,
	}, nil
}
