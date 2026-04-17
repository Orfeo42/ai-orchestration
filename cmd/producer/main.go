package main

import (
	"log/slog"
	"strings"
	"time"

	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/runtime"
)

func main() {
	runtime.InitLogger(slog.LevelInfo)

	cfg, err := loadConfig()
	if err != nil {
		runtime.Die("config", err)
	}

	ctx, stop := runtime.ShutdownContext()
	defer stop()

	rq, err := runtime.NewQueue(cfg.redisURL)
	if err != nil {
		runtime.Die("queue", err)
	}
	defer runtime.CloseAll("queue", rq)

	fj := forgejo.New(cfg.forgejoURL, cfg.forgejoToken)

	deps := producerDeps{fj: fj, rq: rq}

	slog.Info("producer started",
		"poll_interval", cfg.pollInterval,
		"repos", len(cfg.forgejoRepos),
	)

	for {
		if ctx.Err() != nil {
			slog.Info("producer shutting down")
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
