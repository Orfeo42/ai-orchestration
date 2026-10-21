package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	"ai-orchestration/internal/analytics"
	"ai-orchestration/internal/events"
	"ai-orchestration/internal/process"
)

const (
	bucketDay     = `to_char(date_trunc('day', sr.started_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD')`
	bucketProject = `p.project`
	bucketStep    = `sr.uses`
)

const workflowsQuery = `
WITH usage AS (
	SELECT process_id,
		SUM(tokens_input) AS tokens_input,
		SUM(tokens_output) AS tokens_output,
		SUM(tokens_cache_creation) AS tokens_cache_creation,
		SUM(tokens_cache_read) AS tokens_cache_read
	FROM step_run
	GROUP BY process_id
)
SELECT p.workflow_name AS workflow,
	COUNT(*) AS processes,
	COUNT(*) FILTER (WHERE p.status IN ('pending', 'running', 'waiting')) AS active,
	COUNT(*) FILTER (WHERE p.status = 'succeeded') AS succeeded,
	COUNT(*) FILTER (WHERE p.status = 'failed') AS failed,
	COUNT(*) FILTER (WHERE p.status = 'cancelled') AS cancelled,
	CAST(COALESCE(percentile_cont(0.5) WITHIN GROUP (
		ORDER BY EXTRACT(EPOCH FROM (p.ended_at - COALESCE(p.started_at, p.created_at))) * 1000
	) FILTER (WHERE p.status IN ('succeeded', 'failed', 'cancelled') AND p.ended_at IS NOT NULL), 0) AS double precision) AS duration_p50_ms,
	CAST(COALESCE(percentile_cont(0.95) WITHIN GROUP (
		ORDER BY EXTRACT(EPOCH FROM (p.ended_at - COALESCE(p.started_at, p.created_at))) * 1000
	) FILTER (WHERE p.status IN ('succeeded', 'failed', 'cancelled') AND p.ended_at IS NOT NULL), 0) AS double precision) AS duration_p95_ms,
	CAST(COALESCE(SUM(u.tokens_input), 0) AS bigint) AS tokens_input,
	CAST(COALESCE(SUM(u.tokens_output), 0) AS bigint) AS tokens_output,
	CAST(COALESCE(SUM(u.tokens_cache_creation), 0) AS bigint) AS tokens_cache_creation,
	CAST(COALESCE(SUM(u.tokens_cache_read), 0) AS bigint) AS tokens_cache_read
FROM process p
LEFT JOIN usage u ON u.process_id = p.id
WHERE p.created_at >= :from AND p.created_at < :to
GROUP BY p.workflow_name
ORDER BY p.workflow_name`

const stepsQuery = `
WITH filtered AS (
	SELECT sr.step_id, sr.uses, sr.status, sr.outcome, sr.attempt, sr.started_at, sr.ended_at,
		sr.tokens_input, sr.tokens_output, sr.tokens_cache_creation, sr.tokens_cache_read
	FROM step_run sr
	JOIN process p ON p.id = sr.process_id
	WHERE p.workflow_name = :workflow AND sr.started_at >= :from AND sr.started_at < :to
),
outcome_counts AS (
	SELECT step_id, uses,
		CASE WHEN status = 'succeeded' THEN outcome ELSE status END AS outcome_key,
		COUNT(*) AS n
	FROM filtered
	GROUP BY step_id, uses, outcome_key
),
outcomes AS (
	SELECT step_id, uses, jsonb_object_agg(outcome_key, n) AS outcomes
	FROM outcome_counts
	GROUP BY step_id, uses
)
SELECT f.step_id, f.uses,
	COUNT(*) AS runs,
	CAST(o.outcomes AS text) AS outcomes,
	COUNT(*) FILTER (WHERE f.status = 'failed') AS failed,
	COUNT(*) FILTER (WHERE f.attempt > 1) AS retries,
	CAST(COALESCE(percentile_cont(0.5) WITHIN GROUP (
		ORDER BY EXTRACT(EPOCH FROM (f.ended_at - f.started_at)) * 1000
	) FILTER (WHERE f.ended_at IS NOT NULL), 0) AS double precision) AS duration_p50_ms,
	CAST(COALESCE(percentile_cont(0.95) WITHIN GROUP (
		ORDER BY EXTRACT(EPOCH FROM (f.ended_at - f.started_at)) * 1000
	) FILTER (WHERE f.ended_at IS NOT NULL), 0) AS double precision) AS duration_p95_ms,
	CAST(SUM(f.tokens_input) AS bigint) AS tokens_input,
	CAST(SUM(f.tokens_output) AS bigint) AS tokens_output,
	CAST(SUM(f.tokens_cache_creation) AS bigint) AS tokens_cache_creation,
	CAST(SUM(f.tokens_cache_read) AS bigint) AS tokens_cache_read
FROM filtered f
JOIN outcomes o ON o.step_id = f.step_id AND o.uses = f.uses
GROUP BY f.step_id, f.uses, o.outcomes
ORDER BY f.step_id, f.uses`

