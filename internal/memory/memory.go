package memory

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Project struct {
	Key  string
	Path string
}

type Config struct {
	ServerURL string
	Token     string
	StateDir  string
	ClaudeDir string
	Projects  []Project
}

type memoryError string

func (e memoryError) Error() string { return string(e) }

const ErrUpstream memoryError = "memory hub unreachable or rejected request"

const errGit memoryError = "git command failed"

func Sync(ctx context.Context, cfg Config) error {
	mirror := filepath.Join(cfg.StateDir, "canonical-mirror")

	ready, err := refreshMirror(ctx, cfg, mirror)
	if err != nil {
		return err
	}
	if !ready {
		slog.WarnContext(ctx, "canonical repo has no commits yet")
		return nil
	}

	projects, err := materialize(cfg, mirror)
	if err != nil {
		return err
	}

	head, err := runGit(ctx, cfg.Token, mirror, "rev-parse", "--short", "HEAD")
	if err != nil {
		return fmt.Errorf("read mirror head: %w", err)
	}

	slog.InfoContext(ctx, "memory synced", slog.String("head", head), slog.Int("projects", projects))
	return nil
}

func refreshMirror(ctx context.Context, cfg Config, mirror string) (bool, error) {
	_, err := os.Stat(filepath.Join(mirror, ".git"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("stat mirror %s: %w", mirror, err)
	}

	if err != nil {
		if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
			return false, fmt.Errorf("create state dir %s: %w", cfg.StateDir, err)
		}
		remote := strings.TrimSuffix(cfg.ServerURL, "/") + "/git/canonical.git"
		if _, err := runGit(ctx, cfg.Token, cfg.StateDir, "clone", remote, mirror); err != nil {
			return false, fmt.Errorf("%w: clone: %w", ErrUpstream, err)
		}
	} else if _, err := runGit(ctx, cfg.Token, mirror, "fetch", "origin"); err != nil {
		return false, fmt.Errorf("%w: fetch: %w", ErrUpstream, err)
	}

	if _, err := runGit(ctx, cfg.Token, mirror, "rev-parse", "--verify", "--quiet", "origin/main"); err != nil {
		return false, nil
	}
	if _, err := runGit(ctx, cfg.Token, mirror, "reset", "--hard", "origin/main"); err != nil {
		return false, fmt.Errorf("reset mirror: %w", err)
	}
	return true, nil
}

func runGit(ctx context.Context, token string, dir string, args ...string) (string, error) {
	full := append([]string{"-c", "http.extraHeader=Authorization: Bearer " + token}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if token != "" {
		text = strings.ReplaceAll(text, token, "***")
	}
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %v: %s", args[0], errGit, err, text)
	}
	return text, nil
}

func materialize(cfg Config, mirror string) (int, error) {
	global := filepath.Join(mirror, "global")

	if err := syncFile(filepath.Join(global, "CLAUDE.md"), filepath.Join(cfg.ClaudeDir, "CLAUDE.md")); err != nil {
		return 0, err
	}

	for _, name := range []string{"rules", "skills", "agents"} {
		if _, err := replaceDir(filepath.Join(global, name), filepath.Join(cfg.ClaudeDir, name)); err != nil {
			return 0, err
		}
	}

	count := 0
	for _, p := range cfg.Projects {
		src := filepath.Join(mirror, "projects", p.Key, "memory")
		dst := filepath.Join(cfg.ClaudeDir, "projects", slug(p.Path), "memory")
		copied, err := replaceDir(src, dst)
		if err != nil {
			return 0, err
		}
		if copied {
			count++
		}
	}
	return count, nil
}

func slug(path string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, path)
}

func syncFile(src string, dst string) error {
	data, err := os.ReadFile(src)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.Remove(dst); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", dst, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create dir for %s: %w", dst, err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	return nil
}

func replaceDir(src string, dst string) (bool, error) {
	_, err := os.Stat(src)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.RemoveAll(dst); err != nil {
			return false, fmt.Errorf("remove %s: %w", dst, err)
		}
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat %s: %w", src, err)
	}

	parent := filepath.Dir(dst)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return false, fmt.Errorf("create dir %s: %w", parent, err)
	}
	tmp, err := os.MkdirTemp(parent, "."+filepath.Base(dst)+"-")
	if err != nil {
		return false, fmt.Errorf("create temp dir in %s: %w", parent, err)
	}
	if err := copyTree(src, tmp); err != nil {
		return false, errors.Join(err, os.RemoveAll(tmp))
	}
	if err := os.RemoveAll(dst); err != nil {
		return false, errors.Join(fmt.Errorf("remove %s: %w", dst, err), os.RemoveAll(tmp))
	}
	if err := os.Rename(tmp, dst); err != nil {
		return false, errors.Join(fmt.Errorf("rename %s to %s: %w", tmp, dst, err), os.RemoveAll(tmp))
	}
	return true, nil
}

func copyTree(src string, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk %s: %w", path, err)
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("relative path %s: %w", path, err)
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create dir %s: %w", target, err)
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		mode := fs.FileMode(0o644)
		if info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return fmt.Errorf("write %s: %w", target, err)
		}
		if err := os.Chmod(target, mode); err != nil {
			return fmt.Errorf("chmod %s: %w", target, err)
		}
		return nil
	})
}
