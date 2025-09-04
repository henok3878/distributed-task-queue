package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// registry defaults for a type. the type must be active
func GetTypeDefaults(ctx context.Context, db *pgxpool.Pool, typ string) (active bool, queue string, maxAttempts int, err error) {
	err = db.QueryRow(ctx, `
		select active, default_queue, default_max_attempts
		  from task_type
		 where type = $1
	`, typ).Scan(&active, &queue, &maxAttempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, "", 0, err
	}
	return
}

// insert ENQUEUED task; idempotent on idempotency_key.
// returns canonical id/status/queue.
func UpsertEnqueue(ctx context.Context, db *pgxpool.Pool,
	id, typ, queue string, payload []byte, idemKey string, maxAttempts int,
) (outID, outStatus, outQueue string, err error) {
	err = db.QueryRow(ctx, `
		insert into tasks (id, type, queue, status, payload, idempotency_key, max_attempts)
		values ($1, $2, $3, 'ENQUEUED', $4, nullif($5,''), $6)
		on conflict (idempotency_key) do update
		  set updated_at = now()
		returning id, status, queue
	`, id, typ, queue, payload, idemKey, maxAttempts).Scan(&outID, &outStatus, &outQueue)
	return
}
