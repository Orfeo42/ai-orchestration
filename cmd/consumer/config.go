package main

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"ai-orchestration/internal/projects"
)

type config struct {
	forgejoURL    string
	forgejoToken  string
	redisURL      string
	projects      map[string]projects.Config
	projectsRoot  string
	claudeTimeout time.Duration
	gitUserName   string
	gitUserEmail  string
	geminiAPIKey  *string
}

func loadConfig() (config, error) {
	var cfg config
	cfg.forgejoURL = mustEnv("FORGEJO_URL")
	cfg.forgejoToken = mustEnv("FORGEJO_TOKEN")
	cfg.redisURL = envOr("REDIS_URL", "redis://redis:6379")
	cfg.projectsRoot = envOr("PROJECTS_ROOT", projects.DefaultRoot)
	cfg.gitUserName = envOr("GIT_USER_NAME", "AI Orchestrator")
	cfg.gitUserEmail = envOr("GIT_USER_EMAIL", "ai@orchestrator.local")
	if v := os.Getenv("GEMINI_API_KEY"); v != "" {
		cfg.geminiAPIKey = &v
	}

	claudeSecs, err := strconv.Atoi(envOr("CLAUDE_TIMEOUT", "3600"))
	if err != nil {
		return cfg, fmt.Errorf("CLAUDE_TIMEOUT: %w", err)
	}
	cfg.claudeTimeout = time.Duration(claudeSecs) * time.Second

	cfgPath := envOr("PROJECTS_CONFIG", "/config/projects.json")
	projs, err := projects.Load(cfgPath)
	if err != nil {
		return cfg, err
	}
	cfg.projects = projs
	return cfg, nil
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
