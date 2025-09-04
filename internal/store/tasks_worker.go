package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type WorkerTask struct {
	ID          string
	Type        string
	Queue       string
	Status      string
	Attempts    int
	MaxAttempts int
	Payload     []byte
}

func LockTaskForWork(ctx context.Context, tx pgx.Tx, id string) (WorkerTask, error) {
	var t WorkerTask
	err := tx.QueryRow(ctx, `
		SELECT id, type, queue, status, attempts, max_attempts, payload
		  FROM tasks
		 WHERE id = $1
		 FOR UPDATE
	`, id).Scan(&t.ID, &t.Type, &t.Queue, &t.Status, &t.Attempts, &t.MaxAttempts, &t.Payload)
	return t, err
}

func MarkRunning(ctx context.Context, tx pgx.Tx, id string) error {
	_, err := tx.Exec(ctx, `
		UPDATE tasks
		   SET status     = 'RUNNING',
		       attempts   = attempts + 1,
		       last_error = NULL,
		       updated_at = now()
		 WHERE id = $1
	`, id)
	return err
}

func MarkSucceeded(ctx context.Context, tx pgx.Tx, id string, result []byte) error {
	_, err := tx.Exec(ctx, `
		UPDATE tasks
		   SET status     = 'SUCCEEDED',
		       result     = $2,
		       updated_at = now()
		 WHERE id = $1
	`, id, result)
	return err
}

func MarkFailed(ctx context.Context, tx pgx.Tx, id string, lastErr string) error {
	_, err := tx.Exec(ctx, `
		UPDATE tasks
		   SET status     = 'FAILED',
		       last_error = $2,
		       updated_at = now()
		 WHERE id = $1
	`, id, lastErr)
	return err
}

func MarkRetry(ctx context.Context, tx pgx.Tx, id string, lastErr string) error {
	_, err := tx.Exec(ctx, `
		UPDATE tasks
		   SET status     = 'ENQUEUED',
		       last_error = $2,
		       updated_at = now()
		 WHERE id = $1
	`, id, lastErr)
	return err
}
