package personal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/inhere/suga/pkg/agentcore/tools/native"
)

// UndoResult is the file-level outcome of one undo operation. Files contains
// the paths successfully restored or deleted; shell commands are intentionally
// outside this result because undo never attempts to reverse shell_exec.
type UndoResult struct {
	SessionID     string   `json:"session_id"`
	Turns         int      `json:"turns"`
	Restored      int      `json:"restored"`
	Deleted       int      `json:"deleted"`
	Skipped       int      `json:"skipped"`
	Unrecoverable int      `json:"unrecoverable"`
	Files         []string `json:"files,omitempty"`
	Failures      []string `json:"failures,omitempty"`
	Forced        bool     `json:"forced"`
}

type restoreOutcome struct {
	action     CheckpointAction
	reason     string
	unresolved bool
	path       string
}

// Undo restores the latest n checkpoints in reverse turn and call order. The
// core owns the lock for the complete operation, so another writer cannot
// interleave a write between conflict detection and restoration.
func (u *UndoCore) Undo(ctx context.Context, sessionQuery string, n int, force bool) (UndoResult, error) {
	var result UndoResult
	if err := u.validate(); err != nil {
		return result, err
	}
	sessionID, err := ResolveSessionID(ctx, u.Repository, sessionQuery)
	if err != nil {
		return result, err
	}
	result.SessionID = sessionID
	result.Forced = force
	err = u.withSession(ctx, sessionID, func() error {
		rows, err := u.checkpointsLocked(ctx, sessionID, 0)
		if err != nil {
			return err
		}
		selected := selectUndoRows(rows, n)
		for _, row := range selected {
			if !row.HasSnapshots() {
				continue
			}
			result.Turns++
			fullyUndone := true
			// Items are persisted in call order. Reversing them is what makes
			// two writes to one path converge on the turn's initial content.
			for i := len(row.Items) - 1; i >= 0; i-- {
				item := row.Items[i]
				outcome, restoreErr := u.restoreItem(ctx, sessionID, row.TurnID, item, force)
				if outcome.action == CheckpointActionRestore || outcome.action == CheckpointActionDelete {
					result.Files = append(result.Files, outcome.path)
				}
				switch outcome.action {
				case CheckpointActionRestore:
					result.Restored++
				case CheckpointActionDelete:
					result.Deleted++
				case CheckpointActionUnrecoverable:
					result.Unrecoverable++
				default:
					result.Skipped++
				}
				if restoreErr != nil {
					result.Failures = append(result.Failures, restoreErr.Error())
					fullyUndone = false
				}
				if outcome.unresolved {
					fullyUndone = false
				}
			}
			state := SnapshotStatePartial
			if fullyUndone {
				state = SnapshotStateUndone
			}
			if err := u.Snapshots.SetCheckpointState(sessionID, row.TurnID, state); err != nil {
				return fmt.Errorf("set checkpoint %s state: %w", row.TurnID, err)
			}
		}
		return nil
	})
	return result, err
}

func selectUndoRows(rows []Checkpoint, n int) []Checkpoint {
	if n <= 0 {
		n = 1
	}
	selected := make([]Checkpoint, 0, n)
	count := 0
	for _, row := range rows {
		if row.IsShellOnly() {
			// Keep shell-only rows visible in the selected range, but there is
			// no file operation to perform for them.
			selected = append(selected, row)
			continue
		}
		if row.State == SnapshotStateUndone {
			continue
		}
		selected = append(selected, row)
		count++
		if count == n {
			break
		}
	}
	return selected
}

