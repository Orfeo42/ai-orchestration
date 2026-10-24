package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/Orfeo42/ai-orchestration/internal/agent"
	"github.com/Orfeo42/ai-orchestration/internal/memory"
	"github.com/Orfeo42/ai-orchestration/internal/projects"
	"github.com/Orfeo42/ai-orchestration/internal/runtime"
	"github.com/Orfeo42/ai-orchestration/internal/step"
)

type configError string

func (e configError) Error() string { return string(e) }

const (
	errAnthropicAPIKeySet configError = "ANTHROPIC_API_KEY is set: it would bill API credits instead of the subscription; unset it"
	errOAuthTokenMissing  configError = "CLAUDE_CODE_OAUTH_TOKEN is required for the claude queue"
	errRescueTooShort     configError = "JOB_RESCUE_AFTER must be greater than STEP_DEFAULT_TIMEOUT"
	errInvalidMaxWorkers  configError = "DEFAULT_QUEUE_MAX_WORKERS must be a positive integer"
	errEmptyHostname      configError = "hostname is empty"
)

type config struct {
	queues            []string
	forgejoURL        string
	forgejoToken      string
	databaseURL       string
	redisURL          string
	projects          map[string]projects.Config
	projectsRoot      string
	workerID          string
	gitUserName       string
	gitUserEmail      string
	geminiAPIKey      *string
	claude            agent.Runtime
	memory            memory.Config
	defaultMaxWorkers int
	stepTimeout       time.Duration
	rescueAfter       time.Duration
}

func loadConfig(queues []string) (config, error) {
	cfg := config{queues: queues}
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		return cfg, errAnthropicAPIKeySet
	}
	cfg.forgejoURL = runtime.MustEnv("FORGEJO_URL")
	cfg.forgejoToken = runtime.MustEnv("FORGEJO_TOKEN")
	cfg.databaseURL = runtime.MustEnv("DATABASE_URL")
	cfg.redisURL = runtime.EnvOr("REDIS_URL", "redis://redis:6379")
	cfg.projectsRoot = runtime.EnvOr("PROJECTS_ROOT", projects.DefaultRoot)
	cfg.gitUserName = runtime.EnvOr("GIT_USER_NAME", "AI Orchestrator")
	cfg.gitUserEmail = runtime.EnvOr("GIT_USER_EMAIL", "ai@orchestrator.local")
	if v := os.Getenv("GEMINI_API_KEY"); v != "" {
		cfg.geminiAPIKey = &v
	}

	workerID, err := resolveWorkerID()
	if err != nil {
		return cfg, fmt.Errorf("resolve worker id: %w", err)
	}
	cfg.workerID = workerID

	projs, err := projects.Load(runtime.EnvOr("PROJECTS_CONFIG", "/config/projects.json"))
	if err != nil {
		return cfg, err
	}
	cfg.projects = projs

	if err := loadTiming(&cfg); err != nil {
		return cfg, err
	}
	if !slices.Contains(queues, step.QueueClaude) {
		return cfg, nil
	}
	return cfg, loadClaude(&cfg)
}

func loadTiming(cfg *config) error {
	maxWorkers, err := strconv.Atoi(runtime.EnvOr("DEFAULT_QUEUE_MAX_WORKERS", "4"))
	if err != nil || maxWorkers < 1 {
		return errInvalidMaxWorkers
	}
	cfg.defaultMaxWorkers = maxWorkers

	stepTimeout, err := time.ParseDuration(runtime.EnvOr("STEP_DEFAULT_TIMEOUT", "2h"))
	if err != nil {
		return fmt.Errorf("STEP_DEFAULT_TIMEOUT: %w", err)
	}
	rescueAfter, err := time.ParseDuration(runtime.EnvOr("JOB_RESCUE_AFTER", "3h"))
	if err != nil {
		return fmt.Errorf("JOB_RESCUE_AFTER: %w", err)
	}
	if rescueAfter <= stepTimeout {
		return errRescueTooShort
	}
	cfg.stepTimeout = stepTimeout
	cfg.rescueAfter = rescueAfter
	return nil
}

func loadClaude(cfg *config) error {
	if os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") == "" {
		return errOAuthTokenMissing
	}
	claudeSecs, err := strconv.Atoi(runtime.EnvOr("CLAUDE_TIMEOUT", "3600"))
	if err != nil {
		return fmt.Errorf("CLAUDE_TIMEOUT: %w", err)
	}
	cfg.claude.Timeout = time.Duration(claudeSecs) * time.Second
	cfg.claude.SystemPromptFile = runtime.EnvOr("CLAUDE_SYSTEM_PROMPT_FILE", "/opt/orchestrator/claude/headless.md")
	if _, err := os.Stat(cfg.claude.SystemPromptFile); err != nil {
		return fmt.Errorf("CLAUDE_SYSTEM_PROMPT_FILE: %w", err)
	}

	claudeDir := os.Getenv("CLAUDE_CONFIG_DIR")
	if claudeDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("user home dir: %w", err)
		}
		claudeDir = filepath.Join(home, ".claude")
	}
	cfg.memory = memory.Config{
		ServerURL: runtime.MustEnv("MEMORY_SERVER_URL"),
		Token:     runtime.MustEnv("MEMORY_TOKEN"),
		StateDir:  runtime.EnvOr("MEMORY_STATE_DIR", "/state/memory"),
		ClaudeDir: claudeDir,
	}
	for _, p := range cfg.projects {
		cfg.memory.Projects = append(cfg.memory.Projects, memory.Project{
			Key:  p.ResolvedMemoryKey(),
			Path: p.Path(cfg.projectsRoot, cfg.workerID),
		})
	}
	return nil
}

func resolveWorkerID() (string, error) {
	if v := os.Getenv("WORKER_ID"); v != "" {
		return v, nil
	}
	host, err := os.Hostname()
	if err != nil {
		return "", err
	}
	if host == "" {
		return "", errEmptyHostname
	}
	return host, nil
}
