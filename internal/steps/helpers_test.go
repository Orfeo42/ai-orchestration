package steps

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"ai-orchestration/internal/gitops"
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/process"
	"ai-orchestration/internal/projects"
	"ai-orchestration/internal/runstore"
	"ai-orchestration/internal/step"
)

var testIssue = models.ForgejoIssue{
	Number:    7,
	Title:     "Add search",
	Body:      "need search",
	RepoOwner: "acme",
	RepoName:  "tracker",
	Labels:    []string{"To-do"},
}

var testPR = models.ForgejoPR{Number: 11, Title: "feat: Add search"}

func testProjects() map[string]projects.Config {
	return map[string]projects.Config{
		"api": {Repo: "acme/api", BaseBranch: "main"},
		"bad": {Repo: "noslash", BaseBranch: "main"},
	}
}

func newScope(t *testing.T, vars process.Vars) step.Context {
	t.Helper()
	return step.Context{
		Process:  process.Process{Vars: vars},
		WorkerID: "w1",
		Handle:   runstore.NewStepHandle(context.Background(), nil, nil, nil, "sr1", runstore.ServiceOrchestrator),
	}
}

func setVar[T any](t *testing.T, vars process.Vars, k process.Key[T], v T) {
	t.Helper()
	require.NoError(t, process.Set(vars, k, v))
}

func baseVars(t *testing.T, project string) process.Vars {
	t.Helper()
	vars := process.Vars{}
	setVar(t, vars, KeyIssue, testIssue)
	setVar(t, vars, KeyProject, project)
	return vars
}

func prVars(t *testing.T) process.Vars {
	t.Helper()
	vars := baseVars(t, "api")
	setVar(t, vars, KeyPR, testPR)
	setVar(t, vars, KeyBranch, "feature/7-add-search")
	return vars
}

func okGit() *gitops.MockGit {
	return &gitops.MockGit{
		EnsureRepoFunc:           func() error { return nil },
		FetchFunc:                func() error { return nil },
		CheckoutRemoteBranchFunc: func(string) error { return nil },
		CheckoutFunc:             func(string) error { return nil },
		CreateBranchFunc:         func(string) error { return nil },
		AddFunc:                  func(string) error { return nil },
		AddAllFunc:               func() error { return nil },
		CommitFunc:               func(string) error { return nil },
		PushFunc:                 func(string) error { return nil },
	}
}

func factoryFor(g gitops.Git) gitops.Factory {
	return func(string, string, string, string, string) gitops.Git { return g }
}
