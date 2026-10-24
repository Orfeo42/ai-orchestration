package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/Orfeo42/ai-orchestration/internal/events"
	"github.com/Orfeo42/ai-orchestration/internal/process"
)

const stepRunColumns = `id, process_id, step_id, uses, plugin, attempt, iteration, river_job_id, worker_id, status, outcome, phase,
	error, error_kind, tokens_input, tokens_output, tokens_cache_creation, tokens_cache_read, claude_session, output,
	heartbeat_at, started_at, ended_at`

type startProcessParams struct {
	ProcessID  string `db:"process_id"`
	RiverJobID int64  `db:"river_job_id"`
}

type insertStepRunParams struct {
	ID            string     `db:"id"`
	ProcessID     string     `db:"process_id"`
	StepID        string     `db:"step_id"`
	Uses          string     `db:"uses"`
	Plugin        string     `db:"plugin"`
	Attempt       int        `db:"attempt"`
	Iteration     int        `db:"iteration"`
	RiverJobID    int64      `db:"river_job_id"`
	WorkerID      string     `db:"worker_id"`
	Status        string     `db:"status"`
	Outcome       string     `db:"outcome"`
	Phase         string     `db:"phase"`
	ClaudeSession string     `db:"claude_session"`
	Output        string     `db:"output"`
	StartedAt     *time.Time `db:"started_at"`
}

type finishStepRunParams struct {
	ID        string `db:"id"`
	Status    string `db:"status"`
	Outcome   string `db:"outcome"`
	Error     string `db:"error"`
	ErrorKind string `db:"error_kind"`
	Output    string `db:"output"`
}

type staleJobsParams struct {
	Now time.Time `db:"now"`
}

type processIDParams struct {
	ProcessID string `db:"process_id"`
}

type stepPhaseParams struct {
	ID    string `db:"id"`
	Phase string `db:"phase"`
}

type stepIterationParams struct {
	ID        string `db:"id"`
	Iteration int    `db:"iteration"`
}

type stepSessionParams struct {
	ID      string `db:"id"`
	Session string `db:"session"`
}

type stepUsageParams struct {
	ID            string `db:"id"`
	Input         int    `db:"input"`
	Output        int    `db:"output"`
	CacheCreation int    `db:"cache_creation"`
	CacheRead     int    `db:"cache_read"`
}

