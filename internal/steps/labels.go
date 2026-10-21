package steps

import (
	"context"
	"log/slog"

	"ai-orchestration/internal/step"
)

type labelsParams struct {
	Add    []string `json:"add" validate:"omitempty,dive,required"`
	Remove []string `json:"remove" validate:"omitempty,dive,required"`
}

type labelsStep struct {
	d      Deps
	params labelsParams
}

func (l labelsStep) Run(ctx context.Context, sc step.Context) (step.Result, error) {
	issue, err := loadIssue(sc)
	if err != nil {
		return step.Result{}, err
	}
	fj := l.d.Forgejo
	for _, name := range l.params.Add {
		id, err := fj.GetLabelID(issue.RepoOwner, issue.RepoName, name)
		if err != nil {
			slog.WarnContext(ctx, "get label failed",
				"label", name,
				"error", err,
			)
			continue
		}
		if id == nil {
			continue
		}
		if err := fj.AddLabel(issue.RepoOwner, issue.RepoName, issue.Number, *id); err != nil {
			slog.WarnContext(ctx, "add label failed",
				"label", name,
				"error", err,
			)
		}
	}
	for _, name := range l.params.Remove {
		id, err := fj.GetLabelID(issue.RepoOwner, issue.RepoName, name)
		if err != nil {
			slog.WarnContext(ctx, "get label failed",
				"label", name,
				"error", err,
			)
			continue
		}
		if id == nil {
			continue
		}
		if err := fj.RemoveLabel(issue.RepoOwner, issue.RepoName, issue.Number, *id); err != nil {
			slog.WarnContext(ctx, "remove label failed",
				"label", name,
				"error", err,
			)
		}
	}
	return step.Result{Outcome: step.OutcomeOK}, nil
}
