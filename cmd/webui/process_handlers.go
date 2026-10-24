package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Orfeo42/ai-orchestration/internal/analytics"
	"github.com/Orfeo42/ai-orchestration/internal/control"
	"github.com/Orfeo42/ai-orchestration/internal/events"
	"github.com/Orfeo42/ai-orchestration/internal/forgejo"
	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/internal/runstore"
	"github.com/Orfeo42/ai-orchestration/internal/step"
	"github.com/Orfeo42/ai-orchestration/internal/steps"
	"github.com/Orfeo42/ai-orchestration/internal/trigger"
	"github.com/Orfeo42/ai-orchestration/internal/workflow"
)

//go:generate moq -rm -out process_reader_mock.go . processReader:MockProcessReader
//go:generate moq -rm -out process_controller_mock.go . processController:MockProcessController
//go:generate moq -rm -out process_starter_mock.go . processStarter:MockProcessStarter
//go:generate moq -rm -out event_reader_mock.go . eventReader:MockEventReader

type webError string

func (e webError) Error() string { return string(e) }

const errNoteRequired webError = "note required"
const errOutcomeRequired webError = "outcome required"
const errStartFieldsRequired webError = "workflow and subject_key required"
const errInvalidCursor webError = "invalid cursor"
const errInvalidTime webError = "invalid time, expected YYYY-MM-DD or RFC3339"
const errInvalidRange webError = "from must not be after to"

const dateLayout = "2006-01-02"

type processReader interface {
	GetProcess(ctx context.Context, id string) (process.Process, error)
	ListProcesses(ctx context.Context, filter process.ListFilter) (process.Page, error)
	ListStepRuns(ctx context.Context, processID string) ([]process.StepRun, error)
	GetStepRun(ctx context.Context, id string) (process.StepRun, error)
	GetWorkflowVersion(ctx context.Context, id int64) (process.WorkflowVersion, error)
	LatestWorkflowVersions(ctx context.Context) ([]process.WorkflowVersion, error)
	ListWorkflowVersions(ctx context.Context, name string) ([]process.WorkflowVersion, error)
}

type processController interface {
	Cancel(ctx context.Context, processID string) error
	Retry(ctx context.Context, processID, stepID string) error
	Signal(ctx context.Context, processID, outcome string) error
}

type processStarter interface {
	StartForIssue(ctx context.Context, workflow, subjectKey string) (process.Process, error)
}

type eventReader interface {
	EventsRange(ctx context.Context, runID string, sinceSeq int64, limit int) ([]events.Event, error)
	EventsTail(ctx context.Context, runID string, lastStreamID string, block time.Duration) (runstore.EventTail, error)
}

type processHandler struct {
	reader  processReader
	engine  processController
	starter processStarter
	stats   analytics.Querier
	events  eventReader
	ctrl    control.Channel
	fj      forgejo.Forgejo
}

