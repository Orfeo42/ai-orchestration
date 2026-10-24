package runstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/internal/control"
	"github.com/Orfeo42/ai-orchestration/internal/events"
)

func newTestControl(t *testing.T) *control.RedisChannel {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		require.NoError(t, client.Close())
	})
	return control.NewRedisFromClient(client)
}

func newStepMocks() (*MockEventStore, *events.MockSink, *MockStepRunWriter) {
	sink := &events.MockSink{
		EmitFunc: func(context.Context, events.Event) error { return nil },
	}
	evStore := &MockEventStore{
		AppendSinkFunc: func(string) events.Sink { return sink },
	}
	writer := &MockStepRunWriter{
		SetStepPhaseFunc:     func(context.Context, string, string) error { return nil },
		SetStepIterationFunc: func(context.Context, string, int) error { return nil },
		SetStepSessionFunc:   func(context.Context, string, string) error { return nil },
		AddStepUsageFunc:     func(context.Context, string, events.TokenUsage) error { return nil },
	}
	return evStore, sink, writer
}

func waitDone(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ctx cancellation")
	}
}

func lastEmittedKind(sink *events.MockSink) events.Kind {
	calls := sink.EmitCalls()
	return calls[len(calls)-1].Evt.Kind
}

func TestNewStepHandle(t *testing.T) {
	t.Run("uses step run id, appends to its sink and emits run start", func(t *testing.T) {
		evStore, sink, writer := newStepMocks()

		h := NewStepHandle(context.Background(), evStore, writer, nil, "step-1", ServiceOrchestrator)
		defer h.Cancel()

		assert.Equal(t, "step-1", h.ID())
		require.Len(t, evStore.AppendSinkCalls(), 1)
		assert.Equal(t, "step-1", evStore.AppendSinkCalls()[0].RunID)
		require.Len(t, sink.EmitCalls(), 1)
		assert.Equal(t, events.KindRunStart, sink.EmitCalls()[0].Evt.Kind)
		assert.Equal(t, string(ServiceOrchestrator), sink.EmitCalls()[0].Evt.Service)
	})

	t.Run("nil events and writer do not panic", func(t *testing.T) {
		h := NewStepHandle(context.Background(), nil, nil, nil, "step-1", ServiceOrchestrator)
		defer h.Cancel()

		h.SetPhase(context.Background(), PhaseTesting)
		h.AddUsage(context.Background(), events.TokenUsage{})
		h.SetSession(context.Background(), "s")
		h.SetAttempt(context.Background(), 1)
		h.Finalize(context.Background(), PhaseDone, nil)
	})

	t.Run("SetPhase persists through writer and emits phase", func(t *testing.T) {
		evStore, sink, writer := newStepMocks()
		h := NewStepHandle(context.Background(), evStore, writer, nil, "step-1", ServiceOrchestrator)
		defer h.Cancel()

		h.SetPhase(context.Background(), PhaseTesting)

		require.Len(t, writer.SetStepPhaseCalls(), 1)
		assert.Equal(t, "step-1", writer.SetStepPhaseCalls()[0].StepRunID)
		assert.Equal(t, "testing", writer.SetStepPhaseCalls()[0].Phase)
		assert.Equal(t, events.KindPhase, lastEmittedKind(sink))
	})

	t.Run("AddUsage passes the delta to the writer", func(t *testing.T) {
		evStore, _, writer := newStepMocks()
		h := NewStepHandle(context.Background(), evStore, writer, nil, "step-1", ServiceOrchestrator)
		defer h.Cancel()
		usage := events.TokenUsage{Input: 3, Output: 4}

		h.AddUsage(context.Background(), usage)

		require.Len(t, writer.AddStepUsageCalls(), 1)
		assert.Equal(t, "step-1", writer.AddStepUsageCalls()[0].StepRunID)
		assert.Equal(t, usage, writer.AddStepUsageCalls()[0].Usage)
	})

	t.Run("SetSession persists non-empty session", func(t *testing.T) {
		evStore, _, writer := newStepMocks()
		h := NewStepHandle(context.Background(), evStore, writer, nil, "step-1", ServiceOrchestrator)
		defer h.Cancel()

		h.SetSession(context.Background(), "sess-1")

		require.Len(t, writer.SetStepSessionCalls(), 1)
		assert.Equal(t, "step-1", writer.SetStepSessionCalls()[0].StepRunID)
		assert.Equal(t, "sess-1", writer.SetStepSessionCalls()[0].Session)
	})

	t.Run("SetSession ignores empty session", func(t *testing.T) {
		evStore, _, writer := newStepMocks()
		h := NewStepHandle(context.Background(), evStore, writer, nil, "step-1", ServiceOrchestrator)
		defer h.Cancel()

		h.SetSession(context.Background(), "")

		assert.Empty(t, writer.SetStepSessionCalls())
	})

	t.Run("SetAttempt persists iteration", func(t *testing.T) {
		evStore, _, writer := newStepMocks()
		h := NewStepHandle(context.Background(), evStore, writer, nil, "step-1", ServiceOrchestrator)
		defer h.Cancel()

		h.SetAttempt(context.Background(), 3)

		require.Len(t, writer.SetStepIterationCalls(), 1)
		assert.Equal(t, "step-1", writer.SetStepIterationCalls()[0].StepRunID)
		assert.Equal(t, 3, writer.SetStepIterationCalls()[0].Iteration)
	})

	t.Run("Finalize only emits run end", func(t *testing.T) {
		evStore, sink, writer := newStepMocks()
		h := NewStepHandle(context.Background(), evStore, writer, nil, "step-1", ServiceOrchestrator)
		defer h.Cancel()

		h.Finalize(context.Background(), PhaseFailed, errors.New("boom"))

		assert.Equal(t, events.KindRunEnd, lastEmittedKind(sink))
		assert.Equal(t, "failed", sink.EmitCalls()[len(sink.EmitCalls())-1].Evt.Phase)
		assert.Empty(t, writer.SetStepPhaseCalls())
	})

	t.Run("writer failures do not abort", func(t *testing.T) {
		evStore, sink, writer := newStepMocks()
		writer.SetStepPhaseFunc = func(context.Context, string, string) error { return errors.New("db down") }
		h := NewStepHandle(context.Background(), evStore, writer, nil, "step-1", ServiceOrchestrator)
		defer h.Cancel()

		h.SetPhase(context.Background(), PhaseTesting)

		assert.Equal(t, events.KindPhase, lastEmittedKind(sink))
		assert.NoError(t, h.Ctx().Err())
	})

	t.Run("cancel message cancels run ctx", func(t *testing.T) {
		evStore, sink, writer := newStepMocks()
		ctrl := newTestControl(t)
		h := NewStepHandle(context.Background(), evStore, writer, ctrl, "step-1", ServiceOrchestrator)
		defer h.Cancel()

		require.NoError(t, ctrl.Cancel(context.Background(), "step-1"))

		waitDone(t, h.Ctx())
		kinds := []events.Kind{}
		for _, c := range sink.EmitCalls() {
			kinds = append(kinds, c.Evt.Kind)
		}
		assert.Contains(t, kinds, events.KindCancel)
	})
}

