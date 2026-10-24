package main

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/Orfeo42/ai-orchestration/internal/forgejo"
)

const errRunNoPR webError = "run has no associated PR"
const errForgejoUnavailable webError = "forgejo client unavailable"

type fileDiff struct {
	Path      string `json:"path"`
	OldPath   string `json:"old_path,omitempty"`
	Status    string `json:"status"`
	Patch     string `json:"patch"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

type diffResponse struct {
	PRNumber int        `json:"pr_number"`
	Files    []fileDiff `json:"files"`
}

func writePRDiff(w http.ResponseWriter, r *http.Request, fj forgejo.Forgejo, owner, repo string, prNumber int) {
	if fj == nil {
		slog.ErrorContext(r.Context(), "forgejo client unavailable")
		writeError(w, http.StatusServiceUnavailable, errForgejoUnavailable)
		return
	}
	raw, err := fj.GetPRDiff(owner, repo, prNumber)
	if err != nil {
		slog.WarnContext(r.Context(), "get pr diff failed",
			"repo", owner+"/"+repo,
			"pr", prNumber,
			"error", err,
		)
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, diffResponse{PRNumber: prNumber, Files: parseUnifiedDiff(raw)})
}

func parseUnifiedDiff(raw string) []fileDiff {
	if raw == "" {
		return []fileDiff{}
	}
	out := []fileDiff{}
	lines := strings.Split(raw, "\n")
	var cur *fileDiff
	var buf strings.Builder
	flush := func() {
		if cur == nil {
			return
		}
		cur.Patch = buf.String()
		out = append(out, *cur)
		cur = nil
		buf.Reset()
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			path, oldPath := parseDiffHeader(line)
			cur = &fileDiff{Path: path, OldPath: oldPath, Status: "modified"}
			buf.WriteString(line)
			buf.WriteByte('\n')
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case strings.HasPrefix(line, "new file mode"):
			cur.Status = "added"
		case strings.HasPrefix(line, "deleted file mode"):
			cur.Status = "deleted"
		case strings.HasPrefix(line, "rename from "):
			cur.Status = "renamed"
		case strings.HasPrefix(line, "+++ "), strings.HasPrefix(line, "--- "):
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			cur.Additions++
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			cur.Deletions++
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	flush()
	return out
}

func parseDiffHeader(line string) (newPath, oldPath string) {
	rest := strings.TrimPrefix(line, "diff --git ")
	parts := strings.SplitN(rest, " ", 2)
	if len(parts) != 2 {
		return rest, ""
	}
	a := strings.TrimPrefix(strings.TrimSpace(parts[0]), "a/")
	b := strings.TrimPrefix(strings.TrimSpace(parts[1]), "b/")
	if a == b {
		return b, ""
	}
	return b, a
}
