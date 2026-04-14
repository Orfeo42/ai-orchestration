package testrunner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectCmd(t *testing.T) {
	t.Run("go.mod detected", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/m\n"), 0644); err != nil {
			t.Fatal(err)
		}
		cmd, err := detectCmd(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cmd) == 0 || cmd[0] != "go" {
			t.Errorf("cmd = %v, want go test ...", cmd)
		}
	})

	t.Run("pytest.ini detected", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "pytest.ini"), []byte("[pytest]\n"), 0644); err != nil {
			t.Fatal(err)
		}
		cmd, err := detectCmd(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cmd) == 0 || cmd[0] != "pytest" {
			t.Errorf("cmd = %v, want pytest ...", cmd)
		}
	})

	t.Run("pyproject.toml with pytest detected", func(t *testing.T) {
		dir := t.TempDir()
		content := "[tool.pytest.ini_options]\ntestpaths = [\"tests\"]\n"
		if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		cmd, err := detectCmd(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cmd) == 0 || cmd[0] != "pytest" {
			t.Errorf("cmd = %v, want pytest ...", cmd)
		}
	})

	t.Run("package.json detected", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"app"}`), 0644); err != nil {
			t.Fatal(err)
		}
		cmd, err := detectCmd(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cmd) == 0 || cmd[0] != "npm" {
			t.Errorf("cmd = %v, want npm test ...", cmd)
		}
	})

	t.Run("no recognizable files returns nil", func(t *testing.T) {
		dir := t.TempDir()
		cmd, err := detectCmd(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmd != nil {
			t.Errorf("cmd = %v, want nil", cmd)
		}
	})
}
