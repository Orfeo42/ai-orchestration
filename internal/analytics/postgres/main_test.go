package postgres

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"ai-orchestration/internal/db"
)

const templateName = "tpl"

var (
	adminDSN string
	dbSeq    atomic.Int64
)

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	flag.Parse()
	if testing.Short() {
		return m.Run()
	}
	ctx := context.Background()
	container, err := postgres.Run(ctx, "postgres:18.6-alpine",
		postgres.WithDatabase("postgres"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "start postgres:", err)
		return 1
	}
	code := runWithContainer(ctx, m, container)
	if err := container.Terminate(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "terminate postgres:", err)
	}
	return code
}

func runWithContainer(ctx context.Context, m *testing.M, container *postgres.PostgresContainer) int {
	var err error
	adminDSN, err = container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintln(os.Stderr, "dsn:", err)
		return 1
	}
	if err := prepareTemplate(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "template:", err)
		return 1
	}
	return m.Run()
}

func prepareTemplate(ctx context.Context) error {
	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		return err
	}
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+templateName); err != nil {
		return err
	}
	if err := admin.Close(); err != nil {
		return err
	}
	d, err := db.Open(ctx, dsnFor(templateName))
	if err != nil {
		return err
	}
	if err := db.Migrate(ctx, d); err != nil {
		return err
	}
	return d.Close()
}

func dsnFor(name string) string {
	return strings.Replace(adminDSN, "/postgres?", "/"+name+"?", 1)
}

type env struct {
	q         *Querier
	sqlx      *sqlx.DB
	versionID int64
}

type processFixture struct {
	Workflow  string
	Project   string
	Status    string
	Error     string
	ErrorKind string
	CreatedAt time.Time
	StartedAt *time.Time
	EndedAt   *time.Time
}

type stepRunFixture struct {
	ProcessID string
	StepID    string
	Uses      string
	Attempt   int
	Status    string
	Outcome   string
	Input     int
	Output    int
	StartedAt time.Time
	EndedAt   *time.Time
}

type insertProcessParams struct {
	ID        string     `db:"id"`
	VersionID int64      `db:"version_id"`
	Workflow  string     `db:"workflow"`
	Key       string     `db:"key"`
	Project   string     `db:"project"`
	Status    string     `db:"status"`
	Error     string     `db:"error"`
	ErrorKind string     `db:"error_kind"`
	CreatedAt time.Time  `db:"created_at"`
	StartedAt *time.Time `db:"started_at"`
	EndedAt   *time.Time `db:"ended_at"`
}

type insertStepRunParams struct {
	ID        string     `db:"id"`
	ProcessID string     `db:"process_id"`
	StepID    string     `db:"step_id"`
	Uses      string     `db:"uses"`
	Attempt   int        `db:"attempt"`
	Status    string     `db:"status"`
	Outcome   string     `db:"outcome"`
	Input     int        `db:"input"`
	Output    int        `db:"output"`
	StartedAt time.Time  `db:"started_at"`
	EndedAt   *time.Time `db:"ended_at"`
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	admin, err := sql.Open("pgx", adminDSN)
	require.NoError(t, err)
	name := fmt.Sprintf("t%d_%d", time.Now().UnixNano(), dbSeq.Add(1))
	_, err = admin.ExecContext(ctx, "CREATE DATABASE "+name+" TEMPLATE "+templateName)
	require.NoError(t, err)
	require.NoError(t, admin.Close())

	d, err := db.Open(ctx, dsnFor(name))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, d.Close()) })

	var versionID int64
	err = d.SQL.GetContext(ctx, &versionID, `
		INSERT INTO workflow_version (name, hash, definition, source_yaml)
		VALUES ('wf', 'h1', '{}', 'name: wf') RETURNING id`)
	require.NoError(t, err)
	return &env{q: New(d.SQL), sqlx: d.SQL, versionID: versionID}
}

func (e *env) addProcess(t *testing.T, f processFixture) string {
	t.Helper()
	id := uuid.NewString()
	_, err := sqlx.NamedExecContext(context.Background(), e.sqlx, `
		INSERT INTO process (id, workflow_version_id, workflow_name, subject_kind, subject_key, project, status,
			error, error_kind, created_at, updated_at, started_at, ended_at)
		VALUES (:id, :version_id, :workflow, 'issue', :key, :project, :status,
			:error, :error_kind, :created_at, :created_at, :started_at, :ended_at)`,
		insertProcessParams{
			ID: id, VersionID: e.versionID, Workflow: f.Workflow, Key: id, Project: f.Project, Status: f.Status,
			Error: f.Error, ErrorKind: f.ErrorKind, CreatedAt: f.CreatedAt, StartedAt: f.StartedAt, EndedAt: f.EndedAt,
		})
	require.NoError(t, err)
	return id
}

func (e *env) addStepRun(t *testing.T, f stepRunFixture) {
	t.Helper()
	_, err := sqlx.NamedExecContext(context.Background(), e.sqlx, `
		INSERT INTO step_run (id, process_id, step_id, uses, attempt, river_job_id, worker_id, status, outcome,
			tokens_input, tokens_output, started_at, ended_at)
		VALUES (:id, :process_id, :step_id, :uses, :attempt, 1, 'w1', :status, :outcome,
			:input, :output, :started_at, :ended_at)`,
		insertStepRunParams{
			ID: uuid.NewString(), ProcessID: f.ProcessID, StepID: f.StepID, Uses: f.Uses, Attempt: f.Attempt,
			Status: f.Status, Outcome: f.Outcome, Input: f.Input, Output: f.Output, StartedAt: f.StartedAt, EndedAt: f.EndedAt,
		})
	require.NoError(t, err)
}

func ptr(t time.Time) *time.Time {
	return &t
}
