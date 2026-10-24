-- +goose Up
CREATE TABLE IF NOT EXISTS plugin (
    name text PRIMARY KEY,
    base_url text NOT NULL,
    manifest jsonb NOT NULL,
    status text NOT NULL CHECK (status IN ('ok', 'degraded', 'down', 'unreachable')),
    error text NOT NULL DEFAULT '',
    checked_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS workflow (
    name text PRIMARY KEY,
    current_version_id bigint NOT NULL REFERENCES workflow_version (id),
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO workflow (name, current_version_id)
SELECT DISTINCT ON (name) name, id
FROM workflow_version
ORDER BY name, id DESC
ON CONFLICT (name) DO NOTHING;

ALTER TABLE step_run ADD COLUMN IF NOT EXISTS plugin text NOT NULL DEFAULT '';

ALTER TABLE step_run ADD COLUMN IF NOT EXISTS heartbeat_at timestamptz NULL;

CREATE INDEX IF NOT EXISTS process_waiting_kind_idx
    ON process ((waiting_for->>'kind'))
    WHERE status = 'waiting';

-- +goose Down
DROP INDEX IF EXISTS process_waiting_kind_idx;
ALTER TABLE step_run DROP COLUMN IF EXISTS heartbeat_at;
ALTER TABLE step_run DROP COLUMN IF EXISTS plugin;
DROP TABLE IF EXISTS workflow;
DROP TABLE IF EXISTS plugin;
