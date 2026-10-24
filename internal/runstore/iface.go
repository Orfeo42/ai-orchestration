package runstore

import (
	"context"
	"time"

	"github.com/Orfeo42/ai-orchestration/internal/events"
)

//go:generate moq -rm -out store_mock.go . EventStore:MockEventStore StepRunWriter:MockStepRunWriter

type EventStore interface {
	Append(ctx context.Context, runID string, evt events.Event) (events.Event, error)
	EventsRange(ctx context.Context, runID string, sinceSeq int64, limit int) ([]events.Event, error)
	EventsTail(ctx context.Context, runID string, lastStreamID string, block time.Duration) (EventTail, error)
	AppendSink(runID string) events.Sink
	Close() error
}

type StepRunWriter interface {
	SetStepPhase(ctx context.Context, stepRunID, phase string) error
	SetStepIteration(ctx context.Context, stepRunID string, iteration int) error
	SetStepSession(ctx context.Context, stepRunID, session string) error
	AddStepUsage(ctx context.Context, stepRunID string, usage events.TokenUsage) error
}
