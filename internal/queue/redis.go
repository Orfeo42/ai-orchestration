package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisQueue struct {
	rdb *redis.Client
}

func New(url string) (*RedisQueue, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	return &RedisQueue{rdb: redis.NewClient(opts)}, nil
}

func NewFromClient(client *redis.Client) *RedisQueue {
	return &RedisQueue{rdb: client}
}

func (q *RedisQueue) Push(ctx context.Context, queueName string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	slog.Debug("pushing to queue", "queue", queueName)
	return q.rdb.LPush(ctx, queueName, data).Err()
}

func (q *RedisQueue) Pop(ctx context.Context, queueName string, timeout time.Duration) ([]byte, error) {
	result, err := q.rdb.BRPop(ctx, timeout, queueName).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("brpop: %w", err)
	}
	slog.Debug("popped from queue", "queue", queueName)
	return []byte(result[1]), nil
}

func (q *RedisQueue) IsProcessed(ctx context.Context, key string) (bool, error) {
	result, err := q.rdb.SIsMember(ctx, "processed", key).Result()
	if err != nil {
		return false, fmt.Errorf("sismember: %w", err)
	}
	return result, nil
}

func (q *RedisQueue) MarkProcessed(ctx context.Context, key string) error {
	slog.Info("marking issue processed", "key", key)
	return q.rdb.SAdd(ctx, "processed", key).Err()
}

func (q *RedisQueue) MarkInProgress(ctx context.Context, key string, ttl time.Duration) error {
	slog.Info("marking issue in-progress", "key", key)
	return q.rdb.Set(ctx, "in_progress:"+key, "1", ttl).Err()
}

func (q *RedisQueue) IsInProgress(ctx context.Context, key string) (bool, error) {
	result, err := q.rdb.Exists(ctx, "in_progress:"+key).Result()
	if err != nil {
		return false, fmt.Errorf("exists: %w", err)
	}
	return result == 1, nil
}

func (q *RedisQueue) ClearInProgress(ctx context.Context, key string) error {
	slog.Info("clearing in-progress lock", "key", key)
	return q.rdb.Del(ctx, "in_progress:"+key).Err()
}

func (q *RedisQueue) Close() error {
	return q.rdb.Close()
}
