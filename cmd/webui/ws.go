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
	"ai-orchestration/internal/runstore"
)

type wsFrame struct {
	Type  string         `json:"type"`
	Run   *runstore.Run  `json:"run,omitempty"`
	Event *events.Event  `json:"event,omitempty"`
	Last  int64          `json:"last_seq,omitempty"`
	Error string         `json:"error,omitempty"`
}

const (
	wsTailBlock     = 5 * time.Second
	wsTailLimit     = 200
	wsRunPollPeriod = 3 * time.Second
)

func (h *apiHandler) streamRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
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
	if err := h.wsServe(ctx, conn, id); err != nil && !errors.Is(err, context.Canceled) {
		slog.WarnContext(ctx, "ws serve ended",
			"run", id,
			"error", err,
		)
	}
}

func (h *apiHandler) wsServe(ctx context.Context, conn *websocket.Conn, runID string) error {
	run, err := h.store.Get(ctx, runID)
	if err != nil {
		return writeFrame(ctx, conn, wsFrame{Type: "error", Error: err.Error()})
	}

	initial, err := h.store.EventsRange(ctx, runID, 0, 5000)
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

	if err := writeFrame(ctx, conn, wsFrame{Type: "hello", Run: &run, Last: lastSeq}); err != nil {
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
			_ = writeFrame(context.Background(), conn, wsFrame{Type: "bye"})
			return ctx.Err()
		case <-pollTick.C:
			if updated, gerr := h.store.Get(ctx, runID); gerr == nil {
				if err := writeFrame(ctx, conn, wsFrame{Type: "run_update", Run: &updated}); err != nil {
					return err
				}
				if updated.Phase == runstore.PhaseDone || updated.Phase == runstore.PhaseFailed || updated.Phase == runstore.PhaseCancelled {
					tail, terr := h.store.EventsTail(ctx, runID, lastStreamID, time.Second)
					if terr != nil && !errors.Is(terr, context.Canceled) {
						return terr
					}
					for i := range tail.Events {
						if err := writeFrame(ctx, conn, wsFrame{Type: "event", Event: &tail.Events[i]}); err != nil {
							return err
						}
					}
					_ = writeFrame(context.Background(), conn, wsFrame{Type: "bye"})
					return nil
				}
			}
		default:
		}

		tail, err := h.store.EventsTail(ctx, runID, lastStreamID, wsTailBlock)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			slog.WarnContext(ctx, "ws tail failed",
				"run", runID,
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

func writeFrame(ctx context.Context, conn *websocket.Conn, frame wsFrame) error {
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	return conn.Write(wctx, websocket.MessageText, data)
}
