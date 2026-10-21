package engine

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
)

type RiverQueue struct {
	client *river.Client[*sql.Tx]
}

func NewRiverQueue(client *river.Client[*sql.Tx]) *RiverQueue {
	return &RiverQueue{client: client}
}

func (q *RiverQueue) InsertStepTx(ctx context.Context, tx *sql.Tx, args StepArgs, opts StepOpts) (int64, error) {
	res, err := q.client.InsertTx(ctx, tx, args, &river.InsertOpts{Queue: opts.Queue, MaxAttempts: opts.MaxAttempts})
	if err != nil {
		return 0, fmt.Errorf("river insert: %w", err)
	}
	return res.Job.ID, nil
}

func (q *RiverQueue) CancelJobTx(ctx context.Context, tx *sql.Tx, jobID int64) error {
	if _, err := q.client.JobCancelTx(ctx, tx, jobID); err != nil {
		return fmt.Errorf("river cancel: %w", err)
	}
	return nil
}

func (q *RiverQueue) CompleteTx(ctx context.Context, tx *sql.Tx, job *river.Job[StepArgs]) error {
	if _, err := river.JobCompleteTx[*riverdatabasesql.Driver](ctx, tx, job); err != nil {
		return fmt.Errorf("river complete: %w", err)
	}
	return nil
}
