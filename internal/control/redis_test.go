package control

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestChannel(t *testing.T) *RedisChannel {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewRedisFromClient(client)
}

func recvWithTimeout(t *testing.T, ch <-chan Message) Message {
	t.Helper()
	select {
	case m := <-ch:
		return m
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ctrl msg")
		return Message{}
	}
}

func TestRedisChannelCancelRoundTrip(t *testing.T) {
	t.Run("publish then receive cancel", func(t *testing.T) {
		c := newTestChannel(t)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		sub, err := c.Subscribe(ctx, "run-1")
		require.NoError(t, err)
		defer sub.Close()

		require.NoError(t, c.Cancel(ctx, "run-1"))
		got := recvWithTimeout(t, sub.Messages())
		assert.Equal(t, TypeCancel, got.Type)
		assert.Empty(t, got.Note)
	})
}

func TestRedisChannelInjectRoundTrip(t *testing.T) {
	t.Run("publish then receive inject with note", func(t *testing.T) {
		c := newTestChannel(t)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		sub, err := c.Subscribe(ctx, "run-2")
		require.NoError(t, err)
		defer sub.Close()

		require.NoError(t, c.Inject(ctx, "run-2", "also update README"))
		got := recvWithTimeout(t, sub.Messages())
		assert.Equal(t, TypeInject, got.Type)
		assert.Equal(t, "also update README", got.Note)
	})
}

func TestRedisChannelChannelClose(t *testing.T) {
	t.Run("channel closes on subscription Close", func(t *testing.T) {
		c := newTestChannel(t)
		ctx := context.Background()
		sub, err := c.Subscribe(ctx, "run-3")
		require.NoError(t, err)

		require.NoError(t, sub.Close())

		select {
		case _, ok := <-sub.Messages():
			assert.False(t, ok)
		case <-time.After(2 * time.Second):
			t.Fatal("messages channel did not close after Close()")
		}
	})
}
