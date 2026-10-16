package personal

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore/redact"
	"github.com/inhere/suga/pkg/agentcore/tools/native"
)

// CheckpointAction is the work an undo implementation may perform for one
// snapshot. T4a only classifies records; T5 supplies the conflict checks and
// the actual file operations.
type CheckpointAction string

const (
	CheckpointActionRestore       CheckpointAction = "restore"
	CheckpointActionDelete        CheckpointAction = "delete"
	CheckpointActionSkip          CheckpointAction = "skip"
	CheckpointActionUnrecoverable CheckpointAction = "unrecoverable"
)

// CheckpointItem is the user-visible classification of one write-tool call.
type CheckpointItem struct {
	Meta   SnapshotMeta     `json:"meta"`
	Action CheckpointAction `json:"action"`
	Reason string           `json:"reason,omitempty"`
}

// ShellCommand is a redacted shell_exec event associated with a checkpoint.
// shell_exec is intentionally reported, not included in the undo operation.
type ShellCommand struct {
	Command   string    `json:"command"`
	TaskID    string    `json:"task_id,omitempty"`
	Status    string    `json:"status,omitempty"`
	StartedAt time.Time `json:"started_at"`
}

// Checkpoint describes one turn with file snapshots or shell activity. A
// shell-only turn is retained in the list so selecting the latest snapshot
// cannot silently hide an intervening command that may have changed files.
type Checkpoint struct {
	SessionID string           `json:"session_id"`
	TurnID    string           `json:"turn_id"`
	StartedAt time.Time        `json:"started_at"`
	State     SnapshotState    `json:"state,omitempty"`
	Items     []CheckpointItem `json:"items,omitempty"`
	Shell     []ShellCommand   `json:"shell,omitempty"`

	// Summary counts are deliberately derived from the directory and event
	// sources on every read; no checkpoint index is added to the database.
	Calls         int   `json:"calls"`
	Files         int   `json:"files"`
	Bytes         int64 `json:"bytes"`
	Restorable    int   `json:"restorable"`
	Deletable     int   `json:"deletable"`
	Skipped       int   `json:"skipped"`
	Unrecoverable int   `json:"unrecoverable"`
	ShellOnly     bool  `json:"shell_only"`
}

// HasSnapshots reports whether this turn can be selected as an undo target.
func (c Checkpoint) HasSnapshots() bool { return len(c.Items) > 0 }

// ShellOnly reports whether this row has shell activity but no file snapshots.
func (c Checkpoint) IsShellOnly() bool { return c.ShellOnly }

// ErrSessionPrefixAmbiguous means a session query matched more than one
// persisted session. The caller must use a longer prefix or the full ID.
var ErrSessionPrefixAmbiguous = errors.New("session prefix is ambiguous")

