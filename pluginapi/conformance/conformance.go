package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"git.ionstream.ai/ionstream/ai-orchestrator/pluginapi"
	"git.ionstream.ai/ionstream/ai-orchestrator/pluginapi/client"
)

const (
	defaultTimeout   = 2 * time.Minute
	deadlineSlack    = time.Minute
	heartbeatSeconds = 1
	unknownAction    = "conformance.no_such_action"
)

type Target struct {
	BaseURL            string
	Token              string
	Fixtures           map[string]json.RawMessage
	CallbackListenAddr string
	CallbackBaseURL    string
	HTTPClient         *http.Client
	Timeout            time.Duration
}

type suite struct {
	target   Target
	hc       *http.Client
	plugin   *client.Plugin
	core     *recorder
	timeout  time.Duration
	deadline time.Time
	manifest pluginapi.Manifest
}

func Run(t *testing.T, target Target) {
	t.Helper()
	s := &suite{target: target, hc: target.HTTPClient, timeout: target.Timeout}
	if s.hc == nil {
		s.hc = http.DefaultClient
	}
	if s.timeout == 0 {
		s.timeout = defaultTimeout
	}
	s.deadline = time.Now().Add(s.timeout + deadlineSlack)
	s.plugin = client.NewPlugin(target.BaseURL, target.Token, s.hc)
	s.core = startRecorder(t, target.CallbackListenAddr, target.CallbackBaseURL)

	if !t.Run("manifest", s.checkManifest) {
		t.FailNow()
	}
	t.Run("health", s.checkHealth)
	t.Run("auth", s.checkAuth)
	t.Run("fixtures", s.checkFixtures)
	t.Run("unknown_action", s.checkUnknownAction)
	t.Run("invalid_request", s.checkInvalidRequest)
	t.Run("unknown_job", s.checkUnknownJob)
	for _, spec := range s.fixtured() {
		t.Run("action/"+spec.Name, func(t *testing.T) {
			s.checkAction(t, spec, s.target.Fixtures[spec.Name])
		})
	}
	t.Run("conflict", s.checkConflict)
}

func (s *suite) fixtured() []pluginapi.ActionSpec {
	var specs []pluginapi.ActionSpec
	for _, spec := range s.manifest.Actions {
		if _, ok := s.target.Fixtures[spec.Name]; ok {
			specs = append(specs, spec)
		}
	}
	return specs
}

func (s *suite) request(jobID string, input json.RawMessage) pluginapi.JobRequest {
	return pluginapi.JobRequest{
		JobID:            jobID,
		ProcessID:        "conformance",
		Input:            input,
		Callback:         s.core.callback(jobID),
		Deadline:         s.deadline,
		HeartbeatSeconds: heartbeatSeconds,
	}
}

func (s *suite) checkManifest(t *testing.T) {
	m, err := s.plugin.Manifest(t.Context())
	if err != nil {
		t.Fatalf("fetch manifest: %v", err)
	}
	if m.Protocol != pluginapi.ProtocolVersion {
		t.Errorf("protocol = %q, want %q", m.Protocol, pluginapi.ProtocolVersion)
	}
	if err := m.Validate(); err != nil {
		t.Errorf("manifest invalid: %v", err)
	}
	s.manifest = m
}

func (s *suite) checkHealth(t *testing.T) {
	h, err := s.plugin.Health(t.Context())
	if err != nil {
		t.Fatalf("fetch health: %v", err)
	}
	switch h.Status {
	case pluginapi.HealthOK, pluginapi.HealthDegraded, pluginapi.HealthDown:
	default:
		t.Errorf("health status = %q, want ok, degraded or down", h.Status)
	}
}

func (s *suite) checkAuth(t *testing.T) {
	t.Run("missing token", func(t *testing.T) {
		s.expectUnauthorized(t, "")
	})
	t.Run("wrong token", func(t *testing.T) {
		s.expectUnauthorized(t, s.target.Token+"-wrong")
	})
}

