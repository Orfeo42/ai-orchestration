package runtime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/riverqueue/river"

	"github.com/Orfeo42/ai-orchestration/internal/db"
	"github.com/Orfeo42/ai-orchestration/internal/engine"
	"github.com/Orfeo42/ai-orchestration/internal/process/postgres"
	"github.com/Orfeo42/ai-orchestration/internal/step"
)

type Platform struct {
	DB     *db.DB
	Store  *postgres.Store
	River  *river.Client[*sql.Tx]
	Queue  *engine.RiverQueue
	Engine *engine.Engine
}

type PlatformConfig struct {
	DSN      string
	Registry *step.Registry
	Control  engine.Control
	Engine   engine.Config
	River    *river.Config
}

func OpenPlatform(ctx context.Context, cfg PlatformConfig) (*Platform, error) {
	d, err := db.Open(ctx, cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	riverCfg := cfg.River
	if riverCfg == nil {
		riverCfg = &river.Config{}
	}
	client, err := river.NewClient(d.RiverDriver(), riverCfg)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("river client: %w", err), d.Close())
	}
	store := postgres.New(d.SQL)
	q := engine.NewRiverQueue(client)
	return &Platform{
		DB:     d,
		Store:  store,
		River:  client,
		Queue:  q,
		Engine: engine.New(store, cfg.Registry, q, cfg.Control, cfg.Engine),
	}, nil
}

func (p *Platform) Close() error {
	return p.DB.Close()
}
