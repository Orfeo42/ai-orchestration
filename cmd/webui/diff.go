package main

import (
	"errors"
	"net/http"
	"strings"

	"ai-orchestration/internal/runstore"
)

var errRunNoPR = errors.New("run has no associated PR")
var errForgejoUnavailable = errors.New("forgejo client unavailable")

type fileDiff struct {
	Path     string `json:"path"`
	OldPath  string `json:"old_path,omitempty"`
	Status   string `json:"status"`
	Patch    string `json:"patch"`
	Additions int   `json:"additions"`
	Deletions int   `json:"deletions"`
}

func (h *apiHandler) getRunDiff(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, err := h.store.Get(r.Context(), id)
	if errors.Is(err, runstore.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if run.PRNumber == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"pr_number": 0,
			"files":     []fileDiff{},
		})
		return
	}
	if h.fj == nil {
		writeError(w, http.StatusServiceUnavailable, errForgejoUnavailable)
		return
	}
	raw, err := h.fj.GetPRDiff(run.RepoOwner, run.RepoName, run.PRNumber)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	files := parseUnifiedDiff(raw)
	writeJSON(w, http.StatusOK, map[string]any{
		"pr_number": run.PRNumber,
		"files":     files,
	})
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
