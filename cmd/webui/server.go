package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

type server struct {
	addr string
	srv  *http.Server
}

func newServer(addr string, proc *processHandler) (*server, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	registerProcessRoutes(mux, proc)

	static, err := staticHandler()
	if err != nil {
		return nil, fmt.Errorf("static handler: %w", err)
	}
	mux.Handle("/", static)

	srv := &http.Server{
		Addr:              addr,
		Handler:           logRequest(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return &server{addr: addr, srv: srv}, nil
}

func (s *server) run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		slog.Info("webui listening",
			"addr", s.addr,
		)
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	case err := <-errCh:
		return err
	}
}

func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.DebugContext(r.Context(), "http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := s.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("upstream ResponseWriter does not implement Hijacker")
	}
	return hj.Hijack()
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func registerProcessRoutes(mux *http.ServeMux, h *processHandler) {
	mux.HandleFunc("GET /api/processes", h.listProcesses)
	mux.HandleFunc("GET /api/processes/{id}", h.getProcess)
	mux.HandleFunc("POST /api/processes", h.startProcess)
	mux.HandleFunc("POST /api/processes/{id}/cancel", h.cancelProcess)
	mux.HandleFunc("POST /api/processes/{id}/retry", h.retryProcess)
	mux.HandleFunc("POST /api/processes/{id}/signal", h.signalProcess)
	mux.HandleFunc("GET /api/step-runs/{id}", h.getStepRun)
	mux.HandleFunc("GET /api/step-runs/{id}/events", h.getStepRunEvents)
	mux.HandleFunc("POST /api/step-runs/{id}/cancel", h.cancelStepRun)
	mux.HandleFunc("POST /api/step-runs/{id}/inject", h.injectStepRun)
	mux.HandleFunc("GET /api/step-runs/{id}/diff", h.getStepRunDiff)
	mux.HandleFunc("GET /ws/step-runs/{id}", h.streamStepRun)
	mux.HandleFunc("GET /api/workflows", h.listWorkflows)
	mux.HandleFunc("GET /api/workflows/{name}", h.getWorkflow)
	mux.HandleFunc("GET /api/analytics/workflows", h.analyticsWorkflows)
	mux.HandleFunc("GET /api/analytics/steps", h.analyticsSteps)
	mux.HandleFunc("GET /api/analytics/tokens", h.analyticsTokens)
	mux.HandleFunc("GET /api/analytics/failures", h.analyticsFailures)
}
