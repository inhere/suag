-- Migrate:UP
-- Core store for personal sessions: one linear entry log per session, plus
-- turns (agentcore run state) and observability events.
CREATE TABLE IF NOT EXISTS sessions (
    id           TEXT PRIMARY KEY,
    title        TEXT NOT NULL DEFAULT '',
    cwd          TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'open',
    last_turn_id TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    meta_json    TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS turns (
    id          TEXT PRIMARY KEY,
    session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    status      TEXT NOT NULL,
    stop_reason TEXT NOT NULL DEFAULT '',
    state_json  TEXT NOT NULL DEFAULT '',
    started_at  TEXT NOT NULL,
    ended_at    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_turns_session ON turns(session_id, started_at);

CREATE TABLE IF NOT EXISTS entries (
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    seq        INTEGER NOT NULL,
    turn_id    TEXT NOT NULL DEFAULT '',
    type       TEXT NOT NULL,
    json       TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (session_id, seq)
);
CREATE INDEX IF NOT EXISTS idx_entries_turn ON entries(turn_id);

CREATE TABLE IF NOT EXISTS events (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id         TEXT NOT NULL,
    turn_id            TEXT NOT NULL DEFAULT '',
    kind               TEXT NOT NULL,
    name               TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL DEFAULT '',
    started_at         TEXT NOT NULL,
    duration_ms        INTEGER NOT NULL DEFAULT 0,
    ttft_ms            INTEGER NOT NULL DEFAULT 0,
    input_tokens       INTEGER NOT NULL DEFAULT 0,
    output_tokens      INTEGER NOT NULL DEFAULT 0,
    cache_read_tokens  INTEGER NOT NULL DEFAULT 0,
    cache_write_tokens INTEGER NOT NULL DEFAULT 0,
    error              TEXT NOT NULL DEFAULT '',
    attrs_json         TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_events_session ON events(session_id);
CREATE INDEX IF NOT EXISTS idx_events_started ON events(started_at);

-- Migrate:DOWN
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS entries;
DROP TABLE IF EXISTS turns;
DROP TABLE IF EXISTS sessions;
