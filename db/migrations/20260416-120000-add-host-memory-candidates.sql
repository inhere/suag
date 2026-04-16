-- Migrate:UP
CREATE TABLE IF NOT EXISTS memory_candidates (
    candidate_id     TEXT PRIMARY KEY,
    session_id       TEXT NOT NULL,
    turn_id          TEXT NOT NULL,
    run_id           TEXT NOT NULL DEFAULT '',
    source_recap_id  TEXT NOT NULL DEFAULT '',
    target           TEXT NOT NULL,
    kind             TEXT NOT NULL,
    content          TEXT NOT NULL,
    score            REAL NOT NULL DEFAULT 0,
    status           TEXT NOT NULL,
    created_at       TEXT NOT NULL,
    metadata_json    BLOB NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_memory_candidates_session_turn
    ON memory_candidates(session_id, turn_id, created_at, candidate_id);

CREATE INDEX IF NOT EXISTS idx_memory_candidates_status_target
    ON memory_candidates(status, target, created_at);

-- Migrate:DOWN
DROP TABLE IF EXISTS memory_candidates;
