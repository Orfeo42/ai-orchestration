package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Orfeo42/ai-orchestration/internal/engine"
	"github.com/Orfeo42/ai-orchestration/internal/plugins"
	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/internal/step"
	"github.com/Orfeo42/ai-orchestration/internal/workflow"
	"github.com/Orfeo42/ai-orchestration/pluginapi"
)

//go:generate moq -rm -out plugin_registry_mock.go . pluginRegistry:MockPluginRegistry
//go:generate moq -rm -out plugin_catalog_mock.go . pluginCatalog:MockPluginCatalog
//go:generate moq -rm -out job_callbacks_mock.go . jobCallbacks:MockJobCallbacks
//go:generate moq -rm -out callback_verifier_mock.go . callbackVerifier:MockCallbackVerifier
//go:generate moq -rm -out plugin_auth_mock.go . pluginAuth:MockPluginAuth
//go:generate moq -rm -out event_starter_mock.go . eventStarter:MockEventStarter
//go:generate moq -rm -out workflow_store_mock.go . workflowStore:MockWorkflowStore
//go:generate moq -rm -out workflow_codec_mock.go . workflowCodec:MockWorkflowCodec

const errPluginFieldsRequired webError = "name and base_url required"
const errPluginHeaderMissing webError = "plugin name or bearer token missing"
const errPluginMismatch webError = "event plugin does not match authenticated plugin"
const errSubjectKeyRequired webError = "subject key required"
const errWorkflowNotFound webError = "workflow not found"
const errBodyTooLarge webError = "payload too large"

type pluginRegistry interface {
	List(ctx context.Context) ([]plugins.Record, error)
	Register(ctx context.Context, name, baseURL string) (plugins.Record, error)
	Refresh(ctx context.Context, name string) (plugins.Record, error)
	Remove(ctx context.Context, name string) error
}

type pluginCatalog interface {
	Defs() []step.Def
	Triggers() []plugins.TriggerDef
	ValidateTrigger(ev pluginapi.TriggerEvent) error
}

type jobCallbacks interface {
	Result(ctx context.Context, jobID string, res pluginapi.Result) error
	Progress(ctx context.Context, jobID string, p pluginapi.Progress) error
}

type callbackVerifier interface {
	Verify(kind plugins.CallbackKind, jobID string, q url.Values, now time.Time) error
}

type pluginAuth interface {
	Authenticate(name, token string) error
}

type eventStarter interface {
	StartFromEvent(ctx context.Context, ev pluginapi.TriggerEvent) ([]string, error)
}

type workflowStore interface {
	InTx(ctx context.Context, fn func(ctx context.Context, tx process.Tx) error) error
	DeleteWorkflow(ctx context.Context, name string) (bool, error)
}

type workflowCodec interface {
	Parse(raw []byte) (workflow.Definition, error)
	Validate(def workflow.Definition) error
	YAML(def workflow.Definition) ([]byte, error)
}

type defaultCodec struct {
	catalog step.Catalog
}

func (c defaultCodec) Parse(raw []byte) (workflow.Definition, error) { return workflow.ParseJSON(raw) }

func (c defaultCodec) Validate(def workflow.Definition) error { return def.Validate(c.catalog) }

func (c defaultCodec) YAML(def workflow.Definition) ([]byte, error) { return def.YAML() }

type pluginHandler struct {
	registry  pluginRegistry
	catalog   pluginCatalog
	callbacks jobCallbacks
	verifier  callbackVerifier
	auth      pluginAuth
	starter   eventStarter
	store     workflowStore
	codec     workflowCodec
}

