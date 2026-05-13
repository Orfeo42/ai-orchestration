package checkout

import (
	"context"
	"fmt"
	"log/slog"

	"ai-orchestration/internal/gitops"
	"ai-orchestration/internal/pipeline/state"
)

type AssessCheckout struct {
	GitFactory   gitops.Factory
	ForgejoURL   string
	ForgejoToken string
	ProjectsRoot string
	WorkerID     string
}

func (p AssessCheckout) Run(_ context.Context, s *state.Assess) error {
	owner, name, err := splitRepo(s.Proj.Repo)
	if err != nil {
		return err
	}
	path := s.Proj.Path(p.ProjectsRoot, p.WorkerID)
	git := p.GitFactory(path, p.ForgejoURL, p.ForgejoToken, owner, name)
	if err := git.EnsureRepo(); err != nil {
		return fmt.Errorf("ensure repo: %w", err)
	}
	s.ProjectPath = path
	slog.Debug("assess checkout ready", "path", path)
	return nil
}

type ImplCheckout struct {
	GitFactory   gitops.Factory
	ForgejoURL   string
	ForgejoToken string
	ProjectsRoot string
	WorkerID     string
}

func (p ImplCheckout) Run(_ context.Context, s *state.Impl) error {
	owner, name, err := splitRepo(s.Proj.Repo)
	if err != nil {
		return err
	}
	path := s.Proj.Path(p.ProjectsRoot, p.WorkerID)
	git := p.GitFactory(path, p.ForgejoURL, p.ForgejoToken, owner, name)
	if err := git.EnsureRepo(); err != nil {
		return fmt.Errorf("ensure repo: %w", err)
	}
	s.ProjectPath = path
	slog.Debug("impl checkout ready", "path", path)
	return nil
}

type ReviewCheckout struct {
	GitFactory   gitops.Factory
	ForgejoURL   string
	ForgejoToken string
	ProjectsRoot string
	WorkerID     string
}

func (p ReviewCheckout) Run(_ context.Context, s *state.Review) error {
	if s.Proj.Repo == "" {
		return fmt.Errorf("review checkout: project config missing repo")
	}
	owner, name, err := splitRepo(s.Proj.Repo)
	if err != nil {
		return err
	}
	path := s.Proj.Path(p.ProjectsRoot, p.WorkerID)
	git := p.GitFactory(path, p.ForgejoURL, p.ForgejoToken, owner, name)
	if err := git.EnsureRepo(); err != nil {
		return fmt.Errorf("ensure repo: %w", err)
	}
	s.ProjectPath = path
	slog.Debug("review checkout ready", "path", path)
	return nil
}

func splitRepo(repo string) (owner, name string, err error) {
	for i := 0; i < len(repo); i++ {
		if repo[i] == '/' {
			return repo[:i], repo[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("invalid repo %q (expected owner/name)", repo)
}
