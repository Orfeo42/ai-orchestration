package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"ai-orchestration/internal/events"
	"ai-orchestration/internal/process"
)

type wsFrame struct {
	Type    string           `json:"type"`
	StepRun *process.StepRun `json:"step_run,omitempty"`
	Event   *events.Event    `json:"event,omitempty"`
	Last    int64            `json:"last_seq,omitempty"`
	Error   string           `json:"error,omitempty"`
}

const (
	wsTailBlock     = 5 * time.Second
	wsRunPollPeriod = 3 * time.Second
)

type wsTarget interface {
	snapshot(ctx context.Context, frameType string) (wsFrame, bool, error)
}

type stepRunTarget struct {
	reader processReader
	id     string
}

func (t stepRunTarget) snapshot(ctx context.Context, frameType string) (wsFrame, bool, error) {
	stepRun, err := t.reader.GetStepRun(ctx, t.id)
	if err != nil {
		return wsFrame{}, false, err
	}
	return wsFrame{Type: frameType, StepRun: &stepRun}, stepRun.Status != process.StepRunning, nil
}

func (h *processHandler) streamStepRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	serveWS(w, r, id, h.events, stepRunTarget{reader: h.reader, id: id})
}

func serveWS(w http.ResponseWriter, r *http.Request, id string, evts eventReader, target wsTarget) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		slog.WarnContext(r.Context(), "ws accept failed",
			"error", err,
		)
		return
	}
	defer func() {
		if err := conn.CloseNow(); err != nil {
			slog.DebugContext(r.Context(), "ws close",
				"error", err,
			)
		}
	}()

	ctx := conn.CloseRead(r.Context())
	if err := wsServe(ctx, conn, id, evts, target); err != nil && !errors.Is(err, context.Canceled) {
		slog.WarnContext(ctx, "ws serve ended",
			"run", id,
			"error", err,
		)
	}
}

func wsServe(ctx context.Context, conn *websocket.Conn, id string, evts eventReader, target wsTarget) error {
	hello, _, err := target.snapshot(ctx, "hello")
	if err != nil {
		return writeFrame(ctx, conn, wsFrame{Type: "error", Error: err.Error()})
	}

	initial, err := evts.EventsRange(ctx, id, 0, 5000)
	if err != nil {
		return err
	}

	lastSeq := int64(0)
	lastStreamID := "0"
	for _, evt := range initial {
		if evt.Seq > lastSeq {
			lastSeq = evt.Seq
		}
		if evt.ID > lastStreamID {
			lastStreamID = evt.ID
		}
	}

	hello.Last = lastSeq
	if err := writeFrame(ctx, conn, hello); err != nil {
		return err
	}
	for i := range initial {
		if err := writeFrame(ctx, conn, wsFrame{Type: "event", Event: &initial[i]}); err != nil {
			return err
		}
	}

	pollTick := time.NewTicker(wsRunPollPeriod)
	defer pollTick.Stop()

	for {
		select {
		case <-ctx.Done():
			if err := writeFrame(context.Background(), conn, wsFrame{Type: "bye"}); err != nil {
				slog.WarnContext(ctx, "ws bye failed",
					"run", id,
					"error", err,
				)
			}
			return ctx.Err()
		case <-pollTick.C:
			done, err := wsPoll(ctx, conn, id, evts, target, lastStreamID)
			if err != nil {
				return err
			}
			if done {
				return nil
			}
		default:
		}

		tail, err := evts.EventsTail(ctx, id, lastStreamID, wsTailBlock)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			slog.WarnContext(ctx, "ws tail failed",
				"run", id,
				"error", err,
			)
			continue
		}
		lastStreamID = tail.LastID
		for i := range tail.Events {
			if tail.Events[i].Seq > lastSeq {
				lastSeq = tail.Events[i].Seq
			}
			if err := writeFrame(ctx, conn, wsFrame{Type: "event", Event: &tail.Events[i]}); err != nil {
				return err
			}
		}
	}
}

func wsPoll(ctx context.Context, conn *websocket.Conn, id string, evts eventReader, target wsTarget, lastStreamID string) (bool, error) {
	update, terminal, err := target.snapshot(ctx, "run_update")
	if err != nil {
		return false, nil
	}
	if err := writeFrame(ctx, conn, update); err != nil {
		return false, err
	}
	if !terminal {
		return false, nil
	}
	tail, err := evts.EventsTail(ctx, id, lastStreamID, time.Second)
	if err != nil && !errors.Is(err, context.Canceled) {
		return false, err
	}
	for i := range tail.Events {
		if err := writeFrame(ctx, conn, wsFrame{Type: "event", Event: &tail.Events[i]}); err != nil {
			return false, err
		}
	}
	if err := writeFrame(context.Background(), conn, wsFrame{Type: "bye"}); err != nil {
		slog.WarnContext(ctx, "ws bye failed",
			"run", id,
			"error", err,
		)
	}
	return true, nil
}

func writeFrame(ctx context.Context, conn *websocket.Conn, frame wsFrame) error {
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	return conn.Write(wctx, websocket.MessageText, data)
}
