package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/queue"
)

func TestPollRepo(t *testing.T) {
	t.Run("enqueues assess job for eligible issue", func(t *testing.T) {
		fj := &forgejo.MockForgejo{
			GetIssuesFunc: func(owner, repo, label string, assignee *string) ([]models.ForgejoIssue, error) {
				return []models.ForgejoIssue{
					{Number: 1, Title: "Ship it", Labels: []string{"To-do", "project:app"}},
				}, nil
			},
		}
		var pushed [][]byte
		rq := &queue.MockQueue{
			IsProcessedFunc:    func(ctx context.Context, key string) (bool, error) { return false, nil },
			IsInProgressFunc:   func(ctx context.Context, key string) (bool, error) { return false, nil },
			MarkInProgressFunc: func(ctx context.Context, key string, ttl time.Duration) error { return nil },
			PushFunc: func(ctx context.Context, queueName string, v any) error {
				if queueName != queue.QueueAssess {
					t.Errorf("queue name = %q, want %q", queueName, queue.QueueAssess)
				}
				b, err := json.Marshal(v)
				if err != nil {
					return err
				}
				pushed = append(pushed, b)
				return nil
			},
		}
		deps := producerDeps{fj: fj, rq: rq}
		cfg := config{projects: map[string]projectConfig{"app": {Repo: "owner/app", BaseBranch: "main"}}}

		pollRepo(context.Background(), "owner", "repo", deps, cfg)

		if len(pushed) != 1 {
			t.Fatalf("pushed = %d jobs, want 1", len(pushed))
		}
		var job models.AssessJob
		if err := json.Unmarshal(pushed[0], &job); err != nil {
			t.Fatalf("unmarshal job: %v", err)
		}
		if job.IssueNumber != 1 || job.Project != "owner/app" || job.RepoOwner != "owner" || job.RepoName != "repo" {
			t.Errorf("job = %+v, want {owner,repo,#1,owner/app}", job)
		}
	})

	t.Run("skips needs-info issues", func(t *testing.T) {
		fj := &forgejo.MockForgejo{
			GetIssuesFunc: func(owner, repo, label string, assignee *string) ([]models.ForgejoIssue, error) {
				return []models.ForgejoIssue{
					{Number: 2, Labels: []string{"To-do", "project:app", forgejo.LabelNeedsInfo}},
				}, nil
			},
		}
		rq := &queue.MockQueue{
			IsProcessedFunc: func(ctx context.Context, key string) (bool, error) { return false, nil },
			PushFunc: func(ctx context.Context, queueName string, v any) error {
				t.Fatal("Push must not be called for needs-info issue")
				return nil
			},
		}
		deps := producerDeps{fj: fj, rq: rq}
		cfg := config{projects: map[string]projectConfig{"app": {Repo: "owner/app", BaseBranch: "main"}}}
		pollRepo(context.Background(), "owner", "repo", deps, cfg)
	})

	t.Run("skips already-processed issues", func(t *testing.T) {
		fj := &forgejo.MockForgejo{
			GetIssuesFunc: func(owner, repo, label string, assignee *string) ([]models.ForgejoIssue, error) {
				return []models.ForgejoIssue{
					{Number: 3, Labels: []string{"To-do", "project:app"}},
				}, nil
			},
		}
		rq := &queue.MockQueue{
			IsProcessedFunc: func(ctx context.Context, key string) (bool, error) { return true, nil },
			PushFunc: func(ctx context.Context, queueName string, v any) error {
				t.Fatal("Push must not be called for processed issue")
				return nil
			},
		}
		deps := producerDeps{fj: fj, rq: rq}
		cfg := config{projects: map[string]projectConfig{"app": {Repo: "owner/app", BaseBranch: "main"}}}
		pollRepo(context.Background(), "owner", "repo", deps, cfg)
	})

	t.Run("skips issues missing project label", func(t *testing.T) {
		fj := &forgejo.MockForgejo{
			GetIssuesFunc: func(owner, repo, label string, assignee *string) ([]models.ForgejoIssue, error) {
				return []models.ForgejoIssue{{Number: 4, Labels: []string{"To-do"}}}, nil
			},
		}
		rq := &queue.MockQueue{
			IsProcessedFunc: func(ctx context.Context, key string) (bool, error) { return false, nil },
			PushFunc: func(ctx context.Context, queueName string, v any) error {
				t.Fatal("Push must not be called without project label")
				return nil
			},
		}
		deps := producerDeps{fj: fj, rq: rq}
		cfg := config{projects: map[string]projectConfig{}}
		pollRepo(context.Background(), "owner", "repo", deps, cfg)
	})

	t.Run("skips issues already in-progress", func(t *testing.T) {
		fj := &forgejo.MockForgejo{
			GetIssuesFunc: func(owner, repo, label string, assignee *string) ([]models.ForgejoIssue, error) {
				return []models.ForgejoIssue{{Number: 5, Labels: []string{"To-do", "project:app"}}}, nil
			},
		}
		rq := &queue.MockQueue{
			IsProcessedFunc:  func(ctx context.Context, key string) (bool, error) { return false, nil },
			IsInProgressFunc: func(ctx context.Context, key string) (bool, error) { return true, nil },
			PushFunc: func(ctx context.Context, queueName string, v any) error {
				t.Fatal("Push must not be called for in-progress issue")
				return nil
			},
		}
		deps := producerDeps{fj: fj, rq: rq}
		cfg := config{projects: map[string]projectConfig{"app": {Repo: "owner/app", BaseBranch: "main"}}}
		pollRepo(context.Background(), "owner", "repo", deps, cfg)
	})

	t.Run("clears in-progress if push fails", func(t *testing.T) {
		fj := &forgejo.MockForgejo{
			GetIssuesFunc: func(owner, repo, label string, assignee *string) ([]models.ForgejoIssue, error) {
				return []models.ForgejoIssue{{Number: 6, Labels: []string{"To-do", "project:app"}}}, nil
			},
		}
		var cleared bool
		rq := &queue.MockQueue{
			IsProcessedFunc:    func(ctx context.Context, key string) (bool, error) { return false, nil },
			IsInProgressFunc:   func(ctx context.Context, key string) (bool, error) { return false, nil },
			MarkInProgressFunc: func(ctx context.Context, key string, ttl time.Duration) error { return nil },
			PushFunc:           func(ctx context.Context, queueName string, v any) error { return errors.New("redis down") },
			ClearInProgressFunc: func(ctx context.Context, key string) error {
				cleared = true
				return nil
			},
		}
		deps := producerDeps{fj: fj, rq: rq}
		cfg := config{projects: map[string]projectConfig{"app": {Repo: "owner/app", BaseBranch: "main"}}}
		pollRepo(context.Background(), "owner", "repo", deps, cfg)
		if !cleared {
			t.Error("ClearInProgress must be called when push fails")
		}
	})
}
