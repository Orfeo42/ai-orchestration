package runstore

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"ai-orchestration/internal/control"
	"ai-orchestration/internal/events"
	"ai-orchestration/internal/models"
)

type RunHandle struct {
	id           string
	sink         events.Sink
	persist      persister
	ctx          context.Context
	cancel       context.CancelFunc
	notesMu      sync.Mutex
	notes        []string
	seenComments map[int64]time.Time
	interruptMu  sync.Mutex
	interruptGen int
	interrupt    context.CancelFunc
}

func newHandle(id string) *RunHandle {
	return &RunHandle{
		id:           id,
		sink:         events.NoopSink{},
		persist:      nopPersister{},
		seenComments: map[int64]time.Time{},
	}
}

func (h *RunHandle) start(ctx context.Context, ctrl control.Channel, service Service) {
	runCtx, cancel := context.WithCancel(ctx)
	h.ctx = runCtx
	h.cancel = cancel

	if ctrl != nil {
		sub, err := ctrl.Subscribe(runCtx, h.id)
		if err != nil {
			slog.WarnContext(ctx, "ctrl subscribe failed",
				slog.String("run", h.id),
				slog.String("error", err.Error()),
			)
		} else {
			go h.handleControl(runCtx, sub)
		}
	}

	h.Emit(runCtx, events.Event{
		Kind:    events.KindRunStart,
		Service: string(service),
	})
}

func (h *RunHandle) ID() string           { return h.id }
func (h *RunHandle) Sink() events.Sink    { return h.sink }
func (h *RunHandle) Ctx() context.Context { return h.ctx }
func (h *RunHandle) Cancel()              { h.cancel() }

func (h *RunHandle) Emit(ctx context.Context, evt events.Event) {
	if err := h.sink.Emit(ctx, evt); err != nil {
		slog.WarnContext(ctx, "sink emit failed",
			slog.String("run", h.id),
			slog.String("error", err.Error()),
		)
	}
}

func (h *RunHandle) SetPhase(ctx context.Context, phase Phase) {
	if err := h.persist.setPhase(ctx, phase); err != nil {
		slog.WarnContext(ctx, "update phase failed",
			slog.String("run", h.id),
			slog.String("error", err.Error()),
		)
	}
	h.Emit(ctx, events.Event{Kind: events.KindPhase, Phase: string(phase)})
}

func (h *RunHandle) AddUsage(ctx context.Context, u events.TokenUsage) {
	if err := h.persist.addUsage(ctx, u); err != nil {
		slog.WarnContext(ctx, "update usage failed",
			slog.String("run", h.id),
			slog.String("error", err.Error()),
		)
	}
}

func (h *RunHandle) SetSession(ctx context.Context, sessionID string) {
	if sessionID == "" {
		return
	}
	if err := h.persist.setSession(ctx, sessionID); err != nil {
		slog.WarnContext(ctx, "update session failed",
			slog.String("run", h.id),
			slog.String("error", err.Error()),
		)
	}
}

func (h *RunHandle) SetAttempt(ctx context.Context, attempt int) {
	if err := h.persist.setAttempt(ctx, attempt); err != nil {
		slog.WarnContext(ctx, "update attempt failed",
			slog.String("run", h.id),
			slog.String("error", err.Error()),
		)
	}
}

func (h *RunHandle) MarkCommentsSeen(comments []models.IssueComment) {
	for _, c := range comments {
		h.seenComments[c.ID] = c.UpdatedAt
	}
}

func (h *RunHandle) FreshComments(comments []models.IssueComment) []models.IssueComment {
	var fresh []models.IssueComment
	for _, c := range comments {
		prev, ok := h.seenComments[c.ID]
		if !ok || c.UpdatedAt.After(prev) {
			fresh = append(fresh, c)
		}
		h.seenComments[c.ID] = c.UpdatedAt
	}
	return fresh
}

func (h *RunHandle) DrainNotes() []string {
	h.notesMu.Lock()
	defer h.notesMu.Unlock()
	notes := h.notes
	h.notes = nil
	return notes
}

func (h *RunHandle) AppendNote(note string) {
	h.notesMu.Lock()
	defer h.notesMu.Unlock()
	h.notes = append(h.notes, note)
}

func (h *RunHandle) PendingNotes() []string {
	h.notesMu.Lock()
	defer h.notesMu.Unlock()
	return append([]string{}, h.notes...)
}

func (h *RunHandle) PrependNotes(notes []string) {
	h.notesMu.Lock()
	defer h.notesMu.Unlock()
	h.notes = append(notes, h.notes...)
}

func (h *RunHandle) Finalize(ctx context.Context, phase Phase, finalErr error) {
	errStr := ""
	if finalErr != nil {
		errStr = finalErr.Error()
	}
	if err := h.persist.finalize(ctx, phase, errStr); err != nil {
		slog.WarnContext(ctx, "finalize run failed",
			slog.String("run", h.id),
			slog.String("error", err.Error()),
		)
	}
	h.Emit(ctx, events.Event{Kind: events.KindRunEnd, Phase: string(phase)})
}

func (h *RunHandle) handleControl(ctx context.Context, sub control.Subscription) {
	defer func() {
		if err := sub.Close(); err != nil {
			slog.WarnContext(ctx, "ctrl subscription close failed",
				slog.String("run", h.id),
				slog.String("error", err.Error()),
			)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-sub.Messages():
			if !ok {
				return
			}
			switch msg.Type {
			case control.TypeCancel:
				h.Emit(ctx, events.Event{Kind: events.KindCancel})
				h.cancel()
				return
			case control.TypeInject:
				h.AppendNote(msg.Note)
				h.Emit(ctx, events.Event{Kind: events.KindInject, Text: msg.Note})
				h.interruptCurrent()
			}
		}
	}
}