// ResolveSessionID accepts a complete ID or a unique prefix. Exact matches win
// so a full ID remains usable even if another session starts with the same text.
func ResolveSessionID(ctx context.Context, repo *store.Store, query string) (string, error) {
	if repo == nil {
		return "", errors.New("personal store is nil")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("%w: empty session id", store.ErrSessionNotFound)
	}
	if session, err := repo.GetSession(ctx, query); err == nil {
		return session.ID, nil
	} else if !errors.Is(err, store.ErrSessionNotFound) {
		return "", err
	}

	sessions, err := repo.ListSessions(ctx)
	if err != nil {
		return "", err
	}
	var matches []string
	for _, session := range sessions {
		if strings.HasPrefix(session.ID, query) {
			matches = append(matches, session.ID)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("%w: %s", store.ErrSessionNotFound, query)
	case 1:
		return matches[0], nil
	default:
		sort.Strings(matches)
		return "", fmt.Errorf("%w %q: %s", ErrSessionPrefixAmbiguous, query, strings.Join(matches, ", "))
	}
}

// ListCheckpoints returns all turns that have either a file snapshot or a
// shell_exec event, newest first. limit applies to displayed rows; zero means
// no limit. The turn order is the database's started_at,id order, reversed
// here rather than inferred from snapshot directory names.
func ListCheckpoints(ctx context.Context, repo *store.Store, snapshots *SessionSnapshots, sessionQuery string, limit int) ([]Checkpoint, error) {
	if repo == nil || snapshots == nil {
		return nil, errors.New("checkpoint store is nil")
	}
	sessionID, err := ResolveSessionID(ctx, repo, sessionQuery)
	if err != nil {
		return nil, err
	}
	turns, err := repo.ListTurns(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list checkpoint turns: %w", err)
	}
	events, err := repo.ListEvents(ctx, store.EventFilter{SessionID: sessionID})
	if err != nil {
		return nil, fmt.Errorf("list checkpoint events: %w", err)
	}
	shellByTurn := shellEventsByTurn(events)

	rows := make([]Checkpoint, 0, len(turns))
	for i := len(turns) - 1; i >= 0; i-- {
		turn := turns[i]
		metas, err := snapshots.LoadTurn(sessionID, turn.ID)
		if err != nil {
			return nil, fmt.Errorf("load checkpoint %s: %w", turn.ID, err)
		}
		shell := shellByTurn[turn.ID]
		if len(metas) == 0 && len(shell) == 0 {
			continue
		}
		row, err := checkpointFromTurn(sessionID, turn, metas, shell, snapshots)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
		if limit > 0 && len(rows) >= limit {
			break
		}
	}
	return rows, nil
}

// SelectUndoCheckpoints selects the latest n turns that have snapshots and are
// not already undone. Shell-only rows encountered while scanning are included
// before the selected rows, making the omission visible to chat and CLI.
func SelectUndoCheckpoints(ctx context.Context, repo *store.Store, snapshots *SessionSnapshots, sessionQuery string, n int) ([]Checkpoint, error) {
	if n <= 0 {
		n = 1
	}
	all, err := ListCheckpoints(ctx, repo, snapshots, sessionQuery, 0)
	if err != nil {
		return nil, err
	}
	out := make([]Checkpoint, 0, n)
	selected := 0
	for _, row := range all {
		if row.IsShellOnly() {
			out = append(out, row)
			continue
		}
		if row.State == SnapshotStateUndone {
			continue
		}
		out = append(out, row)
		selected++
		if selected == n {
			break
		}
	}
	return out, nil
}

// checkpointFromTurn turns the two authoritative stores into a display model.
// Snapshot directories are the source of file facts; events are only the source
// of shell facts, so a future change to event argument rendering cannot invent
// a synthetic shell snapshot.
func checkpointFromTurn(sessionID string, turn store.Turn, metas []SnapshotMeta, shell []ShellCommand, snapshots *SessionSnapshots) (Checkpoint, error) {
	state := SnapshotStateOpen
	if len(metas) > 0 {
		loaded, err := snapshots.LoadCheckpointState(sessionID, turn.ID)
		if err != nil {
			return Checkpoint{}, fmt.Errorf("load checkpoint state %s: %w", turn.ID, err)
		}
		state = loaded.State
	}
	row := Checkpoint{SessionID: sessionID, TurnID: turn.ID, StartedAt: turn.StartedAt, State: state, Shell: shell}
	for _, meta := range metas {
		item := CheckpointItem{Meta: meta, Action: classifySnapshot(meta)}
		item.Reason = snapshotReason(meta, item.Action)
		row.Items = append(row.Items, item)
		row.Calls++
		row.Bytes += meta.Size
		if meta.Kind != native.SnapshotKindPurged {
			row.Files++
		}
		switch item.Action {
		case CheckpointActionRestore:
			row.Restorable++
		case CheckpointActionDelete:
			row.Deletable++
		case CheckpointActionUnrecoverable:
			row.Unrecoverable++
		default:
			row.Skipped++
		}
	}
	row.ShellOnly = len(row.Items) == 0 && len(row.Shell) > 0
	return row, nil
}

func classifySnapshot(meta SnapshotMeta) CheckpointAction {
	if meta.Unrecoverable != "" || meta.Kind == native.SnapshotKindPurged {
		return CheckpointActionUnrecoverable
	}
	if meta.Status == "pending" {
		return CheckpointActionSkip
	}
	switch meta.Kind {
	case native.SnapshotKindFile, native.SnapshotKindTrash:
		return CheckpointActionRestore
	case native.SnapshotKindAbsent:
		return CheckpointActionDelete
	default:
		return CheckpointActionSkip
	}
}

func snapshotReason(meta SnapshotMeta, action CheckpointAction) string {
	if meta.Unrecoverable != "" {
		return meta.Unrecoverable
	}
	switch action {
	case CheckpointActionDelete:
		return "本回合新建的文件"
	case CheckpointActionUnrecoverable:
		return "文件已永久删除或快照不可恢复"
	case CheckpointActionSkip:
		if meta.Status == "pending" {
			return "快照尚未完成"
		}
		return "快照记录不完整"
	}
	return ""
}

func shellEventsByTurn(events []store.Event) map[string][]ShellCommand {
	byTurn := map[string][]ShellCommand{}
	for _, event := range events {
		if event.Kind != store.EventTool || event.Name != "shell_exec" || event.TurnID == "" {
			continue
		}
		command := shellEventCommand(event.Attrs)
		if command == "" {
			continue
		}
		byTurn[event.TurnID] = append(byTurn[event.TurnID], ShellCommand{
			Command: redact.Secrets(command), TaskID: shellEventString(event.Attrs, "task_id"),
			Status: event.Status, StartedAt: event.StartedAt,
		})
	}
	for turnID, commands := range byTurn {
		if len(commands) > 5 {
			byTurn[turnID] = commands[:5]
		}
	}
	return byTurn
}

func shellEventCommand(attrs map[string]any) string {
	for _, key := range []string{"command", "arg"} {
		if command := shellEventString(attrs, key); command != "" {
			return command
		}
	}
	return ""
}

func shellEventString(attrs map[string]any, key string) string {
	value, _ := attrs[key].(string)
	return strings.TrimSpace(value)
}
