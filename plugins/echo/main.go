package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Orfeo42/ai-orchestration/pluginapi"
	"github.com/Orfeo42/ai-orchestration/pluginapi/server"
)

type echoError string

func (e echoError) Error() string { return string(e) }

const (
	errMissingToken echoError = "ECHO_TOKEN is required"
	errRequested    echoError = "failure requested by input"
)

const OutcomeOther pluginapi.Outcome = "other"

type echoInput struct {
	Message string `json:"message"`
	Outcome string `json:"outcome,omitempty" jsonschema:"ok or other"`
}

type echoOutput struct {
	Message string `json:"message"`
}

type waitInput struct {
	Message string `json:"message"`
	Seconds int    `json:"seconds"`
	Fail    bool   `json:"fail,omitempty"`
}

type pingPayload struct {
	Label string `json:"label"`
}

type echoAction struct {
	spec pluginapi.ActionSpec
}

func (a echoAction) Spec() pluginapi.ActionSpec { return a.spec }

func (a echoAction) Run(ctx context.Context, job pluginapi.Job, _ pluginapi.Reporter) (pluginapi.Output, error) {
	var in echoInput
	if err := json.Unmarshal(job.Input, &in); err != nil {
		return pluginapi.Output{}, fmt.Errorf("decode input: %w", err)
	}
	outcome := pluginapi.OutcomeOK
	if in.Outcome == string(OutcomeOther) {
		outcome = OutcomeOther
	}
	data, err := json.Marshal(echoOutput{Message: in.Message})
	if err != nil {
		return pluginapi.Output{}, fmt.Errorf("encode output: %w", err)
	}
	slog.InfoContext(ctx, "echo", slog.String("job_id", job.ID), slog.String("outcome", string(outcome)))
	return pluginapi.Output{Outcome: outcome, Data: data}, nil
}

type waitAction struct {
	spec pluginapi.ActionSpec
}

func (a waitAction) Spec() pluginapi.ActionSpec { return a.spec }

func (a waitAction) Run(ctx context.Context, job pluginapi.Job, rep pluginapi.Reporter) (pluginapi.Output, error) {
	var in waitInput
	if err := json.Unmarshal(job.Input, &in); err != nil {
		return pluginapi.Output{}, fmt.Errorf("decode input: %w", err)
	}
	for i := range in.Seconds {
		if err := rep.Progress(ctx, fmt.Sprintf("waiting %d/%d", i+1, in.Seconds)); err != nil {
			slog.WarnContext(ctx, "progress failed", slog.String("job_id", job.ID), slog.Any("error", err))
		}
		select {
		case <-ctx.Done():
			return pluginapi.Output{}, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	if in.Fail {
		return pluginapi.Output{}, fmt.Errorf("%w: %w", pluginapi.ErrUpstream, errRequested)
	}
	data, err := json.Marshal(echoOutput{Message: in.Message})
	if err != nil {
		return pluginapi.Output{}, fmt.Errorf("encode output: %w", err)
	}
	return pluginapi.Output{Outcome: pluginapi.OutcomeOK, Data: data}, nil
}

func schemas[In, Out any]() (json.RawMessage, json.RawMessage, error) {
	in, err := pluginapi.SchemaFor[In]()
	if err != nil {
		return nil, nil, fmt.Errorf("input schema: %w", err)
	}
	out, err := pluginapi.SchemaFor[Out]()
	if err != nil {
		return nil, nil, fmt.Errorf("output schema: %w", err)
	}
	return in, out, nil
}

func actions() ([]pluginapi.Action, error) {
	echoIn, echoOut, err := schemas[echoInput, echoOutput]()
	if err != nil {
		return nil, err
	}
	waitIn, waitOut, err := schemas[waitInput, echoOutput]()
	if err != nil {
		return nil, err
	}
	return []pluginapi.Action{
		echoAction{spec: pluginapi.ActionSpec{
			Name:                  "echo",
			Description:           "Returns the message synchronously; outcome other when requested",
			Mode:                  pluginapi.ModeSync,
			InputSchema:           echoIn,
			OutputSchema:          echoOut,
			Outcomes:              []pluginapi.Outcome{pluginapi.OutcomeOK, OutcomeOther},
			DefaultTimeoutSeconds: 30,
		}},
		waitAction{spec: pluginapi.ActionSpec{
			Name:                  "wait",
			Description:           "Waits the given seconds asynchronously reporting progress, then returns the message or fails upstream",
			Mode:                  pluginapi.ModeAsync,
			InputSchema:           waitIn,
			OutputSchema:          waitOut,
			Outcomes:              []pluginapi.Outcome{pluginapi.OutcomeOK},
			DefaultTimeoutSeconds: 600,
		}},
	}, nil
}

func info() (pluginapi.Info, error) {
	payload, err := pluginapi.SchemaFor[pingPayload]()
	if err != nil {
		return pluginapi.Info{}, fmt.Errorf("ping schema: %w", err)
	}
	return pluginapi.Info{
		Name:        "echo",
		Version:     "1.0.0",
		Description: "Reference plugin: sync echo, async wait, ping trigger",
		Triggers: []pluginapi.TriggerSpec{{
			Name:          "ping",
			Description:   "Manual test trigger",
			PayloadSchema: payload,
		}},
	}, nil
}

func run(ctx context.Context) error {
	token := os.Getenv("ECHO_TOKEN")
	if token == "" {
		return errMissingToken
	}
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8090"
	}
	acts, err := actions()
	if err != nil {
		return err
	}
	inf, err := info()
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "echo plugin listening", slog.String("addr", addr))
	return server.Serve(ctx, server.Config{Addr: addr, Token: token}, inf, acts...)
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("echo plugin failed", slog.Any("error", err))
		os.Exit(1)
	}
}
