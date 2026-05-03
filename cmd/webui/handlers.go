package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"ai-orchestration/internal/control"
	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/queue"
	"ai-orchestration/internal/runstore"
)

var errRunNotRetryable = errors.New("run is not in a retryable state")
var errRunMissingPayload = errors.New("run has no stored job payload")
var errRunUnknownService = errors.New("run has unknown service")
var errRunRebuildUnavailable = errors.New("forgejo client unavailable; cannot rebuild legacy run payload")

type apiHandler struct {
	store runstore.Store
	ctrl  control.Channel
	rq    queue.Queue
	fj    forgejo.Forgejo
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

func (h *apiHandler) retryRun(w http.ResponseWriter, r *http.Request) {
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
	if run.Phase != runstore.PhaseFailed && run.Phase != runstore.PhaseCancelled {
		writeError(w, http.StatusConflict, errRunNotRetryable)
		return
	}
	queueName, ok := queueForService(run.Service)
	if !ok {
		writeError(w, http.StatusBadRequest, errRunUnknownService)
		return
	}

	payload := run.JobPayload
	if len(payload) == 0 {
		if h.fj == nil {
			writeError(w, http.StatusUnprocessableEntity, errRunRebuildUnavailable)
			return
		}
		rebuilt, rerr := h.rebuildPayload(run)
		if rerr != nil {
			writeError(w, http.StatusUnprocessableEntity, rerr)
			return
		}
		payload = rebuilt
		if err := h.store.Update(r.Context(), id, func(rn *runstore.Run) {
			rn.JobPayload = payload
		}); err != nil {
			slog.WarnContext(r.Context(), "persist rebuilt payload failed",
				"run", id,
				"error", err,
			)
		}
	}

	if err := h.rq.Push(r.Context(), queueName, payload); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	slog.InfoContext(r.Context(), "retry enqueued",
		"run", id,
		"service", string(run.Service),
		"queue", queueName,
	)
	w.WriteHeader(http.StatusNoContent)
}

func (h *apiHandler) rebuildPayload(run runstore.Run) (json.RawMessage, error) {
	switch run.Service {
	case runstore.ServiceAssess:
		if run.IssueNumber == 0 {
			return nil, errors.New("rebuild assess: missing issue_number")
		}
		issue, err := h.fj.GetIssue(run.RepoOwner, run.RepoName, run.IssueNumber)
		if err != nil {
			return nil, fmt.Errorf("rebuild assess: %w", err)
		}
		return json.Marshal(models.AssessJob{
			RepoOwner:   run.RepoOwner,
			RepoName:    run.RepoName,
			IssueNumber: run.IssueNumber,
			IssueTitle:  issue.Title,
			IssueBody:   issue.Body,
			Project:     run.Project,
			Labels:      issue.Labels,
		})
	case runstore.ServiceImpl:
		if run.IssueNumber == 0 {
			return nil, errors.New("rebuild impl: missing issue_number")
		}
		issue, err := h.fj.GetIssue(run.RepoOwner, run.RepoName, run.IssueNumber)
		if err != nil {
			return nil, fmt.Errorf("rebuild impl: %w", err)
		}
		return json.Marshal(models.ImplJob{
			RepoOwner:   run.RepoOwner,
			RepoName:    run.RepoName,
			IssueNumber: run.IssueNumber,
			IssueTitle:  issue.Title,
			IssueBody:   issue.Body,
			Project:     run.Project,
		})
	case runstore.ServiceReview:
		if run.PRNumber == 0 {
			return nil, errors.New("rebuild review: missing pr_number")
		}
		pr, err := h.fj.GetPR(run.RepoOwner, run.RepoName, run.PRNumber)
		if err != nil {
			return nil, fmt.Errorf("rebuild review: get pr: %w", err)
		}
		diff, err := h.fj.GetPRDiff(run.RepoOwner, run.RepoName, run.PRNumber)
		if err != nil {
			return nil, fmt.Errorf("rebuild review: get diff: %w", err)
		}
		return json.Marshal(models.ReviewJob{
			RepoOwner:   run.RepoOwner,
			RepoName:    run.RepoName,
			PRNumber:    run.PRNumber,
			PRTitle:     pr.Title,
			PRBranch:    pr.Branch,
			Project:     run.Project,
			IssueNumber: run.IssueNumber,
			Diff:        diff,
		})
	}
	return nil, errRunUnknownService
}

func queueForService(s runstore.Service) (string, bool) {
	switch s {
	case runstore.ServiceAssess:
		return queue.QueueAssess, true
	case runstore.ServiceImpl:
		return queue.QueueImpl, true
	case runstore.ServiceReview:
		return queue.QueueReview, true
	}
	return "", false
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

