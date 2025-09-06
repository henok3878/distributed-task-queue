distributed-task-queue

A minimal distributed task queue built with Go, RabbitMQ, and Postgres. It exposes an HTTP API to enqueue tasks, workers to process them with retries and backoff, and Prometheus metrics for visibility.

## Features

- Reliable enqueue with idempotency keys and per-type defaults
- RabbitMQ topology with priorities, retry queues (TTL), and a DLQ
- Postgres persistence with simple task state machine and event log
- Worker example with per-attempt backoff strategies and prefetch control
- Health checks and a focused Prometheus `/metrics` endpoint

## Architecture

- Postgres stores task rows and an event log.
  - Tasks transition: `ENQUEUED → RUNNING → SUCCEEDED | FAILED` (with scheduled retries).
  - Idempotency via a unique `idempotency_key` on `tasks`.
- RabbitMQ handles delivery:
  - Main direct exchange routes to queues (e.g., `tasks.default`, `tasks.high`).
  - Per-priority retry queues (e.g., `tasks.retry.default`) dead-letter back to the main exchange after a TTL.
  - DLX (`tasks.dlx`) routes terminal failures to a DLQ for inspection.
- API service:
  - `POST /enqueue` writes to Postgres and publishes a small message envelope to RabbitMQ.
  - `GET /tasks/{id}` returns current task state.
  - `GET /healthz` checks DB + RabbitMQ topology.
  - `GET /metrics` exposes Prometheus metrics from a custom registry.
- Worker(s):
  - Consume from each priority queue, lock the task row, mark running, execute handler, record result or schedule retry/failure.

Key code entry points:

- API bootstrap: `cmd/api/main.go` and `cmd/api/main.go`
- Enqueue handler: `internal/api/enqueue.go`
- Task read handler: `internal/api/tasks.go`
- Health handler: `internal/api/health.go`
- Metrics: `internal/metrics/metrics.go`
- RMQ topology: `internal/rmq/topology.go`, initializer `cmd/rmq-init/main.go`
- Worker example: `examples/worker/main.go`

## Data Model

Tables (see `db/*.sql`):

- `task_type`: registry of allowed task types with defaults and optional schema (`db/type_registry.sql`).
- `tasks`: persisted tasks with status, attempts, result, payload, and `idempotency_key` (`db/schema.sql`).
- `task_events`: append-only per-task event log with triggers on insert/update (`db/events.sql`).

Statuses: `ENQUEUED`, `RUNNING`, `SUCCEEDED`, `FAILED` (and `DLQ` enumerated for completeness).

Triggers populate `task_events` on inserts and on status changes, including `RETRY` notes with `last_error`.

## Quickstart

Prerequisites:

- Go 1.23+
- Docker + Docker Compose
- Make

1. Copy env and adjust as needed

```
cp .env.example .env
# edit .env as needed (DB, RMQ, QUEUES, etc.)
```

2. Start Postgres and RabbitMQ

```
make up
```

3. Apply the database schema

```
make db-apply
```

4. Seed a sample task type (optional but handy for testing)

```
make db-seed-type
```

5. Ensure RabbitMQ topology (exchanges/queues/bindings based on env)

```
make init
```

6. Run the API

```
make api
# listens on HTTP_PORT (e.g., :8080)
```

7. Run a worker in another terminal

```
go run examples/worker/main.go
```

8. Enqueue a task

```
curl -s -X POST localhost:8080/enqueue \
  -H 'content-type: application/json' \
  -d '{
        "type": "email.send.v1",
        "payload": {"to":"a@b.com","subj":"hi"},
        "idempotency_key": "demo-1"
      }'
```

9. Fetch task status

```
curl -s localhost:8080/tasks/<task_id>
```

RabbitMQ Management UI: http://localhost:15672 (default creds in `.env`).

## Configuration

Required (API/worker):

- `HTTP_PORT`: API listen address, e.g. `:8080` (`cmd/api/main.go`)
- `DB_DSN`: Postgres DSN, e.g. `postgres://postgres:postgres@localhost:5432/appdb?sslmode=disable` (`cmd/api/main.go`)
- `RMQ_USER`, `RMQ_PASS`, `RMQ_HOST`, `RMQ_PORT`, `RMQ_VHOST`: RabbitMQ connection (`internal/rmq/url.go`)
- `RMQ_NAMESPACE`: e.g. `tasks` (used for exchange/queue names) (`internal/rmq/topology.go`)
- `QUEUES`: CSV list of routing keys, e.g. `default,high` (`internal/rmq/topology.go`)

Backoff (worker):

