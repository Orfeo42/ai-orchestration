package queue

import (
	"context"
	"time"
)

//go:generate moq -out mock.go . Queue:MockQueue

type Queue interface {
	Push(ctx context.Context, queueName string, v any) error
	Pop(ctx context.Context, queueName string, timeout time.Duration) ([]byte, error)
	IsProcessed(ctx context.Context, key string) (bool, error)
	MarkProcessed(ctx context.Context, key string) error
	MarkInProgress(ctx context.Context, key string, ttl time.Duration) error
	ClearInProgress(ctx context.Context, key string) error
}
