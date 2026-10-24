package projects

import (
	"errors"
	"testing"
)

func TestPath(t *testing.T) {
	t.Run("workerID and root present", func(t *testing.T) {
		c := Config{Repo: "ionstream/handbook"}
		got := c.Path("/projects", "impl-1")
		if got != "/projects/impl-1/handbook" {
			t.Errorf("got %q, want /projects/impl-1/handbook", got)
		}
	})

	t.Run("empty workerID falls back to flat layout", func(t *testing.T) {
		c := Config{Repo: "ionstream/handbook"}
		got := c.Path("/projects", "")
		if got != "/projects/handbook" {
			t.Errorf("got %q, want /projects/handbook", got)
		}
	})

	t.Run("empty root uses default", func(t *testing.T) {
		c := Config{Repo: "ionstream/handbook"}
		got := c.Path("", "w1")
		if got != "/projects/w1/handbook" {
			t.Errorf("got %q, want /projects/w1/handbook", got)
		}
	})

	t.Run("repo without slash uses repo as name", func(t *testing.T) {
		c := Config{Repo: "single"}
		got := c.Path("/p", "w1")
		if got != "/p/w1/single" {
			t.Errorf("got %q, want /p/w1/single", got)
		}
	})
}

func TestResolvedMemoryKey(t *testing.T) {
	t.Run("explicit key wins", func(t *testing.T) {
		c := Config{Repo: "ionstream/billing-backend", MemoryKey: "HOME-custom"}
		if got := c.ResolvedMemoryKey(); got != "HOME-custom" {
			t.Errorf("got %q, want HOME-custom", got)
		}
	})

	t.Run("default derives from repo name", func(t *testing.T) {
		c := Config{Repo: "ionstream/billing-backend"}
		if got := c.ResolvedMemoryKey(); got != "HOME-sviluppo-ionstream-billing-backend" {
			t.Errorf("got %q, want HOME-sviluppo-ionstream-billing-backend", got)
		}
	})
}

func TestSplitRepo(t *testing.T) {
	t.Run("owner and name", func(t *testing.T) {
		got, err := SplitRepo("ionstream/handbook")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if got != (Repo{Owner: "ionstream", Name: "handbook"}) {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("name keeps further slashes", func(t *testing.T) {
		got, err := SplitRepo("a/b/c")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if got != (Repo{Owner: "a", Name: "b/c"}) {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("no slash is invalid", func(t *testing.T) {
		if _, err := SplitRepo("single"); !errors.Is(err, ErrInvalidRepo) {
			t.Errorf("got %v, want ErrInvalidRepo", err)
		}
	})

	t.Run("empty owner is invalid", func(t *testing.T) {
		if _, err := SplitRepo("/name"); !errors.Is(err, ErrInvalidRepo) {
			t.Errorf("got %v, want ErrInvalidRepo", err)
		}
	})

	t.Run("empty name is invalid", func(t *testing.T) {
		if _, err := SplitRepo("owner/"); !errors.Is(err, ErrInvalidRepo) {
			t.Errorf("got %v, want ErrInvalidRepo", err)
		}
	})
}

func TestProjectForLabels(t *testing.T) {
	projs := map[string]Config{
		"billing": {Repo: "ionstream/billing-backend", Label: "repo/billing-BE"},
		"orch":    {Repo: "ionstream/orchestration-backend", Label: "repo/orch-BE"},
		"nolabel": {Repo: "ionstream/x"},
	}

	t.Run("match", func(t *testing.T) {
		got, ok := ProjectForLabels(projs, []string{"bug", "repo/orch-BE"})
		if !ok || got != "orch" {
			t.Errorf("got %q %v, want orch true", got, ok)
		}
	})

	t.Run("no match", func(t *testing.T) {
		got, ok := ProjectForLabels(projs, []string{"bug"})
		if ok || got != "" {
			t.Errorf("got %q %v, want empty false", got, ok)
		}
	})

	t.Run("empty label ignored", func(t *testing.T) {
		got, ok := ProjectForLabels(projs, []string{""})
		if ok || got != "" {
			t.Errorf("got %q %v, want empty false", got, ok)
		}
	})

	t.Run("multiple matches pick smallest key", func(t *testing.T) {
		got, ok := ProjectForLabels(projs, []string{"repo/orch-BE", "repo/billing-BE"})
		if !ok || got != "billing" {
			t.Errorf("got %q %v, want billing true", got, ok)
		}
	})
}
