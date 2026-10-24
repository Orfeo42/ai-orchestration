package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"git.ionstream.ai/ionstream/ai-orchestrator/pluginapi"
)

const (
	pollInterval    = 10 * time.Millisecond
	duplicateWait   = 500 * time.Millisecond
	finishedMessage = "conformance message"
)

func (s *suite) checkAction(t *testing.T, spec pluginapi.ActionSpec, input json.RawMessage) {
	req := s.request(newUUID(t), input)
	result := s.runJob(t, spec, req)
	t.Run("result", func(t *testing.T) {
		s.checkResult(t, spec, req.JobID, result)
	})
	t.Run("duplicate start", func(t *testing.T) {
		s.checkDuplicate(t, spec, req)
	})
	t.Run("get", func(t *testing.T) {
		s.checkGet(t, req.JobID)
	})
	t.Run("cancel after completion", func(t *testing.T) {
		s.checkCancel(t, req.JobID)
	})
	t.Run("progress", func(t *testing.T) {
		s.checkProgress(t, req.JobID)
	})
	t.Run("events", func(t *testing.T) {
		s.checkEvents(t, spec, req.JobID)
	})
	t.Run("inject", func(t *testing.T) {
		s.checkInject(t, spec, req.JobID)
	})
}

func (s *suite) runJob(t *testing.T, spec pluginapi.ActionSpec, req pluginapi.JobRequest) pluginapi.Result {
	t.Helper()
	state, err := s.plugin.StartJob(t.Context(), spec.Name, req)
	if err != nil {
		t.Fatalf("start job: %v", err)
	}
	if state.JobID != req.JobID {
		t.Fatalf("start job: job_id = %q, want %q", state.JobID, req.JobID)
	}
	if spec.Mode == pluginapi.ModeAsync {
		if state.Status != pluginapi.JobRunning {
			t.Fatalf("start async job: status = %q, want %q", state.Status, pluginapi.JobRunning)
		}
		return s.waitResult(t, req.JobID)
	}
	if state.Status != pluginapi.JobSucceeded && state.Status != pluginapi.JobFailed {
		t.Fatalf("start sync job: status = %q, want %q or %q", state.Status, pluginapi.JobSucceeded, pluginapi.JobFailed)
	}
	if state.Result == nil {
		t.Fatalf("start sync job: result is not set")
	}
	return *state.Result
}

func (s *suite) waitResult(t *testing.T, jobID string) pluginapi.Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), s.timeout)
	defer cancel()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		if results := s.core.resultsFor(jobID); len(results) > 0 {
			return results[0]
		}
		select {
		case <-ctx.Done():
			t.Fatalf("no result callback within %s", s.timeout)
		case <-ticker.C:
		}
	}
}

func (s *suite) checkResult(t *testing.T, spec pluginapi.ActionSpec, jobID string, result pluginapi.Result) {
	if result.JobID != jobID {
		t.Errorf("result job_id = %q, want %q", result.JobID, jobID)
	}
	hasOutcome := result.Outcome != ""
	if hasOutcome == (result.Error != nil) {
		t.Fatalf("result must set exactly one of outcome and error: outcome=%q error=%v", result.Outcome, result.Error)
	}
	if result.Error != nil {
		if result.Error.Kind != pluginapi.ErrorUpstream && result.Error.Kind != pluginapi.ErrorInternal {
			t.Errorf("result error kind = %q, want %q or %q", result.Error.Kind, pluginapi.ErrorUpstream, pluginapi.ErrorInternal)
		}
		return
	}
	if !spec.Declares(result.Outcome) {
		t.Errorf("result outcome %q is not declared by the action", result.Outcome)
	}
	schema, err := pluginapi.CompileSchema(spec.OutputSchema)
	if err != nil {
		t.Fatalf("compile output_schema: %v", err)
	}
	if err := schema.Validate(result.Output); err != nil {
		t.Errorf("result output does not match output_schema: %v", err)
	}
}

func (s *suite) checkDuplicate(t *testing.T, spec pluginapi.ActionSpec, req pluginapi.JobRequest) {
	before := len(s.core.resultsFor(req.JobID))
	state, err := s.plugin.StartJob(t.Context(), spec.Name, req)
	if err != nil {
		t.Fatalf("repeat start: %v", err)
	}
	if state.JobID != req.JobID {
		t.Errorf("repeat start: job_id = %q, want %q", state.JobID, req.JobID)
	}
	time.Sleep(duplicateWait)
	want := before
	if spec.Mode == pluginapi.ModeAsync {
		want = 1
	}
	if got := len(s.core.resultsFor(req.JobID)); got != want {
		t.Errorf("result callbacks = %d, want %d", got, want)
	}
}

func (s *suite) checkGet(t *testing.T, jobID string) {
	state, err := s.plugin.GetJob(t.Context(), jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if state.Status == pluginapi.JobRunning {
		t.Errorf("get job: status = %q after completion", state.Status)
	}
	if state.Result == nil {
		t.Fatalf("get job: result is not set")
	}
	if state.Result.JobID != jobID {
		t.Errorf("get job: result job_id = %q, want %q", state.Result.JobID, jobID)
	}
}

func (s *suite) checkCancel(t *testing.T, jobID string) {
	state, err := s.plugin.CancelJob(t.Context(), jobID)
	if err != nil {
		t.Fatalf("cancel finished job: %v", err)
	}
	if state.Status == pluginapi.JobRunning {
		t.Errorf("cancel finished job: status = %q, want a terminal status", state.Status)
	}
}

func (s *suite) checkProgress(t *testing.T, jobID string) {
	for _, p := range s.core.progressFor(jobID) {
		if p.JobID != jobID {
			t.Errorf("progress job_id = %q, want %q", p.JobID, jobID)
		}
		if p.Phase == "" {
			t.Errorf("progress phase is empty")
		}
	}
}

func (s *suite) checkEvents(t *testing.T, spec pluginapi.ActionSpec, jobID string) {
	var events []pluginapi.Event
	err := s.plugin.StreamEvents(t.Context(), jobID, 0, func(e pluginapi.Event) error {
		events = append(events, e)
		return nil
	})
	if !spec.Has(pluginapi.CapabilityEvents) {
		if !errors.Is(err, pluginapi.ErrUnsupported) {
			t.Errorf("stream events without capability: err = %v, want %v", err, pluginapi.ErrUnsupported)
		}
		return
	}
	if err != nil {
		t.Fatalf("stream events: %v", err)
	}
	var last int64
	for _, e := range events {
		if e.Seq <= last {
			t.Errorf("event seq = %d, want > %d", e.Seq, last)
		}
		last = e.Seq
	}
}

func (s *suite) checkInject(t *testing.T, spec pluginapi.ActionSpec, jobID string) {
	err := s.plugin.SendMessage(t.Context(), jobID, pluginapi.Message{Text: finishedMessage})
	want := pluginapi.ErrJobNotActive
	if !spec.Has(pluginapi.CapabilityInject) {
		want = pluginapi.ErrUnsupported
	}
	if !errors.Is(err, want) {
		t.Errorf("send message: err = %v, want %v", err, want)
	}
}
