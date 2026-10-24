package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
