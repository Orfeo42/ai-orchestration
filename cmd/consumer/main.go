package main

import (
	"context"
	"flag"
	"log/slog"

	"ai-orchestration/internal/agent"
	"ai-orchestration/internal/control"
	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/queue"
	"ai-orchestration/internal/runstore"
	"ai-orchestration/internal/runtime"
)

type consumerDeps struct {
	cfg    config
	rq     queue.Queue
	store  runstore.Store
	ctrl   control.Channel
	fj     forgejo.Forgejo
	claude agent.Agent
}

func main() {
	pipelineFlag := flag.String("pipeline", "", "pipeline to run: assess|impl|review")
	flag.Parse()

	runtime.InitLogger(slog.LevelInfo)

	cfg, err := loadConfig()
	if err != nil {
		runtime.Die("config", err)
	}
	if err := runtime.ConfigureGit(cfg.gitUserName, cfg.gitUserEmail); err != nil {
		runtime.Die("git-config", err)
	}

	ctx, stop := runtime.ShutdownContext()
	defer stop()

	rq, err := runtime.NewQueue(cfg.redisURL)
	if err != nil {
		runtime.Die("queue", err)
	}
	defer runtime.CloseAll("queue", rq)

	store, err := runtime.NewStore(cfg.redisURL)
	if err != nil {
		runtime.Die("runstore", err)
	}
	defer runtime.CloseAll("runstore", store)

	ctrl, err := runtime.NewControl(cfg.redisURL)
	if err != nil {
		runtime.Die("control", err)
	}
	defer runtime.CloseAll("control", ctrl)

	fj := forgejo.New(cfg.forgejoURL, cfg.forgejoToken)

	deps := consumerDeps{
		cfg:   cfg,
		rq:    rq,
		store: store,
		ctrl:  ctrl,
		fj:    fj,
	}

	switch *pipelineFlag {
	case "assess":
		deps.claude = agent.New(cfg.claudeTimeout)
		runAssessConsumer(ctx, deps)
	case "impl":
		deps.claude = agent.New(cfg.claudeTimeout)
		runImplConsumer(ctx, deps)
	case "review":
		runReviewConsumer(ctx, deps)
	default:
		runtime.Die("pipeline", errInvalidPipeline(*pipelineFlag))
	}
}

type pipelineErr string

func (e pipelineErr) Error() string { return string(e) }

func errNotYetImplemented(name string) error {
	return pipelineErr("pipeline " + name + " not yet implemented")
}

func errInvalidPipeline(name string) error {
	if name == "" {
		return pipelineErr("--pipeline flag is required (assess|impl|review)")
	}
	return pipelineErr("invalid --pipeline value: " + name)
}

var _ = context.Background
