package agent

import (
	"testing"

	"ai-orchestration/internal/models"
)

func TestBuildPrompt(t *testing.T) {
	t.Run("no test error", func(t *testing.T) {
		issue := models.ForgejoIssue{Number: 42, Title: "Add login", Body: "Implement login flow"}
		got := buildPrompt(issue, nil)
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
		got := buildPrompt(issue, &errMsg)
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
