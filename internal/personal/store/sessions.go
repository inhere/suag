package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const sessionCols = `id, title, cwd, status, last_turn_id, created_at, updated_at, meta_json`

type rowScanner interface{ Scan(dest ...any) error }

func scanSession(r rowScanner) (Session, error) {
	var (
		s                Session
		created, updated string
		metaJSON         string
	)
	if err := r.Scan(&s.ID, &s.Title, &s.Cwd, &s.Status, &s.LastTurnID, &created, &updated, &metaJSON); err != nil {
		return Session{}, err
	}
	s.CreatedAt, s.UpdatedAt = parseTime(created), parseTime(updated)
	s.Meta = map[string]any{}
	if metaJSON != "" {
		if err := json.Unmarshal([]byte(metaJSON), &s.Meta); err != nil {
			return Session{}, fmt.Errorf("decode session meta: %w", err)
		}
	}
	return s, nil
}

func encodeMeta(m map[string]any) (string, error) {
	if m == nil {
		return "{}", nil
	}
	b, err := json.Marshal(m)
	return string(b), err
}

// CreateSession inserts a new session. Empty Status defaults to open and zero
// timestamps to now.
func (s *Store) CreateSession(ctx context.Context, in Session) error {
	if in.ID == "" {
		return errors.New("session id is required")
	}
	now := time.Now()
	if in.CreatedAt.IsZero() {
		in.CreatedAt = now
	}
	if in.UpdatedAt.IsZero() {
		in.UpdatedAt = in.CreatedAt
	}
	if in.Status == "" {
		in.Status = SessionOpen
	}
	meta, err := encodeMeta(in.Meta)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO sessions(`+sessionCols+`) VALUES (?,?,?,?,?,?,?,?)`,
		in.ID, in.Title, in.Cwd, in.Status, in.LastTurnID, formatTime(in.CreatedAt), formatTime(in.UpdatedAt), meta)
	if err != nil {
		return fmt.Errorf("create session %q: %w", in.ID, err)
	}
	return nil
}

// GetSession returns the session or ErrSessionNotFound.
func (s *Store) GetSession(ctx context.Context, id string) (Session, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM sessions WHERE id = ?`, id)
	out, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrSessionNotFound
	}
	return out, err
}

// ListSessions returns all sessions, most recently updated first.
func (s *Store) ListSessions(ctx context.Context) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+sessionCols+` FROM sessions ORDER BY updated_at DESC, created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		item, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// UpdateSession overwrites title, cwd, status, last_turn_id and meta of an
// existing session. UpdatedAt defaults to now when zero.
func (s *Store) UpdateSession(ctx context.Context, in Session) error {
	if in.UpdatedAt.IsZero() {
		in.UpdatedAt = time.Now()
	}
	meta, err := encodeMeta(in.Meta)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE sessions SET title=?, cwd=?, status=?, last_turn_id=?, updated_at=?, meta_json=? WHERE id=?`,
		in.Title, in.Cwd, in.Status, in.LastTurnID, formatTime(in.UpdatedAt), meta, in.ID)
	if err != nil {
		return fmt.Errorf("update session %q: %w", in.ID, err)
	}
	return requireAffected(res, ErrSessionNotFound)
}

// DeleteSession removes a session with its turns and entries (ON DELETE
// CASCADE). Events are kept: they have no foreign key and record spent tokens.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete session %q: %w", id, err)
	}
	return requireAffected(res, ErrSessionNotFound)
}

// PatchSessionMeta merges patch into meta_json atomically (a nil value deletes
// the key), so concurrent processes cannot roll back each other's keys.
func (s *Store) PatchSessionMeta(ctx context.Context, id string, patch map[string]any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// The first statement is a write so the transaction takes the write lock
	// up front (a read-then-write upgrade can fail with SQLITE_BUSY at once).
	res, err := tx.ExecContext(ctx, `UPDATE sessions SET updated_at = ? WHERE id = ?`, formatTime(time.Now()), id)
	if err != nil {
		return err
	}
	if err := requireAffected(res, ErrSessionNotFound); err != nil {
		return err
	}
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT meta_json FROM sessions WHERE id = ?`, id).Scan(&raw); err != nil {
		return err
	}
	meta := map[string]any{}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &meta); err != nil {
			return err
		}
	}
	for k, v := range patch {
		if v == nil {
			delete(meta, k)
		} else {
			meta[k] = v
		}
	}
	enc, err := encodeMeta(meta)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET meta_json = ? WHERE id = ?`, enc, id); err != nil {
		return err
	}
	return tx.Commit()
}

func requireAffected(res sql.Result, notFound error) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return notFound
	}
	return nil
}
