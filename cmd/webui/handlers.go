package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"ai-orchestration/internal/control"
	"ai-orchestration/internal/runstore"
)

type apiHandler struct {
	store runstore.Store
	ctrl  control.Channel
}

func (h *apiHandler) listRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := runstore.ListFilter{
		Service: runstore.Service(q.Get("service")),
		Phase:   runstore.Phase(q.Get("phase")),
		Limit:   parseIntDefault(q.Get("limit"), 50),
		Cursor:  int64(parseIntDefault(q.Get("cursor"), 0)),
	}
	runs, err := h.store.List(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (h *apiHandler) listActive(w http.ResponseWriter, r *http.Request) {
	runs, err := h.store.Active(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (h *apiHandler) getRun(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, run)
}

func (h *apiHandler) getRunEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	q := r.URL.Query()
	since := int64(parseIntDefault(q.Get("since"), 0))
	limit := parseIntDefault(q.Get("limit"), 1000)
	out, err := h.store.EventsRange(r.Context(), id, since, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out})
}

func (h *apiHandler) cancelRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.ctrl.Cancel(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *apiHandler) injectRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Note string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if body.Note == "" {
		writeError(w, http.StatusBadRequest, errors.New("note required"))
		return
	}
	if err := h.store.Update(r.Context(), id, func(rn *runstore.Run) {
		rn.InjectNotes = append(rn.InjectNotes, body.Note)
	}); err != nil && !errors.Is(err, runstore.ErrNotFound) {
		slog.WarnContext(r.Context(), "inject note persist failed",
			"run", id,
			"error", err,
		)
	}
	if err := h.ctrl.Inject(r.Context(), id, body.Note); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *apiHandler) getDailyTokens(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	to := time.Now().UTC()
	from := to.AddDate(0, 0, -29)
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			from = t
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			to = t
		}
	}
	totals, err := h.store.DailyTokens(r.Context(), from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"from":  from.Format("2006-01-02"),
		"to":    to.Format("2006-01-02"),
		"daily": totals,
	})
}

func (h *apiHandler) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Warn("write json failed",
			"error", err,
		)
	}
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

