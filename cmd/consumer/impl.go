package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"ai-orchestration/internal/agent"
	"ai-orchestration/internal/events"
	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/gemini"
	"ai-orchestration/internal/gitops"
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/pipeline"
	pipelineimpl "ai-orchestration/internal/pipeline/impl"
	"ai-orchestration/internal/pipeline/state"
	"ai-orchestration/internal/queue"
	"ai-orchestration/internal/runstore"
	"ai-orchestration/internal/testrunner"
)

type implDeps struct {
	consumerDeps
	prWriter pipelineimpl.PRDescWriter
}

func runImplConsumer(ctx context.Context, deps consumerDeps) {
	claudePW := agent.NewPRDescWriter(deps.cfg.claudeTimeout)
	var prWriter pipelineimpl.PRDescWriter = claudePW
	if deps.cfg.geminiAPIKey != nil {
		pw, err := gemini.NewPRDescWriter(ctx, *deps.cfg.geminiAPIKey)
		if err != nil {
			slog.ErrorContext(ctx, "gemini pr desc writer init failed, using claude only",
				"error", err,
			)
		} else {
			defer pw.Close()
			prWriter = &fallbackPRWriter{primary: pw, fallback: claudePW}
		}
	}

	id := implDeps{consumerDeps: deps, prWriter: prWriter}

	slog.InfoContext(ctx, "impl consumer started")
	for {
		if ctx.Err() != nil {
			slog.InfoContext(ctx, "impl consumer shutting down")
			return
		}
		raw, err := deps.rq.Pop(ctx, queue.QueueImpl, 10*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.ErrorContext(ctx, "pop impl job failed",
				"error", err,
			)
			continue
		}
		if raw == nil {
			continue
		}

		var job models.ImplJob
		if err := json.Unmarshal(raw, &job); err != nil {
			slog.ErrorContext(ctx, "unmarshal impl job failed",
				"error", err,
			)
			continue
		}
		handleImplJob(ctx, id, job)
	}
}

func handleImplJob(ctx context.Context, deps implDeps, job models.ImplJob) {
	proj, ok := deps.cfg.projects[projectKey(job.Project)]
	if !ok {
		slog.ErrorContext(ctx, "unknown project in impl job",
			"project", job.Project,
			"issue", job.IssueNumber,
		)
		return
	}

	code, err := splitRepo(proj.Repo)
	if err != nil {
		slog.ErrorContext(ctx, "split code repo failed",
			"repo", proj.Repo,
			"error", err,
		)
		return
	}

	handle := runstore.NewRunHandle(ctx, deps.store, deps.ctrl, runstore.Run{
		Service:     runstore.ServiceImpl,
		RepoOwner:   job.RepoOwner,
		RepoName:    job.RepoName,
		IssueNumber: job.IssueNumber,
		Title:       job.IssueTitle,
		Project:     job.Project,
		Phase:       runstore.PhaseImplementing,
	})
	defer handle.Cancel()

	projectPath := proj.Path(deps.cfg.projectsRoot)
	steps := []pipeline.Processor[state.Impl]{
		pipelineimpl.HydrateIssue{},
		pipelineimpl.FetchComments{Fj: deps.fj},
		pipelineimpl.MarkCommentsSeen{},
		pipelineimpl.PrepareBranch{
			GitFactory:   gitops.DefaultFactory,
			ForgejoURL:   deps.cfg.forgejoURL,
			ForgejoToken: deps.cfg.forgejoToken,
			RepoOwner:    code.Owner,
			RepoName:     code.Name,
			ProjectPath:  projectPath,
			BaseBranch:   proj.BaseBranch,
		},
		pipeline.LoopProcessor[state.Impl]{
			Max:   pipelineimpl.MaxAttempts,
			Until: func(s *state.Impl) bool { return s.TestError == nil && s.Attempt > 0 },
			Body: []pipeline.Processor[state.Impl]{
				pipelineimpl.BeginAttempt{},
				pipelineimpl.RefreshFreshComments{Fj: deps.fj},
				pipelineimpl.ImplementAttempt{Claude: deps.claude, ProjectPath: projectPath},
				pipelineimpl.CheckHasChanges{},
				pipelineimpl.RunTests{Runner: testrunner.Run},
			},
		},
		pipelineimpl.OpenPR{
			Fj:         deps.fj,
			Rq:         deps.rq,
			PrWriter:   deps.prWriter,
			ForgejoURL: deps.cfg.forgejoURL,
			CodeOwner:  code.Owner,
			CodeName:   code.Name,
			BaseBranch: proj.BaseBranch,
		},
	}

	s := state.Impl{Job: job, Proj: proj, Handle: handle}
	err = pipeline.Execute(handle.Ctx(), &s, steps)
	key := fmt.Sprintf("%s/%s#%d", job.RepoOwner, job.RepoName, job.IssueNumber)

	switch {
	case errors.Is(err, pipeline.ErrLoopMax):
		slog.ErrorContext(ctx, "impl max attempts reached, last test error unresolved",
			"issue", job.IssueNumber,
			"attempts", s.Attempt,
		)
		handle.Finalize(ctx, runstore.PhaseFailed, err)
		clearAndReturn(ctx, deps.rq, key)
	case errors.Is(err, pipeline.ErrNoChanges):
		slog.WarnContext(ctx, "no changes produced by impl",
			"issue", job.IssueNumber,
		)
		handle.Finalize(ctx, runstore.PhaseFailed, err)
		clearAndReturn(ctx, deps.rq, key)
	case errors.Is(err, context.Canceled):
		handle.Finalize(ctx, runstore.PhaseCancelled, nil)
		clearAndReturn(ctx, deps.rq, key)
	case err != nil:
		slog.ErrorContext(ctx, "impl pipeline failed",
			"issue", job.IssueNumber,
			"error", err,
		)
		handle.Finalize(ctx, runstore.PhaseFailed, err)
		clearAndReturn(ctx, deps.rq, key)
	default:
		handle.Finalize(ctx, runstore.PhaseDone, nil)
		if err := deps.rq.MarkProcessed(ctx, key); err != nil {
			slog.WarnContext(ctx, "mark processed failed",
				"key", key,
				"error", err,
			)
		}
		if err := deps.rq.ClearInProgress(ctx, key); err != nil {
			slog.WarnContext(ctx, "clear in-progress failed",
				"key", key,
				"error", err,
			)
		}
	}
}

func clearAndReturn(ctx context.Context, rq queue.Queue, key string) {
	if err := rq.ClearInProgress(ctx, key); err != nil {
		slog.WarnContext(ctx, "clear in-progress failed",
			"key", key,
			"error", err,
		)
	}
}

type fallbackPRWriter struct {
	primary  pipelineimpl.PRDescWriter
	fallback pipelineimpl.PRDescWriter
}

func (f *fallbackPRWriter) Generate(ctx context.Context, issue models.ForgejoIssue, projectPath, diff string, sink events.Sink) (string, error) {
	desc, err := f.primary.Generate(ctx, issue, projectPath, diff, sink)
	if err == nil {
		return desc, nil
	}
	if f.fallback == nil {
		return "", err
	}
	slog.WarnContext(ctx, "primary pr writer failed, using fallback",
		"error", err,
	)
	return f.fallback.Generate(ctx, issue, projectPath, diff, sink)
}

type repoRef struct {
	Owner string
	Name  string
}

func splitRepo(repo string) (repoRef, error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return repoRef{}, fmt.Errorf("invalid repo %q", repo)
	}
	return repoRef{Owner: parts[0], Name: parts[1]}, nil
}

var _ = forgejo.LabelImplement
