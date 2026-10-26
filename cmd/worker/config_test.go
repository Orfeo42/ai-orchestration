package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orfeo42/ai-orchestration/internal/runtime"
)

func baseEnv(t *testing.T) {
	t.Helper()
	projectsFile := filepath.Join(t.TempDir(), "projects.json")
	require.NoError(t, os.WriteFile(projectsFile, []byte(`{}`), 0o600))
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("FORGEJO_URL", "http://forgejo")
	t.Setenv("FORGEJO_TOKEN", "tok")
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("CORE_PUBLIC_URL", "http://core:8080")
	t.Setenv("CALLBACK_SECRET", "secret")
	t.Setenv("PROJECTS_CONFIG", projectsFile)
	t.Setenv("WORKER_ID", "w1")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
}

func claudeEnv(t *testing.T) {
	t.Helper()
	promptFile := filepath.Join(t.TempDir(), "headless.md")
	require.NoError(t, os.WriteFile(promptFile, []byte("prompt"), 0o600))
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "oauth")
	t.Setenv("CLAUDE_SYSTEM_PROMPT_FILE", promptFile)
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("MEMORY_SERVER_URL", "http://memory")
	t.Setenv("MEMORY_TOKEN", "mem")
}

func TestLoadConfig(t *testing.T) {
	t.Run("default queue only skips claude requirements", func(t *testing.T) {
		baseEnv(t)
		cfg, err := loadConfig([]string{"default"})
		require.NoError(t, err)
		assert.Equal(t, "w1", cfg.workerID)
		assert.Equal(t, 4, cfg.defaultMaxWorkers)
		assert.Equal(t, 2*time.Hour, cfg.stepTimeout)
		assert.Equal(t, 3*time.Hour, cfg.rescueAfter)
		assert.Empty(t, cfg.memory.ServerURL)
	})

	t.Run("claude queue loads claude and memory config", func(t *testing.T) {
		baseEnv(t)
		claudeEnv(t)
		cfg, err := loadConfig([]string{"claude", "default"})
		require.NoError(t, err)
		assert.Equal(t, time.Hour, cfg.claude.Timeout)
		assert.Equal(t, "http://memory", cfg.memory.ServerURL)
	})

	t.Run("claude queue requires oauth token", func(t *testing.T) {
		baseEnv(t)
		claudeEnv(t)
		t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
		_, err := loadConfig([]string{"claude"})
		require.ErrorIs(t, err, errOAuthTokenMissing)
	})

	t.Run("claude queue requires system prompt file", func(t *testing.T) {
		baseEnv(t)
		claudeEnv(t)
		t.Setenv("CLAUDE_SYSTEM_PROMPT_FILE", filepath.Join(t.TempDir(), "missing.md"))
		_, err := loadConfig([]string{"claude"})
		require.ErrorContains(t, err, "CLAUDE_SYSTEM_PROMPT_FILE")
	})

	t.Run("anthropic api key refused", func(t *testing.T) {
		baseEnv(t)
		t.Setenv("ANTHROPIC_API_KEY", "sk")
		_, err := loadConfig([]string{"default"})
		require.ErrorIs(t, err, errAnthropicAPIKeySet)
	})

	t.Run("rescue not greater than timeout", func(t *testing.T) {
		baseEnv(t)
		t.Setenv("STEP_DEFAULT_TIMEOUT", "2h")
		t.Setenv("JOB_RESCUE_AFTER", "2h")
		_, err := loadConfig([]string{"default"})
		require.ErrorIs(t, err, errRescueTooShort)
	})

	t.Run("invalid max workers", func(t *testing.T) {
		baseEnv(t)
		t.Setenv("DEFAULT_QUEUE_MAX_WORKERS", "0")
		_, err := loadConfig([]string{"default"})
		require.ErrorIs(t, err, errInvalidMaxWorkers)
	})

	t.Run("plugin config loaded", func(t *testing.T) {
		baseEnv(t)
		t.Setenv("PLUGIN_REFRESH_INTERVAL", "45s")
		cfg, err := loadConfig([]string{"default"})
		require.NoError(t, err)
		assert.Equal(t, runtime.PluginConfig{PublicURL: "http://core:8080", CallbackSecret: "secret"}, cfg.plugins)
		assert.Equal(t, 45*time.Second, cfg.pluginRefresh)
	})

	t.Run("invalid plugin refresh interval", func(t *testing.T) {
		baseEnv(t)
		t.Setenv("PLUGIN_REFRESH_INTERVAL", "often")
		_, err := loadConfig([]string{"default"})
		require.ErrorContains(t, err, "PLUGIN_REFRESH_INTERVAL")
	})

	t.Run("invalid step timeout", func(t *testing.T) {
		baseEnv(t)
		t.Setenv("STEP_DEFAULT_TIMEOUT", "soon")
		_, err := loadConfig([]string{"default"})
		require.ErrorContains(t, err, "STEP_DEFAULT_TIMEOUT")
	})
}

func TestResolveWorkerID(t *testing.T) {
	t.Run("env wins", func(t *testing.T) {
		t.Setenv("WORKER_ID", "abc")
		got, err := resolveWorkerID()
		require.NoError(t, err)
		assert.Equal(t, "abc", got)
	})

	t.Run("falls back to hostname", func(t *testing.T) {
		t.Setenv("WORKER_ID", "")
		host, err := os.Hostname()
		require.NoError(t, err)
		got, err := resolveWorkerID()
		require.NoError(t, err)
		assert.Equal(t, host, got)
	})
}
