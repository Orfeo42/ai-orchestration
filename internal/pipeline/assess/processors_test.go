package assess

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redisClient "github.com/redis/go-redis/v9"

	"ai-orchestration/internal/agent"
	"ai-orchestration/internal/events"
	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/pipeline"
	"ai-orchestration/internal/pipeline/state"
	"ai-orchestration/internal/projects"
	"ai-orchestration/internal/queue"
	"ai-orchestration/internal/runstore"
)

func newHandle(t *testing.T) *runstore.RunHandle {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redisClient.NewClient(&redisClient.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := runstore.NewRedisFromClient(client)
	return runstore.NewRunHandle(context.Background(), store, nil, runstore.Run{
		Service: runstore.ServiceAssess,
		Phase:   runstore.PhaseAssessing,
	})
}

func TestHydrateIssue(t *testing.T) {
	t.Run("populates state.Issue from job fields", func(t *testing.T) {
		s := &state.Assess{Job: models.AssessJob{
			RepoOwner: "acme", RepoName: "x", IssueNumber: 5,
			IssueTitle: "Add search", IssueBody: "need search endpoint",
			Labels: []string{"To-do", "project:x"},
		}}
		if err := (HydrateIssue{}).Run(context.Background(), s); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if s.Issue.Number != 5 || s.Issue.Title != "Add search" || s.Issue.Body != "need search endpoint" {
			t.Errorf("issue = %+v, want number=5 title/body set", s.Issue)
		}
	})
}

func TestFetchComments(t *testing.T) {
	t.Run("fills state.Comments from forgejo", func(t *testing.T) {
		want := []models.IssueComment{{ID: 1, Author: "alice", Body: "clarify"}}
		fj := &forgejo.MockForgejo{
			GetCommentsFunc: func(owner, repo string, n int) ([]models.IssueComment, error) { return want, nil },
		}
		s := &state.Assess{Job: models.AssessJob{RepoOwner: "a", RepoName: "b", IssueNumber: 1}}
		if err := (FetchComments{Fj: fj}).Run(context.Background(), s); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if len(s.Comments) != 1 || s.Comments[0].Author != "alice" {
			t.Errorf("comments = %+v, want one alice comment", s.Comments)
		}
	})

	t.Run("swallows fetch errors and leaves comments empty", func(t *testing.T) {
		fj := &forgejo.MockForgejo{
			GetCommentsFunc: func(owner, repo string, n int) ([]models.IssueComment, error) {
				return nil, errors.New("forgejo down")
			},
		}
		s := &state.Assess{Job: models.AssessJob{RepoOwner: "a", RepoName: "b", IssueNumber: 1}, Comments: []models.IssueComment{{ID: 7}}}
		if err := (FetchComments{Fj: fj}).Run(context.Background(), s); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if s.Comments != nil {
			t.Errorf("comments = %+v, want nil on fetch error", s.Comments)
		}
	})
}

func TestAssessClarity(t *testing.T) {
	t.Run("clear result returns nil and marks comments seen", func(t *testing.T) {
		now := time.Now()
		claude := &agent.MockAgent{
			AssessIssueFunc: func(ctx context.Context, issue models.ForgejoIssue, comments []models.IssueComment, path string, sink events.Sink) (agent.AssessResult, error) {
				return agent.AssessResult{Assessment: agent.Assessment{Clear: true}, SessionID: "s1"}, nil
			},
		}
		s := &state.Assess{
			Handle:   newHandle(t),
			Proj:     projects.Config{Repo: "o/a"},
			Comments: []models.IssueComment{{ID: 1, UpdatedAt: now}},
		}
		if err := (AssessClarity{Claude: claude}).Run(context.Background(), s); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if s.Result.SessionID != "s1" {
			t.Errorf("result.SessionID = %q, want s1", s.Result.SessionID)
		}
		if fresh := s.Handle.FreshComments([]models.IssueComment{{ID: 1, UpdatedAt: now}}); len(fresh) != 0 {
			t.Errorf("FreshComments = %+v, want empty (all marked seen)", fresh)
		}
	})

	t.Run("unclear result returns ErrIssueUnclear", func(t *testing.T) {
		claude := &agent.MockAgent{
			AssessIssueFunc: func(ctx context.Context, issue models.ForgejoIssue, comments []models.IssueComment, path string, sink events.Sink) (agent.AssessResult, error) {
				return agent.AssessResult{Assessment: agent.Assessment{Clear: false, Questions: "what scope?"}}, nil
			},
		}
		s := &state.Assess{Handle: newHandle(t), Proj: projects.Config{Repo: "o/a"}}
		err := (AssessClarity{Claude: claude}).Run(context.Background(), s)
		if !errors.Is(err, pipeline.ErrIssueUnclear) {
			t.Errorf("err = %v, want ErrIssueUnclear", err)
		}
		if s.Result.Assessment.Questions != "what scope?" {
			t.Errorf("questions = %q, want preserved", s.Result.Assessment.Questions)
		}
	})
}

func TestEnqueueImpl(t *testing.T) {
	t.Run("pushes ImplJob to queue:impl", func(t *testing.T) {
		var got models.ImplJob
		rq := &queue.MockQueue{
			PushFunc: func(ctx context.Context, name string, v any) error {
				if name != queue.QueueImpl {
					t.Errorf("queue = %q, want %q", name, queue.QueueImpl)
				}
				b, _ := json.Marshal(v)
				return json.Unmarshal(b, &got)
			},
		}
		s := &state.Assess{Job: models.AssessJob{
			RepoOwner: "o", RepoName: "r", IssueNumber: 9,
			IssueTitle: "T", IssueBody: "B", Project: "o/a",
		}}
		if err := (EnqueueImpl{Rq: rq}).Run(context.Background(), s); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if got.IssueNumber != 9 || got.IssueTitle != "T" || got.Project != "o/a" {
			t.Errorf("impl job = %+v, want carry-forward", got)
		}
	})
}
