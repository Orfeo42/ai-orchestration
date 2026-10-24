package control

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"
)

const channelFmt = "ctrl:%s"

type RedisChannel struct {
	rdb *redis.Client
}

func NewRedis(url string) (*RedisChannel, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	return &RedisChannel{rdb: redis.NewClient(opts)}, nil
}

func NewRedisFromClient(c *redis.Client) *RedisChannel {
	return &RedisChannel{rdb: c}
}

func (c *RedisChannel) Close() error {
	return c.rdb.Close()
}

func (c *RedisChannel) Cancel(ctx context.Context, runID string) error {
	return c.publish(ctx, runID, Message{Type: TypeCancel})
}

func (c *RedisChannel) Inject(ctx context.Context, runID, note string) error {
	return c.publish(ctx, runID, Message{Type: TypeInject, Note: note})
}

func (c *RedisChannel) publish(ctx context.Context, runID string, msg Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal ctrl msg: %w", err)
	}
	if err := c.rdb.Publish(ctx, fmt.Sprintf(channelFmt, runID), data).Err(); err != nil {
		return fmt.Errorf("publish ctrl msg: %w", err)
	}
	return nil
}

func (c *RedisChannel) Subscribe(ctx context.Context, runID string) (Subscription, error) {
	ps := c.rdb.Subscribe(ctx, fmt.Sprintf(channelFmt, runID))
	if _, err := ps.Receive(ctx); err != nil {
		_ = ps.Close()
		return nil, fmt.Errorf("subscribe: %w", err)
	}
	out := make(chan Message, 16)
	sub := &redisSubscription{ps: ps, out: out}
	go sub.loop(ctx, runID)
	return sub, nil
}

type redisSubscription struct {
	ps  *redis.PubSub
	out chan Message
}

func (s *redisSubscription) Messages() <-chan Message { return s.out }

func (s *redisSubscription) Close() error {
	err := s.ps.Close()
	return err
}

func (s *redisSubscription) loop(ctx context.Context, runID string) {
	defer close(s.out)
	ch := s.ps.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			var parsed Message
			if err := json.Unmarshal([]byte(msg.Payload), &parsed); err != nil {
				slog.WarnContext(ctx, "ctrl msg parse failed",
					"run", runID,
					"payload", msg.Payload,
					"error", err,
				)
				continue
			}
			select {
			case s.out <- parsed:
			case <-ctx.Done():
				return
			}
		}
	}
}
