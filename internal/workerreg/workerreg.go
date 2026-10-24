package workerreg

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	keyPrefix      = "worker:"
	defaultTTL     = 30 * time.Second
	defaultRefresh = 10 * time.Second
)

type Registry struct {
	rdb     *redis.Client
	ttl     time.Duration
	refresh time.Duration
}

func New(redisURL string) (*Registry, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	return &Registry{
		rdb:     redis.NewClient(opts),
		ttl:     defaultTTL,
		refresh: defaultRefresh,
	}, nil
}

func (r *Registry) Close() error {
	return r.rdb.Close()
}

func (r *Registry) Register(ctx context.Context, workerID string) (unregister func(), err error) {
	key := keyPrefix + workerID
	if err := r.rdb.Set(ctx, key, time.Now().UTC().Format(time.RFC3339), r.ttl).Err(); err != nil {
		return nil, fmt.Errorf("set worker key: %w", err)
	}
	slog.InfoContext(ctx, "worker registered", "worker", workerID, "ttl", r.ttl)

	heartbeatCtx, stop := context.WithCancel(context.Background())
	go func() {
		t := time.NewTicker(r.refresh)
		defer t.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ctx.Done():
				return
			case <-t.C:
				if err := r.rdb.Expire(heartbeatCtx, key, r.ttl).Err(); err != nil {
					slog.WarnContext(heartbeatCtx, "worker heartbeat refresh failed",
						"worker", workerID,
						"error", err,
					)
				}
			}
		}
	}()

	return func() {
		stop()
		delCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := r.rdb.Del(delCtx, key).Err(); err != nil {
			slog.WarnContext(delCtx, "worker key delete failed",
				"worker", workerID,
				"error", err,
			)
		} else {
			slog.InfoContext(delCtx, "worker unregistered", "worker", workerID)
		}
	}, nil
}

func (r *Registry) SweepStale(ctx context.Context, projectsRoot, currentWorkerID string) error {
	entries, err := os.ReadDir(projectsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read projects root: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		if id == currentWorkerID {
			continue
		}
		exists, err := r.rdb.Exists(ctx, keyPrefix+id).Result()
		if err != nil {
			slog.WarnContext(ctx, "sweep exists check failed",
				"worker", id,
				"error", err,
			)
			continue
		}
		if exists > 0 {
			continue
		}
		dir := filepath.Join(projectsRoot, id)
		slog.InfoContext(ctx, "sweeping stale worker dir", "worker", id, "path", dir)
		if err := os.RemoveAll(dir); err != nil {
			slog.WarnContext(ctx, "sweep remove failed",
				"worker", id,
				"path", dir,
				"error", err,
			)
		}
	}
	return nil
}

func RemoveWorkerDir(projectsRoot, workerID string) error {
	if projectsRoot == "" || workerID == "" {
		return nil
	}
	dir := filepath.Join(projectsRoot, workerID)
	return os.RemoveAll(dir)
}
