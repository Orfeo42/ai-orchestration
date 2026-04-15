package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/gemini"
	"ai-orchestration/internal/gitops"
	"ai-orchestration/internal/models"
	"ai-orchestration/internal/queue"
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
		geminiAPIKey: mustEnv("GEMINI_API_KEY"),
		redisURL:     envOr("REDIS_URL", "redis://redis:6379"),
		forgejoURL:   mustEnv("FORGEJO_URL"),
		forgejoToken: mustEnv("FORGEJO_TOKEN"),
		gitUserName:  envOr("GIT_USER_NAME", "AI Reviewer"),
		gitUserEmail: envOr("GIT_USER_EMAIL", "ai-reviewer@orchestrator.local"),
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("required env var missing",
			"key", key,
		)
		os.Exit(1)
	}
	return v
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func configureGit(name, email string) error {
	if err := exec.Command("git", "config", "--global", "user.name", name).Run(); err != nil {
		return fmt.Errorf("git config user.name: %w", err)
	}
	if err := exec.Command("git", "config", "--global", "user.email", email).Run(); err != nil {
		return fmt.Errorf("git config user.email: %w", err)
	}
	return nil
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))

	cfg := loadConfig()

	if err := configureGit(cfg.gitUserName, cfg.gitUserEmail); err != nil {
		slog.Error("configure git failed",
			"error", err,
		)
		os.Exit(1)
	}

	ctx := context.Background()

	reviewer, err := gemini.NewReviewer(ctx, cfg.geminiAPIKey)
	if err != nil {
		slog.Error("reviewer init failed",
			"error", err,
		)
		os.Exit(1)
	}
	defer reviewer.Close()

	docsWriter, err := gemini.NewDocsWriter(ctx, cfg.geminiAPIKey)
	if err != nil {
		slog.Error("docs writer init failed",
			"error", err,
		)
		os.Exit(1)
	}
	defer docsWriter.Close()

	fj := forgejo.New(cfg.forgejoURL, cfg.forgejoToken)

	rq, err := queue.New(cfg.redisURL)
	if err != nil {
		slog.Error("redis connect failed",
			"error", err,
		)
		os.Exit(1)
	}
	defer func() {
		if err := rq.Close(); err != nil {
			slog.Error("redis queue close failed",
				"error", err,
			)
		}
	}()

	deps := reviewDeps{
		reviewer:   reviewer,
		docsWriter: docsWriter,
		fj:         fj,
		gitFactory: gitops.DefaultFactory,
	}

	slog.Info("reviewer started")

	for {
		raw, err := rq.Pop(ctx, "queue:review", 10*time.Second)
		if err != nil {
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
