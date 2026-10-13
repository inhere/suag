-- Migrate:UP
-- Full-text index over the session ledger (D8) and the memory source side
-- table (T12).
--
-- entries_fts keeps its own rowid and locates its entry by session_id/seq:
-- entries is an implicit-rowid table and VACUUM (suga gc) may renumber it.
--
-- NOTE: the indexed-type list and the role/text expressions of the backfill
-- below MUST stay identical to ftsIndexedTypes/ftsRoleExpr/ftsTextExpr in
-- store/fts.go (AppendEntries indexes new entries with those constants).
-- TestFTSExpressionsMatchMigration guards the two copies against drift.
CREATE VIRTUAL TABLE IF NOT EXISTS entries_fts USING fts5(
    session_id UNINDEXED,
    seq UNINDEXED,
    role UNINDEXED,
    text,
    tokenize='trigram'
);

-- Entries only ever leave through their session (DeleteSession and gc delete
-- sessions; entries follow by ON DELETE CASCADE), so the index is cleaned per
-- session here instead of per entry.
CREATE TRIGGER IF NOT EXISTS entries_fts_session_delete AFTER DELETE ON sessions
BEGIN
    DELETE FROM entries_fts WHERE session_id = old.id;
END;

-- One-shot backfill of the existing ledger. It runs inside this migration's
-- transaction, so Open never scans or writes for it afterwards.
INSERT INTO entries_fts(session_id, seq, role, text)
SELECT session_id, seq, role, text FROM (
    SELECT session_id, seq,
        type AS role,
        CASE
            WHEN NOT json_valid(json) THEN ''
            WHEN type = 'compaction' THEN trim(coalesce(json_extract(json, '$.summary'), ''), char(32, 9, 10, 13))
            ELSE trim(coalesce(json_extract(json, '$.Content'), '') ||
                CASE WHEN coalesce(json_extract(json, '$.ToolResult.Content'), '') IN ('', coalesce(json_extract(json, '$.Content'), ''))
                    THEN '' ELSE char(10) || json_extract(json, '$.ToolResult.Content') END, char(32, 9, 10, 13))
        END AS text
    FROM entries
    WHERE type IN ('user', 'assistant', 'tool', 'compaction')
) WHERE text <> '';

-- Source/taint/tombstone metadata of memory entries; a tombstone is a row
-- whose deleted_at is set. Used from T12 on.
CREATE TABLE IF NOT EXISTS memory_meta (
    backend     TEXT NOT NULL,
    scope       TEXT NOT NULL,
    entry_id    TEXT NOT NULL,
    session_id  TEXT,
    turn_id     TEXT,
    source      TEXT,
    tainted     INTEGER,
    deleted_at  TEXT,
    fingerprint TEXT,
    created_at  TEXT,
    updated_at  TEXT,
    PRIMARY KEY (backend, scope, entry_id)
);
CREATE INDEX IF NOT EXISTS idx_memory_meta_fingerprint ON memory_meta(fingerprint);

-- Migrate:DOWN
DROP INDEX IF EXISTS idx_memory_meta_fingerprint;
DROP TABLE IF EXISTS memory_meta;
DROP TRIGGER IF EXISTS entries_fts_session_delete;
DROP TABLE IF EXISTS entries_fts;
