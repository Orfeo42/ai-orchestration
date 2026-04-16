package main

import (
	"log/slog"
	"strings"
	"time"

	"ai-orchestration/internal/agent"
	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/gemini"
	"ai-orchestration/internal/gitops"
	"ai-orchestration/internal/runtime"
	"ai-orchestration/internal/testrunner"
)

func main() {
	runtime.InitLogger(slog.LevelInfo)

	cfg, err := loadConfig()
	if err != nil {
		runtime.Die("config", err)
	}

	if err := runtime.ConfigureGit(cfg.gitUserName, cfg.gitUserEmail); err != nil {
		runtime.Die("git-config", err)
	}

	ctx, stop := runtime.ShutdownContext()
	defer stop()

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

	fj := forgejo.New(cfg.forgejoURL, cfg.forgejoToken)
	claude := agent.New(cfg.claudeTimeout)

	deps := orchDeps{
		fj:         fj,
		rq:         rq,
		claude:     claude,
		gitFactory: gitops.DefaultFactory,
		runTests:   testrunner.Run,
		store:      store,
		ctrl:       ctrl,
	}

	claudePW := agent.NewPRDescWriter(cfg.claudeTimeout)
	if cfg.geminiAPIKey != nil {
		pw, err := gemini.NewPRDescWriter(ctx, *cfg.geminiAPIKey)
		if err != nil {
			runtime.Die("pr-desc-writer", err)
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
		if ctx.Err() != nil {
			slog.Info("orchestrator shutting down")
			return
		}
		for _, repoRef := range cfg.forgejoRepos {
			if ctx.Err() != nil {
				return
			}
			parts := strings.SplitN(repoRef, "/", 2)
			if len(parts) != 2 {
				slog.Error("invalid repo ref",
					"ref", repoRef,
				)
				continue
			}
			pollRepo(ctx, parts[0], parts[1], deps, cfg)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(cfg.pollInterval):
		}
	}
}