const tokensQueryFormat = `
SELECT %s AS bucket,
	CAST(SUM(sr.tokens_input) AS bigint) AS tokens_input,
	CAST(SUM(sr.tokens_output) AS bigint) AS tokens_output,
	CAST(SUM(sr.tokens_cache_creation) AS bigint) AS tokens_cache_creation,
	CAST(SUM(sr.tokens_cache_read) AS bigint) AS tokens_cache_read
FROM step_run sr
JOIN process p ON p.id = sr.process_id
WHERE sr.started_at >= :from AND sr.started_at < :to
GROUP BY bucket
ORDER BY bucket`

const failuresQuery = `
SELECT error_kind, error, COUNT(*) AS count, MAX(ended_at) AS last_at
FROM process
WHERE status = 'failed' AND ended_at >= :from AND ended_at < :to
GROUP BY error_kind, error
ORDER BY count DESC, last_at DESC
LIMIT 50`

type rangeParams struct {
	From time.Time `db:"from"`
	To   time.Time `db:"to"`
}

type stepsParams struct {
	Workflow string    `db:"workflow"`
	From     time.Time `db:"from"`
	To       time.Time `db:"to"`
}

type workflowRow struct {
	Workflow            string  `db:"workflow"`
	Processes           int     `db:"processes"`
	Active              int     `db:"active"`
	Succeeded           int     `db:"succeeded"`
	Failed              int     `db:"failed"`
	Cancelled           int     `db:"cancelled"`
	DurationP50Ms       float64 `db:"duration_p50_ms"`
	DurationP95Ms       float64 `db:"duration_p95_ms"`
	TokensInput         int64   `db:"tokens_input"`
	TokensOutput        int64   `db:"tokens_output"`
	TokensCacheCreation int64   `db:"tokens_cache_creation"`
	TokensCacheRead     int64   `db:"tokens_cache_read"`
}

type stepRow struct {
	StepID              string  `db:"step_id"`
	Uses                string  `db:"uses"`
	Runs                int     `db:"runs"`
	Outcomes            string  `db:"outcomes"`
	Failed              int     `db:"failed"`
	Retries             int     `db:"retries"`
	DurationP50Ms       float64 `db:"duration_p50_ms"`
	DurationP95Ms       float64 `db:"duration_p95_ms"`
	TokensInput         int64   `db:"tokens_input"`
	TokensOutput        int64   `db:"tokens_output"`
	TokensCacheCreation int64   `db:"tokens_cache_creation"`
	TokensCacheRead     int64   `db:"tokens_cache_read"`
}

type tokenRow struct {
	Bucket              string `db:"bucket"`
	TokensInput         int64  `db:"tokens_input"`
	TokensOutput        int64  `db:"tokens_output"`
	TokensCacheCreation int64  `db:"tokens_cache_creation"`
	TokensCacheRead     int64  `db:"tokens_cache_read"`
}

type failureRow struct {
	ErrorKind string    `db:"error_kind"`
	Error     string    `db:"error"`
	Count     int       `db:"count"`
	LastAt    time.Time `db:"last_at"`
}

type Querier struct {
	db *sqlx.DB
}

var _ analytics.Querier = (*Querier)(nil)

func New(db *sqlx.DB) *Querier {
	return &Querier{db: db}
}

func (q *Querier) Workflows(ctx context.Context, r analytics.Range) ([]analytics.WorkflowStats, error) {
	query, args, err := sqlx.Named(workflowsQuery, rangeParams{From: r.From.UTC(), To: r.To.UTC()})
	if err != nil {
		return nil, fmt.Errorf("analytics workflows: bind: %w", err)
	}
	var rows []workflowRow
	if err := q.db.SelectContext(ctx, &rows, q.db.Rebind(query), args...); err != nil {
		return nil, fmt.Errorf("analytics workflows: %w", err)
	}
	out := make([]analytics.WorkflowStats, 0, len(rows))
	for _, row := range rows {
		terminal := row.Succeeded + row.Failed + row.Cancelled
		rate := 0.0
		if terminal > 0 {
			rate = float64(row.Succeeded) / float64(terminal)
		}
		out = append(out, analytics.WorkflowStats{
			Workflow:      row.Workflow,
			Processes:     row.Processes,
			Active:        row.Active,
			Succeeded:     row.Succeeded,
			Failed:        row.Failed,
			Cancelled:     row.Cancelled,
			SuccessRate:   rate,
			DurationP50Ms: int64(row.DurationP50Ms),
			DurationP95Ms: int64(row.DurationP95Ms),
			Usage: events.TokenUsage{
				Input:              int(row.TokensInput),
				Output:             int(row.TokensOutput),
				CacheCreationInput: int(row.TokensCacheCreation),
				CacheReadInput:     int(row.TokensCacheRead),
			},
		})
	}
	return out, nil
}

