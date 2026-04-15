package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type config struct {
	forgejoURL      string
	forgejoToken    string
	forgejoRepos    []string
	forgejoAssignee *string
	redisURL        string
	projects        map[string]projectConfig
	pollInterval    time.Duration
	claudeTimeout   time.Duration
	gitUserName     string
	gitUserEmail    string
	geminiAPIKey    *string
}

type projectConfig struct {
	Repo       string `json:"repo"`
	BaseBranch string `json:"base_branch"`
}

const (
	projectsRoot = "/projects"
	maxAttempts  = 3
)

func loadConfig() (config, error) {
	var cfg config
	cfg.forgejoURL = mustEnv("FORGEJO_URL")
	cfg.forgejoToken = mustEnv("FORGEJO_TOKEN")
	cfg.forgejoRepos = strings.Split(mustEnv("FORGEJO_ISSUE_REPOS"), ",")
	for i, r := range cfg.forgejoRepos {
		cfg.forgejoRepos[i] = strings.TrimSpace(r)
	}
	if v := os.Getenv("FORGEJO_ASSIGNEE"); v != "" {
		cfg.forgejoAssignee = &v
	}
	cfg.redisURL = envOr("REDIS_URL", "redis://redis:6379")
	cfg.gitUserName = envOr("GIT_USER_NAME", "AI Orchestrator")
	cfg.gitUserEmail = envOr("GIT_USER_EMAIL", "ai@orchestrator.local")
	if v := os.Getenv("GEMINI_API_KEY"); v != "" {
		cfg.geminiAPIKey = &v
	}

	pollSecs, err := strconv.Atoi(envOr("POLL_INTERVAL", "60"))
	if err != nil {
		return cfg, fmt.Errorf("POLL_INTERVAL: %w", err)
	}
	cfg.pollInterval = time.Duration(pollSecs) * time.Second

	claudeSecs, err := strconv.Atoi(envOr("CLAUDE_TIMEOUT", "3600"))
	if err != nil {
		return cfg, fmt.Errorf("CLAUDE_TIMEOUT: %w", err)
	}
	cfg.claudeTimeout = time.Duration(claudeSecs) * time.Second

	cfgPath := envOr("PROJECTS_CONFIG", "/config/projects.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return cfg, fmt.Errorf("read projects config: %w", err)
	}
	if err := json.Unmarshal(data, &cfg.projects); err != nil {
		return cfg, fmt.Errorf("parse projects config: %w", err)
	}
	return cfg, nil
}

func (p projectConfig) path() string {
	parts := strings.SplitN(p.Repo, "/", 2)
	if len(parts) == 2 {
		return projectsRoot + "/" + parts[1]
	}
	return projectsRoot + "/" + p.Repo
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
