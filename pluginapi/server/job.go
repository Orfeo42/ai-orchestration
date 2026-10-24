package server

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"time"

	"git.ionstream.ai/ionstream/ai-orchestrator/pluginapi"
)

const messageBuffer = 64

type job struct {
	action   *action
	req      pluginapi.JobRequest
	ctx      context.Context
	cancel   context.CancelFunc
	messages chan pluginapi.Message
	done     chan struct{}

	mu        sync.Mutex
	status    pluginapi.JobStatus
	phase     string
	result    *pluginapi.Result
	updatedAt time.Time
	events    []pluginapi.Event
	wake      chan struct{}
}

func newJob(base context.Context, act *action, req pluginapi.JobRequest) *job {
	ctx, cancel := context.WithDeadline(base, req.Deadline)
	return &job{
		action:    act,
		req:       req,
		ctx:       ctx,
		cancel:    cancel,
		messages:  make(chan pluginapi.Message, messageBuffer),
		done:      make(chan struct{}),
		status:    pluginapi.JobRunning,
		updatedAt: time.Now(),
		wake:      make(chan struct{}),
	}
}

func (j *job) async() bool {
	return j.action.spec.Mode == pluginapi.ModeAsync
}

func (j *job) state() pluginapi.JobState {
	j.mu.Lock()
	defer j.mu.Unlock()
	return pluginapi.JobState{
		JobID:     j.req.JobID,
		Action:    j.action.spec.Name,
		Status:    j.status,
		Phase:     j.phase,
		Result:    j.result,
		UpdatedAt: j.updatedAt,
	}
}

func (j *job) matches(actionName string, input json.RawMessage) bool {
	if j.action.spec.Name != actionName {
		return false
	}
	var stored, incoming bytes.Buffer
	if err := json.Compact(&stored, j.req.Input); err != nil {
		return false
	}
	if err := json.Compact(&incoming, input); err != nil {
		return false
	}
	return bytes.Equal(stored.Bytes(), incoming.Bytes())
}

func (j *job) setPhase(phase string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.phase = phase
	j.updatedAt = time.Now()
}

func (j *job) appendEvent(kind string, data json.RawMessage) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.status != pluginapi.JobRunning {
		return
	}
	now := time.Now()
	j.events = append(j.events, pluginapi.Event{Seq: int64(len(j.events) + 1), At: now, Kind: kind, Data: data})
	j.updatedAt = now
	j.broadcast()
}

func (j *job) finish(status pluginapi.JobStatus, result pluginapi.Result) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.status = status
	j.result = &result
	j.updatedAt = time.Now()
	close(j.done)
	j.broadcast()
}

func (j *job) broadcast() {
	close(j.wake)
	j.wake = make(chan struct{})
}

func (j *job) eventsAfter(after int64) ([]pluginapi.Event, bool, <-chan struct{}) {
	j.mu.Lock()
	defer j.mu.Unlock()
	var pending []pluginapi.Event
	if after < int64(len(j.events)) {
		pending = append(pending, j.events[after:]...)
	}
	return pending, j.status != pluginapi.JobRunning, j.wake
}

func (j *job) expired(cutoff time.Time) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.status != pluginapi.JobRunning && j.updatedAt.Before(cutoff)
}

type reporter struct {
	s *Server
	j *job
}

func (r reporter) Progress(ctx context.Context, phase string) error {
	r.j.setPhase(phase)
	if !r.j.async() {
		return nil
	}
	return r.s.postProgress(ctx, r.j, phase)
}

func (r reporter) Event(_ context.Context, kind string, data json.RawMessage) error {
	if !r.j.action.spec.Has(pluginapi.CapabilityEvents) {
		return nil
	}
	r.j.appendEvent(kind, data)
	return nil
}

func (r reporter) Messages() <-chan pluginapi.Message {
	return r.j.messages
}
