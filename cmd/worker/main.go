package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/riverqueue/river"

	"ai-orchestration/internal/agent"
	"ai-orchestration/internal/engine"
	"ai-orchestration/internal/forgejo"
	"ai-orchestration/internal/gemini"
	"ai-orchestration/internal/gitops"
	"ai-orchestration/internal/memory"
	"ai-orchestration/internal/pipeline/impl"
	"ai-orchestration/internal/runstore"
	"ai-orchestration/internal/runtime"
	"ai-orchestration/internal/step"
	"ai-orchestration/internal/steps"
	"ai-orchestration/internal/testrunner"
	"ai-orchestration/internal/workerreg"
)

const (
	errRiverStopped configError = "river client stopped unexpectedly"

	stopGrace     = 30 * time.Second
	stopCancelMax = 10 * time.Second
)

func main() {
	queuesFlag := flag.String("queues", "claude,default", "comma-separated queues to work: claude,default")
	flag.Parse()

	runtime.InitLogger(slog.LevelInfo)

	queues, err := parseQueues(*queuesFlag)
	if err != nil {
		runtime.Die("queues", err)
	}
	cfg, err := loadConfig(queues)
	if err != nil {
		runtime.Die("config", err)
	}
	if err := runtime.ConfigureGit(cfg.gitUserName, cfg.gitUserEmail); err != nil {
		runtime.Die("git-config", err)
	}

	ctx, stop := runtime.ShutdownContext()
	defer stop()

	if err := run(ctx, cfg); err != nil {
		runtime.Die("worker", err)
	}
}

func run(ctx context.Context, cfg config) error {
	claudeEnabled := slices.Contains(cfg.queues, step.QueueClaude)
	if claudeEnabled {
		if err := memory.Sync(ctx, cfg.memory); err != nil {
			return fmt.Errorf("memory sync: %w", err)
		}
	}

	store, err := runtime.NewEventStore(cfg.redisURL)
	if err != nil {
		return fmt.Errorf("runstore: %w", err)
	}
	defer runtime.CloseAll("runstore", store)

	ctrl, err := runtime.NewControl(cfg.redisURL)
	if err != nil {
		return fmt.Errorf("control: %w", err)
	}
	defer runtime.CloseAll("control", ctrl)

	registry, err := workerreg.New(cfg.redisURL)
	if err != nil {
		return fmt.Errorf("workerreg: %w", err)
	}
	defer runtime.CloseAll("workerreg", registry)

	if err := registry.SweepStale(ctx, cfg.projectsRoot, cfg.workerID); err != nil {
		slog.WarnContext(ctx, "sweep stale worker dirs failed",
			slog.Any("error", err),
		)
	}
	unregister, err := registry.Register(ctx, cfg.workerID)
	if err != nil {
		return fmt.Errorf("workerreg register: %w", err)
	}
	defer func() {
		unregister()
		if err := workerreg.RemoveWorkerDir(cfg.projectsRoot, cfg.workerID); err != nil {
			slog.Warn("remove worker dir on shutdown failed",
				slog.String("worker", cfg.workerID),
				slog.Any("error", err),
			)
		}
	}()

	deps := steps.Deps{
		Forgejo:      forgejo.New(cfg.forgejoURL, cfg.forgejoToken),
		GitFactory:   gitops.DefaultFactory,
		ForgejoURL:   cfg.forgejoURL,
		ForgejoToken: cfg.forgejoToken,
		Projects:     cfg.projects,
		ProjectsRoot: cfg.projectsRoot,
		TestRunner:   testrunner.Run,
	}
	if claudeEnabled {
		closeGemini, err := addClaudeDeps(ctx, cfg, &deps)
		if err != nil {
			return err
		}
		defer closeGemini()
	}

	reg := step.NewRegistry()
	if err := steps.RegisterAll(reg, deps); err != nil {
		return fmt.Errorf("register steps: %w", err)
	}

	workers := river.NewWorkers()
	queueConfigs := map[string]river.QueueConfig{}
	for _, name := range cfg.queues {
		maxWorkers := cfg.defaultMaxWorkers
		if name == step.QueueClaude {
			maxWorkers = 1
		}
		queueConfigs[name] = river.QueueConfig{MaxWorkers: maxWorkers}
	}

	p, err := runtime.OpenPlatform(ctx, runtime.PlatformConfig{
		DSN:      cfg.databaseURL,
		Registry: reg,
		Control:  ctrl,
		Engine:   engine.Config{WorkerID: cfg.workerID, DefaultTimeout: cfg.stepTimeout},
		River: &river.Config{
			Queues:               queueConfigs,
			Workers:              workers,
			RescueStuckJobsAfter: cfg.rescueAfter,
			JobTimeout:           cfg.stepTimeout,
		},
	})
	if err != nil {
		return fmt.Errorf("open platform: %w", err)
	}
	defer runtime.CloseAll("platform", p)

	handles := func(ctx context.Context, id string) *runstore.RunHandle {
		return runstore.NewStepHandle(ctx, store, p.Store, ctrl, id, runstore.ServiceOrchestrator)
	}
	hooks := engine.Hooks{BeforeStep: func(ctx context.Context, def step.Def) {
		if def.Queue != step.QueueClaude {
			return
		}
		err := memory.Sync(ctx, cfg.memory)
		if err == nil {
			return
		}
		if errors.Is(err, memory.ErrUpstream) {
			slog.WarnContext(ctx, "memory refresh failed, using last synced memory",
				slog.Any("error", err),
			)
			return
		}
		slog.ErrorContext(ctx, "memory refresh failed, using last synced memory",
			slog.Any("error", err),
		)
	}}
	if err := river.AddWorkerSafely(workers, engine.NewStepWorker(p.Engine, p.Queue, handles, hooks)); err != nil {
		return fmt.Errorf("add step worker: %w", err)
	}

	if err := p.River.Start(context.WithoutCancel(ctx)); err != nil {
		return fmt.Errorf("start river: %w", err)
	}
	slog.InfoContext(ctx, "worker started",
		slog.Any("queues", cfg.queues),
		slog.String("worker_id", cfg.workerID),
	)

	select {
	case <-ctx.Done():
	case <-p.River.Stopped():
		return errRiverStopped
	}
	return stopRiver(p.River)
}

