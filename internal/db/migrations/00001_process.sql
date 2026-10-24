-- +goose Up
CREATE TABLE IF NOT EXISTS workflow_version (
    id bigserial PRIMARY KEY,
    name text NOT NULL,
    hash text NOT NULL,
    definition jsonb NOT NULL,
    source_yaml text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (name, hash)
);

CREATE TABLE IF NOT EXISTS process (
    id uuid PRIMARY KEY,
    workflow_version_id bigint NOT NULL REFERENCES workflow_version (id),
    workflow_name text NOT NULL,
    subject_kind text NOT NULL,
    subject_key text NOT NULL,
    subject_url text NOT NULL DEFAULT '',
    title text NOT NULL DEFAULT '',
    project text NOT NULL DEFAULT '',
    status text NOT NULL CHECK (status IN ('pending', 'running', 'waiting', 'succeeded', 'failed', 'cancelled')),
    current_step text NOT NULL DEFAULT '',
    current_job_id bigint NULL,
    vars jsonb NOT NULL DEFAULT '{}',
    waiting_for jsonb NULL,
    error text NOT NULL DEFAULT '',
    error_kind text NOT NULL DEFAULT '' CHECK (error_kind IN ('', 'upstream', 'internal', 'business')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz NULL,
    ended_at timestamptz NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS process_active_subject_uq
    ON process (workflow_name, subject_key)
    WHERE status IN ('pending', 'running', 'waiting');

CREATE INDEX IF NOT EXISTS process_created_idx ON process (created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS process_status_idx ON process (status);

CREATE TABLE IF NOT EXISTS step_run (
    id uuid PRIMARY KEY,
    process_id uuid NOT NULL REFERENCES process (id) ON DELETE CASCADE,
    step_id text NOT NULL,
    uses text NOT NULL,
    attempt int NOT NULL,
    iteration int NOT NULL DEFAULT 0,
    river_job_id bigint NOT NULL,
    worker_id text NOT NULL,
    status text NOT NULL CHECK (status IN ('running', 'succeeded', 'failed', 'cancelled')),
    outcome text NOT NULL DEFAULT '',
    phase text NOT NULL DEFAULT '',
    error text NOT NULL DEFAULT '',
    error_kind text NOT NULL DEFAULT '' CHECK (error_kind IN ('', 'upstream', 'internal', 'business')),
    tokens_input bigint NOT NULL DEFAULT 0,
    tokens_output bigint NOT NULL DEFAULT 0,
    tokens_cache_creation bigint NOT NULL DEFAULT 0,
    tokens_cache_read bigint NOT NULL DEFAULT 0,
    claude_session text NOT NULL DEFAULT '',
    output jsonb NOT NULL DEFAULT '{}',
    started_at timestamptz NOT NULL DEFAULT now(),
    ended_at timestamptz NULL
);

CREATE INDEX IF NOT EXISTS step_run_process_started_idx ON step_run (process_id, started_at);

CREATE INDEX IF NOT EXISTS step_run_uses_started_idx ON step_run (uses, started_at);

-- +goose Down
DROP TABLE IF EXISTS step_run;
DROP TABLE IF EXISTS process;
DROP TABLE IF EXISTS workflow_version;
