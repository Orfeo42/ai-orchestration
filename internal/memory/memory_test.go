package memory

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const testToken = "s3cr3t-token-value"

type hub struct {
	root string
	work string
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func writeFile(t *testing.T, path string, content string, mode os.FileMode) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), mode))
	require.NoError(t, os.Chmod(path, mode))
}

func newHub(t *testing.T) *hub {
	t.Helper()
	h := &hub{root: t.TempDir(), work: t.TempDir()}
	bare := filepath.Join(h.root, "git", "canonical.git")
	require.NoError(t, os.MkdirAll(bare, 0o755))
	gitIn(t, bare, "init", "--bare", "-b", "main")
	gitIn(t, h.work, "init", "-b", "main")
	gitIn(t, h.work, "remote", "add", "origin", bare)
	return h
}

func (h *hub) put(t *testing.T, rel string, content string, mode os.FileMode) {
	t.Helper()
	writeFile(t, filepath.Join(h.work, rel), content, mode)
}

func (h *hub) remove(t *testing.T, rel string) {
	t.Helper()
	require.NoError(t, os.Remove(filepath.Join(h.work, rel)))
}

func (h *hub) commit(t *testing.T) {
	t.Helper()
	gitIn(t, h.work, "add", "-A")
	gitIn(t, h.work, "commit", "-m", "x")
	gitIn(t, h.work, "push", "origin", "main")
}

func (h *hub) seed(t *testing.T) {
	t.Helper()
	h.put(t, "global/CLAUDE.md", "claude v1", 0o644)
	h.put(t, "global/rules/a.md", "rule a", 0o644)
	h.put(t, "global/rules/b.md", "rule b", 0o644)
	h.put(t, "global/skills/x/bin/run", "#!/bin/sh\n", 0o755)
	h.put(t, "global/skills/x/SKILL.md", "skill", 0o644)
	h.put(t, "global/agents/impl.md", "agent", 0o644)
	h.put(t, "projects/proj1/memory/m1.md", "mem1", 0o644)
	h.put(t, "projects/proj1/memory/m2.md", "mem2", 0o644)
	h.commit(t)
}

