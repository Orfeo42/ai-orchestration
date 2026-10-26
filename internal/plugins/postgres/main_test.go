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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/Orfeo42/ai-orchestration/internal/db"
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
	store *Store
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

	return &env{store: New(d.SQL)}
}
