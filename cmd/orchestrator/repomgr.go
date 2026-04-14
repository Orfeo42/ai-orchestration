package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

func syncAll(projects map[string]projectConfig, forgejoURL, token string) error {
	var lastErr error
	for name, proj := range projects {
		if err := cloneOrPull(proj, forgejoURL, token); err != nil {
			slog.Error("sync failed", "project", name, "error", err)
			lastErr = err
		}
	}
	return lastErr
}

func cloneOrPull(proj projectConfig, forgejoURL, token string) error {
	remote := buildRemote(forgejoURL, token, proj.Repo)
	path := proj.path()

	if _, err := os.Stat(path); err == nil {
		slog.Info("pulling repo", "repo", proj.Repo)
		if err := runCmd("git", "-C", path, "checkout", proj.BaseBranch); err != nil {
			return fmt.Errorf("checkout: %w", err)
		}
		if err := runCmd("git", "-C", path, "pull", remote, proj.BaseBranch); err != nil {
			return fmt.Errorf("pull: %w", err)
		}
		return nil
	}

	slog.Info("cloning repo", "repo", proj.Repo, "path", path)
	parent := path[:strings.LastIndex(path, "/")]
	if err := os.MkdirAll(parent, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	return runCmd("git", "clone", remote, path)
}

func buildRemote(forgejoURL, token, repo string) string {
	base := strings.TrimRight(forgejoURL, "/") + "/" + repo + ".git"
	return strings.Replace(base, "https://", "https://oauth2:"+token+"@", 1)
}

func runCmd(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %w\n%s", args, err, out)
	}
	return nil
}
