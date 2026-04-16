package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"ai-orchestration/internal/events"
	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/gemini"
	"ai-orchestration/internal/gitops"
	"ai-orchestration/internal/models"
)

func TestBuildReviewBody(t *testing.T) {
	t.Run("approved with suggestions", func(t *testing.T) {
		result := models.ReviewResult{
			Summary:     "Looks good.",
			Approved:    true,
			Suggestions: []string{"Add a test", "Rename var"},
		}
		body := buildReviewBody(result)
		if !strings.Contains(body, "APPROVED") {
			t.Error("body missing APPROVED verdict")
		}
		if !strings.Contains(body, "Looks good.") {
			t.Error("body missing summary")
		}
		if !strings.Contains(body, "Add a test") {
			t.Error("body missing suggestion")
		}
	})

	t.Run("not approved with issues", func(t *testing.T) {
		result := models.ReviewResult{
			Summary:  "Has problems.",
			Approved: false,
			Issues:   []string{"Missing error check"},
		}
		body := buildReviewBody(result)
		if !strings.Contains(body, "CHANGES REQUESTED") {
			t.Error("body missing CHANGES REQUESTED verdict")
		}
		if !strings.Contains(body, "Missing error check") {
			t.Error("body missing issue")
		}
	})

	t.Run("no issues or suggestions", func(t *testing.T) {
		result := models.ReviewResult{Summary: "Fine.", Approved: true}
		body := buildReviewBody(result)
		if strings.Contains(body, "### Issues") {
			t.Error("body should not contain Issues section when empty")
		}
		if strings.Contains(body, "### Suggestions") {
			t.Error("body should not contain Suggestions section when empty")
		}
	})
}

func TestRunPipeline(t *testing.T) {
	t.Run("reviewer error propagated", func(t *testing.T) {
		deps := reviewDeps{
			reviewer: &gemini.MockReviewer{
				ReviewFunc: func(ctx context.Context, job models.ReviewJob, sink events.Sink) (models.ReviewResult, error) {
					return models.ReviewResult{}, errors.New("gemini down")
				},
			},
			docsWriter: &gemini.MockDocsWriter{},
			fj:         &forgejo.MockForgejo{},
			gitFactory: func(_, _, _, _, _ string) gitops.Git { return &gitops.MockGit{} },
		}
		job := models.ReviewJob{PRNumber: 1, PRTitle: "feat: thing"}
		err := runPipeline(context.Background(), job, deps, config{})
		if err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("full pipeline success", func(t *testing.T) {
		gitMock := &gitops.MockGit{
			FetchFunc:                func() error { return nil },
			CheckoutRemoteBranchFunc: func(branch string) error { return nil },
		}
		deps := reviewDeps{
			reviewer: &gemini.MockReviewer{
				ReviewFunc: func(ctx context.Context, job models.ReviewJob, sink events.Sink) (models.ReviewResult, error) {
					return models.ReviewResult{Summary: "ok", Approved: true}, nil
				},
			},
			docsWriter: &gemini.MockDocsWriter{
				UpdateReadmeFunc: func(ctx context.Context, job models.ReviewJob, review models.ReviewResult, sink events.Sink) (bool, error) {
					return false, nil
				},
			},
			fj: &forgejo.MockForgejo{
				CreatePRReviewFunc: func(owner, repo string, prNumber int, body string, approved bool) error {
					return nil
				},
			},
			gitFactory: func(_, _, _, _, _ string) gitops.Git { return gitMock },
		}
		job := models.ReviewJob{PRNumber: 42, PRTitle: "feat: x", PRBranch: "feature/1"}
		err := runPipeline(context.Background(), job, deps, config{})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("readme updated commits and pushes", func(t *testing.T) {
		pushed := false
		gitMock := &gitops.MockGit{
			FetchFunc:                func() error { return nil },
			CheckoutRemoteBranchFunc: func(branch string) error { return nil },
			AddFunc:                  func(path string) error { return nil },
			CommitFunc:               func(message string) error { return nil },
			PushFunc: func(branch string) error {
				pushed = true
				return nil
			},
		}
		deps := reviewDeps{
			reviewer: &gemini.MockReviewer{
				ReviewFunc: func(ctx context.Context, job models.ReviewJob, sink events.Sink) (models.ReviewResult, error) {
					return models.ReviewResult{Summary: "ok", Approved: true}, nil
				},
			},
			docsWriter: &gemini.MockDocsWriter{
				UpdateReadmeFunc: func(ctx context.Context, job models.ReviewJob, review models.ReviewResult, sink events.Sink) (bool, error) {
					return true, nil
				},
			},
			fj: &forgejo.MockForgejo{
				CreatePRReviewFunc: func(owner, repo string, prNumber int, body string, approved bool) error {
					return nil
				},
			},
			gitFactory: func(_, _, _, _, _ string) gitops.Git { return gitMock },
		}
		job := models.ReviewJob{PRNumber: 43, PRTitle: "feat: y", PRBranch: "feature/2"}
		if err := runPipeline(context.Background(), job, deps, config{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !pushed {
			t.Error("expected git push to be called")
		}
	})
}
