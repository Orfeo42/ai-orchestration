package main

import (
	"log/slog"
	"os"

	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/runtime"
)

func main() {
	runtime.InitLogger(slog.LevelInfo)

	addr := runtime.EnvOr("LISTEN_ADDR", "127.0.0.1:8080")
	redisURL := runtime.EnvOr("REDIS_URL", "redis://redis:6379")

	ctx, stop := runtime.ShutdownContext()
	defer stop()

	store, err := runtime.NewStore(redisURL)
	if err != nil {
		runtime.Die("runstore", err)
	}
	defer runtime.CloseAll("runstore", store)

	ctrl, err := runtime.NewControl(redisURL)
	if err != nil {
		runtime.Die("control", err)
	}
	defer runtime.CloseAll("control", ctrl)

	rq, err := runtime.NewQueue(redisURL)
	if err != nil {
		runtime.Die("queue", err)
	}
	defer runtime.CloseAll("queue", rq)

	var fj forgejo.Forgejo
	forgejoURL := os.Getenv("FORGEJO_URL")
	forgejoToken := os.Getenv("FORGEJO_TOKEN")
	if forgejoURL != "" && forgejoToken != "" {
		fj = forgejo.New(forgejoURL, forgejoToken)
	} else {
		slog.Warn("FORGEJO_URL/FORGEJO_TOKEN not set; retry of legacy runs without job_payload will fail")
	}

	srv, err := newServer(addr, store, ctrl, rq, fj)
	if err != nil {
		runtime.Die("server", err)
	}

	if err := srv.run(ctx); err != nil {
		runtime.Die("serve", err)
	}
	slog.Info("webui shut down")
}
