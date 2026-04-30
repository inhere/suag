package session

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func validateID(kind, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s id is required", kind)
	}
	return nil
}

func ensureRequiredTables(db *sql.DB) error {
	requiredTables := []string{
		"sessions",
		"turns",
		"runs",
		"run_events",
		"external_tasks",
		"recaps",
		"memory_candidates",
	}

	missing := make([]string, 0, len(requiredTables))
	for _, tableName := range requiredTables {
		exists, err := tableExists(db, tableName)
		if err != nil {
			return err
		}
		if !exists {
			missing = append(missing, tableName)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required tables; run migrations under db/migrations first: %s", strings.Join(missing, ", "))
	}
	return nil
}

func tableExists(db *sql.DB, tableName string) (bool, error) {
	var exists int
	err := db.QueryRow(`
		SELECT COUNT(1)
		FROM sqlite_master
		WHERE type = 'table' AND name = ?
	`, tableName).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check table %s exists: %w", tableName, err)
	}
	return exists > 0, nil
}

func marshalJSON(value map[string]any) ([]byte, error) {
	if value == nil {
		value = map[string]any{}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal json: %w", err)
	}
	return data, nil
}

func unmarshalJSONMap(data []byte) (map[string]any, error) {
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("unmarshal json: %w", err)
	}
	if out == nil {
		return map[string]any{}, nil
	}
	return out, nil
}

func formatTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339Nano)
}

func parseTime(raw string) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return time.Time{}, nil
	}
	ts, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse time: %w", err)
	}
	return ts, nil
}
