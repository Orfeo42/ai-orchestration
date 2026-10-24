package conformance_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"git.ionstream.ai/ionstream/ai-orchestrator/pluginapi"
	"git.ionstream.ai/ionstream/ai-orchestrator/pluginapi/conformance"
	"git.ionstream.ai/ionstream/ai-orchestrator/pluginapi/server"
)

type sampleInput struct {
	N int `json:"n"`
}

type sampleOutput struct {
	V int `json:"v"`
}

type sampleAction struct {
	spec pluginapi.ActionSpec
	run  func(ctx context.Context, rep pluginapi.Reporter) (pluginapi.Output, error)
}

func (a sampleAction) Spec() pluginapi.ActionSpec { return a.spec }

func (a sampleAction) Run(ctx context.Context, _ pluginapi.Job, rep pluginapi.Reporter) (pluginapi.Output, error) {
	return a.run(ctx, rep)
}

func sampleSpec(t *testing.T, name string, mode pluginapi.Mode, outcomes []pluginapi.Outcome, caps ...pluginapi.Capability) pluginapi.ActionSpec {
	t.Helper()
	input, err := pluginapi.SchemaFor[sampleInput]()
	require.NoError(t, err)
	output, err := pluginapi.SchemaFor[sampleOutput]()
	require.NoError(t, err)
	return pluginapi.ActionSpec{
		Name:                  name,
		Description:           "sample " + name,
		Mode:                  mode,
		InputSchema:           input,
		OutputSchema:          output,
		Outcomes:              outcomes,
		Capabilities:          caps,
		DefaultTimeoutSeconds: 5,
	}
}

func okOutput() pluginapi.Output {
	return pluginapi.Output{Outcome: pluginapi.OutcomeOK, Data: json.RawMessage(`{"v":1}`)}
}

func TestRun(t *testing.T) {
	t.Run("sample plugin passes", func(t *testing.T) {
		outcomes := []pluginapi.Outcome{pluginapi.OutcomeOK, "not_found"}
		syncAction := sampleAction{
			spec: sampleSpec(t, "sample.sync", pluginapi.ModeSync, outcomes),
			run: func(context.Context, pluginapi.Reporter) (pluginapi.Output, error) {
				return pluginapi.Output{Outcome: "not_found", Data: json.RawMessage(`{"v":0}`)}, nil
			},
		}
		streamAction := sampleAction{
			spec: sampleSpec(t, "sample.stream", pluginapi.ModeAsync, outcomes[:1], pluginapi.CapabilityEvents, pluginapi.CapabilityInject),
			run: func(ctx context.Context, rep pluginapi.Reporter) (pluginapi.Output, error) {
				if err := rep.Progress(ctx, "working"); err != nil {
					return pluginapi.Output{}, err
				}
				if err := rep.Event(ctx, "phase", json.RawMessage(`{"n":1}`)); err != nil {
					return pluginapi.Output{}, err
				}
				if err := rep.Event(ctx, "result", json.RawMessage(`{"n":2}`)); err != nil {
					return pluginapi.Output{}, err
				}
				return okOutput(), nil
			},
		}
		failAction := sampleAction{
			spec: sampleSpec(t, "sample.fail", pluginapi.ModeAsync, outcomes[:1]),
			run: func(context.Context, pluginapi.Reporter) (pluginapi.Output, error) {
				return pluginapi.Output{}, fmt.Errorf("dependency down: %w", pluginapi.ErrUpstream)
			},
		}
		srv, err := server.New(
			server.Config{Token: "secret", RetryBackoff: 10 * time.Millisecond},
			pluginapi.Info{Name: "sample", Version: "1.0.0", Description: "sample plugin"},
			syncAction, streamAction, failAction,
		)
		require.NoError(t, err)
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
			defer cancel()
			require.NoError(t, srv.Shutdown(ctx))
		})
		ts := httptest.NewServer(srv)
		t.Cleanup(ts.Close)

		conformance.Run(t, conformance.Target{
			BaseURL: ts.URL,
			Token:   "secret",
			Fixtures: map[string]json.RawMessage{
				"sample.sync":   json.RawMessage(`{"n":1}`),
				"sample.stream": json.RawMessage(`{"n":2}`),
				"sample.fail":   json.RawMessage(`{"n":3}`),
			},
			Timeout: 10 * time.Second,
		})
	})
}
