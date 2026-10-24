package agent

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ai-orchestration/internal/models"
)

func TestBuildPrompt(t *testing.T) {
	t.Run("no test error", func(t *testing.T) {
		issue := models.ForgejoIssue{Number: 42, Title: "Add login", Body: "Implement login flow"}
		got := buildPrompt(issue, nil, nil)
		if got == "" {
			t.Error("prompt is empty")
		}
		for _, want := range []string{"42", "Add login", "Implement login flow"} {
			found := false
			for i := range got {
				if i+len(want) <= len(got) && got[i:i+len(want)] == want {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("prompt missing %q", want)
			}
		}
	})

	t.Run("with test error appended", func(t *testing.T) {
		issue := models.ForgejoIssue{Number: 1, Title: "Fix bug", Body: "desc"}
		errMsg := "FAIL: TestSomething"
		got := buildPrompt(issue, nil, &errMsg)
		if len(got) == 0 {
			t.Fatal("prompt is empty")
		}
		needle := "FAIL: TestSomething"
		found := false
		for i := range got {
			if i+len(needle) <= len(got) && got[i:i+len(needle)] == needle {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("prompt missing test error %q", needle)
		}
	})

	t.Run("fresh comments rendered as authoritative clarifications", func(t *testing.T) {
		issue := models.ForgejoIssue{Number: 9, Title: "Add search", Body: "Users need search"}
		fresh := []models.IssueComment{
			{ID: 1, Author: "alice", Body: "use fuzzy match"},
			{ID: 2, Author: "bob", Body: "limit to 20 results"},
		}
		got := buildPrompt(issue, fresh, nil)
		for _, want := range []string{"@alice", "use fuzzy match", "@bob", "limit to 20 results", "New or edited"} {
			if !strings.Contains(got, want) {
				t.Errorf("prompt missing %q", want)
			}
		}
	})

	t.Run("no fresh comments omits comments section", func(t *testing.T) {
		issue := models.ForgejoIssue{Number: 10, Title: "X", Body: "Y"}
		got := buildPrompt(issue, nil, nil)
		for _, forbidden := range []string{"New or edited", "authoritative clarifications", "(no comments)"} {
			if strings.Contains(got, forbidden) {
				t.Errorf("prompt must omit %q when no fresh comments, got: %s", forbidden, got)
			}
		}
	})
}

func TestParseAssessment(t *testing.T) {
	t.Run("clear true", func(t *testing.T) {
		got, err := parseAssessment(`{"clear": true}`)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if !got.Clear {
			t.Error("Clear = false, want true")
		}
	})

	t.Run("clear false with questions", func(t *testing.T) {
		got, err := parseAssessment(`{"clear": false, "questions": "- what X?"}`)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if got.Clear {
			t.Error("Clear = true, want false")
		}
		if got.Questions != "- what X?" {
			t.Errorf("Questions = %q", got.Questions)
		}
	})

	t.Run("json on last line ignores preamble", func(t *testing.T) {
		got, err := parseAssessment("thinking...\nmore text\n{\"clear\": true}")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if !got.Clear {
			t.Error("Clear = false, want true")
		}
	})

	t.Run("clear false without questions errors", func(t *testing.T) {
		_, err := parseAssessment(`{"clear": false}`)
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("no json line errors", func(t *testing.T) {
		_, err := parseAssessment("just prose, no json")
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("malformed json errors", func(t *testing.T) {
		_, err := parseAssessment(`{"clear": notbool}`)
		if err == nil {
			t.Error("expected error")
		}
	})
}

func TestLastJSONLine(t *testing.T) {
	t.Run("single json line", func(t *testing.T) {
		got := lastJSONLine(`{"a":1}`)
		if got != `{"a":1}` {
			t.Errorf("got %q", got)
		}
	})

	t.Run("last line is json", func(t *testing.T) {
		got := lastJSONLine("prose\n{\"a\":1}")
		if got != `{"a":1}` {
			t.Errorf("got %q", got)
		}
	})

	t.Run("no json returns empty", func(t *testing.T) {
		if got := lastJSONLine("nothing\nhere"); got != "" {
			t.Errorf("got %q", got)
		}
	})
}

func TestLogEvent(t *testing.T) {
	t.Run("empty line no panic", func(t *testing.T) {
		logEvent("")
	})

	t.Run("non-json line no panic", func(t *testing.T) {
		logEvent("plain text output")
	})

	t.Run("valid assistant event no panic", func(t *testing.T) {
		logEvent(`{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`)
	})

	t.Run("tool_result string no panic", func(t *testing.T) {
		logEvent(`{"type":"tool_result","content":"some result"}`)
	})
}

func TestBaseArgs(t *testing.T) {
	t.Run("without system prompt file", func(t *testing.T) {
		got := baseArgs(Runtime{}, "Bash,Read")
		assert.NotContains(t, got, "--append-system-prompt-file")
		idx := slices.Index(got, "--allowedTools")
		require.NotEqual(t, -1, idx)
		assert.Equal(t, "Bash,Read", got[idx+1])
	})

	t.Run("with system prompt file", func(t *testing.T) {
		got := baseArgs(Runtime{SystemPromptFile: "/tmp/headless.md"}, "Bash,Read")
		idx := slices.Index(got, "--append-system-prompt-file")
		require.NotEqual(t, -1, idx)
		assert.Equal(t, "/tmp/headless.md", got[idx+1])
	})
}
