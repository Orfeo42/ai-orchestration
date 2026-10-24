package forgejo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Orfeo42/ai-orchestration/internal/models"
)

func TestGetIssues(t *testing.T) {
	t.Run("returns parsed issues", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			payload := []map[string]any{
				{
					"id":     1,
					"number": 10,
					"title":  "Fix bug",
					"body":   "details",
					"labels": []map[string]any{{"name": "To-do"}, {"name": "project:myapp"}},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(payload); err != nil {
				t.Errorf("encode: %v", err)
			}
		}))
		defer srv.Close()

		c := New(srv.URL, "tok")
		issues, err := c.GetIssues("owner", "repo", "To-do", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(issues) != 1 {
			t.Fatalf("len = %d, want 1", len(issues))
		}
		if issues[0].Number != 10 {
			t.Errorf("number = %d, want 10", issues[0].Number)
		}
		if issues[0].Title != "Fix bug" {
			t.Errorf("title = %q, want Fix bug", issues[0].Title)
		}
		if len(issues[0].Labels) != 2 {
			t.Errorf("labels len = %d, want 2", len(issues[0].Labels))
		}
	})

	t.Run("server error returns error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		c := New(srv.URL, "tok")
		_, err := c.GetIssues("owner", "repo", "To-do", nil)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestGetLabelID(t *testing.T) {
	t.Run("label found returns id", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			payload := []map[string]any{
				{"id": 5, "name": "done"},
				{"id": 7, "name": "in-progress"},
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(payload); err != nil {
				t.Errorf("encode: %v", err)
			}
		}))
		defer srv.Close()

		c := New(srv.URL, "tok")
		id, err := c.GetLabelID("owner", "repo", "in-progress")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id == nil || *id != 7 {
			t.Errorf("id = %v, want 7", id)
		}
	})

	t.Run("label not found returns nil", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode([]any{}); err != nil {
				t.Errorf("encode: %v", err)
			}
		}))
		defer srv.Close()

		c := New(srv.URL, "tok")
		id, err := c.GetLabelID("owner", "repo", "missing")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != nil {
			t.Errorf("id = %v, want nil", id)
		}
	})
}

func TestCreatePR(t *testing.T) {
	t.Run("creates and returns pr", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				t.Errorf("method = %q, want POST", r.Method)
			}
			pr := models.ForgejoPR{Number: 42, Title: "feat: thing"}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			if err := json.NewEncoder(w).Encode(pr); err != nil {
				t.Errorf("encode: %v", err)
			}
		}))
		defer srv.Close()

		c := New(srv.URL, "tok")
		pr, err := c.CreatePR("owner", "repo", "feat: thing", "body", "feature/1", "main")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if pr.Number != 42 {
			t.Errorf("pr.Number = %d, want 42", pr.Number)
		}
		if pr.RepoOwner != "owner" || pr.RepoName != "repo" {
			t.Errorf("pr repo = %s/%s, want owner/repo", pr.RepoOwner, pr.RepoName)
		}
	})
}

func TestCurrentUser(t *testing.T) {
	t.Run("returns login of token owner", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/user" {
				t.Errorf("path = %s, want /api/v1/user", r.URL.Path)
			}
			if _, err := w.Write([]byte(`{"login":"ai-bot","id":7}`)); err != nil {
				t.Errorf("write: %v", err)
			}
		}))
		defer srv.Close()

		got, err := New(srv.URL, "tok").CurrentUser()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "ai-bot" {
			t.Errorf("login = %q, want ai-bot", got)
		}
	})

	t.Run("non-2xx is an error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()

		if _, err := New(srv.URL, "tok").CurrentUser(); err == nil {
			t.Fatal("expected error")
		}
	})
}
