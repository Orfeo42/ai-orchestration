package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"

	"ai-orchestration/internal/process"
)

var (
	_ process.Store   = (*Store)(nil)
	_ process.Querier = (*queries)(nil)
	_ process.Tx      = (*txStore)(nil)
)

type queries struct {
	q sqlx.ExtContext
}

type Store struct {
	queries
	db *sqlx.DB
}

type txStore struct {
	queries
	tx *sqlx.Tx
}

func New(db *sqlx.DB) *Store {
	return &Store{queries: queries{q: db}, db: db}
}

func (t *txStore) SQL() *sql.Tx {
	return t.tx.Tx
}

func (s *Store) InTx(ctx context.Context, fn func(ctx context.Context, tx process.Tx) error) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := fn(ctx, &txStore{queries: queries{q: tx}, tx: tx}); err != nil {
		rollbackErr := tx.Rollback()
		if errors.Is(rollbackErr, sql.ErrTxDone) {
			rollbackErr = nil
		}
		return errors.Join(err, rollbackErr)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
