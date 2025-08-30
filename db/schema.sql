CREATE TABLE IF NOT EXISTS tasks (
    id               TEXT PRIMARY KEY,
    type             TEXT NOT NULL,
    queue            TEXT NOT NULL,
    status           TEXT NOT NULL CHECK (
        status IN ('ENQUEUED', 'RUNNING', 'SUCCEEDED', 'FAILED', 'DLQ')
    ),
    attempts         INTEGER NOT NULL DEFAULT 0 CHECK(attempts >= 0),
    max_attempts      INTEGER NOT NULL DEFAULT 5 CHECK(max_attempts >= 1), 
    last_error       TEXT,
    result           JSONB,
    payload          JSONB NOT NULL,
    idempotency_key  TEXT UNIQUE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tasks_type_fk
      FOREIGN KEY (type) REFERENCES task_type(type) ON DELETE RESTRICT
);

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_set_updated_at ON tasks;
CREATE TRIGGER trg_set_updated_at
    BEFORE UPDATE ON tasks
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();