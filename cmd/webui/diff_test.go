package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ai-orchestration/internal/runstore"
)

func TestParseUnifiedDiff(t *testing.T) {
	t.Run("single modified file with additions and deletions", func(t *testing.T) {
		raw := "diff --git a/foo.go b/foo.go\nindex aaa..bbb 100644\n--- a/foo.go\n+++ b/foo.go\n@@ -1,3 +1,4 @@\n line one\n-old line\n+new line\n+extra line\n line three\n"
		got := parseUnifiedDiff(raw)
		require.Len(t, got, 1)
		assert.Equal(t, "foo.go", got[0].Path)
		assert.Equal(t, "modified", got[0].Status)
		assert.Equal(t, 2, got[0].Additions)
		assert.Equal(t, 1, got[0].Deletions)
		assert.Contains(t, got[0].Patch, "@@ -1,3 +1,4 @@")
	})

	t.Run("added file marked as added", func(t *testing.T) {
		raw := "diff --git a/new.go b/new.go\nnew file mode 100644\n--- /dev/null\n+++ b/new.go\n@@ -0,0 +1,1 @@\n+hello\n"
		got := parseUnifiedDiff(raw)
		require.Len(t, got, 1)
		assert.Equal(t, "new.go", got[0].Path)
		assert.Equal(t, "added", got[0].Status)
		assert.Equal(t, 1, got[0].Additions)
	})

	t.Run("multiple files split correctly", func(t *testing.T) {
		raw := "diff --git a/a b/a\n--- a/a\n+++ b/a\n+x\ndiff --git a/b b/b\n--- a/b\n+++ b/b\n+y\n"
		got := parseUnifiedDiff(raw)
		require.Len(t, got, 2)
		assert.Equal(t, "a", got[0].Path)
		assert.Equal(t, "b", got[1].Path)
	})

	t.Run("empty input returns empty slice", func(t *testing.T) {
		got := parseUnifiedDiff("")
		assert.Empty(t, got)
	})
}

func TestGetRunDiff(t *testing.T) {
	t.Run("returns parsed files for run with pr", func(t *testing.T) {
		s := newTestServer(t)
		ctx := context.Background()
		require.NoError(t, s.store.Create(ctx, runstore.Run{
			ID:        "rD",
			Service:   runstore.ServiceImpl,
			RepoOwner: "acme",
			RepoName:  "x",
			PRNumber:  42,
			Phase:     runstore.PhaseDone,
			StartedAt: time.Now().UTC(),
		}))
		s.fj.GetPRDiffFunc = func(owner, repo string, prNumber int) (string, error) {
			assert.Equal(t, "acme", owner)
			assert.Equal(t, "x", repo)
			assert.Equal(t, 42, prNumber)
			return "diff --git a/foo.go b/foo.go\n--- a/foo.go\n+++ b/foo.go\n+added\n", nil
		}

		resp, err := http.Get(s.ts.URL + "/api/runs/rD/diff")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var body struct {
			PRNumber int        `json:"pr_number"`
			Files    []fileDiff `json:"files"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		assert.Equal(t, 42, body.PRNumber)
		require.Len(t, body.Files, 1)
		assert.Equal(t, "foo.go", body.Files[0].Path)
	})

	t.Run("returns empty list when run has no pr", func(t *testing.T) {
		s := newTestServer(t)
		ctx := context.Background()
		require.NoError(t, s.store.Create(ctx, runstore.Run{
			ID:        "rE",
			Service:   runstore.ServiceAssess,
			Phase:     runstore.PhaseDone,
			StartedAt: time.Now().UTC(),
		}))
		resp, err := http.Get(s.ts.URL + "/api/runs/rE/diff")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var body struct {
			PRNumber int        `json:"pr_number"`
			Files    []fileDiff `json:"files"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		assert.Equal(t, 0, body.PRNumber)
		assert.Empty(t, body.Files)
	})

	t.Run("returns 404 for unknown run", func(t *testing.T) {
		s := newTestServer(t)
		resp, err := http.Get(s.ts.URL + "/api/runs/missing/diff")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}
