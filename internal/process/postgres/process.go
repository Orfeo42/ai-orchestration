package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/Orfeo42/ai-orchestration/internal/process"
)

const (
	defaultPageSize = 50
	maxPageSize     = 200
	zeroUUID        = "00000000-0000-0000-0000-000000000000"
)

const processColumns = `p.id, p.workflow_version_id, p.workflow_name, p.subject_kind, p.subject_key, p.subject_url,
	p.title, p.project, p.status, p.current_step, p.current_job_id, p.vars, p.waiting_for, p.error, p.error_kind,
	p.created_at, p.updated_at, p.started_at, p.ended_at`

const usageColumns = `u.usage_input, u.usage_output, u.usage_cache_creation, u.usage_cache_read`

const usageJoin = `LEFT JOIN LATERAL (
	SELECT
		CAST(COALESCE(SUM(s.tokens_input), 0) AS bigint) AS usage_input,
		CAST(COALESCE(SUM(s.tokens_output), 0) AS bigint) AS usage_output,
		CAST(COALESCE(SUM(s.tokens_cache_creation), 0) AS bigint) AS usage_cache_creation,
		CAST(COALESCE(SUM(s.tokens_cache_read), 0) AS bigint) AS usage_cache_read
	FROM step_run s WHERE s.process_id = p.id
) u ON true`

type idParams struct {
	ID string `db:"id"`
}

type listParams struct {
	Status          string    `db:"status"`
	WorkflowName    string    `db:"workflow_name"`
	Project         string    `db:"project"`
	HasCursor       bool      `db:"has_cursor"`
	CursorCreatedAt time.Time `db:"c_created_at"`
	CursorID        string    `db:"c_id"`
	Limit           int       `db:"limit"`
}

type waitingParams struct {
	Kind string `db:"kind"`
}

type nextParams struct {
	ID    string   `db:"id"`
	Step  string   `db:"step"`
	JobID int64    `db:"job_id"`
	From  []string `db:"from"`
}

type waitParams struct {
	ID         string `db:"id"`
	WaitingFor string `db:"waiting_for"`
}

type finishParams struct {
	ID        string `db:"id"`
	Status    string `db:"status"`
	Error     string `db:"error"`
	ErrorKind string `db:"error_kind"`
}

type mergeVarsParams struct {
	ID   string `db:"id"`
	Vars string `db:"vars"`
}

