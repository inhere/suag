package personal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/inhere/suga/pkg/agentcore/tools/native"
)

type trashOrigin struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	RemovedAt string `json:"removed_at"`
}

// restoreTrash puts back the one trash entry created by file_remove. The
// origin file is the authority for the source basename and destination; this
// prevents an unrelated trash directory from being restored over a session
// path. Purged directories deliberately make the checkpoint partial.
func (u *UndoCore) restoreTrash(ctx context.Context, _ string, _ string, meta SnapshotMeta) (restoreOutcome, error) {
	path := filepath.Join(meta.Root, filepath.FromSlash(meta.Rel))
	out := restoreOutcome{action: CheckpointActionSkip, path: path}
	if err := ctx.Err(); err != nil {
		out.unresolved = true
		return out, err
	}
	if len(meta.TrashEntries) != 1 {
		out.reason = "删除快照缺少唯一回收区条目"
		out.unresolved = true
		return out, nil
	}
	entry := filepath.Clean(meta.TrashEntries[0])
	info, err := os.Lstat(entry)
	if errors.Is(err, os.ErrNotExist) {
		out.action = CheckpointActionUnrecoverable
		out.reason = "回收区条目已过期或不存在"
		out.unresolved = true
		return out, nil
	}
	if err != nil {
		out.reason = "无法读取回收区条目"
		out.unresolved = true
		return out, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		out.reason = "回收区条目不是安全目录"
		out.unresolved = true
		return out, nil
	}

	originData, err := os.ReadFile(filepath.Join(entry, "origin.json"))
	if err != nil {
		out.reason = "回收区来源记录缺失"
		out.unresolved = true
		return out, err
	}
	var origin trashOrigin
	if err := json.Unmarshal(originData, &origin); err != nil {
		out.reason = "回收区来源记录损坏"
		out.unresolved = true
		return out, err
	}
	if origin.RemovedAt != "" {
		removedAt, err := time.Parse(time.RFC3339, origin.RemovedAt)
		if err != nil {
			out.reason = "回收区来源时间损坏"
			out.unresolved = true
			return out, err
		}
		if time.Now().UTC().After(removedAt.Add(native.TrashRetention)) {
			out.action = CheckpointActionUnrecoverable
			out.reason = "回收区条目已超过 7 天保留期"
			out.unresolved = true
			return out, nil
		}
	}
	if !sameUndoPath(origin.Path, path) {
		out.reason = "回收区来源与快照目标不一致"
		out.unresolved = true
		return out, nil
	}
	sourceName := origin.Name
	if sourceName == "" {
		sourceName = filepath.Base(origin.Path)
	}
	if !safeTrashName(sourceName) {
		out.reason = "回收区来源文件名无效"
		out.unresolved = true
		return out, nil
	}
	expectedName := filepath.Base(origin.Path)
	if !sameUndoName(sourceName, expectedName) && !(sameUndoName(expectedName, "origin.json") && sourceName == ".suga-content") {
		out.reason = "回收区来源文件名与目标不一致"
		out.unresolved = true
		return out, nil
	}
	source := filepath.Join(entry, sourceName)
	sourceInfo, err := os.Lstat(source)
	if errors.Is(err, os.ErrNotExist) {
		out.action = CheckpointActionUnrecoverable
		out.reason = "回收区内容已过期或不存在"
		out.unresolved = true
		return out, nil
	}
	if err != nil {
		out.reason = "无法读取回收区内容"
		out.unresolved = true
		return out, err
	}
	if !sourceInfo.IsDir() && sourceInfo.Mode()&os.ModeSymlink == 0 && (!sourceInfo.Mode().IsRegular() || native.IsHardLinked(source, sourceInfo)) {
		out.reason = "回收区内容是硬链接或特殊文件"
		out.unresolved = true
		return out, nil
	}
	if _, err := os.Lstat(path); err == nil {
		out.reason = "冲突：原位置已有文件"
		out.unresolved = true
		return out, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		out.reason = "无法检查原位置"
		out.unresolved = true
		return out, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		out.reason = "无法创建原位置目录"
		out.unresolved = true
		return out, err
	}

	if err := native.MovePath(source, path); err == nil {
		// The content has left the trash. Remove only this now-empty entry;
		// a cleanup failure does not turn a successful restore into a conflict.
		_ = os.RemoveAll(entry)
		out.action = CheckpointActionRestore
		if len(meta.PurgedDirs) > 0 {
			out.reason = fmt.Sprintf("部分恢复：%d 个目录已永久删除", len(meta.PurgedDirs))
			out.unresolved = true
		}
		return out, nil
	} else if !sourceInfo.IsDir() {
		out.reason = "恢复回收区内容失败"
		out.unresolved = true
		return out, err
	}

	// A directory rename can fail on another volume or because one Windows
	// handle is shared. Retry its children one at a time so unaffected files
	// still return and the checkpoint records a partial restore.
	restored, failures, walkErr := restoreTrashTree(source, path)
	if walkErr != nil {
		failures = append(failures, walkErr.Error())
	}
	if restored {
		out.action = CheckpointActionRestore
	}
	if len(meta.PurgedDirs) > 0 {
		out.reason = fmt.Sprintf("部分恢复：%d 个目录已永久删除", len(meta.PurgedDirs))
		out.unresolved = true
	}
	if len(failures) > 0 {
		out.reason = "回收区目录部分恢复"
		out.unresolved = true
	}
	if !restored && len(failures) == 0 {
		out.reason = "回收区目录未恢复"
		out.unresolved = true
	}
	if len(failures) > 0 {
		return out, fmt.Errorf("restore trash: %s", strings.Join(failures, "; "))
	}
	return out, nil
}

func safeTrashName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && !strings.ContainsAny(name, `/\\`)
}

func sameUndoName(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func sameUndoPath(a, b string) bool {
	a, _ = filepath.Abs(filepath.Clean(a))
	b, _ = filepath.Abs(filepath.Clean(b))
	a, b = native.CanonicalPath(a), native.CanonicalPath(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func restoreTrashTree(source, destination string) (bool, []string, error) {
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return false, nil, err
	}
	var dirs []string
	var failures []string
	restored := false
	walkErr := filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := destination
		if rel != "." {
			target = filepath.Join(destination, rel)
		}
		if entry.IsDir() {
			dirs = append(dirs, path)
			if err := os.MkdirAll(target, 0o755); err != nil {
				failures = append(failures, fmt.Sprintf("%s: %v", path, err))
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			failures = append(failures, fmt.Sprintf("%s: unsupported file type", path))
			return nil
		}
		if info.Mode().IsRegular() && native.IsHardLinked(path, info) {
			failures = append(failures, fmt.Sprintf("%s: hard-linked file", path))
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		if _, err := os.Lstat(target); err == nil {
			failures = append(failures, fmt.Sprintf("%s: destination already exists", target))
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, fmt.Sprintf("%s: cannot inspect destination: %v", target, err))
			return nil
		}
		if err := native.MovePath(path, target); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		restored = true
		return nil
	})
	if walkErr != nil {
		return restored, failures, walkErr
	}

	for i := len(dirs) - 1; i >= 0; i-- {
		if err := os.Remove(dirs[i]); err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, fmt.Sprintf("%s: %v", dirs[i], err))
		}
	}
	if len(failures) == 0 {
		if err := os.Remove(source); err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, fmt.Sprintf("%s: %v", source, err))
		}
	}
	return restored, failures, nil
}
