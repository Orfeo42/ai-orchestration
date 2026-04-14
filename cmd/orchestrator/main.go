package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"ai-orchestration/internal/agent"
	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/gemini"
	"ai-orchestration/internal/gitops"
	"ai-orchestration/internal/queue"
	"ai-orchestration/internal/testrunner"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := loadConfig()
	if err != nil {
		slog.Error("load config failed",
			"error", err,
		)
		os.Exit(1)
	}

	if err := configureGit(cfg.gitUserName, cfg.gitUserEmail); err != nil {
		slog.Error("configure git failed",
			"error", err,
		)
		os.Exit(1)
	}

	ctx := context.Background()

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

	claude := agent.New(cfg.claudeTimeout)

	deps := orchDeps{
		fj:         fj,
		rq:         rq,
		claude:     claude,
		gitFactory: gitops.DefaultFactory,
		runTests:   testrunner.Run,
	}

	claudePW := agent.NewPRDescWriter(cfg.claudeTimeout)
	if cfg.geminiAPIKey != nil {
		pw, err := gemini.NewPRDescWriter(ctx, *cfg.geminiAPIKey)
		if err != nil {
			slog.Error("pr desc writer init failed",
				"error", err,
			)
			os.Exit(1)
		}
		defer pw.Close()
		deps.prWriter = &fallbackPRWriter{primary: pw, fallback: claudePW}
	} else {
		deps.prWriter = claudePW
	}

	if err := syncAll(cfg.projects, cfg.forgejoURL, cfg.forgejoToken); err != nil {
		slog.Warn("initial sync had errors",
			"error", err,
		)
	}

	slog.Info("orchestrator started",
		"poll_interval", cfg.pollInterval,
	)

	for {
		for _, repoRef := range cfg.forgejoRepos {
			parts := strings.SplitN(repoRef, "/", 2)
			if len(parts) != 2 {
				slog.Error("invalid repo ref",
					"ref", repoRef,
				)
				continue
			}
			pollRepo(ctx, parts[0], parts[1], deps, cfg)
		}
		time.Sleep(cfg.pollInterval)
	}
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