func (s *queries) StartStepRun(ctx context.Context, run process.StepRun) error {
	res, err := sqlx.NamedExecContext(ctx, s.q, `
		UPDATE process
		SET started_at = COALESCE(started_at, now()), updated_at = now()
		WHERE id = :process_id AND status = 'running' AND current_job_id = :river_job_id`,
		startProcessParams{ProcessID: run.ProcessID, RiverJobID: run.RiverJobID})
	if err != nil {
		return fmt.Errorf("start step run %s: %w", run.StepID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("start step run %s rows affected: %w", run.StepID, err)
	}
	if n == 0 {
		return fmt.Errorf("start step run %s on process %s: %w", run.StepID, run.ProcessID, process.ErrNotActive)
	}
	output, err := encodeVars(run.Output)
	if err != nil {
		return fmt.Errorf("start step run %s: %w", run.StepID, err)
	}
	params := insertStepRunParams{
		ID:            run.ID,
		ProcessID:     run.ProcessID,
		StepID:        run.StepID,
		Uses:          run.Uses,
		Plugin:        run.Plugin,
		Attempt:       run.Attempt,
		Iteration:     run.Iteration,
		RiverJobID:    run.RiverJobID,
		WorkerID:      run.WorkerID,
		Status:        string(run.Status),
		Outcome:       run.Outcome,
		Phase:         run.Phase,
		ClaudeSession: run.ClaudeSession,
		Output:        output,
	}
	if !run.StartedAt.IsZero() {
		params.StartedAt = &run.StartedAt
	}
	_, err = sqlx.NamedExecContext(ctx, s.q, `
		INSERT INTO step_run (id, process_id, step_id, uses, plugin, attempt, iteration, river_job_id, worker_id, status,
			outcome, phase, claude_session, output, started_at)
		VALUES (:id, :process_id, :step_id, :uses, :plugin, :attempt, :iteration, :river_job_id, :worker_id, :status,
			:outcome, :phase, :claude_session, CAST(:output AS jsonb), COALESCE(CAST(:started_at AS timestamptz), now()))`,
		params)
	if err != nil {
		return fmt.Errorf("insert step run %s: %w", run.StepID, err)
	}
	return nil
}

func (s *queries) FinishStepRun(ctx context.Context, result process.StepResult) (bool, error) {
	output, err := encodeVars(result.Output)
	if err != nil {
		return false, fmt.Errorf("finish step run %s: %w", result.StepRunID, err)
	}
	res, err := sqlx.NamedExecContext(ctx, s.q, `
		UPDATE step_run
		SET status = :status, outcome = :outcome, error = :error, error_kind = :error_kind,
			output = CAST(:output AS jsonb), ended_at = now()
		WHERE id = :id AND status = 'running'`,
		finishStepRunParams{
			ID:        result.StepRunID,
			Status:    string(result.Status),
			Outcome:   result.Outcome,
			Error:     result.Error,
			ErrorKind: string(result.ErrorKind),
			Output:    output,
		})
	if err != nil {
		return false, fmt.Errorf("finish step run %s: %w", result.StepRunID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("finish step run %s rows affected: %w", result.StepRunID, err)
	}
	return n == 1, nil
}

func (s *queries) GetStepRun(ctx context.Context, id string) (process.StepRun, error) {
	rows, err := sqlx.NamedQueryContext(ctx, s.q, `SELECT `+stepRunColumns+` FROM step_run WHERE id = :id`, idParams{ID: id})
	if err != nil {
		return process.StepRun{}, fmt.Errorf("get step run %s: %w", id, err)
	}
	var found []stepRunRow
	if err := sqlx.StructScan(rows, &found); err != nil {
		return process.StepRun{}, fmt.Errorf("get step run %s: %w", id, err)
	}
	if len(found) == 0 {
		return process.StepRun{}, fmt.Errorf("get step run %s: %w", id, process.ErrNotFound)
	}
	return toStepRun(found[0])
}

func (s *queries) ListStepRuns(ctx context.Context, processID string) ([]process.StepRun, error) {
	rows, err := sqlx.NamedQueryContext(ctx, s.q, `
		SELECT `+stepRunColumns+` FROM step_run
		WHERE process_id = :process_id
		ORDER BY started_at, id`, processIDParams{ProcessID: processID})
	if err != nil {
		return nil, fmt.Errorf("list step runs %s: %w", processID, err)
	}
	var found []stepRunRow
	if err := sqlx.StructScan(rows, &found); err != nil {
		return nil, fmt.Errorf("list step runs %s: %w", processID, err)
	}
	return toStepRuns(found)
}

func (s *queries) ActiveStepRun(ctx context.Context, processID string) (process.StepRun, bool, error) {
	rows, err := sqlx.NamedQueryContext(ctx, s.q, `
		SELECT `+stepRunColumns+` FROM step_run
		WHERE process_id = :process_id AND status = 'running'
		ORDER BY started_at DESC, id DESC
		LIMIT 1`, processIDParams{ProcessID: processID})
	if err != nil {
		return process.StepRun{}, false, fmt.Errorf("active step run %s: %w", processID, err)
	}
	var found []stepRunRow
	if err := sqlx.StructScan(rows, &found); err != nil {
		return process.StepRun{}, false, fmt.Errorf("active step run %s: %w", processID, err)
	}
	if len(found) == 0 {
		return process.StepRun{}, false, nil
	}
	run, err := toStepRun(found[0])
	if err != nil {
		return process.StepRun{}, false, err
	}
	return run, true, nil
}

func (s *queries) SetStepPhase(ctx context.Context, stepRunID, phase string) error {
	_, err := sqlx.NamedExecContext(ctx, s.q, `UPDATE step_run SET phase = :phase WHERE id = :id`,
		stepPhaseParams{ID: stepRunID, Phase: phase})
	if err != nil {
		return fmt.Errorf("set step phase %s: %w", stepRunID, err)
	}
	return nil
}

func (s *queries) SetStepIteration(ctx context.Context, stepRunID string, iteration int) error {
	_, err := sqlx.NamedExecContext(ctx, s.q, `UPDATE step_run SET iteration = :iteration WHERE id = :id`,
		stepIterationParams{ID: stepRunID, Iteration: iteration})
	if err != nil {
		return fmt.Errorf("set step iteration %s: %w", stepRunID, err)
	}
	return nil
}

func (s *queries) SetStepSession(ctx context.Context, stepRunID, session string) error {
	_, err := sqlx.NamedExecContext(ctx, s.q, `UPDATE step_run SET claude_session = :session WHERE id = :id`,
		stepSessionParams{ID: stepRunID, Session: session})
	if err != nil {
		return fmt.Errorf("set step session %s: %w", stepRunID, err)
	}
	return nil
}

func (s *queries) AddStepUsage(ctx context.Context, stepRunID string, usage events.TokenUsage) error {
	_, err := sqlx.NamedExecContext(ctx, s.q, `
		UPDATE step_run
		SET tokens_input = tokens_input + :input,
			tokens_output = tokens_output + :output,
			tokens_cache_creation = tokens_cache_creation + :cache_creation,
			tokens_cache_read = tokens_cache_read + :cache_read
		WHERE id = :id`,
		stepUsageParams{
			ID:            stepRunID,
			Input:         usage.Input,
			Output:        usage.Output,
			CacheCreation: usage.CacheCreationInput,
			CacheRead:     usage.CacheReadInput,
		})
	if err != nil {
		return fmt.Errorf("add step usage %s: %w", stepRunID, err)
	}
	return nil
}

func (s *queries) TouchStepHeartbeat(ctx context.Context, stepRunID string) (bool, error) {
	res, err := sqlx.NamedExecContext(ctx, s.q, `
		UPDATE step_run SET heartbeat_at = now()
		WHERE id = :id AND status = 'running'`, idParams{ID: stepRunID})
	if err != nil {
		return false, fmt.Errorf("touch step heartbeat %s: %w", stepRunID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("touch step heartbeat %s rows affected: %w", stepRunID, err)
	}
	return n > 0, nil
}

func (s *queries) ListStaleJobs(ctx context.Context, now time.Time) ([]process.StaleJob, error) {
	rows, err := sqlx.NamedQueryContext(ctx, s.q, `
		SELECT p.id AS process_id, sr.id AS step_run_id,
			p.waiting_for->'job'->>'plugin' AS plugin,
			CAST(p.waiting_for->'job'->>'deadline' AS timestamptz) AS deadline,
			CAST(p.waiting_for->'job'->>'deadline' AS timestamptz) < :now AS expired
		FROM process p
		JOIN step_run sr ON sr.id = CAST(p.waiting_for->'job'->>'step_run_id' AS uuid) AND sr.status = 'running'
		WHERE p.status = 'waiting' AND p.waiting_for->>'kind' = 'job'
			AND (COALESCE(sr.heartbeat_at, sr.started_at)
					< CAST(:now AS timestamptz) - make_interval(secs => 3 * CAST(p.waiting_for->'job'->>'heartbeat_seconds' AS int))
				OR CAST(p.waiting_for->'job'->>'deadline' AS timestamptz) < :now)
		ORDER BY sr.started_at`, staleJobsParams{Now: now})
	if err != nil {
		return nil, fmt.Errorf("list stale jobs: %w", err)
	}
	var found []staleJobRow
	if err := sqlx.StructScan(rows, &found); err != nil {
		return nil, fmt.Errorf("list stale jobs: %w", err)
	}
	out := make([]process.StaleJob, 0, len(found))
	for _, r := range found {
		out = append(out, process.StaleJob{ProcessID: r.ProcessID, StepRunID: r.StepRunID, Plugin: r.Plugin, Deadline: r.Deadline, Expired: r.Expired})
	}
	return out, nil
}
