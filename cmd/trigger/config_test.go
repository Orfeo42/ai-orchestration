package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "projects.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"billing":{"repo":"ionstream/billing-backend","base_branch":"main","label":"repo/billing-BE"},"nolabel":{"repo":"ionstream/x","base_branch":"main"}}`), 0o600))
	t.Setenv("FORGEJO_URL", "https://git.example.com")
	t.Setenv("FORGEJO_TOKEN", "tok")
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("PROJECTS_CONFIG", path)
}

func TestLoadConfig(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		setRequiredEnv(t)

		cfg, err := loadConfig()

		require.NoError(t, err)
		assert.Equal(t, "https://git.example.com", cfg.forgejoURL)
		assert.Equal(t, "tok", cfg.forgejoToken)
		assert.Equal(t, "postgres://x", cfg.databaseURL)
		assert.Equal(t, "/workflows", cfg.workflowsDir)
		assert.Equal(t, 60*time.Second, cfg.pollInterval)
		assert.Equal(t, "repo/billing-BE", cfg.projects["billing"].Label)
		assert.Len(t, cfg.projects, 2)
	})

	t.Run("overrides", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("WORKFLOWS_DIR", "/w")
		t.Setenv("POLL_INTERVAL", "5m")

		cfg, err := loadConfig()

		require.NoError(t, err)
		assert.Equal(t, "/w", cfg.workflowsDir)
		assert.Equal(t, 5*time.Minute, cfg.pollInterval)
	})

	t.Run("bad poll interval", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("POLL_INTERVAL", "soon")

		_, err := loadConfig()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "POLL_INTERVAL")
	})

	t.Run("missing projects file", func(t *testing.T) {
		setRequiredEnv(t)
		t.Setenv("PROJECTS_CONFIG", filepath.Join(t.TempDir(), "missing.json"))

		_, err := loadConfig()

		require.Error(t, err)
	})
}
