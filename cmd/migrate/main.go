package main

import (
	"log/slog"

	"github.com/Orfeo42/ai-orchestration/internal/db"
	"github.com/Orfeo42/ai-orchestration/internal/runtime"
)

func main() {
	runtime.InitLogger(slog.LevelInfo)
	ctx, stop := runtime.ShutdownContext()
	defer stop()

	d, err := db.Open(ctx, runtime.MustEnv("DATABASE_URL"))
	if err != nil {
		runtime.Die("database", err)
	}
	err = db.Migrate(ctx, d)
	runtime.CloseAll("database", d)
	if err != nil {
		runtime.Die("migrate", err)
	}
	slog.InfoContext(ctx, "migrations applied")
}
