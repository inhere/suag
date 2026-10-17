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
-- existing ledger, and no handoff record was indexed before this one, so the
-- backfill takes only the handoff rows. (A first version checked every entry
-- with NOT EXISTS against entries_fts, whose session_id/seq are not indexed:
-- a full scan per row, which never finished on a large ledger and blocked
-- every start of v0.31.0.)
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
    AND type = 'handoff'
) WHERE text <> '';

-- Migrate:DOWN
DELETE FROM entries_fts WHERE role = 'handoff';