type catalogStep struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Queue       string          `json:"queue"`
	Outcomes    []step.Outcome  `json:"outcomes"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
	Plugin      string          `json:"plugin,omitempty"`
}

type catalogResponse struct {
	Steps    []catalogStep        `json:"steps"`
	Triggers []plugins.TriggerDef `json:"triggers"`
}

type workflowProblems struct {
	Error    string   `json:"error"`
	Problems []string `json:"problems"`
}

type registerPluginRequest struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
}

func (h *pluginHandler) getCatalog(w http.ResponseWriter, _ *http.Request) {
	defs := h.catalog.Defs()
	resp := catalogResponse{Steps: make([]catalogStep, 0, len(defs)), Triggers: h.catalog.Triggers()}
	if resp.Triggers == nil {
		resp.Triggers = []plugins.TriggerDef{}
	}
	for _, d := range defs {
		outcomes := d.Outcomes
		if outcomes == nil {
			outcomes = []step.Outcome{}
		}
		resp.Steps = append(resp.Steps, catalogStep{
			Name:        d.Name,
			Description: d.Description,
			Queue:       d.Queue,
			Outcomes:    outcomes,
			InputSchema: d.InputSchema,
			Plugin:      d.Plugin,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *pluginHandler) listPlugins(w http.ResponseWriter, r *http.Request) {
	records, err := h.registry.List(r.Context())
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, err)
		return
	}
	if records == nil {
		records = []plugins.Record{}
	}
	writeJSON(w, http.StatusOK, records)
}

func (h *pluginHandler) registerPlugin(w http.ResponseWriter, r *http.Request) {
	var req registerPluginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, err)
		return
	}
	if req.Name == "" || req.BaseURL == "" {
		writeAPIError(w, r, http.StatusBadRequest, errPluginFieldsRequired)
		return
	}
	rec, err := h.registry.Register(r.Context(), req.Name, req.BaseURL)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, plugins.ErrNoToken), errors.Is(err, plugins.ErrNameMismatch), errors.Is(err, pluginapi.ErrInvalidManifest):
			status = http.StatusUnprocessableEntity
		case errors.Is(err, plugins.ErrUnreachable):
			status = http.StatusBadGateway
		}
		writeAPIError(w, r, status, err)
		return
	}
	slog.InfoContext(r.Context(), "plugin registered", slog.String("plugin", rec.Name))
	writeJSON(w, http.StatusCreated, rec)
}

func (h *pluginHandler) refreshPlugin(w http.ResponseWriter, r *http.Request) {
	rec, err := h.registry.Refresh(r.Context(), r.PathValue("name"))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, plugins.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeAPIError(w, r, status, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (h *pluginHandler) removePlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.registry.Remove(r.Context(), name); err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, plugins.ErrNotFound):
			status = http.StatusNotFound
		case errors.Is(err, plugins.ErrInUse):
			status = http.StatusConflict
		}
		writeAPIError(w, r, status, err)
		return
	}
	slog.InfoContext(r.Context(), "plugin removed", slog.String("plugin", name))
	w.WriteHeader(http.StatusNoContent)
}

func (h *pluginHandler) saveWorkflow(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, err)
		return
	}
	def, err := h.codec.Parse(raw)
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest, err)
		return
	}
	if err := h.codec.Validate(def); err != nil {
		problems := []string{err.Error()}
		if joined, ok := err.(interface{ Unwrap() []error }); ok {
			problems = problems[:0]
			for _, e := range joined.Unwrap() {
				problems = append(problems, e.Error())
			}
		}
		slog.WarnContext(r.Context(), "workflow rejected",
			slog.String("workflow", def.Name),
			slog.Int("problems", len(problems)),
		)
		writeJSON(w, http.StatusUnprocessableEntity, workflowProblems{Error: err.Error(), Problems: problems})
		return
	}
	yaml, err := h.codec.YAML(def)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, err)
		return
	}
	defJSON, err := json.Marshal(def)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, err)
		return
	}
	var saved process.WorkflowVersion
	err = h.store.InTx(r.Context(), func(ctx context.Context, tx process.Tx) error {
		v, err := tx.UpsertWorkflowVersion(ctx, process.WorkflowVersion{
			Name:       def.Name,
			Hash:       workflow.Hash(yaml),
			Definition: defJSON,
			SourceYAML: string(yaml),
		})
		if err != nil {
			return err
		}
		saved = v
		return tx.SetCurrentWorkflow(ctx, def.Name, v.ID)
	})
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, err)
		return
	}
	slog.InfoContext(r.Context(), "workflow saved",
		slog.String("workflow", def.Name),
		slog.Int64("version_id", saved.ID),
	)
	writeJSON(w, http.StatusOK, saved)
}

func (h *pluginHandler) deleteWorkflow(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	deleted, err := h.store.DeleteWorkflow(r.Context(), name)
	if err != nil {
		writeAPIError(w, r, http.StatusInternalServerError, err)
		return
	}
	if !deleted {
		writeAPIError(w, r, http.StatusNotFound, errWorkflowNotFound)
		return
	}
	slog.InfoContext(r.Context(), "workflow deleted", slog.String("workflow", name))
	w.WriteHeader(http.StatusNoContent)
}

func (h *pluginHandler) jobResult(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("job_id")
	if err := h.verifier.Verify(plugins.CallbackResult, id, r.URL.Query(), time.Now()); err != nil {
		writeProtocolError(w, r, http.StatusUnauthorized, pluginapi.CodeUnauthorized, err)
		return
	}
	body, status, err := readPluginBody(w, r)
	if err != nil {
		writeProtocolError(w, r, status, protocolCode(status), err)
		return
	}
	var res pluginapi.Result
	if err := json.Unmarshal(body, &res); err != nil {
		writeProtocolError(w, r, http.StatusUnprocessableEntity, pluginapi.CodeInvalidInput, err)
		return
	}
	if err := h.callbacks.Result(r.Context(), id, res); err != nil {
		writeCallbackError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *pluginHandler) jobProgress(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("job_id")
	if err := h.verifier.Verify(plugins.CallbackProgress, id, r.URL.Query(), time.Now()); err != nil {
		writeProtocolError(w, r, http.StatusUnauthorized, pluginapi.CodeUnauthorized, err)
		return
	}
	body, status, err := readPluginBody(w, r)
	if err != nil {
		writeProtocolError(w, r, status, protocolCode(status), err)
		return
	}
	var p pluginapi.Progress
	if err := json.Unmarshal(body, &p); err != nil {
		writeProtocolError(w, r, http.StatusUnprocessableEntity, pluginapi.CodeInvalidInput, err)
		return
	}
	if err := h.callbacks.Progress(r.Context(), id, p); err != nil {
		writeCallbackError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *pluginHandler) postTrigger(w http.ResponseWriter, r *http.Request) {
	name := r.Header.Get(pluginapi.HeaderPluginName)
	token, hasBearer := strings.CutPrefix(r.Header.Get(pluginapi.HeaderAuthorization), pluginapi.BearerPrefix)
	if name == "" || !hasBearer || token == "" {
		writeProtocolError(w, r, http.StatusUnauthorized, pluginapi.CodeUnauthorized, errPluginHeaderMissing)
		return
	}
	if err := h.auth.Authenticate(name, token); err != nil {
		writeProtocolError(w, r, http.StatusUnauthorized, pluginapi.CodeUnauthorized, err)
		return
	}
	body, status, err := readPluginBody(w, r)
	if err != nil {
		writeProtocolError(w, r, status, protocolCode(status), err)
		return
	}
	var ev pluginapi.TriggerEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		writeProtocolError(w, r, http.StatusUnprocessableEntity, pluginapi.CodeInvalidInput, err)
		return
	}
	if ev.Plugin != name {
		writeProtocolError(w, r, http.StatusUnprocessableEntity, pluginapi.CodeInvalidInput, errPluginMismatch)
		return
	}
	if err := h.catalog.ValidateTrigger(ev); err != nil {
		if errors.Is(err, plugins.ErrNotFound) || errors.Is(err, plugins.ErrUnknownTrigger) || errors.Is(err, plugins.ErrInvalidPayload) {
			writeProtocolError(w, r, http.StatusUnprocessableEntity, pluginapi.CodeInvalidInput, err)
			return
		}
		writeProtocolError(w, r, http.StatusInternalServerError, pluginapi.CodeInternal, err)
		return
	}
	if ev.Subject.Key == "" {
		writeProtocolError(w, r, http.StatusUnprocessableEntity, pluginapi.CodeInvalidInput, errSubjectKeyRequired)
		return
	}
	ids, err := h.starter.StartFromEvent(r.Context(), ev)
	if err != nil {
		writeProtocolError(w, r, http.StatusInternalServerError, pluginapi.CodeInternal, err)
		return
	}
	if ids == nil {
		ids = []string{}
	}
	writeJSON(w, http.StatusAccepted, pluginapi.TriggerAccepted{ProcessIDs: ids})
}

func readPluginBody(w http.ResponseWriter, r *http.Request) ([]byte, int, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, pluginapi.DefaultMaxBodyBytes))
	if err == nil {
		return body, http.StatusOK, nil
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return nil, http.StatusRequestEntityTooLarge, errBodyTooLarge
	}
	return nil, http.StatusUnprocessableEntity, err
}

func protocolCode(status int) pluginapi.ErrorCode {
	if status == http.StatusRequestEntityTooLarge {
		return pluginapi.CodePayloadTooLarge
	}
	return pluginapi.CodeInvalidInput
}

func writeCallbackError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, process.ErrNotActive), errors.Is(err, process.ErrNotFound), errors.Is(err, plugins.ErrNotPluginStep):
		writeProtocolError(w, r, http.StatusGone, pluginapi.CodeJobNotActive, err)
	case errors.Is(err, pluginapi.ErrInvalidInput):
		writeProtocolError(w, r, http.StatusUnprocessableEntity, pluginapi.CodeInvalidInput, err)
	case errors.Is(err, engine.ErrJobPending):
		writeProtocolError(w, r, http.StatusServiceUnavailable, pluginapi.CodeUpstreamFailure, err)
	default:
		writeProtocolError(w, r, http.StatusInternalServerError, pluginapi.CodeInternal, err)
	}
}

func writeProtocolError(w http.ResponseWriter, r *http.Request, status int, code pluginapi.ErrorCode, err error) {
	logAPIError(r, status, err)
	writeJSON(w, status, pluginapi.APIError{Code: code, Message: err.Error()})
}

func writeAPIError(w http.ResponseWriter, r *http.Request, status int, err error) {
	logAPIError(r, status, err)
	writeError(w, status, err)
}

func logAPIError(r *http.Request, status int, err error) {
	if status >= http.StatusInternalServerError && status != http.StatusServiceUnavailable {
		slog.ErrorContext(r.Context(), "plugin api failed",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Any("error", err),
		)
		return
	}
	slog.WarnContext(r.Context(), "plugin api rejected",
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Int("status", status),
		slog.Any("error", err),
	)
}
