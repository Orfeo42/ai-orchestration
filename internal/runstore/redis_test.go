package runstore

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ai-orchestration/internal/events"
)

func newTestStore(t *testing.T) (*RedisStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewRedisFromClient(client), mr
}

func sampleRun() Run {
	return Run{
		ID:          "run-1",
		Service:     ServiceOrchestrator,
		RepoOwner:   "acme",
		RepoName:    "widgets",
		IssueNumber: 42,
		Title:       "Add login",
		Project:     "widgets",
		Phase:       PhaseAssessing,
		StartedAt:   time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
	}
}

func TestRedisStoreCreateGet(t *testing.T) {
	t.Run("round-trips run", func(t *testing.T) {
		store, _ := newTestStore(t)
		ctx := context.Background()
		run := sampleRun()
		require.NoError(t, store.Create(ctx, run))

		got, err := store.Get(ctx, "run-1")
		require.NoError(t, err)
		assert.Equal(t, run.ID, got.ID)
		assert.Equal(t, run.Phase, got.Phase)
		assert.Equal(t, run.RepoOwner, got.RepoOwner)
	})

	t.Run("missing returns ErrNotFound", func(t *testing.T) {
		store, _ := newTestStore(t)
		_, err := store.Get(context.Background(), "missing")
		assert.ErrorIs(t, err, ErrNotFound)
	})
}

func TestRedisStoreUpdate(t *testing.T) {
	t.Run("mutates existing run", func(t *testing.T) {
		store, _ := newTestStore(t)
		ctx := context.Background()
		require.NoError(t, store.Create(ctx, sampleRun()))

		err := store.Update(ctx, "run-1", func(r *Run) {
			r.Phase = PhaseImplementing
			r.Attempt = 2
		})
		require.NoError(t, err)

		got, err := store.Get(ctx, "run-1")
		require.NoError(t, err)
		assert.Equal(t, PhaseImplementing, got.Phase)
		assert.Equal(t, 2, got.Attempt)
	})
}

func TestRedisStoreAppendAndRange(t *testing.T) {
	t.Run("seq increments and range filters", func(t *testing.T) {
		store, _ := newTestStore(t)
		ctx := context.Background()
		require.NoError(t, store.Create(ctx, sampleRun()))

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
}

func TestRedisStoreFinalizeAggregatesTokens(t *testing.T) {
	t.Run("HINCRBY daily totals on finalize", func(t *testing.T) {
		store, _ := newTestStore(t)
		ctx := context.Background()
		run := sampleRun()
		run.Usage = events.TokenUsage{Input: 100, Output: 50, CacheCreationInput: 7, CacheReadInput: 3}
		require.NoError(t, store.Create(ctx, run))

		require.NoError(t, store.Finalize(ctx, "run-1", PhaseDone, ""))

		got, err := store.Get(ctx, "run-1")
		require.NoError(t, err)
		assert.Equal(t, PhaseDone, got.Phase)
		require.NotNil(t, got.EndedAt)

		now := time.Now().UTC()
		totals, err := store.DailyTokens(ctx, now, now)
		require.NoError(t, err)
		require.Len(t, totals, 1)
		key := now.Format(dailyDateFmt)
		assert.Equal(t, run.Usage, totals[key])
	})

	t.Run("finalize removes from active set", func(t *testing.T) {
		store, _ := newTestStore(t)
		ctx := context.Background()
		require.NoError(t, store.Create(ctx, sampleRun()))

		active, err := store.Active(ctx)
		require.NoError(t, err)
		require.Len(t, active, 1)

		require.NoError(t, store.Finalize(ctx, "run-1", PhaseFailed, "boom"))

		active, err = store.Active(ctx)
		require.NoError(t, err)
		assert.Empty(t, active)
	})
}

func TestRedisStoreList(t *testing.T) {
	t.Run("filters by service and phase", func(t *testing.T) {
		store, _ := newTestStore(t)
		ctx := context.Background()

		base := sampleRun()
		base.ID = "a"
		base.StartedAt = time.Now().Add(-3 * time.Minute)
		require.NoError(t, store.Create(ctx, base))

		base.ID = "b"
		base.Service = ServiceReviewer
		base.StartedAt = time.Now().Add(-2 * time.Minute)
		require.NoError(t, store.Create(ctx, base))

		base.ID = "c"
		base.Service = ServiceOrchestrator
		base.Phase = PhaseDone
		base.StartedAt = time.Now().Add(-1 * time.Minute)
		require.NoError(t, store.Create(ctx, base))

		out, err := store.List(ctx, ListFilter{Service: ServiceOrchestrator, Limit: 10})
		require.NoError(t, err)
		require.Len(t, out, 2)

		out, err = store.List(ctx, ListFilter{Phase: PhaseDone, Limit: 10})
		require.NoError(t, err)
		require.Len(t, out, 1)
		assert.Equal(t, "c", out[0].ID)
	})
}

func TestRedisStoreAppendSink(t *testing.T) {
	t.Run("sink writes via Append", func(t *testing.T) {
		store, _ := newTestStore(t)
		ctx := context.Background()
		require.NoError(t, store.Create(ctx, sampleRun()))

		sink := store.AppendSink("run-1")
		require.NoError(t, sink.Emit(ctx, events.Event{Kind: events.KindPhase, Phase: string(PhaseImplementing)}))

		all, err := store.EventsRange(ctx, "run-1", 0, 10)
		require.NoError(t, err)
		require.Len(t, all, 1)
		assert.Equal(t, events.KindPhase, all[0].Kind)
	})
}
