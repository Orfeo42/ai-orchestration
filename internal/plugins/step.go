package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/internal/step"
	"github.com/Orfeo42/ai-orchestration/internal/template"
	"github.com/Orfeo42/ai-orchestration/pluginapi"
)

type pluginStep struct {
	catalog *Catalog
	plugin  string
	spec    pluginapi.ActionSpec
	with    json.RawMessage
}

type scopeSubject struct {
	Kind  string `json:"kind"`
	Key   string `json:"key"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

type scopeProcess struct {
	ID       string `json:"id"`
	Workflow string `json:"workflow"`
	Project  string `json:"project"`
}

type scope struct {
	Vars    process.Vars `json:"vars"`
	Subject scopeSubject `json:"subject"`
	Process scopeProcess `json:"process"`
}

func newStep(c *Catalog, rec Record, spec pluginapi.ActionSpec, with json.RawMessage) (step.Step, error) {
	if len(with) == 0 {
		with = json.RawMessage(`{}`)
	}
	if template.HasExpressions(with) {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(with, &object); err != nil {
			return nil, fmt.Errorf("%w: %s/%s: with must be an object: %w", step.ErrInvalidParams, rec.Name, spec.Name, err)
		}
		return &pluginStep{catalog: c, plugin: rec.Name, spec: spec, with: with}, nil
	}
	schema, err := pluginapi.CompileSchema(spec.InputSchema)
	if err != nil {
		return nil, fmt.Errorf("%w: %s/%s: %w", step.ErrInvalidParams, rec.Name, spec.Name, err)
	}
	if err := schema.Validate(with); err != nil {
		return nil, fmt.Errorf("%w: %s/%s: %w", step.ErrInvalidParams, rec.Name, spec.Name, err)
	}
	return &pluginStep{catalog: c, plugin: rec.Name, spec: spec, with: with}, nil
}

func (s *pluginStep) Run(ctx context.Context, sc step.Context) (step.Result, error) {
	rec, ok := s.catalog.Plugin(s.plugin)
	if !ok {
		return step.Result{}, fmt.Errorf("%w: plugin %s is not registered", step.ErrUpstream, s.plugin)
	}
	if rec.Status == StatusDown || rec.Status == StatusUnreachable {
		return step.Result{}, fmt.Errorf("%w: plugin %s is %s", step.ErrUpstream, s.plugin, rec.Status)
	}

	scopeJSON, err := json.Marshal(scope{
		Vars:    sc.Process.Vars,
		Subject: scopeSubject{Kind: string(sc.Process.Subject.Kind), Key: sc.Process.Subject.Key, URL: sc.Process.Subject.URL, Title: sc.Process.Title},
		Process: scopeProcess{ID: sc.Process.ID, Workflow: sc.Process.WorkflowName, Project: sc.Process.Project},
	})
	if err != nil {
		return step.Result{}, fmt.Errorf("%w: marshal scope: %w", step.ErrPermanent, err)
	}
	input, err := template.Resolve(s.with, scopeJSON)
	if err != nil {
		return step.Result{}, fmt.Errorf("%w: resolve input of %s/%s: %w", step.ErrPermanent, s.plugin, s.spec.Name, err)
	}
	schema, err := pluginapi.CompileSchema(s.spec.InputSchema)
	if err != nil {
		return step.Result{}, fmt.Errorf("%w: %s/%s: %w", step.ErrPermanent, s.plugin, s.spec.Name, err)
	}
	if err := schema.Validate(input); err != nil {
		return step.Result{}, fmt.Errorf("%w: input of %s/%s: %w", step.ErrPermanent, s.plugin, s.spec.Name, err)
	}

	timeout := DefaultJobTimeout
	if s.spec.DefaultTimeoutSeconds > 0 {
		timeout = time.Duration(s.spec.DefaultTimeoutSeconds) * time.Second
	}
	if sc.Timeout > 0 {
		timeout = sc.Timeout
	}
	deadline := time.Now().UTC().Add(timeout)

	pc, err := s.catalog.clients.For(rec.Name, rec.BaseURL)
	if err != nil {
		return step.Result{}, fmt.Errorf("%w: %w", step.ErrPermanent, err)
	}
	state, err := pc.StartJob(ctx, s.spec.Name, pluginapi.JobRequest{
		JobID:            sc.StepRunID,
		ProcessID:        sc.Process.ID,
		Input:            input,
		Callback:         s.catalog.signer.Callback(sc.StepRunID, deadline),
		Deadline:         deadline,
		HeartbeatSeconds: DefaultHeartbeatSeconds,
	})
	if err != nil {
		if errors.Is(err, pluginapi.ErrInvalidInput) || errors.Is(err, pluginapi.ErrUnknownAction) ||
			errors.Is(err, pluginapi.ErrUnauthorized) || errors.Is(err, pluginapi.ErrJobConflict) {
			return step.Result{}, fmt.Errorf("%w: start %s/%s: %w", step.ErrPermanent, s.plugin, s.spec.Name, err)
		}
		return step.Result{}, fmt.Errorf("%w: start %s/%s: %w", step.ErrUpstream, s.plugin, s.spec.Name, err)
	}

	if state.Status == pluginapi.JobRunning {
		slog.InfoContext(ctx, "plugin job started",
			slog.String("process_id", sc.Process.ID),
			slog.String("step", sc.StepID),
			slog.String("plugin", s.plugin),
			slog.String("action", s.spec.Name),
			slog.String("job_id", sc.StepRunID),
		)
		return step.Result{Wait: &step.WaitRequest{
			Kind: process.WaitJob,
			Job:  &step.JobWait{Plugin: rec.Name, HeartbeatSeconds: DefaultHeartbeatSeconds, Deadline: deadline},
		}}, nil
	}
	if state.Result == nil {
		return step.Result{}, fmt.Errorf("%w: job %s ended %s without a result", step.ErrUpstream, sc.StepRunID, state.Status)
	}
	res := *state.Result
	if res.Error != nil {
		if res.Error.Kind == pluginapi.ErrorUpstream {
			return step.Result{}, fmt.Errorf("%w: %s", step.ErrUpstream, res.Error.Message)
		}
		return step.Result{}, fmt.Errorf("%w: %s", ErrJobFailed, res.Error.Message)
	}
	if err := checkResult(s.spec, res); err != nil {
		return step.Result{}, fmt.Errorf("%w: %w", step.ErrPermanent, err)
	}
	if sc.Handle != nil {
		sc.Handle.AddUsage(ctx, toUsage(res.Usage))
	}
	out := step.Result{Outcome: step.Outcome(res.Outcome)}
	if len(res.Output) > 0 {
		out.Vars = process.Vars{sc.StepID: res.Output}
	}
	return out, nil
}
