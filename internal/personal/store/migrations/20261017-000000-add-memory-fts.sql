-- Migrate:UP
-- Derived full-text index for files-backed memory entries. Markdown remains the
-- source of truth; this table can be deleted and rebuilt on the next search.
CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(
    scope UNINDEXED,
    entry_id UNINDEXED,
    text,
    content_hash UNINDEXED,
    updated UNINDEXED,
    tokenize='trigram'
);

-- Migrate:DOWN
DROP TABLE IF EXISTS memory_fts;
