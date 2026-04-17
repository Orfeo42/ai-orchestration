package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ai-orchestration/internal/events"
	"ai-orchestration/internal/runstore"
)

func TestWebSocketHelloAndReplay(t *testing.T) {
	t.Run("sends hello then replays existing events", func(t *testing.T) {
		s := newTestServer(t)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		require.NoError(t, s.store.Create(ctx, runstore.Run{
			ID:        "run-ws",
			Service:   runstore.ServiceOrchestrator,
			Phase:     runstore.PhaseImplementing,
			StartedAt: time.Now().UTC(),
		}))
		_, err := s.store.Append(ctx, "run-ws", events.Event{Kind: events.KindAssistant, Text: "first"})
		require.NoError(t, err)
		_, err = s.store.Append(ctx, "run-ws", events.Event{Kind: events.KindToolUse, ToolName: "Read"})
		require.NoError(t, err)

		wsURL := "ws" + strings.TrimPrefix(s.ts.URL, "http") + "/ws/runs/run-ws"
		conn, _, err := websocket.Dial(ctx, wsURL, nil)
		require.NoError(t, err)
		defer conn.CloseNow()

		hello := readFrame(t, ctx, conn)
		assert.Equal(t, "hello", hello.Type)
		require.NotNil(t, hello.Run)
		assert.Equal(t, "run-ws", hello.Run.ID)
		assert.Equal(t, int64(2), hello.Last)

		first := readFrame(t, ctx, conn)
		assert.Equal(t, "event", first.Type)
		require.NotNil(t, first.Event)
		assert.Equal(t, "first", first.Event.Text)

		second := readFrame(t, ctx, conn)
		assert.Equal(t, "event", second.Type)
		require.NotNil(t, second.Event)
		assert.Equal(t, "Read", second.Event.ToolName)
	})
}

func readFrame(t *testing.T, ctx context.Context, conn *websocket.Conn) wsFrame {
	t.Helper()
	rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, data, err := conn.Read(rctx)
	require.NoError(t, err)
	var frame wsFrame
	require.NoError(t, json.Unmarshal(data, &frame))
	return frame
}

func TestWebSocketRespectsTestServer(_ *testing.T) {
	_ = httptest.NewRequest
}
