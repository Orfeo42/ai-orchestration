package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ai-orchestration/internal/events"
	"ai-orchestration/internal/process"
	"ai-orchestration/internal/runstore"
)

func readFrame(t *testing.T, ctx context.Context, conn *websocket.Conn) wsFrame {
	t.Helper()
	rctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	_, data, err := conn.Read(rctx)
	require.NoError(t, err)
	var frame wsFrame
	require.NoError(t, json.Unmarshal(data, &frame))
	return frame
}

func TestStepRunWebSocket(t *testing.T) {
	t.Run("sends hello with step run then replays events", func(t *testing.T) {
		s := newProcessTestServer(t)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.reader.GetStepRunFunc = func(context.Context, string) (process.StepRun, error) {
			return process.StepRun{ID: "sr-ws", Status: process.StepRunning}, nil
		}
		s.events.EventsRangeFunc = func(context.Context, string, int64, int) ([]events.Event, error) {
			return []events.Event{{ID: "1-0", Seq: 1, Text: "first"}, {ID: "2-0", Seq: 2, ToolName: "Read"}}, nil
		}
		s.events.EventsTailFunc = func(ctx context.Context, _ string, last string, _ time.Duration) (runstore.EventTail, error) {
			<-ctx.Done()
			return runstore.EventTail{LastID: last}, ctx.Err()
		}

		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(s.url, "http")+"/ws/step-runs/sr-ws", nil)
		require.NoError(t, err)
		defer conn.CloseNow()

		hello := readFrame(t, ctx, conn)
		assert.Equal(t, "hello", hello.Type)
		require.NotNil(t, hello.StepRun)
		assert.Equal(t, "sr-ws", hello.StepRun.ID)
		assert.Equal(t, int64(2), hello.Last)

		first := readFrame(t, ctx, conn)
		require.NotNil(t, first.Event)
		assert.Equal(t, "first", first.Event.Text)

		second := readFrame(t, ctx, conn)
		require.NotNil(t, second.Event)
		assert.Equal(t, "Read", second.Event.ToolName)
	})

	t.Run("unknown step run sends error frame", func(t *testing.T) {
		s := newProcessTestServer(t)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.reader.GetStepRunFunc = func(context.Context, string) (process.StepRun, error) {
			return process.StepRun{}, process.ErrNotFound
		}

		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(s.url, "http")+"/ws/step-runs/nope", nil)
		require.NoError(t, err)
		defer conn.CloseNow()

		frame := readFrame(t, ctx, conn)
		assert.Equal(t, "error", frame.Type)
		assert.Contains(t, frame.Error, "not found")
	})

	t.Run("terminal step run emits run_update then bye", func(t *testing.T) {
		s := newProcessTestServer(t)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s.reader.GetStepRunFunc = func(context.Context, string) (process.StepRun, error) {
			return process.StepRun{ID: "sr-done", Status: process.StepSucceeded}, nil
		}
		s.events.EventsRangeFunc = func(context.Context, string, int64, int) ([]events.Event, error) {
			return nil, nil
		}
		s.events.EventsTailFunc = func(ctx context.Context, _ string, last string, block time.Duration) (runstore.EventTail, error) {
			select {
			case <-ctx.Done():
				return runstore.EventTail{LastID: last}, ctx.Err()
			case <-time.After(min(block, 100*time.Millisecond)):
				return runstore.EventTail{LastID: last}, nil
			}
		}

		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(s.url, "http")+"/ws/step-runs/sr-done", nil)
		require.NoError(t, err)
		defer conn.CloseNow()

		assert.Equal(t, "hello", readFrame(t, ctx, conn).Type)
		update := readFrame(t, ctx, conn)
		assert.Equal(t, "run_update", update.Type)
		require.NotNil(t, update.StepRun)
		assert.Equal(t, process.StepSucceeded, update.StepRun.Status)
		assert.Equal(t, "bye", readFrame(t, ctx, conn).Type)
	})
}
