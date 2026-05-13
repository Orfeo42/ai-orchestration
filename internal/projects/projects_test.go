package projects

import (
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
