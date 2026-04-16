package runstore

import (
	"context"
	"time"

	"ai-orchestration/internal/events"
)

//go:generate moq -rm -out store_mock.go . Store:MockStore

type Store interface {
	Create(ctx context.Context, run Run) error
	Get(ctx context.Context, id string) (Run, error)
	Update(ctx context.Context, id string, mutate func(*Run)) error
	Finalize(ctx context.Context, id string, phase Phase, errStr string) error

	Append(ctx context.Context, runID string, evt events.Event) (events.Event, error)
	EventsRange(ctx context.Context, runID string, sinceSeq int64, limit int) ([]events.Event, error)
	EventsTail(ctx context.Context, runID string, lastStreamID string, block time.Duration) ([]events.Event, string, error)

	List(ctx context.Context, filter ListFilter) ([]Run, error)
	Active(ctx context.Context) ([]Run, error)

	DailyTokens(ctx context.Context, from, to time.Time) (map[string]events.TokenUsage, error)

	AppendSink(runID string) events.Sink

	Close() error
}
