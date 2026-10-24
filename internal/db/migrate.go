package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/rivermigrate"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Migrate(ctx context.Context, d *DB) error {
	riverMigrator, err := rivermigrate.New(d.RiverDriver(), nil)
	if err != nil {
		return fmt.Errorf("create river migrator: %w", err)
	}
	riverResult, err := riverMigrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return fmt.Errorf("migrate river: %w", err)
	}

	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("open migrations fs: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, d.SQL.DB, sub)
	if err != nil {
		return fmt.Errorf("create goose provider: %w", err)
	}
	gooseResults, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("migrate goose: %w", err)
	}

	slog.InfoContext(ctx, "database migrated",
		slog.Int("river_applied", len(riverResult.Versions)),
		slog.Int("app_applied", len(gooseResults)),
	)
	return nil
}
