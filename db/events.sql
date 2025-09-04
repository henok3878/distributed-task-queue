-- per-task event log
CREATE TABLE IF NOT EXISTS task_events (
    id       BIGSERIAL PRIMARY KEY,
    task_id  TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    event    TEXT NOT NULL CHECK (event IN ('ENQUEUED','RUNNING','SUCCEEDED','FAILED','RETRY','DLQ')),
    note     TEXT,
    at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- index for lookups and ordering
CREATE INDEX IF NOT EXISTS task_events_task_id_at_idx
    ON task_events (task_id, at);

-- insert trigger: when a task row is inserted (ENQUEUED), record an event
CREATE OR REPLACE FUNCTION trg_task_events_insert()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO task_events(task_id, event, note)
    VALUES (NEW.id, NEW.status, NULL);
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_task_events_after_insert ON tasks;
CREATE TRIGGER trg_task_events_after_insert
AFTER INSERT ON tasks
FOR EACH ROW EXECUTE FUNCTION trg_task_events_insert();

-- update trigger: when status changes, record an event
-- if we go RUNNING -> ENQUEUED, that means a scheduled retry
CREATE OR REPLACE FUNCTION trg_task_events_update()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status IS DISTINCT FROM OLD.status THEN
        IF OLD.status = 'RUNNING' AND NEW.status = 'ENQUEUED' THEN
            INSERT INTO task_events(task_id, event, note)
            VALUES (NEW.id, 'RETRY', NEW.last_error);
        ELSE
            INSERT INTO task_events(task_id, event, note)
            VALUES (NEW.id, NEW.status, NEW.last_error);
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_task_events_after_update ON tasks;
CREATE TRIGGER trg_task_events_after_update
AFTER UPDATE OF status, last_error ON tasks
FOR EACH ROW EXECUTE FUNCTION trg_task_events_update();