func (u *UndoCore) restoreItem(ctx context.Context, sessionID, turnID string, item CheckpointItem, force bool) (restoreOutcome, error) {
	meta := item.Meta
	path := filepath.Join(meta.Root, filepath.FromSlash(meta.Rel))
	out := restoreOutcome{action: CheckpointActionSkip, path: path}
	if item.RootStatus != SnapshotRootTrusted {
		out.reason = item.Reason
		out.unresolved = true
		return out, nil
	}
	if meta.Status != native.SnapshotStatusComplete && meta.Status != native.SnapshotStatusIncomplete {
		out.reason = "快照尚未完成"
		out.unresolved = true
		return out, nil
	}
	switch meta.Kind {
	case native.SnapshotKindFile:
		return u.restoreFile(ctx, sessionID, turnID, meta, force)
	case native.SnapshotKindAbsent:
		return u.deleteAbsent(ctx, sessionID, turnID, meta, force)
	case native.SnapshotKindTrash:
		out.reason = "删除快照将在后续任务恢复"
		out.unresolved = true
		return out, nil
	default:
		out.action = CheckpointActionUnrecoverable
		out.reason = snapshotReason(meta, out.action)
		out.unresolved = true
		return out, nil
	}
}

func (u *UndoCore) restoreFile(ctx context.Context, sessionID, turnID string, meta SnapshotMeta, force bool) (restoreOutcome, error) {
	path := filepath.Join(meta.Root, filepath.FromSlash(meta.Rel))
	out := restoreOutcome{action: CheckpointActionSkip, path: path}
	before, err := u.readBefore(sessionID, turnID, meta)
	if err != nil {
		out.reason = "原始快照缺失或损坏"
		out.unresolved = true
		return out, err
	}
	if meta.SHA256Before == "" || nativeSHA256(before) != meta.SHA256Before {
		out.reason = "原始快照哈希校验失败"
		out.unresolved = true
		return out, nil
	}
	if meta.SHA256After == "" {
		out.reason = "快照缺少写入后哈希"
		out.unresolved = true
		return out, nil
	}

	root, rel, current, status, err := openCurrentFile(meta)
	if err != nil {
		out.reason = "无法读取当前文件"
		out.unresolved = true
		return out, err
	}
	if status == currentAbsent {
		_ = root.Close()
		out.reason = "当前文件已不存在"
		out.unresolved = true
		return out, nil
	}
	if status != currentRegular {
		_ = root.Close()
		out.reason = "当前路径不是普通文件"
		out.unresolved = true
		return out, nil
	}
	defer root.Close()
	currentHash := nativeSHA256(current)
	if currentHash == meta.SHA256Before {
		// The desired state is already present. This is a successful, idempotent
		// undo rather than a conflict and does not need another write.
		return restoreOutcome{action: CheckpointActionSkip, path: path}, nil
	}
	if currentHash != meta.SHA256After && !force {
		out.reason = "冲突：快照之后被改动过"
		out.unresolved = true
		return out, nil
	}

	finish, err := u.preparePreUndo(ctx, sessionID, turnID, meta, current)
	if err != nil {
		out.reason = "回滚前快照失败"
		out.unresolved = true
		return out, err
	}
	if err := writeRestoredFile(root, rel, before, meta.Mode); err != nil {
		_ = finish(native.SnapshotStatusIncomplete, "")
		out.reason = "恢复文件失败"
		out.unresolved = true
		return out, err
	}
	if err := finish(native.SnapshotStatusComplete, meta.SHA256Before); err != nil {
		out.reason = "记录回滚前快照失败"
		out.unresolved = true
		return out, err
	}
	out.action = CheckpointActionRestore
	return out, nil
}