func (q *Querier) Steps(ctx context.Context, workflow string, r analytics.Range) ([]analytics.StepStats, error) {
	query, args, err := sqlx.Named(stepsQuery, stepsParams{Workflow: workflow, From: r.From.UTC(), To: r.To.UTC()})
	if err != nil {
		return nil, fmt.Errorf("analytics steps: bind: %w", err)
	}
	var rows []stepRow
	if err := q.db.SelectContext(ctx, &rows, q.db.Rebind(query), args...); err != nil {
		return nil, fmt.Errorf("analytics steps: %w", err)
	}
	out := make([]analytics.StepStats, 0, len(rows))
	for _, row := range rows {
		outcomes := map[string]int{}
		if err := json.Unmarshal([]byte(row.Outcomes), &outcomes); err != nil {
			return nil, fmt.Errorf("analytics steps: decode outcomes of %s: %w", row.StepID, err)
		}
		out = append(out, analytics.StepStats{
			Workflow:      workflow,
			StepID:        row.StepID,
			Uses:          row.Uses,
			Runs:          row.Runs,
			Outcomes:      outcomes,
			FailureRate:   float64(row.Failed) / float64(row.Runs),
			Retries:       row.Retries,
			DurationP50Ms: int64(row.DurationP50Ms),
			DurationP95Ms: int64(row.DurationP95Ms),
			Usage: events.TokenUsage{
				Input:              int(row.TokensInput),
				Output:             int(row.TokensOutput),
				CacheCreationInput: int(row.TokensCacheCreation),
				CacheReadInput:     int(row.TokensCacheRead),
			},
		})
	}
	return out, nil
}

func (q *Querier) Tokens(ctx context.Context, r analytics.Range, g analytics.Grouping) ([]analytics.TokenPoint, error) {
	var bucket string
	switch g {
	case analytics.GroupDay:
		bucket = bucketDay
	case analytics.GroupProject:
		bucket = bucketProject
	case analytics.GroupStep:
		bucket = bucketStep
	default:
		return nil, analytics.ErrInvalidGrouping
	}
	query, args, err := sqlx.Named(fmt.Sprintf(tokensQueryFormat, bucket), rangeParams{From: r.From.UTC(), To: r.To.UTC()})
	if err != nil {
		return nil, fmt.Errorf("analytics tokens: bind: %w", err)
	}
	var rows []tokenRow
	if err := q.db.SelectContext(ctx, &rows, q.db.Rebind(query), args...); err != nil {
		return nil, fmt.Errorf("analytics tokens: %w", err)
	}
	out := make([]analytics.TokenPoint, 0, len(rows))
	for _, row := range rows {
		out = append(out, analytics.TokenPoint{
			Bucket: row.Bucket,
			Usage: events.TokenUsage{
				Input:              int(row.TokensInput),
				Output:             int(row.TokensOutput),
				CacheCreationInput: int(row.TokensCacheCreation),
				CacheReadInput:     int(row.TokensCacheRead),
			},
		})
	}
	return out, nil
}

func (q *Querier) Failures(ctx context.Context, r analytics.Range) ([]analytics.FailureGroup, error) {
	query, args, err := sqlx.Named(failuresQuery, rangeParams{From: r.From.UTC(), To: r.To.UTC()})
	if err != nil {
		return nil, fmt.Errorf("analytics failures: bind: %w", err)
	}
	var rows []failureRow
	if err := q.db.SelectContext(ctx, &rows, q.db.Rebind(query), args...); err != nil {
		return nil, fmt.Errorf("analytics failures: %w", err)
	}
	out := make([]analytics.FailureGroup, 0, len(rows))
	for _, row := range rows {
		out = append(out, analytics.FailureGroup{
			ErrorKind: process.ErrorKind(row.ErrorKind),
			Error:     row.Error,
			Count:     row.Count,
			LastAt:    row.LastAt,
		})
	}
	return out, nil
}
