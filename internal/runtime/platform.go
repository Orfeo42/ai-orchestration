package runtime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/riverqueue/river"

	"github.com/Orfeo42/ai-orchestration/internal/db"
	"github.com/Orfeo42/ai-orchestration/internal/engine"
	"github.com/Orfeo42/ai-orchestration/internal/plugins"
	pluginstore "github.com/Orfeo42/ai-orchestration/internal/plugins/postgres"
	"github.com/Orfeo42/ai-orchestration/internal/process/postgres"
	"github.com/Orfeo42/ai-orchestration/internal/step"
)

const pluginHTTPTimeout = 2 * time.Minute

type Plugins struct {
	Store   *pluginstore.Store
	Clients *plugins.Clients
	Signer  *plugins.Signer
	Catalog *plugins.Catalog
}

type Platform struct {
	DB      *db.DB
	Store   *postgres.Store
	River   *river.Client[*sql.Tx]
	Queue   *engine.RiverQueue
	Engine  *engine.Engine
	Plugins Plugins
}

type PluginConfig struct {
	PublicURL      string
	CallbackSecret string
}

type PlatformConfig struct {
	DSN      string
	Registry *step.Registry
	Plugins  PluginConfig
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
	pl := newPlugins(d, cfg)
	if err := pl.Catalog.Refresh(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("load plugins: %w", err), d.Close())
	}
	store := postgres.New(d.SQL)
	q := engine.NewRiverQueue(client)
	engCfg := cfg.Engine
	engCfg.Jobs = pl.Clients
	return &Platform{
		DB:      d,
		Store:   store,
		River:   client,
		Queue:   q,
		Engine:  engine.New(store, pl.Catalog, q, cfg.Control, engCfg),
		Plugins: pl,
	}, nil
}

func newPlugins(d *db.DB, cfg PlatformConfig) Plugins {
	store := pluginstore.New(d.SQL)
	clients := plugins.NewClients(plugins.EnvTokens, &http.Client{Timeout: pluginHTTPTimeout})
	signer := plugins.NewSigner([]byte(cfg.Plugins.CallbackSecret), cfg.Plugins.PublicURL)
	catalog := plugins.NewCatalog(cfg.Registry, store, clients, signer)
	clients.Bind(catalog)
	return Plugins{Store: store, Clients: clients, Signer: signer, Catalog: catalog}
}

func (p *Platform) RefreshPlugins(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if err := p.Plugins.Catalog.Refresh(ctx); err != nil {
			slog.ErrorContext(ctx, "refresh plugin catalog failed", slog.Any("error", err))
		}
	}
}

func (p *Platform) Close() error {
	return p.DB.Close()
}
