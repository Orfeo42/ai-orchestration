package main

import (
	"log/slog"

	"github.com/riverqueue/river"

	"ai-orchestration/internal/analytics/postgres"
	"ai-orchestration/internal/engine"
	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/projects"
	"ai-orchestration/internal/runtime"
	"ai-orchestration/internal/step"
	"ai-orchestration/internal/steps"
	"ai-orchestration/internal/trigger"
)

func main() {
	runtime.InitLogger(slog.LevelInfo)

	addr := runtime.EnvOr("LISTEN_ADDR", "127.0.0.1:8080")
	redisURL := runtime.EnvOr("REDIS_URL", "redis://redis:6379")
	projectsPath := runtime.EnvOr("PROJECTS_CONFIG", "/config/projects.json")
	dsn := runtime.MustEnv("DATABASE_URL")
	forgejoURL := runtime.MustEnv("FORGEJO_URL")
	forgejoToken := runtime.MustEnv("FORGEJO_TOKEN")

	ctx, stop := runtime.ShutdownContext()
	defer stop()

	events, err := runtime.NewEventStore(redisURL)
	if err != nil {
		runtime.Die("events", err)
	}
	defer runtime.CloseAll("events", events)

	ctrl, err := runtime.NewControl(redisURL)
	if err != nil {
		runtime.Die("control", err)
	}
	defer runtime.CloseAll("control", ctrl)

	projectCfgs, err := projects.Load(projectsPath)
	if err != nil {
		runtime.Die("projects", err)
	}

	reg := step.NewRegistry()
	if err := steps.RegisterAll(reg, steps.Deps{}); err != nil {
		runtime.Die("steps", err)
	}

	p, err := runtime.OpenPlatform(ctx, runtime.PlatformConfig{
		DSN:      dsn,
		Registry: reg,
		Control:  ctrl,
		Engine:   engine.Config{WorkerID: "webui"},
		River:    &river.Config{},
	})
	if err != nil {
		runtime.Die("platform", err)
	}
	defer runtime.CloseAll("platform", p)

	fj := forgejo.New(forgejoURL, forgejoToken)

	proc := &processHandler{
		reader:  p.Store,
		engine:  p.Engine,
		starter: trigger.New(fj, p.Store, p.Engine, reg, trigger.Config{ForgejoURL: forgejoURL, Projects: projectCfgs}),
		stats:   postgres.New(p.DB.SQL),
		events:  events,
		ctrl:    ctrl,
		fj:      fj,
	}

	srv, err := newServer(addr, proc)
	if err != nil {
		runtime.Die("server", err)
	}

	if err := srv.run(ctx); err != nil {
		runtime.Die("serve", err)
	}
	slog.Info("webui shut down")
}
