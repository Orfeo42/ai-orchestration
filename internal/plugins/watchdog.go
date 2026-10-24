package plugins

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/pluginapi"
)

type StaleLister interface {
	ListStaleJobs(ctx context.Context, now time.Time) ([]process.StaleJob, error)
	TouchStepHeartbeat(ctx context.Context, stepRunID string) (bool, error)
}

type Watchdog struct {
	stale     StaleLister
	catalog   *Catalog
	clients   *Clients
	callbacks *Callbacks
}

func NewWatchdog(stale StaleLister, catalog *Catalog, clients *Clients, callbacks *Callbacks) *Watchdog {
	return &Watchdog{stale: stale, catalog: catalog, clients: clients, callbacks: callbacks}
}

func (w *Watchdog) Check(ctx context.Context, now time.Time) error {
	jobs, err := w.stale.ListStaleJobs(ctx, now)
	if err != nil {
		return fmt.Errorf("list stale jobs: %w", err)
	}
	for _, job := range jobs {
		w.handle(ctx, job)
	}
	return nil
}

func (w *Watchdog) handle(ctx context.Context, job process.StaleJob) {
	rec, ok := w.catalog.Plugin(job.Plugin)
	if !ok {
		w.fail(ctx, job, pluginapi.ErrorUpstream, "plugin not registered")
		return
	}
	if job.Expired {
		if err := w.clients.CancelJob(ctx, job.Plugin, job.StepRunID); err != nil {
			slog.WarnContext(ctx, "watchdog could not cancel expired plugin job",
				slog.String("step_run_id", job.StepRunID), slog.String("plugin", job.Plugin), slog.Any("error", err))
		}
		w.fail(ctx, job, pluginapi.ErrorInternal, "deadline exceeded")
		return
	}
	pc, err := w.clients.For(rec.Name, rec.BaseURL)
	if err != nil {
		w.fail(ctx, job, pluginapi.ErrorUpstream, fmt.Sprintf("plugin client unavailable: %v", err))
		return
	}
	state, err := pc.GetJob(ctx, job.StepRunID)
	if err != nil {
		w.fail(ctx, job, pluginapi.ErrorUpstream, fmt.Sprintf("job lost: %v", err))
		return
	}
	switch {
	case state.Status == pluginapi.JobRunning:
		if _, err := w.stale.TouchStepHeartbeat(ctx, job.StepRunID); err != nil {
			slog.WarnContext(ctx, "watchdog could not touch heartbeat",
				slog.String("step_run_id", job.StepRunID), slog.String("plugin", job.Plugin), slog.Any("error", err))
			return
		}
		slog.WarnContext(ctx, "plugin job alive but heartbeats missed",
			slog.String("step_run_id", job.StepRunID), slog.String("plugin", job.Plugin))
	case state.Result != nil:
		w.deliver(ctx, job, *state.Result, "plugin result fetched")
	default:
		w.fail(ctx, job, pluginapi.ErrorUpstream, "missing result")
	}
}

func (w *Watchdog) fail(ctx context.Context, job process.StaleJob, kind pluginapi.ErrorKind, message string) {
	w.deliver(ctx, job, pluginapi.Result{Error: &pluginapi.JobError{Kind: kind, Message: message}}, message)
}

func (w *Watchdog) deliver(ctx context.Context, job process.StaleJob, res pluginapi.Result, reason string) {
	res.JobID = job.StepRunID
	err := w.callbacks.Result(ctx, job.StepRunID, res)
	if errors.Is(err, process.ErrNotActive) {
		return
	}
	if err != nil {
		slog.WarnContext(ctx, "watchdog could not deliver plugin result",
			slog.String("step_run_id", job.StepRunID), slog.String("plugin", job.Plugin), slog.Any("error", err))
		return
	}
	slog.WarnContext(ctx, "plugin job recovered by watchdog",
		slog.String("step_run_id", job.StepRunID), slog.String("plugin", job.Plugin), slog.String("reason", reason))
}
