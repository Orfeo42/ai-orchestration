package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/Orfeo42/ai-orchestration/pluginapi"
)

const mimeJSON = "application/json"

func (s *Server) authenticate(next http.Handler) http.Handler {
	want := []byte(pluginapi.BearerPrefix + s.cfg.Token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get(pluginapi.HeaderAuthorization))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			s.writeError(w, r, http.StatusUnauthorized, pluginapi.CodeUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) writeBody(w http.ResponseWriter, r *http.Request, status int, data []byte) {
	w.Header().Set("Content-Type", mimeJSON)
	w.WriteHeader(status)
	if _, err := w.Write(data); err != nil {
		s.cfg.Logger.WarnContext(r.Context(), "write response", slog.String("error", err.Error()))
	}
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, status int, code pluginapi.ErrorCode, message string) {
	data, err := json.Marshal(pluginapi.APIError{Code: code, Message: message})
	if err != nil {
		s.cfg.Logger.ErrorContext(r.Context(), "encode error response", slog.String("error", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.writeBody(w, r, status, data)
}

func (s *Server) writeState(w http.ResponseWriter, r *http.Request, status int, state pluginapi.JobState) {
	data, err := json.Marshal(state)
	if err != nil {
		s.cfg.Logger.ErrorContext(r.Context(), "encode job state", slog.String("error", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.writeBody(w, r, status, data)
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	data, err := json.Marshal(s.manifest)
	if err != nil {
		s.cfg.Logger.ErrorContext(r.Context(), "encode manifest", slog.String("error", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.writeBody(w, r, http.StatusOK, data)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	data, err := json.Marshal(s.cfg.HealthCheck(r.Context()))
	if err != nil {
		s.cfg.Logger.ErrorContext(r.Context(), "encode health", slog.String("error", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.writeBody(w, r, http.StatusOK, data)
}

func (s *Server) lookup(id string) (*job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	return j, ok
}

func (s *Server) handleStartJob(w http.ResponseWriter, r *http.Request) {
	act, ok := s.actions[r.PathValue("action")]
	if !ok {
		s.writeError(w, r, http.StatusNotFound, pluginapi.CodeUnknownAction, "unknown action")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes)
	var req pluginapi.JobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			s.writeError(w, r, http.StatusRequestEntityTooLarge, pluginapi.CodePayloadTooLarge, err.Error())
			return
		}
		s.writeError(w, r, http.StatusUnprocessableEntity, pluginapi.CodeInvalidInput, err.Error())
		return
	}

	problem := ""
	switch {
	case req.JobID == "":
		problem = "job_id is required"
	case req.Callback.ResultURL == "" || req.Callback.ProgressURL == "":
		problem = "callback.result_url and callback.progress_url are required"
	case req.Deadline.IsZero() || !req.Deadline.After(time.Now()):
		problem = "deadline must be in the future"
	case req.HeartbeatSeconds < 1:
		problem = "heartbeat_seconds must be at least 1"
	}
	if problem == "" {
		if err := act.input.Validate(req.Input); err != nil {
			problem = err.Error()
		}
	}
	if problem != "" {
		s.writeError(w, r, http.StatusUnprocessableEntity, pluginapi.CodeInvalidInput, problem)
		return
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		s.writeError(w, r, http.StatusServiceUnavailable, pluginapi.CodeUpstreamFailure, "server is shutting down")
		return
	}
	if existing, found := s.jobs[req.JobID]; found {
		s.mu.Unlock()
		if !existing.matches(act.spec.Name, req.Input) {
			s.writeError(w, r, http.StatusConflict, pluginapi.CodeJobConflict, pluginapi.ErrJobConflict.Error())
			return
		}
		state := existing.state()
		status := http.StatusOK
		if state.Status == pluginapi.JobRunning {
			status = http.StatusAccepted
		}
		s.writeState(w, r, status, state)
		return
	}
	j := newJob(s.base, act, req)
	s.jobs[req.JobID] = j
	if j.async() {
		s.wg.Add(2)
	} else {
		s.wg.Add(1)
	}
	s.mu.Unlock()

	if j.async() {
		accepted := j.state()
		go func() {
			defer s.wg.Done()
			s.execute(j)
			s.deliver(j)
		}()
		go s.heartbeat(j)
		s.writeState(w, r, http.StatusAccepted, accepted)
		return
	}
	defer s.wg.Done()
	stop := context.AfterFunc(r.Context(), j.cancel)
	defer stop()
	s.execute(j)
	s.writeState(w, r, http.StatusOK, j.state())
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	j, ok := s.lookup(r.PathValue("job_id"))
	if !ok {
		s.writeError(w, r, http.StatusNotFound, pluginapi.CodeJobNotFound, pluginapi.ErrJobNotFound.Error())
		return
	}
	s.writeState(w, r, http.StatusOK, j.state())
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	j, ok := s.lookup(r.PathValue("job_id"))
	if !ok {
		s.writeError(w, r, http.StatusNotFound, pluginapi.CodeJobNotFound, pluginapi.ErrJobNotFound.Error())
		return
	}
	state := j.state()
	if state.Status != pluginapi.JobRunning {
		s.writeState(w, r, http.StatusOK, state)
		return
	}
	j.cancel()
	s.writeState(w, r, http.StatusAccepted, j.state())
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	j, ok := s.lookup(r.PathValue("job_id"))
	if !ok {
		s.writeError(w, r, http.StatusNotFound, pluginapi.CodeJobNotFound, pluginapi.ErrJobNotFound.Error())
		return
	}
	if !j.action.spec.Has(pluginapi.CapabilityEvents) {
		s.writeError(w, r, http.StatusConflict, pluginapi.CodeUnsupported, pluginapi.ErrUnsupported.Error())
		return
	}
	after := int64(0)
	if raw := r.URL.Query().Get("after"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			s.writeError(w, r, http.StatusUnprocessableEntity, pluginapi.CodeInvalidInput, "after must be a non-negative integer")
			return
		}
		after = parsed
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	if err := rc.Flush(); err != nil {
		s.cfg.Logger.WarnContext(r.Context(), "flush event stream", slog.String("error", err.Error()))
		return
	}
	for {
		pending, terminal, wake := j.eventsAfter(after)
		for _, ev := range pending {
			data, err := json.Marshal(ev)
			if err != nil {
				s.cfg.Logger.ErrorContext(r.Context(), "encode event", slog.String("error", err.Error()))
				return
			}
			if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", ev.Seq, data); err != nil {
				s.cfg.Logger.WarnContext(r.Context(), "write event", slog.String("error", err.Error()))
				return
			}
			after = ev.Seq
		}
		if len(pending) > 0 {
			if err := rc.Flush(); err != nil {
				s.cfg.Logger.WarnContext(r.Context(), "flush event stream", slog.String("error", err.Error()))
				return
			}
		}
		if terminal {
			return
		}
		select {
		case <-wake:
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	j, ok := s.lookup(r.PathValue("job_id"))
	if !ok {
		s.writeError(w, r, http.StatusNotFound, pluginapi.CodeJobNotFound, pluginapi.ErrJobNotFound.Error())
		return
	}
	if !j.action.spec.Has(pluginapi.CapabilityInject) {
		s.writeError(w, r, http.StatusConflict, pluginapi.CodeUnsupported, pluginapi.ErrUnsupported.Error())
		return
	}
	if j.state().Status != pluginapi.JobRunning {
		s.writeError(w, r, http.StatusConflict, pluginapi.CodeJobNotActive, pluginapi.ErrJobNotActive.Error())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes)
	var msg pluginapi.Message
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		s.writeError(w, r, http.StatusUnprocessableEntity, pluginapi.CodeInvalidInput, err.Error())
		return
	}
	if msg.Text == "" {
		s.writeError(w, r, http.StatusUnprocessableEntity, pluginapi.CodeInvalidInput, "text is required")
		return
	}
	select {
	case j.messages <- msg:
		w.WriteHeader(http.StatusAccepted)
	case <-j.done:
		s.writeError(w, r, http.StatusConflict, pluginapi.CodeJobNotActive, pluginapi.ErrJobNotActive.Error())
	case <-r.Context().Done():
	}
}
