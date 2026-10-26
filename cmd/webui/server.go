package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/Orfeo42/ai-orchestration/pluginapi"
)

type server struct {
	addr string
	srv  *http.Server
}

func newServer(addr string, proc *processHandler, plug *pluginHandler) (*server, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	registerProcessRoutes(mux, proc)
	registerPluginRoutes(mux, plug)

	static, err := staticHandler()
	if err != nil {
		return nil, fmt.Errorf("static handler: %w", err)
	}
	mux.Handle("/", static)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
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

func registerPluginRoutes(mux *http.ServeMux, h *pluginHandler) {
	mux.HandleFunc("GET /api/catalog", h.getCatalog)
	mux.HandleFunc("GET /api/plugins", h.listPlugins)
	mux.HandleFunc("POST /api/plugins", h.registerPlugin)
	mux.HandleFunc("POST /api/plugins/{name}/refresh", h.refreshPlugin)
	mux.HandleFunc("DELETE /api/plugins/{name}", h.removePlugin)
	mux.HandleFunc("POST /api/workflows", h.saveWorkflow)
	mux.HandleFunc("DELETE /api/workflows/{name}", h.deleteWorkflow)
	mux.HandleFunc("POST /api/v1/jobs/{job_id}/result", h.jobResult)
	mux.HandleFunc("POST /api/v1/jobs/{job_id}/progress", h.jobProgress)
	mux.HandleFunc("POST "+pluginapi.CorePathTrigger, h.postTrigger)
}
