package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"ai-orchestration/internal/agent"
	"ai-orchestration/internal/events"
	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/gemini"
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
		ref, err := splitRepo("acme/myrepo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ref.Owner != "acme" || ref.Name != "myrepo" {
			t.Errorf("got %q/%q, want acme/myrepo", ref.Owner, ref.Name)
		}
	})

	t.Run("no slash returns error", func(t *testing.T) {
		_, err := splitRepo("noslash")
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

	t.Run("unclear issue does not mark processed so author answers are reprocessed", func(t *testing.T) {
		fj := &forgejo.MockForgejo{
			GetIssuesFunc: func(owner, repo, label string, assignee *string) ([]models.ForgejoIssue, error) {
				return []models.ForgejoIssue{
					{Number: 99, Title: "Vague", Body: "x", Labels: []string{"To-do", "project:app"}},
				}, nil
			},
			GetCommentsFunc: func(owner, repo string, issueNumber int) ([]models.IssueComment, error) {
				return nil, nil
			},
			GetLabelIDFunc: func(owner, repo, name string) (*int, error) {
				id := 1
				return &id, nil
			},
			AddLabelFunc:      func(owner, repo string, issueNumber, labelID int) error { return nil },
			RemoveLabelFunc:   func(owner, repo string, issueNumber, labelID int) error { return nil },
			CreateCommentFunc: func(owner, repo string, issueNumber int, body string) error { return nil },
		}
		rq := &queue.MockQueue{
			IsProcessedFunc:     func(ctx context.Context, key string) (bool, error) { return false, nil },
			MarkInProgressFunc:  func(ctx context.Context, key string, ttl time.Duration) error { return nil },
			MarkProcessedFunc:   func(ctx context.Context, key string) error { return nil },
			ClearInProgressFunc: func(ctx context.Context, key string) error { return nil },
		}
		claudeMock := &agent.MockAgent{
			AssessIssueFunc: func(ctx context.Context, issue models.ForgejoIssue, comments []models.IssueComment, projectPath string, sink events.Sink) (agent.AssessResult, error) {
				return agent.AssessResult{Assessment: agent.Assessment{Clear: false, Questions: "- missing scope"}}, nil
			},
			ImplementIssueFunc: func(ctx context.Context, issue models.ForgejoIssue, projectPath, sessionID string, testError *string, sink events.Sink) (agent.ImplementResult, error) {
				t.Fatal("ImplementIssue must not be called when unclear")
				return agent.ImplementResult{}, nil
			},
		}
		deps := orchDeps{
			fj:         fj,
			rq:         rq,
			claude:     claudeMock,
			gitFactory: noopGitFactory,
		}
		cfg := config{projects: map[string]projectConfig{"app": {Repo: "owner/app", BaseBranch: "main"}}}
		pollRepo(context.Background(), "owner", "repo", deps, cfg)

		if len(rq.MarkProcessedCalls()) != 0 {
			t.Errorf("MarkProcessed calls = %d, want 0 (unclear issue must remain reprocessable)", len(rq.MarkProcessedCalls()))
		}
		if len(rq.ClearInProgressCalls()) != 1 {
			t.Errorf("ClearInProgress calls = %d, want 1", len(rq.ClearInProgressCalls()))
		}
		if len(fj.CreateCommentCalls()) != 1 {
			t.Errorf("CreateComment calls = %d, want 1", len(fj.CreateCommentCalls()))
		}
	})

	t.Run("needs-info label skips issue without assessing", func(t *testing.T) {
		fj := &forgejo.MockForgejo{
			GetIssuesFunc: func(owner, repo, label string, assignee *string) ([]models.ForgejoIssue, error) {
				return []models.ForgejoIssue{
					{Number: 100, Title: "Pending author", Labels: []string{"To-do", "project:app", forgejo.LabelNeedsInfo}},
				}, nil
			},
		}
		rq := &queue.MockQueue{
			IsProcessedFunc: func(ctx context.Context, key string) (bool, error) { return false, nil },
		}
		claudeMock := &agent.MockAgent{
			AssessIssueFunc: func(ctx context.Context, issue models.ForgejoIssue, comments []models.IssueComment, projectPath string, sink events.Sink) (agent.AssessResult, error) {
				t.Fatal("AssessIssue must not be called while needs-info label is present")
				return agent.AssessResult{}, nil
			},
		}
		deps := orchDeps{fj: fj, rq: rq, claude: claudeMock, gitFactory: noopGitFactory}
		cfg := config{projects: map[string]projectConfig{"app": {Repo: "owner/app", BaseBranch: "main"}}}
		pollRepo(context.Background(), "owner", "repo", deps, cfg)
	})
}

func TestFallbackPRWriter(t *testing.T) {
	issue := models.ForgejoIssue{Number: 1, Title: "t"}

	t.Run("primary success does not call fallback", func(t *testing.T) {
		fallbackCalled := false
		primary := &gemini.MockPRDescWriter{
			GenerateFunc: func(ctx context.Context, issue models.ForgejoIssue, projectPath, diff string, sink events.Sink) (string, error) {
				return "ok", nil
			},
		}
		fallback := &gemini.MockPRDescWriter{
			GenerateFunc: func(ctx context.Context, issue models.ForgejoIssue, projectPath, diff string, sink events.Sink) (string, error) {
				fallbackCalled = true
				return "", nil
			},
		}
		w := &fallbackPRWriter{primary: primary, fallback: fallback}
		got, err := w.Generate(context.Background(), issue, "/p", "d", events.NoopSink{})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if got != "ok" {
			t.Errorf("got %q, want ok", got)
		}
		if fallbackCalled {
			t.Error("fallback should not have been called")
		}
	})

	t.Run("primary failure triggers fallback", func(t *testing.T) {
		primary := &gemini.MockPRDescWriter{
			GenerateFunc: func(ctx context.Context, issue models.ForgejoIssue, projectPath, diff string, sink events.Sink) (string, error) {
				return "", errors.New("gemini out of credit")
			},
		}
		fallback := &gemini.MockPRDescWriter{
			GenerateFunc: func(ctx context.Context, issue models.ForgejoIssue, projectPath, diff string, sink events.Sink) (string, error) {
				return "from claude", nil
			},
		}
		w := &fallbackPRWriter{primary: primary, fallback: fallback}
		got, err := w.Generate(context.Background(), issue, "/p", "d", events.NoopSink{})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if got != "from claude" {
			t.Errorf("got %q, want 'from claude'", got)
		}
	})

	t.Run("nil fallback returns primary error", func(t *testing.T) {
		primaryErr := errors.New("boom")
		primary := &gemini.MockPRDescWriter{
			GenerateFunc: func(ctx context.Context, issue models.ForgejoIssue, projectPath, diff string, sink events.Sink) (string, error) {
				return "", primaryErr
			},
		}
		w := &fallbackPRWriter{primary: primary}
		_, err := w.Generate(context.Background(), issue, "/p", "d", events.NoopSink{})
		if !errors.Is(err, primaryErr) {
			t.Errorf("err = %v, want %v", err, primaryErr)
		}
	})

	t.Run("both fail returns fallback error", func(t *testing.T) {
		fallbackErr := errors.New("claude failed")
		primary := &gemini.MockPRDescWriter{
			GenerateFunc: func(ctx context.Context, issue models.ForgejoIssue, projectPath, diff string, sink events.Sink) (string, error) {
				return "", errors.New("primary failed")
			},
		}
		fallback := &gemini.MockPRDescWriter{
			GenerateFunc: func(ctx context.Context, issue models.ForgejoIssue, projectPath, diff string, sink events.Sink) (string, error) {
				return "", fallbackErr
			},
		}
		w := &fallbackPRWriter{primary: primary, fallback: fallback}
		_, err := w.Generate(context.Background(), issue, "/p", "d", events.NoopSink{})
		if !errors.Is(err, fallbackErr) {
			t.Errorf("err = %v, want %v", err, fallbackErr)
		}
	})
}

func TestProcessIssue(t *testing.T) {
	t.Run("no changes returns errNoChanges", func(t *testing.T) {
		issue := models.ForgejoIssue{Number: 5, Title: "Thing", Labels: []string{}}
		proj := projectConfig{Repo: "owner/app", BaseBranch: "main"}

		claudeMock := &agent.MockAgent{
			AssessIssueFunc: func(ctx context.Context, issue models.ForgejoIssue, comments []models.IssueComment, projectPath string, sink events.Sink) (agent.AssessResult, error) {
				return agent.AssessResult{Assessment: agent.Assessment{Clear: true}}, nil
			},
			ImplementIssueFunc: func(ctx context.Context, issue models.ForgejoIssue, projectPath, sessionID string, testError *string, sink events.Sink) (agent.ImplementResult, error) {
				return agent.ImplementResult{}, nil
			},
		}
		gitMock := &gitops.MockGit{
			CheckoutFunc:     func(branch string) error { return nil },
			CreateBranchFunc: func(branch string) error { return nil },
			HasChangesFunc:   func() (bool, error) { return false, nil },
		}

		deps := orchDeps{
			fj: &forgejo.MockForgejo{
				GetCommentsFunc: func(owner, repo string, issueNumber int) ([]models.IssueComment, error) { return nil, nil },
			},
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
			AssessIssueFunc: func(ctx context.Context, issue models.ForgejoIssue, comments []models.IssueComment, projectPath string, sink events.Sink) (agent.AssessResult, error) {
				return agent.AssessResult{Assessment: agent.Assessment{Clear: true}}, nil
			},
			ImplementIssueFunc: func(ctx context.Context, issue models.ForgejoIssue, projectPath, sessionID string, testError *string, sink events.Sink) (agent.ImplementResult, error) {
				return agent.ImplementResult{}, errors.New("claude failed")
			},
		}
		gitMock := &gitops.MockGit{
			CheckoutFunc:     func(branch string) error { return nil },
			CreateBranchFunc: func(branch string) error { return nil },
		}

		deps := orchDeps{
			fj: &forgejo.MockForgejo{
				GetCommentsFunc: func(owner, repo string, issueNumber int) ([]models.IssueComment, error) { return nil, nil },
			},
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

	t.Run("unclear issue posts comment and returns errIssueUnclear", func(t *testing.T) {
		issue := models.ForgejoIssue{Number: 7, Title: "Vague", Body: "do the thing", Labels: []string{}}
		proj := projectConfig{Repo: "owner/app", BaseBranch: "main"}

		claudeMock := &agent.MockAgent{
			AssessIssueFunc: func(ctx context.Context, issue models.ForgejoIssue, comments []models.IssueComment, projectPath string, sink events.Sink) (agent.AssessResult, error) {
				return agent.AssessResult{Assessment: agent.Assessment{Clear: false, Questions: "- which thing?\n- acceptance criteria?"}}, nil
			},
			ImplementIssueFunc: func(ctx context.Context, issue models.ForgejoIssue, projectPath, sessionID string, testError *string, sink events.Sink) (agent.ImplementResult, error) {
				t.Fatal("ImplementIssue must not be called when assessment is unclear")
				return agent.ImplementResult{}, nil
			},
		}
		fjMock := &forgejo.MockForgejo{
			GetCommentsFunc:   func(owner, repo string, issueNumber int) ([]models.IssueComment, error) { return nil, nil },
			CreateCommentFunc: func(owner, repo string, issueNumber int, body string) error { return nil },
			GetLabelIDFunc: func(owner, repo, name string) (*int, error) {
				id := 1
				return &id, nil
			},
			AddLabelFunc:    func(owner, repo string, issueNumber, labelID int) error { return nil },
			RemoveLabelFunc: func(owner, repo string, issueNumber, labelID int) error { return nil },
		}

		deps := orchDeps{
			fj:         fjMock,
			rq:         &queue.MockQueue{},
			claude:     claudeMock,
			gitFactory: noopGitFactory,
			runTests:   func(projectPath string) (bool, string) { return true, "" },
		}

		err := processIssue(context.Background(), issue, proj, "owner", "repo", deps, config{})
		if !errors.Is(err, errIssueUnclear) {
			t.Fatalf("err = %v, want errIssueUnclear", err)
		}

		comments := fjMock.CreateCommentCalls()
		if len(comments) != 1 {
			t.Fatalf("CreateComment calls = %d, want 1", len(comments))
		}
		if !strings.Contains(comments[0].Body, "which thing?") {
			t.Errorf("comment body missing questions: %q", comments[0].Body)
		}

		added := fjMock.AddLabelCalls()
		if len(added) != 1 {
			t.Errorf("AddLabel calls = %d, want 1", len(added))
		}
	})

	t.Run("assess error propagated without comment", func(t *testing.T) {
		issue := models.ForgejoIssue{Number: 8, Title: "Thing", Labels: []string{}}
		proj := projectConfig{Repo: "owner/app", BaseBranch: "main"}

		claudeMock := &agent.MockAgent{
			AssessIssueFunc: func(ctx context.Context, issue models.ForgejoIssue, comments []models.IssueComment, projectPath string, sink events.Sink) (agent.AssessResult, error) {
				return agent.AssessResult{}, errors.New("claude timeout")
			},
			ImplementIssueFunc: func(ctx context.Context, issue models.ForgejoIssue, projectPath, sessionID string, testError *string, sink events.Sink) (agent.ImplementResult, error) {
				t.Fatal("ImplementIssue must not be called when assess fails")
				return agent.ImplementResult{}, nil
			},
		}
		fjMock := &forgejo.MockForgejo{
			GetCommentsFunc: func(owner, repo string, issueNumber int) ([]models.IssueComment, error) { return nil, nil },
		}

		deps := orchDeps{
			fj:         fjMock,
			rq:         &queue.MockQueue{},
			claude:     claudeMock,
			gitFactory: noopGitFactory,
			runTests:   func(projectPath string) (bool, string) { return true, "" },
		}

		err := processIssue(context.Background(), issue, proj, "owner", "repo", deps, config{})
		if err == nil || errors.Is(err, errIssueUnclear) {
			t.Errorf("err = %v, want wrapped assess error", err)
		}
	})
}

func TestHandleUnclearIssue(t *testing.T) {
	t.Run("posts comment, adds needs-info, removes in-progress", func(t *testing.T) {
		issue := models.ForgejoIssue{Number: 42, Title: "t"}
		labelIDs := map[string]int{forgejo.LabelNeedsInfo: 10, forgejo.LabelInProgress: 20}
		fjMock := &forgejo.MockForgejo{
			CreateCommentFunc: func(owner, repo string, issueNumber int, body string) error { return nil },
			GetLabelIDFunc: func(owner, repo, name string) (*int, error) {
				id, ok := labelIDs[name]
				if !ok {
					return nil, nil
				}
				return &id, nil
			},
			AddLabelFunc:    func(owner, repo string, issueNumber, labelID int) error { return nil },
			RemoveLabelFunc: func(owner, repo string, issueNumber, labelID int) error { return nil },
		}
		deps := orchDeps{fj: fjMock}

		err := handleUnclearIssue(context.Background(), issue, "owner", "repo", "- q1", deps)
		if !errors.Is(err, errIssueUnclear) {
			t.Fatalf("err = %v, want errIssueUnclear", err)
		}

		added := fjMock.AddLabelCalls()
		if len(added) != 1 || added[0].LabelID != 10 {
			t.Errorf("AddLabel = %+v, want one call with labelID 10", added)
		}
		removed := fjMock.RemoveLabelCalls()
		if len(removed) != 1 || removed[0].LabelID != 20 {
			t.Errorf("RemoveLabel = %+v, want one call with labelID 20", removed)
		}
	})

	t.Run("comment failure returns error", func(t *testing.T) {
		fjMock := &forgejo.MockForgejo{
			CreateCommentFunc: func(owner, repo string, issueNumber int, body string) error {
				return errors.New("network down")
			},
		}
		deps := orchDeps{fj: fjMock}

		err := handleUnclearIssue(context.Background(), models.ForgejoIssue{Number: 1}, "owner", "repo", "- q", deps)
		if err == nil || errors.Is(err, errIssueUnclear) {
			t.Errorf("err = %v, want wrapped comment error", err)
		}
	})
}
