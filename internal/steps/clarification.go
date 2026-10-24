package steps

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Orfeo42/ai-orchestration/internal/forgejo"
	"github.com/Orfeo42/ai-orchestration/internal/process"
	"github.com/Orfeo42/ai-orchestration/internal/step"
)

const OutcomeAnswered step.Outcome = "answered"

type clarificationStep struct {
	d Deps
}

func (c clarificationStep) Run(ctx context.Context, sc step.Context) (step.Result, error) {
	issue, err := loadIssue(sc)
	if err != nil {
		return step.Result{}, err
	}
	questions, ok, err := process.Get(sc.Process.Vars, KeyQuestions)
	if err != nil {
		return step.Result{}, err
	}
	if !ok {
		return step.Result{}, fmt.Errorf("%w: %s", ErrMissingVar, KeyQuestions)
	}
	body := "Requirements unclear, need clarification before implementation:\n\n" + questions
	if err := c.d.Forgejo.CreateComment(issue.RepoOwner, issue.RepoName, issue.Number, body); err != nil {
		return step.Result{}, fmt.Errorf("%w: create clarification comment: %w", step.ErrUpstream, err)
	}
	labelID, err := c.d.Forgejo.GetLabelID(issue.RepoOwner, issue.RepoName, forgejo.LabelNeedsInfo)
	if err == nil && labelID != nil {
		if err := c.d.Forgejo.AddLabel(issue.RepoOwner, issue.RepoName, issue.Number, *labelID); err != nil {
			slog.WarnContext(ctx, "add needs-info label failed",
				"error", err,
			)
		}
	}
	return step.Result{Wait: &step.WaitRequest{Kind: process.WaitComment, ResumeOutcome: OutcomeAnswered}}, nil
}
