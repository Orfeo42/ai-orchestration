package events

import "context"

//go:generate moq -rm -out sink_mock.go . Sink:MockSink

type Sink interface {
	Emit(ctx context.Context, evt Event) error
}

type SinkFunc func(ctx context.Context, evt Event) error

func (f SinkFunc) Emit(ctx context.Context, evt Event) error {
	return f(ctx, evt)
}

type NoopSink struct{}

func (NoopSink) Emit(context.Context, Event) error { return nil }
