package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
)

type DB struct {
	SQL  *sqlx.DB
	Pool *pgxpool.Pool
}

func Open(ctx context.Context, dsn string) (*DB, error) {
	sqlDB, err := sqlx.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sql: %w", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("ping sql: %w", err), sqlDB.Close())
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("open pool: %w", err), sqlDB.Close())
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.Join(fmt.Errorf("ping pool: %w", err), sqlDB.Close())
	}
	return &DB{SQL: sqlDB, Pool: pool}, nil
}

func (d *DB) Close() error {
	d.Pool.Close()
	return d.SQL.Close()
}

func (d *DB) RiverDriver() *riverdatabasesql.Driver {
	return riverdatabasesql.NewWithPgxListener(d.SQL.DB, d.Pool)
}
