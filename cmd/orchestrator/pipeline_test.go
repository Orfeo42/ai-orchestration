package main

import (
	"context"
	"errors"
	"testing"

	"ai-orchestration/internal/agent"
	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/gitops"
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/queue"
)

func TestBranchSlug(t *testing.T) {
	t.Run("lowercase with dashes", func(t *testing.T) {
		got := branchSlug("Add Login Feature")
		if got != "add-login-feature" {
			t.Errorf("got %q, want %q", got, "add-login-feature")
		}
	})

	t.Run("special chars replaced", func(t *testing.T) {
		got := branchSlug("fix: bug/issue #42!")
		want := "fix-bug-issue-42"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("truncated to 50 chars", func(t *testing.T) {
		title := "this is a very long title that exceeds the fifty character limit easily"
		got := branchSlug(title)
		if len(got) > 50 {
			t.Errorf("len = %d, want <= 50", len(got))
		}
	})
}

func TestContainsLabel(t *testing.T) {
	t.Run("label present returns true", func(t *testing.T) {
		if !containsLabel([]string{"To-do", "in-progress"}, "in-progress") {
			t.Error("expected true")
		}
	})

	t.Run("label absent returns false", func(t *testing.T) {
		if containsLabel([]string{"To-do", "done"}, "in-progress") {
			t.Error("expected false")
		}
	})

	t.Run("empty slice returns false", func(t *testing.T) {
		if containsLabel([]string{}, "any") {
			t.Error("expected false")
		}
	})
}

func TestProjectLabel(t *testing.T) {
	t.Run("returns project name", func(t *testing.T) {
		got := projectLabel([]string{"To-do", "project:myapp"})
		if got != "myapp" {
			t.Errorf("got %q, want %q", got, "myapp")
		}
	})

	t.Run("no project label returns empty", func(t *testing.T) {
		got := projectLabel([]string{"To-do", "done"})
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

func TestSplitRepo(t *testing.T) {
	t.Run("valid owner/name", func(t *testing.T) {
		owner, name, err := splitRepo("acme/myrepo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if owner != "acme" || name != "myrepo" {
			t.Errorf("got %q/%q, want acme/myrepo", owner, name)
		}
	})

	t.Run("no slash returns error", func(t *testing.T) {
		_, _, err := splitRepo("noslash")
		if err == nil {
			t.Error("expected error")
		}
	})
}

func noopGitFactory(_ string, _, _, _, _ string) gitops.Git {
	return &gitops.MockGit{
		CheckoutFunc:             func(branch string) error { return nil },
		CreateBranchFunc:         func(branch string) error { return nil },
		HasChangesFunc:           func() (bool, error) { return true, nil },
		AddAllFunc:               func() error { return nil },
		CommitFunc:               func(message string) error { return nil },
		PushFunc:                 func(branch string) error { return nil },
		FetchFunc:                func() error { return nil },
		CheckoutRemoteBranchFunc: func(branch string) error { return nil },
		AddFunc:                  func(path string) error { return nil },
	}
}

func TestPollRepo(t *testing.T) {
	t.Run("no project label skips issue", func(t *testing.T) {
		fj := &forgejo.MockForgejo{
			GetIssuesFunc: func(owner, repo, label string, assignee *string) ([]models.ForgejoIssue, error) {
				return []models.ForgejoIssue{
					{Number: 1, Title: "No project label", Labels: []string{"To-do"}},
				}, nil
			},
		}
		rq := &queue.MockQueue{
			IsProcessedFunc: func(ctx context.Context, key string) (bool, error) {
				return false, nil
			},
		}
		deps := orchDeps{
			fj:         fj,
			rq:         rq,
			gitFactory: noopGitFactory,
		}
		cfg := config{projects: map[string]projectConfig{}}
		pollRepo(context.Background(), "owner", "repo", deps, cfg)
	})

	t.Run("already processed issue skipped", func(t *testing.T) {
		fj := &forgejo.MockForgejo{
			GetIssuesFunc: func(owner, repo, label string, assignee *string) ([]models.ForgejoIssue, error) {
				return []models.ForgejoIssue{
					{Number: 2, Title: "Already done", Labels: []string{"To-do", "project:app"}},
				}, nil
			},
		}
		rq := &queue.MockQueue{
			IsProcessedFunc: func(ctx context.Context, key string) (bool, error) {
				return true, nil
			},
		}
		deps := orchDeps{
			fj:         fj,
			rq:         rq,
			gitFactory: noopGitFactory,
		}
		cfg := config{projects: map[string]projectConfig{"app": {Repo: "owner/app", BaseBranch: "main"}}}
		pollRepo(context.Background(), "owner", "repo", deps, cfg)
	})

	t.Run("fetch issues error logged, no panic", func(t *testing.T) {
		fj := &forgejo.MockForgejo{
			GetIssuesFunc: func(owner, repo, label string, assignee *string) ([]models.ForgejoIssue, error) {
				return nil, errors.New("network error")
			},
		}
		deps := orchDeps{
			fj:         fj,
			gitFactory: noopGitFactory,
		}
		cfg := config{projects: map[string]projectConfig{}}
		pollRepo(context.Background(), "owner", "repo", deps, cfg)
	})
}

func TestProcessIssue(t *testing.T) {
	t.Run("no changes returns errNoChanges", func(t *testing.T) {
		issue := models.ForgejoIssue{Number: 5, Title: "Thing", Labels: []string{}}
		proj := projectConfig{Repo: "owner/app", BaseBranch: "main"}

		claudeMock := &agent.MockAgent{
			ImplementIssueFunc: func(issue models.ForgejoIssue, projectPath string, testError *string, resume bool) (string, error) {
				return "", nil
			},
		}
		gitMock := &gitops.MockGit{
			CheckoutFunc:     func(branch string) error { return nil },
			CreateBranchFunc: func(branch string) error { return nil },
			HasChangesFunc:   func() (bool, error) { return false, nil },
		}

		deps := orchDeps{
			fj:     &forgejo.MockForgejo{},
			rq:     &queue.MockQueue{},
			claude: claudeMock,
			gitFactory: func(repoPath, forgejoURL, token, owner, repo string) gitops.Git {
				return gitMock
			},
			runTests: func(projectPath string) (bool, string) { return true, "" },
		}

		err := processIssue(context.Background(), issue, proj, "owner", "repo", deps, config{})
		if !errors.Is(err, errNoChanges) {
			t.Errorf("err = %v, want errNoChanges", err)
		}
	})

	t.Run("claude error propagated", func(t *testing.T) {
		issue := models.ForgejoIssue{Number: 6, Title: "Thing", Labels: []string{}}
		proj := projectConfig{Repo: "owner/app", BaseBranch: "main"}

		claudeMock := &agent.MockAgent{
			ImplementIssueFunc: func(issue models.ForgejoIssue, projectPath string, testError *string, resume bool) (string, error) {
				return "", errors.New("claude failed")
			},
		}
		gitMock := &gitops.MockGit{
			CheckoutFunc:     func(branch string) error { return nil },
			CreateBranchFunc: func(branch string) error { return nil },
		}

		deps := orchDeps{
			fj:     &forgejo.MockForgejo{},
			rq:     &queue.MockQueue{},
			claude: claudeMock,
			gitFactory: func(repoPath, forgejoURL, token, owner, repo string) gitops.Git {
				return gitMock
			},
			runTests: func(projectPath string) (bool, string) { return true, "" },
		}

		err := processIssue(context.Background(), issue, proj, "owner", "repo", deps, config{})
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}
