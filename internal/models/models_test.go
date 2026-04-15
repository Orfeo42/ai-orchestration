package models

import (
	"testing"
)

func TestParseReviewText(t *testing.T) {
	t.Run("full response approved", func(t *testing.T) {
		input := `SUMMARY: Looks good overall.
APPROVED: yes
ISSUES:
SUGGESTIONS:
- Consider adding a test`
		got := ParseReviewText(input)
		if got.Summary != "Looks good overall." {
			t.Errorf("summary = %q, want %q", got.Summary, "Looks good overall.")
		}
		if !got.Approved {
			t.Error("approved = false, want true")
		}
		if len(got.Issues) != 0 {
			t.Errorf("issues len = %d, want 0", len(got.Issues))
		}
		if len(got.Suggestions) != 1 || got.Suggestions[0] != "Consider adding a test" {
			t.Errorf("suggestions = %v, want [Consider adding a test]", got.Suggestions)
		}
	})

	t.Run("not approved with issues", func(t *testing.T) {
		input := `SUMMARY: Has blocking issues.
APPROVED: no
ISSUES:
- Missing error handling
- Nil pointer dereference
SUGGESTIONS:`
		got := ParseReviewText(input)
		if got.Approved {
			t.Error("approved = true, want false")
		}
		if len(got.Issues) != 2 {
			t.Fatalf("issues len = %d, want 2", len(got.Issues))
		}
		if got.Issues[0] != "Missing error handling" {
			t.Errorf("issues[0] = %q, want %q", got.Issues[0], "Missing error handling")
		}
		if got.Issues[1] != "Nil pointer dereference" {
			t.Errorf("issues[1] = %q, want %q", got.Issues[1], "Nil pointer dereference")
		}
	})

	t.Run("empty input", func(t *testing.T) {
		got := ParseReviewText("")
		if got.Approved {
			t.Error("approved = true, want false")
		}
		if got.Summary != "" {
			t.Errorf("summary = %q, want empty", got.Summary)
		}
		if len(got.Issues) != 0 {
			t.Errorf("issues len = %d, want 0", len(got.Issues))
		}
		if len(got.Suggestions) != 0 {
			t.Errorf("suggestions len = %d, want 0", len(got.Suggestions))
		}
	})

	t.Run("approved yes case insensitive", func(t *testing.T) {
		input := "SUMMARY: ok\nAPPROVED: YES\n"
		got := ParseReviewText(input)
		if !got.Approved {
			t.Error("approved = false, want true")
		}
	})
}
