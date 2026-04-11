-- Migrate:UP
CREATE TABLE IF NOT EXISTS sessions (
    session_id    TEXT PRIMARY KEY,
    title         TEXT NOT NULL,
    status        TEXT NOT NULL,
    last_turn_id  TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    metadata_json BLOB NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS turns (
    turn_id         TEXT PRIMARY KEY,
    session_id      TEXT NOT NULL,
    initiator_type  TEXT NOT NULL,
    initiator_id    TEXT NOT NULL DEFAULT '',
    input_message_id TEXT NOT NULL DEFAULT '',
    input_summary   TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL,
    primary_run_id  TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL,
    completed_at    TEXT NOT NULL DEFAULT '',
    metadata_json   BLOB NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS runs (
    run_id              TEXT PRIMARY KEY,
    session_id          TEXT NOT NULL,
    turn_id             TEXT NOT NULL,
    agent_id            TEXT NOT NULL,
    role                TEXT NOT NULL DEFAULT '',
    parent_run_id       TEXT NOT NULL DEFAULT '',
    resume_from_run_id  TEXT NOT NULL DEFAULT '',
    state               TEXT NOT NULL,
    started_at          TEXT NOT NULL,
    ended_at            TEXT NOT NULL DEFAULT '',
    failure_code        TEXT NOT NULL DEFAULT '',
    summary             TEXT NOT NULL DEFAULT '',
    metadata_json       BLOB NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS run_events (
    event_id      TEXT PRIMARY KEY,
    session_id    TEXT NOT NULL,
    turn_id       TEXT NOT NULL,
    run_id        TEXT NOT NULL,
    agent_id      TEXT NOT NULL DEFAULT '',
    event_type    TEXT NOT NULL,
    sequence      INTEGER NOT NULL,
    payload_json  BLOB NOT NULL DEFAULT '{}',
    source        TEXT NOT NULL,
    created_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS external_tasks (
    external_task_id TEXT PRIMARY KEY,
    session_id       TEXT NOT NULL,
    turn_id          TEXT NOT NULL,
    run_id           TEXT NOT NULL,
    adapter          TEXT NOT NULL,
    remote_agent_name TEXT NOT NULL DEFAULT '',
    remote_session_id TEXT NOT NULL DEFAULT '',
    remote_task_id   TEXT NOT NULL DEFAULT '',
    state            TEXT NOT NULL,
    last_heartbeat_at TEXT NOT NULL DEFAULT '',
    result_summary   TEXT NOT NULL DEFAULT '',
    metadata_json    BLOB NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS recaps (
    recap_id      TEXT PRIMARY KEY,
    session_id    TEXT NOT NULL,
    turn_id       TEXT NOT NULL,
    run_id        TEXT NOT NULL DEFAULT '',
    kind          TEXT NOT NULL,
    target        TEXT NOT NULL,
    content       TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    metadata_json BLOB NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_turns_session_id ON turns(session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_runs_session_turn ON runs(session_id, turn_id, started_at);
CREATE INDEX IF NOT EXISTS idx_run_events_session_seq ON run_events(session_id, sequence);
CREATE INDEX IF NOT EXISTS idx_external_tasks_run_id ON external_tasks(run_id);
CREATE INDEX IF NOT EXISTS idx_recaps_session_turn ON recaps(session_id, turn_id, created_at);

-- Migrate:DOWN
DROP TABLE IF EXISTS recaps;
DROP TABLE IF EXISTS external_tasks;
DROP TABLE IF EXISTS run_events;
DROP TABLE IF EXISTS runs;
DROP TABLE IF EXISTS turns;
DROP TABLE IF EXISTS sessions;
