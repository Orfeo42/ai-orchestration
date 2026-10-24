package checkout

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Orfeo42/ai-orchestration/internal/gitops"
	"github.com/Orfeo42/ai-orchestration/internal/pipeline/state"
	"github.com/Orfeo42/ai-orchestration/internal/projects"
)

type AssessCheckout struct {
	GitFactory   gitops.Factory
	ForgejoURL   string
	ForgejoToken string
	ProjectsRoot string
	WorkerID     string
}

func (p AssessCheckout) Run(_ context.Context, s *state.Assess) error {
	repo, err := projects.SplitRepo(s.Proj.Repo)
	if err != nil {
		return err
	}
	path := s.Proj.Path(p.ProjectsRoot, p.WorkerID)
	git := p.GitFactory(path, p.ForgejoURL, p.ForgejoToken, repo.Owner, repo.Name)
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
	repo, err := projects.SplitRepo(s.Proj.Repo)
	if err != nil {
		return err
	}
	path := s.Proj.Path(p.ProjectsRoot, p.WorkerID)
	git := p.GitFactory(path, p.ForgejoURL, p.ForgejoToken, repo.Owner, repo.Name)
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
	repo, err := projects.SplitRepo(s.Proj.Repo)
	if err != nil {
		return err
	}
	path := s.Proj.Path(p.ProjectsRoot, p.WorkerID)
	git := p.GitFactory(path, p.ForgejoURL, p.ForgejoToken, repo.Owner, repo.Name)
	if err := git.EnsureRepo(); err != nil {
		return fmt.Errorf("ensure repo: %w", err)
	}
	s.ProjectPath = path
	slog.Debug("review checkout ready", "path", path)
	return nil
}
