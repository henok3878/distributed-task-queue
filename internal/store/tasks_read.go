package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type TaskRow struct {
	ID          string
	Type        string
	Queue       string
	Status      string
	Attempts    int
	MaxAttempts int
	LastError   *string
	ResultJSON  []byte
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func GetTask(ctx context.Context, db *pgxpool.Pool, id string) (TaskRow, error) {
	var t TaskRow
	err := db.QueryRow(ctx, `
		select id, type, queue, status, attempts, max_attempts,
		       last_error,
		       coalesce(result, '{}'::jsonb) as result,
		       created_at, updated_at
		  from tasks
		 where id = $1
	`, id).Scan(&t.ID, &t.Type, &t.Queue, &t.Status, &t.Attempts, &t.MaxAttempts,
		&t.LastError, &t.ResultJSON, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}