func (h *hub) config(t *testing.T) Config {
	t.Helper()
	return Config{
		ServerURL: "file://" + h.root,
		Token:     testToken,
		StateDir:  t.TempDir(),
		ClaudeDir: filepath.Join(t.TempDir(), "claude"),
		Projects:  []Project{{Key: "proj1", Path: "/projects/w1/billing-backend"}},
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestSlug(t *testing.T) {
	t.Run("slashes and dashes", func(t *testing.T) {
		require.Equal(t, "-projects-w1-billing-backend", slug("/projects/w1/billing-backend"))
	})

	t.Run("dots and underscores", func(t *testing.T) {
		require.Equal(t, "-home-u-a-b-c", slug("/home/u/a.b_c"))
	})
}

func TestSync(t *testing.T) {
	memDir := func(cfg Config) string {
		return filepath.Join(cfg.ClaudeDir, "projects", "-projects-w1-billing-backend", "memory")
	}

	t.Run("first sync clones and materializes everything", func(t *testing.T) {
		h := newHub(t)
		h.seed(t)
		cfg := h.config(t)

		require.NoError(t, Sync(t.Context(), cfg))

		data, err := os.ReadFile(filepath.Join(cfg.ClaudeDir, "CLAUDE.md"))
		require.NoError(t, err)
		require.Equal(t, "claude v1", string(data))
		require.FileExists(t, filepath.Join(cfg.ClaudeDir, "rules", "a.md"))
		require.FileExists(t, filepath.Join(cfg.ClaudeDir, "agents", "impl.md"))
		require.FileExists(t, filepath.Join(memDir(cfg), "m1.md"))

		info, err := os.Stat(filepath.Join(cfg.ClaudeDir, "skills", "x", "bin", "run"))
		require.NoError(t, err)
		require.NotZero(t, info.Mode()&0o100)
		info, err = os.Stat(filepath.Join(cfg.ClaudeDir, "skills", "x", "SKILL.md"))
		require.NoError(t, err)
		require.Zero(t, info.Mode()&0o111)
	})

	t.Run("second sync applies upstream deletions and updates", func(t *testing.T) {
		h := newHub(t)
		h.seed(t)
		cfg := h.config(t)
		require.NoError(t, Sync(t.Context(), cfg))

		h.remove(t, "global/rules/a.md")
		h.remove(t, "projects/proj1/memory/m1.md")
		h.put(t, "global/CLAUDE.md", "claude v2", 0o644)
		h.commit(t)
		require.NoError(t, Sync(t.Context(), cfg))

		require.False(t, exists(filepath.Join(cfg.ClaudeDir, "rules", "a.md")))
		require.FileExists(t, filepath.Join(cfg.ClaudeDir, "rules", "b.md"))
		require.False(t, exists(filepath.Join(memDir(cfg), "m1.md")))
		require.FileExists(t, filepath.Join(memDir(cfg), "m2.md"))
		data, err := os.ReadFile(filepath.Join(cfg.ClaudeDir, "CLAUDE.md"))
		require.NoError(t, err)
		require.Equal(t, "claude v2", string(data))
	})

	t.Run("upstream CLAUDE.md removal removes local file", func(t *testing.T) {
		h := newHub(t)
		h.seed(t)
		cfg := h.config(t)
		require.NoError(t, Sync(t.Context(), cfg))

		h.remove(t, "global/CLAUDE.md")
		h.commit(t)
		require.NoError(t, Sync(t.Context(), cfg))

		require.False(t, exists(filepath.Join(cfg.ClaudeDir, "CLAUDE.md")))
	})

	t.Run("session files outside memory survive", func(t *testing.T) {
		h := newHub(t)
		h.seed(t)
		cfg := h.config(t)
		session := filepath.Join(cfg.ClaudeDir, "projects", "-projects-w1-billing-backend", "session.jsonl")
		writeFile(t, session, "transcript", 0o644)

		require.NoError(t, Sync(t.Context(), cfg))

		require.FileExists(t, session)
		require.FileExists(t, filepath.Join(memDir(cfg), "m1.md"))
	})

	t.Run("project absent upstream removes stale memory", func(t *testing.T) {
		h := newHub(t)
		h.seed(t)
		cfg := h.config(t)
		cfg.Projects = append(cfg.Projects, Project{Key: "ghost", Path: "/projects/ghost"})
		ghostMem := filepath.Join(cfg.ClaudeDir, "projects", "-projects-ghost", "memory")
		writeFile(t, filepath.Join(ghostMem, "old.md"), "stale", 0o644)

		require.NoError(t, Sync(t.Context(), cfg))

		require.False(t, exists(ghostMem))
		require.FileExists(t, filepath.Join(memDir(cfg), "m1.md"))
	})

	t.Run("empty repo returns nil and materializes nothing", func(t *testing.T) {
		h := newHub(t)
		cfg := h.config(t)

		require.NoError(t, Sync(t.Context(), cfg))

		require.False(t, exists(cfg.ClaudeDir))
	})

	t.Run("unreachable remote is upstream error", func(t *testing.T) {
		h := newHub(t)
		cfg := h.config(t)
		cfg.ServerURL = "file://" + filepath.Join(t.TempDir(), "missing")

		err := Sync(t.Context(), cfg)

		require.ErrorIs(t, err, ErrUpstream)
		require.NotContains(t, err.Error(), testToken)
	})

	t.Run("token is not persisted in mirror git config", func(t *testing.T) {
		h := newHub(t)
		h.seed(t)
		cfg := h.config(t)
		require.NoError(t, Sync(t.Context(), cfg))
		require.NoError(t, Sync(t.Context(), cfg))

		data, err := os.ReadFile(filepath.Join(cfg.StateDir, "canonical-mirror", ".git", "config"))
		require.NoError(t, err)
		require.NotContains(t, string(data), testToken)
	})
}
