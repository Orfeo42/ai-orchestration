package gitops

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestEnsureRepo(t *testing.T) {
	t.Run("clones when target dir does not exist", func(t *testing.T) {
		base := t.TempDir()
		bare := filepath.Join(base, "src.git")
		if err := exec.Command("git", "init", "--bare", bare).Run(); err != nil {
			t.Fatalf("init bare: %v", err)
		}
		seedBareWithCommit(t, bare)

		dst := filepath.Join(base, "worker", "repo")
		g := &GitOps{path: dst, owner: "o", repo: "r", remoteWithAuth: bare}
		if err := g.EnsureRepo(); err != nil {
			t.Fatalf("EnsureRepo: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dst, ".git")); err != nil {
			t.Fatalf(".git missing after clone: %v", err)
		}
	})

	t.Run("fetches when repo already exists", func(t *testing.T) {
		base := t.TempDir()
		bare := filepath.Join(base, "src.git")
		if err := exec.Command("git", "init", "--bare", bare).Run(); err != nil {
			t.Fatalf("init bare: %v", err)
		}
		seedBareWithCommit(t, bare)

		dst := filepath.Join(base, "worker", "repo")
		clone := exec.Command("git", "clone", bare, dst)
		if out, err := clone.CombinedOutput(); err != nil {
			t.Fatalf("seed clone: %v\n%s", err, out)
		}

		g := &GitOps{path: dst, owner: "o", repo: "r", remoteWithAuth: bare}
		if err := g.EnsureRepo(); err != nil {
			t.Fatalf("EnsureRepo on existing: %v", err)
		}
	})
}

func seedBareWithCommit(t *testing.T, bare string) {
	t.Helper()
	work := filepath.Join(t.TempDir(), "seed")
	if out, err := exec.Command("git", "clone", bare, work).CombinedOutput(); err != nil {
		t.Fatalf("clone bare: %v\n%s", err, out)
	}
	for _, args := range [][]string{
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = work
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(work, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	for _, args := range [][]string{
		{"add", "."},
		{"commit", "-m", "init"},
		{"push"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = work
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}
