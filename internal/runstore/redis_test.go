package runstore

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ai-orchestration/internal/events"
)

func newTestEvents(t *testing.T) *RedisEvents {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return NewRedisEventsFromClient(client)
}

func TestRedisEventsAppendAndRange(t *testing.T) {
	t.Run("seq increments and range filters", func(t *testing.T) {
		store := newTestEvents(t)
		ctx := context.Background()

		for range 5 {
			_, err := store.Append(ctx, "run-1", events.Event{Kind: events.KindAssistant, Text: "msg"})
			require.NoError(t, err)
		}

		all, err := store.EventsRange(ctx, "run-1", 0, 100)
		require.NoError(t, err)
		require.Len(t, all, 5)
		assert.Equal(t, int64(1), all[0].Seq)
		assert.Equal(t, int64(5), all[4].Seq)

		after, err := store.EventsRange(ctx, "run-1", 3, 100)
		require.NoError(t, err)
		require.Len(t, after, 2)
		assert.Equal(t, int64(4), after[0].Seq)
	})

	t.Run("empty run id is rejected", func(t *testing.T) {
		store := newTestEvents(t)
		_, err := store.Append(context.Background(), "", events.Event{})
		require.Error(t, err)
	})
}

func TestRedisEventsAppendSink(t *testing.T) {
	t.Run("sink writes via Append", func(t *testing.T) {
		store := newTestEvents(t)
		ctx := context.Background()

		sink := store.AppendSink("run-1")
		require.NoError(t, sink.Emit(ctx, events.Event{Kind: events.KindPhase, Phase: string(PhaseImplementing)}))

		all, err := store.EventsRange(ctx, "run-1", 0, 10)
		require.NoError(t, err)
		require.Len(t, all, 1)
		assert.Equal(t, events.KindPhase, all[0].Kind)
	})
}

func TestRedisEventsTail(t *testing.T) {
	t.Run("returns events after last stream id", func(t *testing.T) {
		store := newTestEvents(t)
		ctx := context.Background()
		first, err := store.Append(ctx, "run-1", events.Event{Kind: events.KindAssistant, Text: "a"})
		require.NoError(t, err)
		_, err = store.Append(ctx, "run-1", events.Event{Kind: events.KindAssistant, Text: "b"})
		require.NoError(t, err)

		tail, err := store.EventsTail(ctx, "run-1", first.ID, 0)
		require.NoError(t, err)
		require.Len(t, tail.Events, 1)
		assert.Equal(t, "b", tail.Events[0].Text)
	})
}
