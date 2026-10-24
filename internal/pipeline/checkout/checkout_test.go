package checkout

import (
	"context"
	"errors"
	"testing"

	"github.com/Orfeo42/ai-orchestration/internal/gitops"
	"github.com/Orfeo42/ai-orchestration/internal/pipeline/state"
	"github.com/Orfeo42/ai-orchestration/internal/projects"
)

func TestImplCheckout(t *testing.T) {
	t.Run("calls EnsureRepo and stores ProjectPath", func(t *testing.T) {
		var ensureCalls int
		var seenPath string
		mock := &gitops.MockGit{
			EnsureRepoFunc: func() error {
				ensureCalls++
				return nil
			},
		}
		factory := func(repoPath, _, _, owner, repo string) gitops.Git {
			seenPath = repoPath
			return mock
		}
		s := &state.Impl{Proj: projects.Config{Repo: "ionstream/handbook"}}
		p := ImplCheckout{
			GitFactory:   factory,
			ProjectsRoot: "/projects",
			WorkerID:     "impl-1",
		}
		if err := p.Run(context.Background(), s); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if ensureCalls != 1 {
			t.Errorf("EnsureRepo called %d times, want 1", ensureCalls)
		}
		want := "/projects/impl-1/handbook"
		if seenPath != want {
			t.Errorf("factory got path %q, want %q", seenPath, want)
		}
		if s.ProjectPath != want {
			t.Errorf("state.ProjectPath = %q, want %q", s.ProjectPath, want)
		}
	})

	t.Run("propagates EnsureRepo error", func(t *testing.T) {
		boom := errors.New("clone failed")
		factory := func(_, _, _, _, _ string) gitops.Git {
			return &gitops.MockGit{EnsureRepoFunc: func() error { return boom }}
		}
		s := &state.Impl{Proj: projects.Config{Repo: "o/x"}}
		err := ImplCheckout{GitFactory: factory, ProjectsRoot: "/p", WorkerID: "w"}.Run(context.Background(), s)
		if !errors.Is(err, boom) {
			t.Errorf("err = %v, want wrap of %v", err, boom)
		}
	})

	t.Run("rejects malformed repo", func(t *testing.T) {
		factory := func(_, _, _, _, _ string) gitops.Git { return &gitops.MockGit{} }
		s := &state.Impl{Proj: projects.Config{Repo: "no-slash"}}
		err := ImplCheckout{GitFactory: factory, ProjectsRoot: "/p", WorkerID: "w"}.Run(context.Background(), s)
		if err == nil {
			t.Error("err = nil, want error for malformed repo")
		}
	})
}

func TestAssessCheckout(t *testing.T) {
	t.Run("populates state.ProjectPath", func(t *testing.T) {
		mock := &gitops.MockGit{EnsureRepoFunc: func() error { return nil }}
		factory := func(_, _, _, _, _ string) gitops.Git { return mock }
		s := &state.Assess{Proj: projects.Config{Repo: "o/r"}}
		err := AssessCheckout{GitFactory: factory, ProjectsRoot: "/p", WorkerID: "a-1"}.Run(context.Background(), s)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if s.ProjectPath != "/p/a-1/r" {
			t.Errorf("ProjectPath = %q, want /p/a-1/r", s.ProjectPath)
		}
	})
}

func TestReviewCheckout(t *testing.T) {
	t.Run("requires non-empty proj.Repo", func(t *testing.T) {
		factory := func(_, _, _, _, _ string) gitops.Git { return &gitops.MockGit{} }
		s := &state.Review{}
		err := ReviewCheckout{GitFactory: factory, ProjectsRoot: "/p", WorkerID: "w"}.Run(context.Background(), s)
		if err == nil {
			t.Error("err = nil, want error for empty repo")
		}
	})

	t.Run("populates state.ProjectPath", func(t *testing.T) {
		mock := &gitops.MockGit{EnsureRepoFunc: func() error { return nil }}
		factory := func(_, _, _, _, _ string) gitops.Git { return mock }
		s := &state.Review{Proj: projects.Config{Repo: "o/r"}}
		err := ReviewCheckout{GitFactory: factory, ProjectsRoot: "/p", WorkerID: "rv-1"}.Run(context.Background(), s)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if s.ProjectPath != "/p/rv-1/r" {
			t.Errorf("ProjectPath = %q, want /p/rv-1/r", s.ProjectPath)
		}
	})
}
