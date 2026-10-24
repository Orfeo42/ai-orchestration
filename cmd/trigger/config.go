package main

import (
	"fmt"
	"log/slog"
	"time"

	"ai-orchestration/internal/projects"
	"ai-orchestration/internal/runtime"
)

type config struct {
	forgejoURL   string
	forgejoToken string
	databaseURL  string
	workflowsDir string
	pollInterval time.Duration
	projects     map[string]projects.Config
}

func loadConfig() (config, error) {
	cfg := config{
		forgejoURL:   runtime.MustEnv("FORGEJO_URL"),
		forgejoToken: runtime.MustEnv("FORGEJO_TOKEN"),
		databaseURL:  runtime.MustEnv("DATABASE_URL"),
		workflowsDir: runtime.EnvOr("WORKFLOWS_DIR", "/workflows"),
	}

	interval, err := time.ParseDuration(runtime.EnvOr("POLL_INTERVAL", "60s"))
	if err != nil {
		return config{}, fmt.Errorf("POLL_INTERVAL: %w", err)
	}
	cfg.pollInterval = interval

	projs, err := projects.Load(runtime.EnvOr("PROJECTS_CONFIG", "/config/projects.json"))
	if err != nil {
		return config{}, err
	}
	cfg.projects = projs

	for name, p := range projs {
		if p.Label != "" {
			continue
		}
		slog.Warn("project has no label and can never be triggered",
			slog.String("project", name),
		)
	}
	return cfg, nil
}
