package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"ai-orchestration/internal/agent"
	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/gitops"
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/queue"
	"ai-orchestration/internal/testrunner"
)

var (
	errNoChanges    = errors.New("no changes produced")
	errIssueUnclear = errors.New("issue requirements unclear")
)

type prDescWriter interface {
	Generate(ctx context.Context, issue models.ForgejoIssue, projectPath, diff string) (string, error)
}

type fallbackPRWriter struct {
	primary  prDescWriter
	fallback prDescWriter
}

func (f *fallbackPRWriter) Generate(ctx context.Context, issue models.ForgejoIssue, projectPath, diff string) (string, error) {
	desc, err := f.primary.Generate(ctx, issue, projectPath, diff)
	if err == nil {
		return desc, nil
	}
	if f.fallback == nil {
		return "", err
	}
	slog.WarnContext(ctx, "primary pr writer failed, using fallback",
		"error", err,
	)
	return f.fallback.Generate(ctx, issue, projectPath, diff)
}

type orchDeps struct {
	fj         forgejo.Forgejo
	rq         queue.Queue
	claude     agent.Agent
	prWriter   prDescWriter
	gitFactory gitops.Factory
	runTests   testrunner.Runner
}

func pollRepo(ctx context.Context, owner, repo string, deps orchDeps, cfg config) {
	issues, err := deps.fj.GetIssues(owner, repo, forgejo.LabelImplement, cfg.forgejoAssignee)
	if err != nil {
		slog.ErrorContext(ctx, "fetch issues failed",
			"repo", owner+"/"+repo,
			"error", err,
		)
		return
	}

	for _, issue := range issues {
		key := fmt.Sprintf("%s/%s#%d", owner, repo, issue.Number)

		processed, err := deps.rq.IsProcessed(ctx, key)
		if err != nil {
			slog.ErrorContext(ctx, "check processed failed",
				"key", key,
				"error", err,
			)
			continue
		}
		if processed || containsLabel(issue.Labels, forgejo.LabelDone) {
			continue
		}

		projectName := projectLabel(issue.Labels)
		if projectName == "" {
			slog.WarnContext(ctx, "issue has no project label, skipping",
				"issue", issue.Number,
			)
			continue
		}
		proj, ok := cfg.projects[projectName]
		if !ok {
			slog.ErrorContext(ctx, "unknown project",
				"project", projectName,
				"issue", issue.Number,
			)
			continue
		}

		if err := deps.rq.MarkInProgress(ctx, key, 2*time.Hour); err != nil {
			slog.ErrorContext(ctx, "mark in-progress failed",
				"key", key,
				"error", err,
			)
			continue
		}

		startIssue(ctx, deps.fj, owner, repo, issue)

		slog.InfoContext(ctx, "processing issue",
			"issue", issue.Number,
			"project", proj.Repo,
		)
		procErr := processIssue(ctx, issue, proj, owner, repo, deps, cfg)
		if procErr != nil && !errors.Is(procErr, errIssueUnclear) {
			slog.ErrorContext(ctx, "issue processing failed",
				"issue", issue.Number,
				"error", procErr,
			)
			if clearErr := deps.rq.ClearInProgress(ctx, key); clearErr != nil {
				slog.ErrorContext(ctx, "clear in-progress failed",
					"key", key,
					"error", clearErr,
				)
			}
			if labelID, lerr := deps.fj.GetLabelID(owner, repo, forgejo.LabelInProgress); lerr == nil && labelID != nil {
				if rerr := deps.fj.RemoveLabel(owner, repo, issue.Number, *labelID); rerr != nil {
					slog.WarnContext(ctx, "remove in-progress label failed",
						"error", rerr,
					)
				}
			}
			continue
		}

		if err := deps.rq.ClearInProgress(ctx, key); err != nil {
			slog.WarnContext(ctx, "clear in-progress failed",
				"key", key,
				"error", err,
			)
		}
		if err := deps.rq.MarkProcessed(ctx, key); err != nil {
			slog.WarnContext(ctx, "mark processed failed",
				"key", key,
				"error", err,
			)
		}
	}
}

func startIssue(ctx context.Context, fj forgejo.Forgejo, owner, repo string, issue models.ForgejoIssue) {
	if labelID, err := fj.GetLabelID(owner, repo, forgejo.LabelInProgress); err == nil && labelID != nil {
		if err := fj.AddLabel(owner, repo, issue.Number, *labelID); err != nil {
			slog.WarnContext(ctx, "add in-progress label failed",
				"error", err,
			)
		}
	}
}