func TestInterruptible(t *testing.T) {
	t.Run("inject cancels current interruptible ctx but not run ctx", func(t *testing.T) {
		evStore, _, writer := newStepMocks()
		ctrl := newTestControl(t)
		h := NewStepHandle(context.Background(), evStore, writer, ctrl, "step-1", ServiceOrchestrator)
		defer h.Cancel()
		invCtx, cancel := h.Interruptible(h.Ctx())
		defer cancel()

		require.NoError(t, ctrl.Inject(context.Background(), "step-1", "note one"))

		waitDone(t, invCtx)
		assert.NoError(t, h.Ctx().Err())
		assert.Equal(t, []string{"note one"}, h.DrainNotes())
	})

	t.Run("second inject is still handled", func(t *testing.T) {
		evStore, sink, writer := newStepMocks()
		ctrl := newTestControl(t)
		h := NewStepHandle(context.Background(), evStore, writer, ctrl, "step-1", ServiceOrchestrator)
		defer h.Cancel()
		first, cancelFirst := h.Interruptible(h.Ctx())
		require.NoError(t, ctrl.Inject(context.Background(), "step-1", "one"))
		waitDone(t, first)
		cancelFirst()

		second, cancelSecond := h.Interruptible(h.Ctx())
		defer cancelSecond()
		require.NoError(t, ctrl.Inject(context.Background(), "step-1", "two"))

		waitDone(t, second)
		assert.NoError(t, h.Ctx().Err())
		assert.Equal(t, []string{"one", "two"}, h.DrainNotes())
		injects := 0
		for _, c := range sink.EmitCalls() {
			if c.Evt.Kind == events.KindInject {
				injects++
			}
		}
		assert.Equal(t, 2, injects)
	})

	t.Run("new call replaces previous registration", func(t *testing.T) {
		evStore, _, writer := newStepMocks()
		ctrl := newTestControl(t)
		h := NewStepHandle(context.Background(), evStore, writer, ctrl, "step-1", ServiceOrchestrator)
		defer h.Cancel()
		first, cancelFirst := h.Interruptible(h.Ctx())
		defer cancelFirst()
		second, cancelSecond := h.Interruptible(h.Ctx())
		defer cancelSecond()

		require.NoError(t, ctrl.Inject(context.Background(), "step-1", "x"))

		waitDone(t, second)
		assert.NoError(t, first.Err())
	})

	t.Run("cancel func cancels ctx and unregisters", func(t *testing.T) {
		evStore, _, writer := newStepMocks()
		ctrl := newTestControl(t)
		h := NewStepHandle(context.Background(), evStore, writer, ctrl, "step-1", ServiceOrchestrator)
		defer h.Cancel()
		invCtx, cancel := h.Interruptible(h.Ctx())

		cancel()

		assert.Error(t, invCtx.Err())
		require.NoError(t, ctrl.Inject(context.Background(), "step-1", "late"))
		require.Eventually(t, func() bool {
			return len(h.PendingNotes()) == 1
		}, 2*time.Second, 10*time.Millisecond)
		assert.NoError(t, h.Ctx().Err())
	})

	t.Run("stale cancel func does not unregister newer invocation", func(t *testing.T) {
		evStore, _, writer := newStepMocks()
		ctrl := newTestControl(t)
		h := NewStepHandle(context.Background(), evStore, writer, ctrl, "step-1", ServiceOrchestrator)
		defer h.Cancel()
		_, cancelFirst := h.Interruptible(h.Ctx())
		second, cancelSecond := h.Interruptible(h.Ctx())
		defer cancelSecond()

		cancelFirst()
		require.NoError(t, ctrl.Inject(context.Background(), "step-1", "x"))

		waitDone(t, second)
	})
}
