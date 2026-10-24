package pluginapi

import (
	"context"
	"encoding/json"
	"time"
)

type Job struct {
	ID        string
	ProcessID string
	Input     json.RawMessage
	Deadline  time.Time
}

type Output struct {
	Outcome Outcome
	Data    json.RawMessage
	Usage   Usage
}

type Reporter interface {
	Progress(ctx context.Context, phase string) error
	Event(ctx context.Context, kind string, data json.RawMessage) error
	Messages() <-chan Message
}

type Action interface {
	Spec() ActionSpec
	Run(ctx context.Context, job Job, rep Reporter) (Output, error)
}
