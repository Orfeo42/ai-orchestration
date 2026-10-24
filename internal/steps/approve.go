package steps

import (
	"context"

	"ai-orchestration/internal/process"
	"ai-orchestration/internal/step"
)

const (
	OutcomeApproved step.Outcome = "approved"
	OutcomeRejected step.Outcome = "rejected"
)

type approveStep struct{}

func (approveStep) Run(_ context.Context, _ step.Context) (step.Result, error) {
	return step.Result{Wait: &step.WaitRequest{Kind: process.WaitSignal}}, nil
}
