package steps

import (
	"context"

	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/internal/step"
)

const (
	OutcomeApproved step.Outcome = "approved"
	OutcomeRejected step.Outcome = "rejected"
)

type approveStep struct{}

func (approveStep) Run(_ context.Context, _ step.Context) (step.Result, error) {
	return step.Result{Wait: &step.WaitRequest{Kind: process.WaitSignal}}, nil
}
