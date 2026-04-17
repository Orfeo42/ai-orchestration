package runstore

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"ai-orchestration/internal/control"
	"ai-orchestration/internal/events"
	"ai-orchestration/internal/models"
)

type RunHandle struct {
	id           string
	sink         events.Sink
	store        Store
	ctx          context.Context
	cancel       context.CancelFunc
	notesMu      sync.Mutex
	notes        []string
	seenComments map[int64]time.Time
}

func NewRunHandle(ctx context.Context, store Store, ctrl control.Channel, initial Run) *RunHandle {
	if initial.ID == "" {
		initial.ID = uuid.NewString()
	}
	if initial.StartedAt.IsZero() {
		initial.StartedAt = time.Now().UTC()
	}

	h := &RunHandle{
		id:           initial.ID,
		sink:         events.NoopSink{},
		seenComments: map[int64]time.Time{},
	}

	if store != nil {
		if err := store.Create(ctx, initial); err != nil {
			slog.WarnContext(ctx, "create run failed",
				"error", err,
			)
		} else {
			h.store = store
			h.sink = store.AppendSink(h.id)
		}
	}

	runCtx, cancel := context.WithCancel(ctx)
	h.ctx = runCtx
	h.cancel = cancel

	if ctrl != nil {
		sub, err := ctrl.Subscribe(runCtx, h.id)
		if err != nil {
			slog.WarnContext(ctx, "ctrl subscribe failed",
				"run", h.id,
				"error", err,
			)
		} else {
			go h.handleControl(runCtx, sub)
		}
	}

	h.Emit(runCtx, events.Event{
		Kind:    events.KindRunStart,
		Service: string(initial.Service),
	})

	return h
}

func (h *RunHandle) ID() string            { return h.id }
func (h *RunHandle) Sink() events.Sink     { return h.sink }
func (h *RunHandle) Ctx() context.Context  { return h.ctx }
func (h *RunHandle) Cancel()               { h.cancel() }
func (h *RunHandle) Store() Store          { return h.store }

func (h *RunHandle) Emit(ctx context.Context, evt events.Event) {
	if h.sink == nil {
		return
	}
	if err := h.sink.Emit(ctx, evt); err != nil {
		slog.WarnContext(ctx, "sink emit failed",
			"error", err,
		)
	}
}

func (h *RunHandle) SetPhase(ctx context.Context, phase Phase) {
	if h.store != nil {
		if err := h.store.Update(ctx, h.id, func(r *Run) {
			r.Phase = phase
		}); err != nil {
			slog.WarnContext(ctx, "update phase failed",
				"run", h.id,
				"error", err,
			)
		}
	}
	h.Emit(ctx, events.Event{Kind: events.KindPhase, Phase: string(phase)})
}

func (h *RunHandle) AddUsage(ctx context.Context, u events.TokenUsage) {
	if h.store == nil {
		return
	}
	if err := h.store.Update(ctx, h.id, func(r *Run) {
		r.Usage = r.Usage.Add(u)
	}); err != nil {
		slog.WarnContext(ctx, "update usage failed",
			"run", h.id,
			"error", err,
		)
	}
}

func (h *RunHandle) SetSession(ctx context.Context, sessionID string) {
	if h.store == nil || sessionID == "" {
		return
	}
	if err := h.store.Update(ctx, h.id, func(r *Run) {
		r.ClaudeSession = sessionID
	}); err != nil {
		slog.WarnContext(ctx, "update session failed",
			"run", h.id,
			"error", err,
		)
	}
}

func (h *RunHandle) SetAttempt(ctx context.Context, attempt int) {
	if h.store == nil {
		return
	}
	if err := h.store.Update(ctx, h.id, func(r *Run) {
		r.Attempt = attempt
	}); err != nil {
		slog.WarnContext(ctx, "update attempt failed",
			"run", h.id,
			"error", err,
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
	if h.store == nil {
		return
	}
	errStr := ""
	if finalErr != nil {
		errStr = finalErr.Error()
	}
	if err := h.store.Finalize(ctx, h.id, phase, errStr); err != nil {
		slog.WarnContext(ctx, "finalize run failed",
			"run", h.id,
			"error", err,
		)
	}
	h.Emit(ctx, events.Event{Kind: events.KindRunEnd, Phase: string(phase)})
}

func (h *RunHandle) handleControl(ctx context.Context, sub control.Subscription) {
	defer func() {
		if err := sub.Close(); err != nil {
			slog.WarnContext(ctx, "ctrl subscription close failed",
				"run", h.id,
				"error", err,
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
				h.cancel()
				return
			}
		}
	}
}
