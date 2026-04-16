package main

import (
	"encoding/json"
	"log/slog"
	"time"

	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/gemini"
	"ai-orchestration/internal/gitops"
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/runtime"
)

type config struct {
	geminiAPIKey string
	redisURL     string
	forgejoURL   string
	forgejoToken string
	gitUserName  string
	gitUserEmail string
}

func loadConfig() config {
	return config{
		geminiAPIKey: runtime.MustEnv("GEMINI_API_KEY"),
		redisURL:     runtime.EnvOr("REDIS_URL", "redis://redis:6379"),
		forgejoURL:   runtime.MustEnv("FORGEJO_URL"),
		forgejoToken: runtime.MustEnv("FORGEJO_TOKEN"),
		gitUserName:  runtime.EnvOr("GIT_USER_NAME", "AI Reviewer"),
		gitUserEmail: runtime.EnvOr("GIT_USER_EMAIL", "ai-reviewer@orchestrator.local"),
	}
}

func main() {
	runtime.InitLogger(slog.LevelDebug)

	cfg := loadConfig()

	if err := runtime.ConfigureGit(cfg.gitUserName, cfg.gitUserEmail); err != nil {
		runtime.Die("git-config", err)
	}

	ctx, stop := runtime.ShutdownContext()
	defer stop()

	reviewer, err := gemini.NewReviewer(ctx, cfg.geminiAPIKey)
	if err != nil {
		runtime.Die("reviewer", err)
	}
	defer reviewer.Close()

	docsWriter, err := gemini.NewDocsWriter(ctx, cfg.geminiAPIKey)
	if err != nil {
		runtime.Die("docs-writer", err)
	}
	defer docsWriter.Close()

	fj := forgejo.New(cfg.forgejoURL, cfg.forgejoToken)

	rq, err := runtime.NewQueue(cfg.redisURL)
	if err != nil {
		runtime.Die("queue", err)
	}
	defer runtime.CloseAll("queue", rq)

	store, err := runtime.NewStore(cfg.redisURL)
	if err != nil {
		runtime.Die("runstore", err)
	}
	defer runtime.CloseAll("runstore", store)

	ctrl, err := runtime.NewControl(cfg.redisURL)
	if err != nil {
		runtime.Die("control", err)
	}
	defer runtime.CloseAll("control", ctrl)

	deps := reviewDeps{
		reviewer:   reviewer,
		docsWriter: docsWriter,
		fj:         fj,
		gitFactory: gitops.DefaultFactory,
		store:      store,
		ctrl:       ctrl,
	}

	slog.Info("reviewer started")

	for {
		if ctx.Err() != nil {
			slog.Info("reviewer shutting down")
			return
		}
		raw, err := rq.Pop(ctx, "queue:review", 10*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("queue pop failed",
				"error", err,
			)
			continue
		}
		if raw == nil {
			continue
		}

		var job models.ReviewJob
		if err := json.Unmarshal(raw, &job); err != nil {
			slog.Error("unmarshal job failed",
				"error", err,
			)
			continue
		}

		slog.Info("reviewing PR",
			"pr", job.PRNumber,
			"repo", job.RepoOwner+"/"+job.RepoName,
		)
		if err := runPipeline(ctx, job, deps, cfg); err != nil {
			slog.Error("review failed",
				"pr", job.PRNumber,
				"error", err,
			)
		}
	}
}
