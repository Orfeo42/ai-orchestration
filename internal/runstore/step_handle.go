package runstore

import (
	"context"

	"github.com/Orfeo42/ai-orchestration/internal/control"
)

func NewStepHandle(ctx context.Context, events EventStore, steps StepRunWriter, ctrl control.Channel, stepRunID string, service Service) *RunHandle {
	h := newHandle(stepRunID)
	if events != nil {
		h.sink = events.AppendSink(stepRunID)
	}
	if steps != nil {
		h.persist = stepPersister{steps: steps, id: stepRunID}
	}

	h.start(ctx, ctrl, service)
	return h
}

func (h *RunHandle) Interruptible(ctx context.Context) (context.Context, context.CancelFunc) {
	child, cancel := context.WithCancel(ctx)

	h.interruptMu.Lock()
	h.interruptGen++
	gen := h.interruptGen
	h.interrupt = cancel
	h.interruptMu.Unlock()

	return child, func() {
		cancel()
		h.interruptMu.Lock()
		defer h.interruptMu.Unlock()
		if h.interruptGen == gen {
			h.interrupt = nil
		}
	}
}

func (h *RunHandle) interruptCurrent() {
	h.interruptMu.Lock()
	defer h.interruptMu.Unlock()
	if h.interrupt != nil {
		h.interrupt()
	}
}