- Strategy select via `BACKOFF_STRATEGY`: `list` (default) | `fixed` | `exponential` (`internal/backoff/backoff.go:56`)
  - list: `BACKOFFS="5s,30s,2m,10m,1h"`
  - fixed: `BACKOFF_FIXED="30s"`
  - exponential: `BACKOFF_BASE`, `BACKOFF_FACTOR`, `BACKOFF_MAX`, `BACKOFF_JITTER`
- `WORKER_PREFETCH`: unacked message prefetch per consumer (default `32`).

Compose-only helpers (for `make up`):

- `PG_USER`, `PG_PASSWORD`, `PG_DATABASE` for the containerized Postgres.

## RabbitMQ Topology

Initializer (`cmd/rmq-init`) is idempotent and derives names from `RMQ_NAMESPACE` and `QUEUES`:

- Exchanges: `<ns>.direct` (main), `<ns>.dlx` (dead-letter)
- Queues: `<ns>.<rk>` for each routing key; `<ns>.dlq` for global dead letters
- Retry queues: `<ns>.retry.<rk>` with `x-dead-letter-exchange=<ns>.direct` so messages re-enter the main flow after TTL

The worker publishes retries directly to the retry queue with a per-message TTL; the queue then dead-letters back to the main exchange with the original routing key.

## API Reference

- GET `/` → service info
- GET `/healthz` → checks DB ping, existence of exchanges/queues (`internal/api/health.go`)
- POST `/enqueue` → create or coalesce a task
  - Body:
    - `type` (string, required)
    - `payload` (JSON, required)
    - `queue` (string, optional; must be one of `QUEUES` if provided)
    - `max_attempts` (int, optional; default from `task_type`)
    - `idempotency_key` (string, optional; coalesces duplicate requests)
  - On success: HTTP 201 with `{id,status,queue}`
  - Errors: 400 on validation/unknown type, 503 on RMQ publish, 500 on DB errors
  - Source: `internal/api/enqueue.go`
- GET `/tasks/{id}` → current task state with a parsed `result` field (`internal/api/tasks.go`)

## Worker Behavior

- Consumes from every priority queue (`examples/worker/main.go`).
- For each message `{id,type}`:
  - Begin DB tx; `SELECT ... FOR UPDATE` the task row (`internal/store/tasks_worker.go`).
  - If already `SUCCEEDED`, ack and skip (idempotent re-consume).
  - Guard `attempts < max_attempts`.
  - Mark `RUNNING` and increment attempts.
  - Execute handler (`examples/worker/main.go`).
    - On success: `SUCCEEDED` with `result` JSON → ack.
    - On error with attempts left: set `ENQUEUED` + `last_error`, commit, publish to retry queue with TTL (backoff), ack.
    - On terminal error: mark `FAILED`, publish to DLX with routing key, ack.

Default demo handler implements `email.send.v1` with a stub response.

## Prometheus Metrics

Endpoint: `GET /metrics` exposes a custom registry only containing app metrics (`internal/metrics/metrics.go`).

- Registered in `metrics.MustRegisterAll()` (`cmd/api/main.go`).
- Exposed via `metrics.Expose(mux, "GET /metrics")` (`cmd/api/main.go`).

Metrics:

- `dq_enqueue_total{type,queue,status}`: Counter of enqueue attempts (`ok|error`).
- `dq_enqueue_latency_seconds{type,queue}`: Histogram of enqueue handler latency.

Examples (PromQL):

- Error rate by type/queue: `sum(rate(dq_enqueue_total{status="error"}[5m])) by (type, queue)`
- P95 latency: `histogram_quantile(0.95, sum(rate(dq_enqueue_latency_seconds_bucket[5m])) by (le, type, queue))`

Note: Runtime/process metrics are not exported by default. To include them, register `prometheus.NewGoCollector()` and `prometheus.NewProcessCollector(...)` into the custom registry in `internal/metrics/metrics.go`.

## Make Targets

- `make up` / `make down` / `make destroy` – manage Postgres and RabbitMQ containers
- `make db-apply` – apply schema and triggers
- `make db-seed-type` – seed a sample type (`email.send.v1 → high`)
- `make init` – ensure RabbitMQ exchanges/queues/bindings (idempotent)
- `make api` – run the API locally
- `make logs` / `make ps` / `make config` – inspect containers
- `make db-shell` – psql inside the container

## Operational Notes

- Idempotency: repeated `POST /enqueue` with the same `idempotency_key` returns the original task id/queue/status without duplicating work (`internal/store/tasks_enqueue.go`).
- Observability: instrument more endpoints by adding counters/histograms to `internal/metrics/metrics.go` and registering them.
- Resiliency: worker uses transactional writes-before-ack to avoid losing results in the face of failures.
- Cleanup: data volumes are preserved across `make down`; use `make destroy` for a fresh slate.

## License

MIT
