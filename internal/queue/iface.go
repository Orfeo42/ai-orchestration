package queue

import (
	"context"
	"time"
)

//go:generate moq -rm -out mock.go . Queue:MockQueue

const (
	QueueAssess = "queue:assess"
	QueueImpl   = "queue:impl"
	QueueReview = "queue:review"
)

type Queue interface {
	Push(ctx context.Context, queueName string, v any) error
	Pop(ctx context.Context, queueName string, timeout time.Duration) ([]byte, error)
	IsProcessed(ctx context.Context, key string) (bool, error)
	MarkProcessed(ctx context.Context, key string) error
	MarkInProgress(ctx context.Context, key string, ttl time.Duration) error
	IsInProgress(ctx context.Context, key string) (bool, error)
	ClearInProgress(ctx context.Context, key string) error
}