func (s *suite) expectUnauthorized(t *testing.T, token string) {
	t.Helper()
	_, err := client.NewPlugin(s.target.BaseURL, token, s.hc).Manifest(t.Context())
	if !errors.Is(err, pluginapi.ErrUnauthorized) {
		t.Errorf("manifest with token %q: err = %v, want %v", token, err, pluginapi.ErrUnauthorized)
	}
	var httpErr *client.HTTPError
	if errors.As(err, &httpErr) && httpErr.Status != http.StatusUnauthorized {
		t.Errorf("manifest with token %q: status = %d, want %d", token, httpErr.Status, http.StatusUnauthorized)
	}
}

func (s *suite) checkFixtures(t *testing.T) {
	for _, spec := range s.manifest.Actions {
		fixture, ok := s.target.Fixtures[spec.Name]
		if !ok {
			t.Errorf("action %q has no fixture", spec.Name)
			continue
		}
		schema, err := pluginapi.CompileSchema(spec.InputSchema)
		if err != nil {
			t.Errorf("action %q input_schema: %v", spec.Name, err)
			continue
		}
		if err := schema.Validate(fixture); err != nil {
			t.Errorf("action %q fixture does not match input_schema: %v", spec.Name, err)
		}
	}
}

func (s *suite) checkUnknownAction(t *testing.T) {
	_, err := s.plugin.StartJob(t.Context(), unknownAction, s.request(newUUID(t), json.RawMessage(`{}`)))
	if !errors.Is(err, pluginapi.ErrUnknownAction) {
		t.Errorf("start unknown action: err = %v, want %v", err, pluginapi.ErrUnknownAction)
	}
}

func (s *suite) postStatus(ctx context.Context, action string, body []byte) (int, error) {
	target := s.target.BaseURL + "/v1/actions/" + action + "/jobs"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set(pluginapi.HeaderAuthorization, pluginapi.BearerPrefix+s.target.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.hc.Do(req)
	if err != nil {
		return 0, err
	}
	return resp.StatusCode, resp.Body.Close()
}

func (s *suite) checkInvalidRequest(t *testing.T) {
	first := s.manifest.Actions[0].Name
	t.Run("non json body", func(t *testing.T) {
		status, err := s.postStatus(t.Context(), first, []byte("not json"))
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		if status != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want %d", status, http.StatusUnprocessableEntity)
		}
	})
	t.Run("empty callback urls", func(t *testing.T) {
		input, ok := s.target.Fixtures[first]
		if !ok {
			input = json.RawMessage(`{}`)
		}
		req := s.request(newUUID(t), input)
		req.Callback = pluginapi.Callback{}
		body, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("encode request: %v", err)
		}
		status, err := s.postStatus(t.Context(), first, body)
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		if status != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want %d", status, http.StatusUnprocessableEntity)
		}
	})
}

func (s *suite) checkUnknownJob(t *testing.T) {
	id := newUUID(t)
	t.Run("get", func(t *testing.T) {
		_, err := s.plugin.GetJob(t.Context(), id)
		if !errors.Is(err, pluginapi.ErrJobNotFound) {
			t.Errorf("get unknown job: err = %v, want %v", err, pluginapi.ErrJobNotFound)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		_, err := s.plugin.CancelJob(t.Context(), id)
		if !errors.Is(err, pluginapi.ErrJobNotFound) {
			t.Errorf("cancel unknown job: err = %v, want %v", err, pluginapi.ErrJobNotFound)
		}
	})
}

func (s *suite) checkConflict(t *testing.T) {
	specs := s.fixtured()
	if len(specs) < 2 {
		t.Skip("conflict check needs at least two actions with fixtures")
	}
	first, second := specs[0], specs[1]
	id := newUUID(t)
	if _, err := s.plugin.StartJob(t.Context(), first.Name, s.request(id, s.target.Fixtures[first.Name])); err != nil {
		t.Fatalf("start %s: %v", first.Name, err)
	}
	_, err := s.plugin.StartJob(t.Context(), second.Name, s.request(id, s.target.Fixtures[second.Name]))
	if !errors.Is(err, pluginapi.ErrJobConflict) {
		t.Errorf("start %s with job id used by %s: err = %v, want %v", second.Name, first.Name, err, pluginapi.ErrJobConflict)
	}
}
