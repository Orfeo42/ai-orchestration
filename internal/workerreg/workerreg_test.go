package workerreg

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newRegistry(t *testing.T) (*Registry, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return &Registry{rdb: client, ttl: 2 * time.Second, refresh: 200 * time.Millisecond}, mr
}

func TestRegister(t *testing.T) {
	t.Run("sets worker key with TTL", func(t *testing.T) {
		r, mr := newRegistry(t)
		ctx := context.Background()
		unregister, err := r.Register(ctx, "w-1")
		if err != nil {
			t.Fatalf("Register: %v", err)
		}
		defer unregister()

		if !mr.Exists("worker:w-1") {
			t.Error("worker:w-1 missing")
		}
	})

	t.Run("unregister deletes the key", func(t *testing.T) {
		r, mr := newRegistry(t)
		ctx := context.Background()
		unregister, err := r.Register(ctx, "w-2")
		if err != nil {
			t.Fatalf("Register: %v", err)
		}
		unregister()
		time.Sleep(50 * time.Millisecond)
		if mr.Exists("worker:w-2") {
			t.Error("worker:w-2 should have been deleted")
		}
	})
}

func TestSweepStale(t *testing.T) {
	t.Run("removes dirs without live worker key, preserves live ones", func(t *testing.T) {
		r, mr := newRegistry(t)
		root := t.TempDir()
		mustMkdir(t, filepath.Join(root, "live-1", "repo"))
		mustMkdir(t, filepath.Join(root, "dead-1", "repo"))
		mustMkdir(t, filepath.Join(root, "self-1"))

		mr.Set("worker:live-1", "alive")

		if err := r.SweepStale(context.Background(), root, "self-1"); err != nil {
			t.Fatalf("SweepStale: %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, "live-1")); err != nil {
			t.Errorf("live-1 dir removed unexpectedly: %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, "dead-1")); !os.IsNotExist(err) {
			t.Errorf("dead-1 dir should be gone, stat err = %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, "self-1")); err != nil {
			t.Errorf("self-1 dir removed unexpectedly: %v", err)
		}
	})

	t.Run("missing root is not an error", func(t *testing.T) {
		r, _ := newRegistry(t)
		if err := r.SweepStale(context.Background(), filepath.Join(t.TempDir(), "nope"), "w"); err != nil {
			t.Errorf("SweepStale missing root: %v", err)
		}
	})
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
}
