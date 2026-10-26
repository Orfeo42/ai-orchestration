package postgres

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"

	"github.com/Orfeo42/ai-orchestration/internal/process"
)

const workflowVersionColumns = `id, name, hash, definition, source_yaml, created_at`

const qualifiedVersionColumns = `v.id, v.name, v.hash, v.definition, v.source_yaml, v.created_at`

type workflowPointerParams struct {
	Name      string `db:"name"`
	VersionID int64  `db:"version_id"`
}

type upsertVersionParams struct {
	Name       string `db:"name"`
	Hash       string `db:"hash"`
	Definition string `db:"definition"`
	SourceYAML string `db:"source_yaml"`
}

type versionIDParams struct {
	ID int64 `db:"id"`
}

type versionNameParams struct {
	Name string `db:"name"`
}

type noParams struct{}

func (s *queries) UpsertWorkflowVersion(ctx context.Context, v process.WorkflowVersion) (process.WorkflowVersion, error) {
	rows, err := sqlx.NamedQueryContext(ctx, s.q, `
		INSERT INTO workflow_version (name, hash, definition, source_yaml)
		VALUES (:name, :hash, CAST(:definition AS jsonb), :source_yaml)
		ON CONFLICT (name, hash) DO UPDATE SET name = EXCLUDED.name
		RETURNING `+workflowVersionColumns,
		upsertVersionParams{Name: v.Name, Hash: v.Hash, Definition: string(v.Definition), SourceYAML: v.SourceYAML})
	if err != nil {
		return process.WorkflowVersion{}, fmt.Errorf("upsert workflow version %s: %w", v.Name, err)
	}
	var found []workflowVersionRow
	if err := sqlx.StructScan(rows, &found); err != nil {
		return process.WorkflowVersion{}, fmt.Errorf("upsert workflow version %s: %w", v.Name, err)
	}
	if len(found) == 0 {
		return process.WorkflowVersion{}, fmt.Errorf("upsert workflow version %s: %w", v.Name, process.ErrNotFound)
	}
	return toWorkflowVersion(found[0]), nil
}

func (s *queries) GetWorkflowVersion(ctx context.Context, id int64) (process.WorkflowVersion, error) {
	rows, err := sqlx.NamedQueryContext(ctx, s.q, `SELECT `+workflowVersionColumns+` FROM workflow_version WHERE id = :id`,
		versionIDParams{ID: id})
	if err != nil {
		return process.WorkflowVersion{}, fmt.Errorf("get workflow version %d: %w", id, err)
	}
	var found []workflowVersionRow
	if err := sqlx.StructScan(rows, &found); err != nil {
		return process.WorkflowVersion{}, fmt.Errorf("get workflow version %d: %w", id, err)
	}
	if len(found) == 0 {
		return process.WorkflowVersion{}, fmt.Errorf("get workflow version %d: %w", id, process.ErrNotFound)
	}
	return toWorkflowVersion(found[0]), nil
}

func (s *queries) LatestWorkflowVersions(ctx context.Context) ([]process.WorkflowVersion, error) {
	rows, err := sqlx.NamedQueryContext(ctx, s.q, `
		SELECT `+qualifiedVersionColumns+`
		FROM workflow w
		JOIN workflow_version v ON v.id = w.current_version_id
		ORDER BY w.name`, noParams{})
	if err != nil {
		return nil, fmt.Errorf("latest workflow versions: %w", err)
	}
	var found []workflowVersionRow
	if err := sqlx.StructScan(rows, &found); err != nil {
		return nil, fmt.Errorf("latest workflow versions: %w", err)
	}
	return toWorkflowVersions(found), nil
}

func (s *queries) ListWorkflowVersions(ctx context.Context, name string) ([]process.WorkflowVersion, error) {
	rows, err := sqlx.NamedQueryContext(ctx, s.q, `
		SELECT `+workflowVersionColumns+` FROM workflow_version
		WHERE name = :name
		ORDER BY id DESC`, versionNameParams{Name: name})
	if err != nil {
		return nil, fmt.Errorf("list workflow versions %s: %w", name, err)
	}
	var found []workflowVersionRow
	if err := sqlx.StructScan(rows, &found); err != nil {
		return nil, fmt.Errorf("list workflow versions %s: %w", name, err)
	}
	return toWorkflowVersions(found), nil
}

func (s *queries) SetCurrentWorkflow(ctx context.Context, name string, versionID int64) error {
	_, err := sqlx.NamedExecContext(ctx, s.q, `
		INSERT INTO workflow (name, current_version_id, updated_at)
		VALUES (:name, :version_id, now())
		ON CONFLICT (name) DO UPDATE SET current_version_id = EXCLUDED.current_version_id, updated_at = now()`,
		workflowPointerParams{Name: name, VersionID: versionID})
	if err != nil {
		return fmt.Errorf("set current workflow %s: %w", name, err)
	}
	return nil
}

func (s *queries) SeedWorkflow(ctx context.Context, name string, versionID int64) (bool, error) {
	res, err := sqlx.NamedExecContext(ctx, s.q, `
		INSERT INTO workflow (name, current_version_id)
		VALUES (:name, :version_id)
		ON CONFLICT (name) DO NOTHING`,
		workflowPointerParams{Name: name, VersionID: versionID})
	if err != nil {
		return false, fmt.Errorf("seed workflow %s: %w", name, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("seed workflow %s rows affected: %w", name, err)
	}
	return n > 0, nil
}

func (s *queries) DeleteWorkflow(ctx context.Context, name string) (bool, error) {
	res, err := sqlx.NamedExecContext(ctx, s.q, `DELETE FROM workflow WHERE name = :name`, versionNameParams{Name: name})
	if err != nil {
		return false, fmt.Errorf("delete workflow %s: %w", name, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete workflow %s rows affected: %w", name, err)
	}
	return n > 0, nil
}
