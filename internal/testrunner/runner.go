package testrunner

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Runner is a function that runs tests in projectPath and returns (passed, errorOutput).
type Runner func(projectPath string) (bool, string)

func Run(projectPath string) (bool, string) {
	cmd, err := detectCmd(projectPath)
	if err != nil || cmd == nil {
		slog.Debug("no test runner detected", "path", projectPath)
		return true, ""
	}

	slog.Info("running tests", "path", projectPath, "cmd", strings.Join(cmd, " "))

	c := exec.Command(cmd[0], cmd[1:]...)
	c.Dir = projectPath

	done := make(chan struct{})
	var out []byte
	var runErr error
	go func() {
		out, runErr = c.CombinedOutput()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(180 * time.Second):
		if killErr := c.Process.Kill(); killErr != nil {
			slog.Warn("kill test process failed", "error", killErr)
		}
		return false, "tests timed out after 180s"
	}

	if runErr != nil {
		slog.Warn("tests failed", "path", projectPath)
		output := string(out)
		if len(output) > 3000 {
			output = output[:3000]
		}
		return false, output
	}

	slog.Info("tests passed", "path", projectPath)
	return true, ""
}

func detectCmd(path string) ([]string, error) {
	if fileExists(filepath.Join(path, "go.mod")) {
		return []string{"go", "test", "-short", "./..."}, nil
	}
	if fileExists(filepath.Join(path, "pytest.ini")) || fileExists(filepath.Join(path, "setup.cfg")) {
		return []string{"pytest", "--tb=short", "-q"}, nil
	}
	if fileExists(filepath.Join(path, "pyproject.toml")) {
		data, err := os.ReadFile(filepath.Join(path, "pyproject.toml"))
		if err != nil {
			return nil, fmt.Errorf("read pyproject.toml: %w", err)
		}
		if strings.Contains(string(data), "pytest") {
			return []string{"pytest", "--tb=short", "-q"}, nil
		}
	}
	if fileExists(filepath.Join(path, "package.json")) {
		return []string{"npm", "test", "--", "--watchAll=false", "--passWithNoTests"}, nil
	}
	return nil, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
