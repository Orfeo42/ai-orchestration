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

type teeSink struct {
	sinks []Sink
}

func Tee(sinks ...Sink) Sink {
	filtered := make([]Sink, 0, len(sinks))
	for _, s := range sinks {
		if s == nil {
			continue
		}
		filtered = append(filtered, s)
	}
	if len(filtered) == 0 {
		return NoopSink{}
	}
	if len(filtered) == 1 {
		return filtered[0]
	}
	return &teeSink{sinks: filtered}
}

func (t *teeSink) Emit(ctx context.Context, evt Event) error {
	var firstErr error
	for _, s := range t.sinks {
		if err := s.Emit(ctx, evt); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