func stopRiver(client *river.Client[*sql.Tx]) error {
	grace, cancelGrace := context.WithTimeout(context.Background(), stopGrace)
	defer cancelGrace()
	err := client.Stop(grace)
	if err == nil {
		return nil
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("stop river: %w", err)
	}
	slog.Warn("shutdown grace expired, cancelling running steps")
	hard, cancelHard := context.WithTimeout(context.Background(), stopCancelMax)
	defer cancelHard()
	if err := client.StopAndCancel(hard); err != nil {
		return fmt.Errorf("stop and cancel river: %w", err)
	}
	return nil
}

func addClaudeDeps(ctx context.Context, cfg config, deps *steps.Deps) (func(), error) {
	claudePW := agent.NewPRDescWriter(cfg.claude)
	claudeReviewer := agent.NewReviewer(cfg.claude, cfg.projectsRoot)
	claudeDocs := agent.NewDocsWriter(cfg.claude)

	deps.Claude = agent.New(cfg.claude)
	deps.PRWriter = claudePW
	deps.DocsWriter = claudeDocs
	deps.Reviewers = map[string]gemini.Reviewer{"claude": claudeReviewer, "gemini": claudeReviewer}
	if cfg.geminiAPIKey == nil {
		slog.InfoContext(ctx, "GEMINI_API_KEY not set, using claude for review, docs and pr description")
		return func() {}, nil
	}

	gemReviewer, err := gemini.NewReviewer(ctx, *cfg.geminiAPIKey)
	if err != nil {
		return nil, fmt.Errorf("gemini reviewer: %w", err)
	}
	gemDocs, err := gemini.NewDocsWriter(ctx, *cfg.geminiAPIKey)
	if err != nil {
		gemReviewer.Close()
		return nil, fmt.Errorf("gemini docs writer: %w", err)
	}
	deps.Reviewers["gemini"] = gemini.FallbackReviewer{Primary: gemReviewer, Secondary: claudeReviewer}
	deps.DocsWriter = gemini.FallbackDocsWriter{Primary: gemDocs, Secondary: claudeDocs}

	gemPW, err := gemini.NewPRDescWriter(ctx, *cfg.geminiAPIKey)
	if err != nil {
		slog.ErrorContext(ctx, "gemini pr desc writer init failed, using claude only",
			slog.Any("error", err),
		)
		return func() {
			gemReviewer.Close()
			gemDocs.Close()
		}, nil
	}
	deps.PRWriter = &impl.FallbackPRWriter{Primary: gemPW, Fallback: claudePW}
	return func() {
		gemReviewer.Close()
		gemDocs.Close()
		gemPW.Close()
	}, nil
}
