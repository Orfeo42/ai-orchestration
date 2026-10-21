package runstore

import (
	"context"

	"ai-orchestration/internal/events"
)

type persister interface {
	setPhase(ctx context.Context, phase Phase) error
	addUsage(ctx context.Context, u events.TokenUsage) error
	setSession(ctx context.Context, sessionID string) error
	setAttempt(ctx context.Context, attempt int) error
	finalize(ctx context.Context, phase Phase, errStr string) error
}

type nopPersister struct{}

func (nopPersister) setPhase(context.Context, Phase) error             { return nil }
func (nopPersister) addUsage(context.Context, events.TokenUsage) error { return nil }
func (nopPersister) setSession(context.Context, string) error          { return nil }
func (nopPersister) setAttempt(context.Context, int) error             { return nil }
func (nopPersister) finalize(context.Context, Phase, string) error     { return nil }

type stepPersister struct {
	steps StepRunWriter
	id    string
}

func (p stepPersister) setPhase(ctx context.Context, phase Phase) error {
	return p.steps.SetStepPhase(ctx, p.id, string(phase))
}

func (p stepPersister) addUsage(ctx context.Context, u events.TokenUsage) error {
	return p.steps.AddStepUsage(ctx, p.id, u)
}

func (p stepPersister) setSession(ctx context.Context, sessionID string) error {
	return p.steps.SetStepSession(ctx, p.id, sessionID)
}

func (p stepPersister) setAttempt(ctx context.Context, attempt int) error {
	return p.steps.SetStepIteration(ctx, p.id, attempt)
}

func (stepPersister) finalize(context.Context, Phase, string) error { return nil }
