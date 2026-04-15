package gitops

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

type GitOps struct {
	path           string
	owner          string
	repo           string
	remoteWithAuth string
}

func New(repoPath, forgejoURL, token, owner, repo string) *GitOps {
	remote := strings.TrimRight(forgejoURL, "/") + "/" + owner + "/" + repo + ".git"
	remote = strings.Replace(remote, "https://", "https://oauth2:"+token+"@", 1)
	return &GitOps{
		path:           repoPath,
		owner:          owner,
		repo:           repo,
		remoteWithAuth: remote,
	}
}

func (g *GitOps) CreateBranch(branch string) error {
	slog.Info("creating branch", "branch", branch, "repo", g.owner+"/"+g.repo)
	cmd := exec.Command("git", "checkout", "-b", branch)
	cmd.Dir = g.path
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "already exists") {
			slog.Warn("branch already exists, checking out", "branch", branch)
			return g.run("git", "checkout", branch)
		}
		return fmt.Errorf("create branch %s: %w\n%s", branch, err, out)
	}
	return nil
}

func (g *GitOps) Checkout(branch string) error {
	slog.Info("checking out branch", "branch", branch, "repo", g.owner+"/"+g.repo)
	return g.run("git", "checkout", branch)
}

func (g *GitOps) Fetch() error {
	slog.Debug("fetching from remote", "repo", g.owner+"/"+g.repo)
	return g.run("git", "fetch", g.remoteWithAuth)
}

func (g *GitOps) CheckoutRemoteBranch(branch string) error {
	slog.Info("checking out remote branch", "branch", branch, "repo", g.owner+"/"+g.repo)
	return g.run("git", "checkout", "-B", branch, "origin/"+branch)
}

func (g *GitOps) Add(path string) error {
	return g.run("git", "add", path)
}

func (g *GitOps) AddAll() error {
	return g.run("git", "add", "-A")
}

func (g *GitOps) Commit(message string) error {
	slog.Info("committing changes", "message", message, "repo", g.owner+"/"+g.repo)
	return g.run("git", "commit", "-m", message)
}

func (g *GitOps) Push(branch string) error {
	slog.Info("pushing branch", "branch", branch, "repo", g.owner+"/"+g.repo)
	return g.run("git", "push", g.remoteWithAuth, branch)
}

func (g *GitOps) HasChanges() (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = g.path
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	return strings.TrimSpace(string(out)) != "", nil
}

func (g *GitOps) run(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = g.path
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %w\n%s", args, err, out)
	}
	return nil
}
