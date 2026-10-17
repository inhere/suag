-- Migrate:UP
-- Handoff summaries become searchable (design D8): entries_fts gains the
-- 'handoff' role, whose text is the record's $.summary - the same expression
-- the compaction branch uses.
--
-- NOTE: the indexed-type list and the role/text expressions below MUST stay
-- identical to ftsIndexedTypes/ftsRoleExpr/ftsTextExpr in store/fts.go
-- (AppendEntries indexes new entries with those constants).
-- TestFTSExpressionsMatchMigration guards the copies against drift.
--
-- The earlier migration has already indexed the conversation types of an
-- existing ledger, so this backfill only adds the rows the index is still
-- missing: inserting everything again would duplicate those rows.
INSERT INTO entries_fts(session_id, seq, role, text)
SELECT session_id, seq, role, text FROM (
    SELECT session_id, seq,
        type AS role,
        CASE
            WHEN NOT json_valid(json) THEN ''
            WHEN type IN ('compaction', 'handoff') THEN trim(coalesce(json_extract(json, '$.summary'), ''), char(32, 9, 10, 13))
            ELSE trim(coalesce(json_extract(json, '$.Content'), '') ||
                CASE WHEN coalesce(json_extract(json, '$.ToolResult.Content'), '') IN ('', coalesce(json_extract(json, '$.Content'), ''))
                    THEN '' ELSE char(10) || json_extract(json, '$.ToolResult.Content') END, char(32, 9, 10, 13))
        END AS text
    FROM entries
    WHERE type IN ('user', 'assistant', 'tool', 'compaction', 'handoff')
    -- entries is qualified on purpose: an unqualified session_id/seq would
    -- resolve to entries_fts's own columns inside the subquery and match
    -- everything.
    AND NOT EXISTS (SELECT 1 FROM entries_fts f WHERE f.session_id = entries.session_id AND f.seq = entries.seq)
) WHERE text <> '';

-- Migrate:DOWN
DELETE FROM entries_fts WHERE role = 'handoff';