func (u *UndoCore) deleteAbsent(ctx context.Context, sessionID, turnID string, meta SnapshotMeta, force bool) (restoreOutcome, error) {
	path := filepath.Join(meta.Root, filepath.FromSlash(meta.Rel))
	out := restoreOutcome{action: CheckpointActionSkip, path: path}
	root, rel, current, status, err := openCurrentFile(meta)
	if err != nil {
		out.reason = "无法读取当前文件"
		out.unresolved = true
		return out, err
	}
	if status == currentAbsent {
		_ = root.Close()
		return out, nil
	}
	if status != currentRegular {
		_ = root.Close()
		out.reason = "当前路径不是普通文件"
		out.unresolved = true
		return out, nil
	}
	defer root.Close()
	// file_write historically leaves sha256_after empty for kind=absent. If a
	// caller has a newer record with an after hash, apply the same conflict rule.
	if meta.SHA256After != "" && nativeSHA256(current) != meta.SHA256After && !force {
		out.reason = "冲突：快照之后被改动过"
		out.unresolved = true
		return out, nil
	}
	finish, err := u.preparePreUndo(ctx, sessionID, turnID, meta, current)
	if err != nil {
		out.reason = "回滚前快照失败"
		out.unresolved = true
		return out, err
	}
	if err := root.Remove(rel); err != nil {
		_ = finish(native.SnapshotStatusIncomplete, "")
		out.reason = "删除新建文件失败"
		out.unresolved = true
		return out, err
	}
	if err := finish(native.SnapshotStatusComplete, ""); err != nil {
		out.reason = "记录回滚前快照失败"
		out.unresolved = true
		return out, err
	}
	out.action = CheckpointActionDelete
	return out, nil
}

const (
	currentAbsent = iota
	currentRegular
	currentOther
)

func openCurrentFile(meta SnapshotMeta) (*os.Root, string, []byte, int, error) {
	rel := filepath.Clean(filepath.FromSlash(meta.Rel))
	if rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, "", nil, currentOther, errors.New("snapshot path is not relative to its root")
	}
	root, err := os.OpenRoot(meta.Root)
	if err != nil {
		return nil, "", nil, currentOther, err
	}
	info, err := root.Lstat(rel)
	if errors.Is(err, os.ErrNotExist) {
		return root, rel, nil, currentAbsent, nil
	}
	if err != nil {
		_ = root.Close()
		return nil, "", nil, currentOther, err
	}
	if !info.Mode().IsRegular() {
		return root, rel, nil, currentOther, nil
	}
	data, err := root.ReadFile(rel)
	if err != nil {
		_ = root.Close()
		return nil, "", nil, currentOther, err
	}
	return root, rel, data, currentRegular, nil
}

func writeRestoredFile(root *os.Root, rel string, before []byte, mode string) error {
	if dir := filepath.Dir(rel); dir != "." {
		if err := root.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	perm := os.FileMode(0o644)
	if mode != "" {
		value, err := strconv.ParseUint(mode, 8, 32)
		if err != nil {
			return fmt.Errorf("invalid snapshot mode %q: %w", mode, err)
		}
		perm = os.FileMode(value)
	}
	if err := root.WriteFile(rel, before, perm); err != nil {
		return err
	}
	return root.Chmod(rel, perm)
}

func (u *UndoCore) readBefore(sessionID, turnID string, meta SnapshotMeta) ([]byte, error) {
	if meta.File != "before" || meta.Seq <= 0 {
		return nil, errors.New("snapshot before image is not declared")
	}
	path := filepath.Join(u.Snapshots.dir, sessionID, "snapshots", turnID,
		fmt.Sprintf("%03d-%s", meta.Seq, snapshotCallHash(meta.CallID)), "before")
	return os.ReadFile(path)
}

func (u *UndoCore) preparePreUndo(ctx context.Context, sessionID, turnID string, meta SnapshotMeta, current []byte) (func(string, string) error, error) {
	callID := fmt.Sprintf("undo-%d-%s-%d", meta.Seq, meta.CallID, time.Now().UnixNano())
	scope := withObserveScope(ctx, sessionID, turnID)
	before, err := u.Snapshots.BeforeUndo(scope, native.SnapshotRequest{
		CallID: callID, Tool: "undo", Root: meta.Root, Rel: meta.Rel, RealRel: meta.RealRel,
		Kind: native.SnapshotKindFile, Before: current,
	})
	if err != nil {
		return nil, err
	}
	return func(status, after string) error {
		return u.Snapshots.AfterUndo(scope, native.SnapshotResult{
			CallID: callID, Tool: "undo", Root: meta.Root, Rel: meta.Rel, RealRel: meta.RealRel,
			Kind: native.SnapshotKindFile, AfterSHA256: after, Size: int64(len(current)), Status: status,
			Unrecoverable: before.Unrecoverable,
		})
	}, nil
}
