CREATE TABLE IF NOT EXISTS task_type (
    type TEXT PRIMARY KEY, 
    active BOOLEAN NOT NULL DEFAULT true, 
    default_queue TEXT NOT NULL, 
    default_max_attempts INT NOT NULL DEFAULT 5, 
    payload_schema JSONB
);