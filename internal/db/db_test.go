package db

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

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

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
	adminDSN, err = container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintln(os.Stderr, "dsn:", err)
		return 1
	}
	code := m.Run()
	if err := container.Terminate(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "terminate postgres:", err)
	}
	return code
}

func newDatabase(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("integration")
	}
	admin, err := sql.Open("pgx", adminDSN)
	require.NoError(t, err)
	defer func() { assert.NoError(t, admin.Close()) }()
	name := fmt.Sprintf("t%d_%d", time.Now().UnixNano(), dbSeq.Add(1))
	_, err = admin.Exec("CREATE DATABASE " + name)
	require.NoError(t, err)
	return strings.Replace(adminDSN, "/postgres?", "/"+name+"?", 1)
}

func tableExists(t *testing.T, d *DB, name string) bool {
	t.Helper()
	arg := struct {
		Name string `db:"name"`
	}{Name: "public." + name}
	query, args, err := sqlx.Named("SELECT to_regclass(:name) IS NOT NULL", arg)
	require.NoError(t, err)
	var exists bool
	require.NoError(t, d.SQL.GetContext(context.Background(), &exists, d.SQL.Rebind(query), args...))
	return exists
}

func TestOpen(t *testing.T) {
	t.Run("connects and closes", func(t *testing.T) {
		d, err := Open(context.Background(), newDatabase(t))
		require.NoError(t, err)
		require.NoError(t, d.Close())
	})

	t.Run("unreachable server returns error", func(t *testing.T) {
		if testing.Short() {
			t.Skip("integration")
		}
		_, err := Open(context.Background(), "postgres://test:test@127.0.0.1:1/x?sslmode=disable&connect_timeout=1")
		require.Error(t, err)
	})
}

func TestMigrate(t *testing.T) {
	t.Run("fresh database gets river and app tables", func(t *testing.T) {
		ctx := context.Background()
		d, err := Open(ctx, newDatabase(t))
		require.NoError(t, err)
		t.Cleanup(func() { assert.NoError(t, d.Close()) })

		require.NoError(t, Migrate(ctx, d))

		assert.True(t, tableExists(t, d, "river_job"))
		assert.True(t, tableExists(t, d, "process"))
		assert.True(t, tableExists(t, d, "step_run"))
		assert.True(t, tableExists(t, d, "workflow_version"))
	})

	t.Run("second run is a no-op", func(t *testing.T) {
		ctx := context.Background()
		d, err := Open(ctx, newDatabase(t))
		require.NoError(t, err)
		t.Cleanup(func() { assert.NoError(t, d.Close()) })

		require.NoError(t, Migrate(ctx, d))
		require.NoError(t, Migrate(ctx, d))
	})
}

func TestMigrationIdempotent(t *testing.T) {
	t.Run("up twice, down twice, up again all succeed", func(t *testing.T) {
		ctx := context.Background()
		d, err := Open(ctx, newDatabase(t))
		require.NoError(t, err)
		t.Cleanup(func() { assert.NoError(t, d.Close()) })

		up1, down1 := splitMigration(t, "migrations/00001_process.sql")
		up2, down2 := splitMigration(t, "migrations/00002_plugins.sql")

		for _, stmt := range []string{up1, up2, up2, down2, down2, up2, down2, down1, down1, up1, up2} {
			_, err = d.SQL.ExecContext(ctx, stmt)
			require.NoError(t, err)
		}
		assert.True(t, tableExists(t, d, "process"))
		assert.True(t, tableExists(t, d, "workflow"))
		assert.True(t, tableExists(t, d, "plugin"))
	})
}

func splitMigration(t *testing.T, path string) (string, string) {
	t.Helper()
	raw, err := migrationsFS.ReadFile(path)
	require.NoError(t, err)
	upPart, downPart, found := strings.Cut(string(raw), "-- +goose Down")
	require.True(t, found)
	return strings.TrimPrefix(strings.TrimSpace(upPart), "-- +goose Up"), strings.TrimSpace(downPart)
}