func (s *queries) CreateProcess(ctx context.Context, p process.Process) (bool, error) {
	row, err := fromProcess(p)
	if err != nil {
		return false, fmt.Errorf("create process: %w", err)
	}
	res, err := sqlx.NamedExecContext(ctx, s.q, `
		INSERT INTO process (id, workflow_version_id, workflow_name, subject_kind, subject_key, subject_url,
			title, project, status, current_step, current_job_id, vars, waiting_for, error, error_kind)
		SELECT CAST(:id AS uuid), CAST(:workflow_version_id AS bigint), CAST(:workflow_name AS text),
			CAST(:subject_kind AS text), CAST(:subject_key AS text), CAST(:subject_url AS text),
			CAST(:title AS text), CAST(:project AS text), CAST(:status AS text), CAST(:current_step AS text),
			CAST(:current_job_id AS bigint), CAST(:vars AS jsonb), CAST(:waiting_for AS jsonb),
			CAST(:error AS text), CAST(:error_kind AS text)
		WHERE NOT EXISTS (
			SELECT 1 FROM process WHERE workflow_name = :workflow_name AND subject_key = :subject_key
		)
		ON CONFLICT DO NOTHING`, row)
	if err != nil {
		return false, fmt.Errorf("create process: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("create process rows affected: %w", err)
	}
	return n == 1, nil
}

func (s *queries) GetProcess(ctx context.Context, id string) (process.Process, error) {
	rows, err := sqlx.NamedQueryContext(ctx, s.q, `
		SELECT `+processColumns+`, `+usageColumns+`
		FROM process p `+usageJoin+`
		WHERE p.id = :id`, idParams{ID: id})
	if err != nil {
		return process.Process{}, fmt.Errorf("get process %s: %w", id, err)
	}
	var found []processRow
	if err := sqlx.StructScan(rows, &found); err != nil {
		return process.Process{}, fmt.Errorf("get process %s: %w", id, err)
	}
	if len(found) == 0 {
		return process.Process{}, fmt.Errorf("get process %s: %w", id, process.ErrNotFound)
	}
	return toProcess(found[0])
}

func (s *queries) LockProcess(ctx context.Context, id string) (process.Process, error) {
	rows, err := sqlx.NamedQueryContext(ctx, s.q, `
		SELECT `+processColumns+`
		FROM process p
		WHERE p.id = :id
		FOR UPDATE`, idParams{ID: id})
	if err != nil {
		return process.Process{}, fmt.Errorf("lock process %s: %w", id, err)
	}
	var found []processRow
	if err := sqlx.StructScan(rows, &found); err != nil {
		return process.Process{}, fmt.Errorf("lock process %s: %w", id, err)
	}
	if len(found) == 0 {
		return process.Process{}, fmt.Errorf("lock process %s: %w", id, process.ErrNotFound)
	}
	return toProcess(found[0])
}

func (s *queries) ListProcesses(ctx context.Context, filter process.ListFilter) (process.Page, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultPageSize
	}
	limit = min(limit, maxPageSize)
	params := listParams{
		Status:          string(filter.Status),
		WorkflowName:    filter.WorkflowName,
		Project:         filter.Project,
		CursorCreatedAt: time.Unix(0, 0).UTC(),
		CursorID:        zeroUUID,
		Limit:           limit + 1,
	}
	if filter.Cursor != nil {
		params.HasCursor = true
		params.CursorCreatedAt = filter.Cursor.CreatedAt
		params.CursorID = filter.Cursor.ID
	}
	rows, err := sqlx.NamedQueryContext(ctx, s.q, `
		SELECT `+processColumns+`, `+usageColumns+`
		FROM process p `+usageJoin+`
		WHERE (:status = '' OR p.status = :status)
		  AND (:workflow_name = '' OR p.workflow_name = :workflow_name)
		  AND (:project = '' OR p.project = :project)
		  AND (NOT :has_cursor OR (p.created_at, p.id) < (CAST(:c_created_at AS timestamptz), CAST(:c_id AS uuid)))
		ORDER BY p.created_at DESC, p.id DESC
		LIMIT :limit`, params)
	if err != nil {
		return process.Page{}, fmt.Errorf("list processes: %w", err)
	}
	var found []processRow
	if err := sqlx.StructScan(rows, &found); err != nil {
		return process.Page{}, fmt.Errorf("list processes: %w", err)
	}
	var next *process.Cursor
	if len(found) > limit {
		found = found[:limit]
		last := found[limit-1]
		next = &process.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	processes, err := toProcesses(found)
	if err != nil {
		return process.Page{}, fmt.Errorf("list processes: %w", err)
	}
	return process.Page{Processes: processes, Next: next}, nil
}

func (s *queries) ListWaiting(ctx context.Context, kind process.WaitKind) ([]process.Process, error) {
	rows, err := sqlx.NamedQueryContext(ctx, s.q, `
		SELECT `+processColumns+`, `+usageColumns+`
		FROM process p `+usageJoin+`
		WHERE p.status = 'waiting' AND p.waiting_for->>'kind' = :kind
		ORDER BY p.created_at, p.id`, waitingParams{Kind: string(kind)})
	if err != nil {
		return nil, fmt.Errorf("list waiting %s: %w", kind, err)
	}
	var found []processRow
	if err := sqlx.StructScan(rows, &found); err != nil {
		return nil, fmt.Errorf("list waiting %s: %w", kind, err)
	}
	return toProcesses(found)
}

func (s *queries) SetNext(ctx context.Context, processID string, next process.Next) (bool, error) {
	from := make([]string, 0, len(next.From))
	for _, st := range next.From {
		from = append(from, string(st))
	}
	res, err := sqlx.NamedExecContext(ctx, s.q, `
		UPDATE process
		SET status = 'running', current_step = :step, current_job_id = :job_id, waiting_for = NULL,
			error = '', error_kind = '', ended_at = NULL, updated_at = now()
		WHERE id = :id AND status = ANY(CAST(:from AS text[]))`,
		nextParams{ID: processID, Step: next.Step, JobID: next.JobID, From: from})
	if err != nil {
		return false, fmt.Errorf("set next %s: %w", processID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("set next %s rows affected: %w", processID, err)
	}
	return n == 1, nil
}

func (s *queries) SetWaiting(ctx context.Context, processID string, wait process.Wait) (bool, error) {
	raw, err := encodeWait(wait)
	if err != nil {
		return false, fmt.Errorf("set waiting %s: %w", processID, err)
	}
	res, err := sqlx.NamedExecContext(ctx, s.q, `
		UPDATE process
		SET status = 'waiting', waiting_for = CAST(:waiting_for AS jsonb), current_job_id = NULL, updated_at = now()
		WHERE id = :id AND status = 'running'`, waitParams{ID: processID, WaitingFor: raw})
	if err != nil {
		return false, fmt.Errorf("set waiting %s: %w", processID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("set waiting %s rows affected: %w", processID, err)
	}
	return n == 1, nil
}

func (s *queries) FinishProcess(ctx context.Context, processID string, finish process.Finish) (bool, error) {
	res, err := sqlx.NamedExecContext(ctx, s.q, `
		UPDATE process
		SET status = :status, error = :error, error_kind = :error_kind, ended_at = now(),
			current_job_id = NULL, waiting_for = NULL, updated_at = now()
		WHERE id = :id AND status NOT IN ('succeeded', 'failed', 'cancelled')`,
		finishParams{ID: processID, Status: string(finish.Status), Error: finish.Error, ErrorKind: string(finish.ErrorKind)})
	if err != nil {
		return false, fmt.Errorf("finish process %s: %w", processID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("finish process %s rows affected: %w", processID, err)
	}
	return n == 1, nil
}

func (s *queries) MergeVars(ctx context.Context, processID string, vars process.Vars) error {
	raw, err := encodeVars(vars)
	if err != nil {
		return fmt.Errorf("merge vars %s: %w", processID, err)
	}
	res, err := sqlx.NamedExecContext(ctx, s.q, `
		UPDATE process
		SET vars = vars || CAST(:vars AS jsonb), updated_at = now()
		WHERE id = :id`, mergeVarsParams{ID: processID, Vars: raw})
	if err != nil {
		return fmt.Errorf("merge vars %s: %w", processID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("merge vars %s rows affected: %w", processID, err)
	}
	if n == 0 {
		return fmt.Errorf("merge vars %s: %w", processID, process.ErrNotFound)
	}
	return nil
}