func processIssue(
	ctx context.Context,
	issue models.ForgejoIssue,
	proj projectConfig,
	issueOwner, issueRepo string,
	deps orchDeps,
	cfg config,
) error {
	codeOwner, codeName, err := splitRepo(proj.Repo)
	if err != nil {
		return err
	}

	assess, err := deps.claude.AssessIssue(issue, proj.path())
	if err != nil {
		return fmt.Errorf("assess issue: %w", err)
	}
	if !assess.Clear {
		return handleUnclearIssue(ctx, issue, issueOwner, issueRepo, assess.Questions, deps)
	}

	branch := fmt.Sprintf("feature/%d-%s", issue.Number, branchSlug(issue.Title))
	var testError *string

	for attempt := range maxAttempts {
		git := deps.gitFactory(proj.path(), cfg.forgejoURL, cfg.forgejoToken, codeOwner, codeName)

		if attempt == 0 {
			if err := git.Checkout(proj.BaseBranch); err != nil {
				return fmt.Errorf("checkout base: %w", err)
			}
			if err := git.CreateBranch(branch); err != nil {
				return fmt.Errorf("create branch: %w", err)
			}
		} else {
			slog.InfoContext(ctx, "retry impl",
				"attempt", attempt+1,
				"issue", issue.Number,
			)
		}

		resume := attempt == 0 && containsLabel(issue.Labels, forgejo.LabelInProgress)
		if _, err := deps.claude.ImplementIssue(issue, proj.path(), testError, resume); err != nil {
			return fmt.Errorf("claude impl: %w", err)
		}

		changed, err := git.HasChanges()
		if err != nil {
			return fmt.Errorf("check changes: %w", err)
		}
		if !changed {
			slog.WarnContext(ctx, "no changes after impl",
				"issue", issue.Number,
			)
			return errNoChanges
		}

		ok, tErr := deps.runTests(proj.path())
		if ok {
			break
		}

		truncated := tErr
		if len(truncated) > 200 {
			truncated = truncated[:200]
		}
		slog.WarnContext(ctx, "tests failed",
			"attempt", attempt+1,
			"issue", issue.Number,
			"error", truncated,
		)
		testError = &tErr

		if attempt+1 >= maxAttempts {
			return fmt.Errorf("max impl attempts reached, last test error: %s", tErr)
		}
	}

	return createPRAndQueue(ctx, issue, proj, branch, issueOwner, issueRepo, codeOwner, codeName, deps, cfg)
}

func handleUnclearIssue(
	ctx context.Context,
	issue models.ForgejoIssue,
	issueOwner, issueRepo, questions string,
	deps orchDeps,
) error {
	slog.InfoContext(ctx, "issue requirements unclear, requesting clarification",
		"issue", issue.Number,
	)

	body := "Requirements unclear, need clarification before implementation:\n\n" + questions
	if err := deps.fj.CreateComment(issueOwner, issueRepo, issue.Number, body); err != nil {
		return fmt.Errorf("create clarification comment: %w", err)
	}

	if labelID, err := deps.fj.GetLabelID(issueOwner, issueRepo, forgejo.LabelNeedsInfo); err == nil && labelID != nil {
		if err := deps.fj.AddLabel(issueOwner, issueRepo, issue.Number, *labelID); err != nil {
			slog.WarnContext(ctx, "add needs-info label failed",
				"error", err,
			)
		}
	} else if err != nil {
		slog.WarnContext(ctx, "lookup needs-info label failed",
			"error", err,
		)
	}

	if labelID, err := deps.fj.GetLabelID(issueOwner, issueRepo, forgejo.LabelInProgress); err == nil && labelID != nil {
		if err := deps.fj.RemoveLabel(issueOwner, issueRepo, issue.Number, *labelID); err != nil {
			slog.WarnContext(ctx, "remove in-progress label failed",
				"error", err,
			)
		}
	}

	return errIssueUnclear
}

