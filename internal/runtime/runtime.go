package runtime

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"ai-orchestration/internal/control"
	"ai-orchestration/internal/runstore"
)

func InitLogger(level slog.Level) {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
}

func ShutdownContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}

func ConfigureGit(name, email string) error {
	if err := exec.Command("git", "config", "--global", "user.name", name).Run(); err != nil {
		return fmt.Errorf("git config user.name: %w", err)
	}
	if err := exec.Command("git", "config", "--global", "user.email", email).Run(); err != nil {
		return fmt.Errorf("git config user.email: %w", err)
	}
	return nil
}

func NewEventStore(url string) (*runstore.RedisEvents, error) {
	return runstore.NewRedisEvents(url)
}

func NewControl(url string) (*control.RedisChannel, error) {
	return control.NewRedis(url)
}

func MustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("required env var missing",
			"key", key,
		)
		os.Exit(1)
	}
	return v
}

func EnvOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func CloseAll(label string, closers ...io.Closer) {
	for _, c := range closers {
		if c == nil {
			continue
		}
		if err := c.Close(); err != nil {
			slog.Error("close failed",
				"component", label,
				"error", err,
			)
		}
	}
}

func Die(component string, err error) {
	slog.Error("fatal",
		"component", component,
		"error", err,
	)
	os.Exit(1)
}
