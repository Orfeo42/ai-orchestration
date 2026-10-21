package main

import (
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"ai-orchestration/internal/engine"
	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/runtime"
	"ai-orchestration/internal/step"
	"ai-orchestration/internal/steps"
	"ai-orchestration/internal/trigger"
)

func main() {
	runtime.InitLogger(slog.LevelInfo)

	cfg, err := loadConfig()
	if err != nil {
		runtime.Die("config", err)
	}

	ctx, stop := runtime.ShutdownContext()
	defer stop()

	fj := forgejo.New(cfg.forgejoURL, cfg.forgejoToken)
	bot, err := fj.CurrentUser()
	if err != nil {
		runtime.Die("forgejo current user", err)
	}

	reg := step.NewRegistry()
	if err := steps.RegisterAll(reg, steps.Deps{}); err != nil {
		runtime.Die("register steps", err)
	}

	p, err := runtime.OpenPlatform(ctx, runtime.PlatformConfig{
		DSN:      cfg.databaseURL,
		Registry: reg,
		Control:  nil,
		Engine:   engine.Config{WorkerID: "trigger"},
		River:    &river.Config{},
	})
	if err != nil {
		runtime.Die("platform", err)
	}
	defer runtime.CloseAll("platform", p)

	svc := trigger.New(fj, p.Store, p.Engine, reg, trigger.Config{
		ForgejoURL: cfg.forgejoURL,
		Projects:   cfg.projects,
		BotUser:    bot,
	})

	if err := svc.SyncWorkflows(ctx, cfg.workflowsDir); err != nil {
		runtime.Die("sync workflows", err)
	}

	slog.InfoContext(ctx, "trigger started",
		slog.Duration("poll_interval", cfg.pollInterval),
		slog.String("bot_user", bot),
		slog.Int("projects", len(cfg.projects)),
	)

	ticker := time.NewTicker(cfg.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.InfoContext(ctx, "trigger shutting down")
			return
		case <-ticker.C:
		}
		if err := svc.SyncWorkflows(ctx, cfg.workflowsDir); err != nil {
			slog.ErrorContext(ctx, "sync workflows failed", slog.Any("error", err))
		}
		if err := svc.Poll(ctx); err != nil {
			slog.ErrorContext(ctx, "poll failed", slog.Any("error", err))
		}
		if err := svc.ResumeWaiting(ctx); err != nil {
			slog.ErrorContext(ctx, "resume waiting failed", slog.Any("error", err))
		}
	}
}