func createPRAndQueue(
	ctx context.Context,
	issue models.ForgejoIssue,
	proj projectConfig,
	branch, issueOwner, issueRepo, codeOwner, codeName string,
	deps orchDeps,
	cfg config,
) error {
	git := deps.gitFactory(proj.path(), cfg.forgejoURL, cfg.forgejoToken, codeOwner, codeName)

	if err := git.AddAll(); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if err := git.Commit(fmt.Sprintf("feat: implement issue #%d - %s", issue.Number, issue.Title)); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	if err := git.Push(branch); err != nil {
		return fmt.Errorf("git push: %w", err)
	}

	prBody := fmt.Sprintf("Implements [%s/%s#%d](%s/%s/%s/issues/%d)",
		issueOwner, issueRepo, issue.Number,
		cfg.forgejoURL, issueOwner, issueRepo, issue.Number)

	pr, err := deps.fj.CreatePR(codeOwner, codeName, "feat: "+issue.Title, prBody, branch, proj.BaseBranch)
	if err != nil {
		return fmt.Errorf("create pr: %w", err)
	}

	diff, err := deps.fj.GetPRDiff(codeOwner, codeName, pr.Number)
	if err != nil {
		slog.WarnContext(ctx, "get pr diff failed",
			"error", err,
		)
	}

	if deps.prWriter != nil && diff != "" {
		desc, err := deps.prWriter.Generate(ctx, issue, proj.path(), diff)
		if err != nil {
			slog.WarnContext(ctx, "pr description generation failed",
				"error", err,
			)
		} else {
			body := fmt.Sprintf("relates to: %s/%s#%d\n\n%s", issueOwner, issueRepo, issue.Number, desc)
			if err := deps.fj.UpdatePR(codeOwner, codeName, pr.Number, body); err != nil {
				slog.WarnContext(ctx, "update pr body failed",
					"error", err,
				)
			}
		}
	}

	if doneID, err := deps.fj.GetLabelID(issueOwner, issueRepo, forgejo.LabelDone); err == nil && doneID != nil {
		if err := deps.fj.AddLabel(issueOwner, issueRepo, issue.Number, *doneID); err != nil {
			slog.WarnContext(ctx, "add done label failed",
				"error", err,
			)
		}
	}
	if ipID, err := deps.fj.GetLabelID(issueOwner, issueRepo, forgejo.LabelInProgress); err == nil && ipID != nil {
		if err := deps.fj.RemoveLabel(issueOwner, issueRepo, issue.Number, *ipID); err != nil {
			slog.WarnContext(ctx, "remove in-progress label failed",
				"error", err,
			)
		}
	}

	/*if err := deps.fj.CreateComment(issueOwner, issueRepo, issue.Number,
		fmt.Sprintf("PR created: [%s/%s#%d](%s/%s/%s/pulls/%d)",
			codeOwner, codeName, pr.Number,
			cfg.forgejoURL, codeOwner, codeName, pr.Number)); err != nil {
		slog.WarnContext(ctx, "create pr comment failed",
			"error", err,
		)
	}*/

	job := models.ReviewJob{
		RepoOwner:   codeOwner,
		RepoName:    codeName,
		PRNumber:    pr.Number,
		PRTitle:     "feat: " + issue.Title,
		PRBranch:    branch,
		ProjectPath: proj.path(),
		IssueNumber: issue.Number,
		Diff:        diff,
	}
	if err := deps.rq.Push(ctx, "queue:review", job); err != nil {
		return fmt.Errorf("push review job: %w", err)
	}

	if err := git.Checkout(proj.BaseBranch); err != nil {
		slog.WarnContext(ctx, "checkout base after PR failed",
			"error", err,
		)
	}

	slog.InfoContext(ctx, "issue queued for review",
		"issue", issue.Number,
		"pr", fmt.Sprintf("%s/%s#%d", codeOwner, codeName, pr.Number),
	)
	return nil
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func branchSlug(title string) string {
	slug := slugRe.ReplaceAllString(strings.ToLower(title), "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 50 {
		slug = slug[:50]
	}
	return slug
}

func containsLabel(labels []string, target string) bool {
	for _, l := range labels {
		if l == target {
			return true
		}
	}
	return false
}

func projectLabel(labels []string) string {
	for _, l := range labels {
		if after, ok := strings.CutPrefix(l, forgejo.LabelProjectPrefix); ok {
			return after
		}
	}
	return ""
}

func splitRepo(repo string) (string, string, error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid repo %q", repo)
	}
	return parts[0], parts[1], nil
}
