package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Orfeo42/ai-orchestration/pluginapi"
)

const (
	maxDeliveryBackoff = time.Minute
	deliveryGrace      = 10 * time.Minute
)

func (s *Server) postProgress(ctx context.Context, j *job, phase string) error {
	err := s.core.Progress(ctx, j.req.Callback, pluginapi.Progress{JobID: j.req.JobID, Phase: phase, At: time.Now()})
	if errors.Is(err, pluginapi.ErrJobNotActive) {
		j.cancel()
		return pluginapi.ErrJobNotActive
	}
	return err
}

func (s *Server) execute(j *job) {
	out, err := s.safeRun(j)
	status, result := s.outcome(j, out, err)
	j.finish(status, result)
	j.cancel()
}

func (s *Server) safeRun(j *job) (out pluginapi.Output, err error) {
	defer func() {
		if p := recover(); p != nil {
			out = pluginapi.Output{}
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	return j.action.impl.Run(j.ctx, pluginapi.Job{
		ID:        j.req.JobID,
		ProcessID: j.req.ProcessID,
		Input:     j.req.Input,
		Deadline:  j.req.Deadline,
	}, reporter{s: s, j: j})
}

func (s *Server) outcome(j *job, out pluginapi.Output, runErr error) (pluginapi.JobStatus, pluginapi.Result) {
	result := pluginapi.Result{JobID: j.req.JobID}
	ctxErr := j.ctx.Err()
	if errors.Is(ctxErr, context.Canceled) {
		s.cfg.Logger.InfoContext(j.ctx, "job cancelled",
			slog.String("job_id", j.req.JobID),
			slog.String("action", j.action.spec.Name))
		result.Error = &pluginapi.JobError{Kind: pluginapi.ErrorInternal, Message: "job cancelled"}
		return pluginapi.JobCancelled, result
	}

	failure := &pluginapi.JobError{Kind: pluginapi.ErrorInternal}
	switch {
	case errors.Is(ctxErr, context.DeadlineExceeded):
		failure.Message = "deadline exceeded"
	case runErr != nil && errors.Is(runErr, pluginapi.ErrUpstream):
		failure.Kind = pluginapi.ErrorUpstream
		failure.Message = runErr.Error()
	case runErr != nil:
		failure.Message = runErr.Error()
	case !j.action.spec.Declares(out.Outcome):
		failure.Message = fmt.Sprintf("%v: %q", pluginapi.ErrUndeclaredOutcome, out.Outcome)
	default:
		if err := j.action.output.Validate(out.Data); err != nil {
			failure.Message = err.Error()
			break
		}
		result.Outcome = out.Outcome
		result.Output = out.Data
		result.Usage = out.Usage
		return pluginapi.JobSucceeded, result
	}

	attrs := []any{
		slog.String("job_id", j.req.JobID),
		slog.String("action", j.action.spec.Name),
		slog.String("error", failure.Message),
	}
	if failure.Kind == pluginapi.ErrorUpstream {
		s.cfg.Logger.WarnContext(j.ctx, "job failed", attrs...)
	} else {
		s.cfg.Logger.ErrorContext(j.ctx, "job failed", attrs...)
	}
	result.Error = failure
	return pluginapi.JobFailed, result
}

func (s *Server) heartbeat(j *job) {
	defer s.wg.Done()
	ticker := time.NewTicker(time.Duration(j.req.HeartbeatSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-j.done:
			return
		case <-s.base.Done():
			return
		case <-ticker.C:
		}
		err := s.postProgress(s.base, j, j.state().Phase)
		if err == nil || errors.Is(err, pluginapi.ErrJobNotActive) {
			continue
		}
		s.cfg.Logger.WarnContext(s.base, "heartbeat failed",
			slog.String("job_id", j.req.JobID),
			slog.String("action", j.action.spec.Name),
			slog.String("error", err.Error()))
	}
}

func (s *Server) deliver(j *job) {
	ctx, cancel := context.WithDeadline(s.base, j.req.Deadline.Add(deliveryGrace))
	defer cancel()
	result := *j.state().Result
	backoff := s.cfg.RetryBackoff
	for {
		err := s.core.Complete(ctx, j.req.Callback, result)
		if err == nil {
			return
		}
		attrs := []any{
			slog.String("job_id", j.req.JobID),
			slog.String("action", j.action.spec.Name),
			slog.String("error", err.Error()),
		}
		if errors.Is(err, pluginapi.ErrJobNotActive) {
			s.cfg.Logger.InfoContext(ctx, "result dropped, job no longer active", attrs...)
			return
		}
		if !errors.Is(err, pluginapi.ErrUpstream) {
			s.cfg.Logger.WarnContext(ctx, "result delivery rejected", attrs...)
			return
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			s.cfg.Logger.WarnContext(s.base, "result delivery abandoned", attrs...)
			return
		case <-timer.C:
		}
		backoff = min(backoff*2, maxDeliveryBackoff)
	}
}
