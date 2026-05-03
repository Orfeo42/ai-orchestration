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

	"ai-orchestration/internal/control"
	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/queue"
	"ai-orchestration/internal/runstore"
)

type server struct {
	addr  string
	store runstore.Store
	ctrl  control.Channel
	rq    queue.Queue
	fj    forgejo.Forgejo
	srv   *http.Server
}

func newServer(addr string, store runstore.Store, ctrl control.Channel, rq queue.Queue, fj forgejo.Forgejo) (*server, error) {
	mux := http.NewServeMux()
	api := &apiHandler{store: store, ctrl: ctrl, rq: rq, fj: fj}

	mux.HandleFunc("GET /api/runs", api.listRuns)
	mux.HandleFunc("GET /api/runs/active", api.listActive)
	mux.HandleFunc("GET /api/runs/{id}", api.getRun)
	mux.HandleFunc("GET /api/runs/{id}/events", api.getRunEvents)
	mux.HandleFunc("POST /api/runs/{id}/cancel", api.cancelRun)
	mux.HandleFunc("POST /api/runs/{id}/inject", api.injectRun)
	mux.HandleFunc("POST /api/runs/{id}/retry", api.retryRun)
	mux.HandleFunc("GET /api/runs/{id}/diff", api.getRunDiff)
	mux.HandleFunc("GET /api/stats/tokens", api.getDailyTokens)
	mux.HandleFunc("GET /healthz", api.healthz)
	mux.HandleFunc("GET /ws/runs/{id}", api.streamRun)

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
	return &server{addr: addr, store: store, ctrl: ctrl, rq: rq, fj: fj, srv: srv}, nil
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