type processPageResponse struct {
	Processes  []process.Process `json:"processes"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

type processDetailResponse struct {
	Process  process.Process         `json:"process"`
	StepRuns []process.StepRun       `json:"step_runs"`
	Workflow process.WorkflowVersion `json:"workflow"`
}

type startProcessRequest struct {
	Workflow   string `json:"workflow"`
	SubjectKey string `json:"subject_key"`
}

type retryProcessRequest struct {
	Step string `json:"step"`
}

type signalProcessRequest struct {
	Outcome string `json:"outcome"`
}

type injectStepRunRequest struct {
	Note string `json:"note"`
}

func writeProcessError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, process.ErrNotFound), errors.Is(err, errRunNoPR), errors.Is(err, trigger.ErrUnknownWorkflow):
		status = http.StatusNotFound
	case errors.Is(err, process.ErrNotActive), errors.Is(err, trigger.ErrProcessExists):
		status = http.StatusConflict
	case errors.Is(err, workflow.ErrUnknownStep), errors.Is(err, workflow.ErrUnhandledResult), errors.Is(err, step.ErrUnknownStep),
		errors.Is(err, trigger.ErrInvalidSubject), errors.Is(err, trigger.ErrNoProject):
		status = http.StatusBadRequest
	}
	if status >= http.StatusInternalServerError {
		slog.ErrorContext(r.Context(), "process api failed",
			"method", r.Method,
			"path", r.URL.Path,
			"error", err,
		)
		writeError(w, status, err)
		return
	}
	slog.WarnContext(r.Context(), "process api rejected",
		"method", r.Method,
		"path", r.URL.Path,
		"status", status,
		"error", err,
	)
	writeError(w, status, err)
}

func writeBadRequest(w http.ResponseWriter, r *http.Request, err error) {
	slog.WarnContext(r.Context(), "process api bad request",
		"method", r.Method,
		"path", r.URL.Path,
		"error", err,
	)
	writeError(w, http.StatusBadRequest, err)
}

func (h *processHandler) listProcesses(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := process.ListFilter{
		WorkflowName: q.Get("workflow"),
		Project:      q.Get("project"),
		Limit:        parseIntDefault(q.Get("limit"), 50),
	}
	if v := q.Get("status"); v != "" {
		status, err := process.ParseStatus(v)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
		filter.Status = status
	}
	if v := q.Get("cursor"); v != "" {
		cursor, err := decodeCursor(v)
		if err != nil {
			writeBadRequest(w, r, err)
			return
		}
		filter.Cursor = &cursor
	}
	page, err := h.reader.ListProcesses(r.Context(), filter)
	if err != nil {
		writeProcessError(w, r, err)
		return
	}
	resp := processPageResponse{Processes: page.Processes}
	if resp.Processes == nil {
		resp.Processes = []process.Process{}
	}
	if page.Next != nil {
		resp.NextCursor, err = encodeCursor(*page.Next)
		if err != nil {
			writeProcessError(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func encodeCursor(c process.Cursor) (string, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeCursor(s string) (process.Cursor, error) {
	var c process.Cursor
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
	if err != nil {
		return c, errInvalidCursor
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, errInvalidCursor
	}
	return c, nil
}

func (h *processHandler) getProcess(w http.ResponseWriter, r *http.Request) {
	p, err := h.reader.GetProcess(r.Context(), r.PathValue("id"))
	if err != nil {
		writeProcessError(w, r, err)
		return
	}
	runs, err := h.reader.ListStepRuns(r.Context(), p.ID)
	if err != nil {
		writeProcessError(w, r, err)
		return
	}
	wf, err := h.reader.GetWorkflowVersion(r.Context(), p.WorkflowVersionID)
	if err != nil {
		writeProcessError(w, r, err)
		return
	}
	if runs == nil {
		runs = []process.StepRun{}
	}
	writeJSON(w, http.StatusOK, processDetailResponse{Process: p, StepRuns: runs, Workflow: wf})
}

func (h *processHandler) startProcess(w http.ResponseWriter, r *http.Request) {
	var body startProcessRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeBadRequest(w, r, err)
		return
	}
	if body.Workflow == "" || body.SubjectKey == "" {
		writeBadRequest(w, r, errStartFieldsRequired)
		return
	}
	p, err := h.starter.StartForIssue(r.Context(), body.Workflow, body.SubjectKey)
	if err != nil {
		writeProcessError(w, r, err)
		return
	}
	slog.InfoContext(r.Context(), "process started",
		"process", p.ID,
		"workflow", body.Workflow,
		"subject", body.SubjectKey,
	)
	writeJSON(w, http.StatusCreated, p)
}

func (h *processHandler) cancelProcess(w http.ResponseWriter, r *http.Request) {
	if err := h.engine.Cancel(r.Context(), r.PathValue("id")); err != nil {
		writeProcessError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *processHandler) retryProcess(w http.ResponseWriter, r *http.Request) {
	var body retryProcessRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeBadRequest(w, r, err)
		return
	}
	if err := h.engine.Retry(r.Context(), r.PathValue("id"), body.Step); err != nil {
		writeProcessError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *processHandler) signalProcess(w http.ResponseWriter, r *http.Request) {
	var body signalProcessRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeBadRequest(w, r, err)
		return
	}
	if body.Outcome == "" {
		writeBadRequest(w, r, errOutcomeRequired)
		return
	}
	if err := h.engine.Signal(r.Context(), r.PathValue("id"), body.Outcome); err != nil {
		writeProcessError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *processHandler) getStepRun(w http.ResponseWriter, r *http.Request) {
	run, err := h.reader.GetStepRun(r.Context(), r.PathValue("id"))
	if err != nil {
		writeProcessError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (h *processHandler) getStepRunEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	since := int64(parseIntDefault(q.Get("since"), 0))
	limit := parseIntDefault(q.Get("limit"), 1000)
	out, err := h.events.EventsRange(r.Context(), r.PathValue("id"), since, limit)
	if err != nil {
		writeProcessError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]events.Event{"events": out})
}

func (h *processHandler) cancelStepRun(w http.ResponseWriter, r *http.Request) {
	if err := h.ctrl.Cancel(r.Context(), r.PathValue("id")); err != nil {
		writeProcessError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *processHandler) injectStepRun(w http.ResponseWriter, r *http.Request) {
	var body injectStepRunRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeBadRequest(w, r, err)
		return
	}
	if body.Note == "" {
		writeBadRequest(w, r, errNoteRequired)
		return
	}
	if err := h.ctrl.Inject(r.Context(), r.PathValue("id"), body.Note); err != nil {
		writeProcessError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *processHandler) getStepRunDiff(w http.ResponseWriter, r *http.Request) {
	run, err := h.reader.GetStepRun(r.Context(), r.PathValue("id"))
	if err != nil {
		writeProcessError(w, r, err)
		return
	}
	p, err := h.reader.GetProcess(r.Context(), run.ProcessID)
	if err != nil {
		writeProcessError(w, r, err)
		return
	}
	pr, ok, err := process.Get(p.Vars, steps.KeyPR)
	if err != nil {
		writeProcessError(w, r, err)
		return
	}
	if !ok || pr.Number == 0 || pr.RepoOwner == "" || pr.RepoName == "" {
		writeProcessError(w, r, errRunNoPR)
		return
	}
	writePRDiff(w, r, h.fj, pr.RepoOwner, pr.RepoName, pr.Number)
}

func (h *processHandler) listWorkflows(w http.ResponseWriter, r *http.Request) {
	versions, err := h.reader.LatestWorkflowVersions(r.Context())
	if err != nil {
		writeProcessError(w, r, err)
		return
	}
	if versions == nil {
		versions = []process.WorkflowVersion{}
	}
	writeJSON(w, http.StatusOK, map[string][]process.WorkflowVersion{"workflows": versions})
}

func (h *processHandler) getWorkflow(w http.ResponseWriter, r *http.Request) {
	versions, err := h.reader.ListWorkflowVersions(r.Context(), r.PathValue("name"))
	if err != nil {
		writeProcessError(w, r, err)
		return
	}
	if versions == nil {
		versions = []process.WorkflowVersion{}
	}
	writeJSON(w, http.StatusOK, map[string][]process.WorkflowVersion{"versions": versions})
}

func parseTime(s string) (time.Time, error) {
	if t, err := time.Parse(dateLayout, s); err == nil {
		return t, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, errInvalidTime
	}
	return t, nil
}

func parseRange(r *http.Request) (analytics.Range, error) {
	q := r.URL.Query()
	to := time.Now().UTC()
	from := to.AddDate(0, 0, -30)
	var err error
	if v := q.Get("from"); v != "" {
		if from, err = parseTime(v); err != nil {
			return analytics.Range{}, err
		}
	}
	if v := q.Get("to"); v != "" {
		if to, err = parseTime(v); err != nil {
			return analytics.Range{}, err
		}
		if len(v) == len(dateLayout) {
			to = to.AddDate(0, 0, 1)
		}
	}
	if from.After(to) {
		return analytics.Range{}, errInvalidRange
	}
	return analytics.Range{From: from, To: to}, nil
}

func (h *processHandler) analyticsWorkflows(w http.ResponseWriter, r *http.Request) {
	rng, err := parseRange(r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	out, err := h.stats.Workflows(r.Context(), rng)
	if err != nil {
		writeProcessError(w, r, err)
		return
	}
	if out == nil {
		out = []analytics.WorkflowStats{}
	}
	writeJSON(w, http.StatusOK, map[string][]analytics.WorkflowStats{"workflows": out})
}

func (h *processHandler) analyticsSteps(w http.ResponseWriter, r *http.Request) {
	rng, err := parseRange(r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	out, err := h.stats.Steps(r.Context(), r.URL.Query().Get("workflow"), rng)
	if err != nil {
		writeProcessError(w, r, err)
		return
	}
	if out == nil {
		out = []analytics.StepStats{}
	}
	writeJSON(w, http.StatusOK, map[string][]analytics.StepStats{"steps": out})
}

func (h *processHandler) analyticsTokens(w http.ResponseWriter, r *http.Request) {
	rng, err := parseRange(r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	group, err := analytics.ParseGrouping(r.URL.Query().Get("group"))
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	out, err := h.stats.Tokens(r.Context(), rng, group)
	if err != nil {
		writeProcessError(w, r, err)
		return
	}
	if out == nil {
		out = []analytics.TokenPoint{}
	}
	writeJSON(w, http.StatusOK, map[string][]analytics.TokenPoint{"points": out})
}

func (h *processHandler) analyticsFailures(w http.ResponseWriter, r *http.Request) {
	rng, err := parseRange(r)
	if err != nil {
		writeBadRequest(w, r, err)
		return
	}
	out, err := h.stats.Failures(r.Context(), rng)
	if err != nil {
		writeProcessError(w, r, err)
		return
	}
	if out == nil {
		out = []analytics.FailureGroup{}
	}
	writeJSON(w, http.StatusOK, map[string][]analytics.FailureGroup{"failures": out})
}
