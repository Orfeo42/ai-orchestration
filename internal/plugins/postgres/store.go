package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/Orfeo42/ai-orchestration/internal/plugins"
	"github.com/Orfeo42/ai-orchestration/pluginapi"
)

var _ plugins.Store = (*Store)(nil)

const pluginColumns = `name, base_url, manifest, status, error, checked_at, created_at, updated_at`

type Store struct {
	db sqlx.ExtContext
}

func New(db sqlx.ExtContext) *Store {
	return &Store{db: db}
}

type pluginRow struct {
	Name      string    `db:"name"`
	BaseURL   string    `db:"base_url"`
	Manifest  string    `db:"manifest"`
	Status    string    `db:"status"`
	Error     string    `db:"error"`
	CheckedAt time.Time `db:"checked_at"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

type upsertParams struct {
	Name      string    `db:"name"`
	BaseURL   string    `db:"base_url"`
	Manifest  string    `db:"manifest"`
	Status    string    `db:"status"`
	Error     string    `db:"error"`
	CheckedAt time.Time `db:"checked_at"`
}

type nameParams struct {
	Name string `db:"name"`
}

type noParams struct{}

func toRecord(row pluginRow) (plugins.Record, error) {
	var manifest pluginapi.Manifest
	if err := json.Unmarshal([]byte(row.Manifest), &manifest); err != nil {
		return plugins.Record{}, fmt.Errorf("decode manifest of plugin %s: %w", row.Name, err)
	}
	return plugins.Record{
		Name:      row.Name,
		BaseURL:   row.BaseURL,
		Manifest:  manifest,
		Status:    plugins.Status(row.Status),
		Error:     row.Error,
		CheckedAt: row.CheckedAt,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}, nil
}

func (s *Store) UpsertPlugin(ctx context.Context, r plugins.Record) error {
	manifest, err := json.Marshal(r.Manifest)
	if err != nil {
		return fmt.Errorf("upsert plugin %s: encode manifest: %w", r.Name, err)
	}
	_, err = sqlx.NamedExecContext(ctx, s.db, `
		INSERT INTO plugin (name, base_url, manifest, status, error, checked_at)
		VALUES (:name, :base_url, CAST(:manifest AS jsonb), :status, :error, :checked_at)
		ON CONFLICT (name) DO UPDATE SET
			base_url = EXCLUDED.base_url,
			manifest = EXCLUDED.manifest,
			status = EXCLUDED.status,
			error = EXCLUDED.error,
			checked_at = EXCLUDED.checked_at,
			updated_at = now()`,
		upsertParams{
			Name:      r.Name,
			BaseURL:   r.BaseURL,
			Manifest:  string(manifest),
			Status:    string(r.Status),
			Error:     r.Error,
			CheckedAt: r.CheckedAt,
		})
	if err != nil {
		return fmt.Errorf("upsert plugin %s: %w", r.Name, err)
	}
	return nil
}

func (s *Store) GetPlugin(ctx context.Context, name string) (plugins.Record, error) {
	rows, err := sqlx.NamedQueryContext(ctx, s.db, `SELECT `+pluginColumns+` FROM plugin WHERE name = :name`,
		nameParams{Name: name})
	if err != nil {
		return plugins.Record{}, fmt.Errorf("get plugin %s: %w", name, err)
	}
	var found []pluginRow
	if err := sqlx.StructScan(rows, &found); err != nil {
		return plugins.Record{}, fmt.Errorf("get plugin %s: %w", name, err)
	}
	if len(found) == 0 {
		return plugins.Record{}, fmt.Errorf("get plugin %s: %w", name, plugins.ErrNotFound)
	}
	return toRecord(found[0])
}

func (s *Store) ListPlugins(ctx context.Context) ([]plugins.Record, error) {
	rows, err := sqlx.NamedQueryContext(ctx, s.db, `SELECT `+pluginColumns+` FROM plugin ORDER BY name`, noParams{})
	if err != nil {
		return nil, fmt.Errorf("list plugins: %w", err)
	}
	var found []pluginRow
	if err := sqlx.StructScan(rows, &found); err != nil {
		return nil, fmt.Errorf("list plugins: %w", err)
	}
	records := make([]plugins.Record, 0, len(found))
	for _, row := range found {
		record, err := toRecord(row)
		if err != nil {
			return nil, fmt.Errorf("list plugins: %w", err)
		}
		records = append(records, record)
	}
	return records, nil
}

func (s *Store) DeletePlugin(ctx context.Context, name string) (bool, error) {
	res, err := sqlx.NamedExecContext(ctx, s.db, `DELETE FROM plugin WHERE name = :name`, nameParams{Name: name})
	if err != nil {
		return false, fmt.Errorf("delete plugin %s: %w", name, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete plugin %s rows affected: %w", name, err)
	}
	return n > 0, nil
}
