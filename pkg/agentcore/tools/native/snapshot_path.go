package native

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

// SnapshotPath is the checked identity of a snapshot target. Abs keeps the
// lexical path used by the write tool; RealRel is checked against the current
// resolved location before undo performs any I/O on the target.
type SnapshotPath struct {
	Root    string
	Rel     string
	RealRel string
	Abs     string
}

// ResolveSnapshotPath validates the root-relative identity recorded by a write
// tool. It rejects non-canonical or escaping paths and detects a changed
// symlinked parent/target by comparing the current real relative path.
func ResolveSnapshotPath(root, rel, realRel string) (SnapshotPath, error) {
	root = filepath.Clean(root)
	if strings.TrimSpace(root) == "" || !filepath.IsAbs(root) {
		return SnapshotPath{}, fmt.Errorf("snapshot root must be absolute: %q", root)
	}
	cleanRel, err := snapshotRelativePath(rel, "rel")
	if err != nil {
		return SnapshotPath{}, err
	}
	cleanRealRel, err := snapshotRelativePath(realRel, "real_rel")
	if err != nil {
		return SnapshotPath{}, err
	}
	abs := filepath.Join(root, filepath.FromSlash(cleanRel))
	if _, err := relInWorkspace(root, abs); err != nil {
		return SnapshotPath{}, fmt.Errorf("snapshot rel escapes root: %w", err)
	}
	actualRealRel, err := realRelInRoot(root, abs)
	if err != nil {
		return SnapshotPath{}, fmt.Errorf("resolve snapshot target: %w", err)
	}
	if !sameSnapshotRel(actualRealRel, cleanRealRel) {
		return SnapshotPath{}, fmt.Errorf("snapshot real_rel changed: recorded %q, current %q", cleanRealRel, filepath.ToSlash(actualRealRel))
	}
	return SnapshotPath{Root: root, Rel: cleanRel, RealRel: cleanRealRel, Abs: abs}, nil
}

func snapshotRelativePath(value, label string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("snapshot %s is empty", label)
	}
	if filepath.IsAbs(value) || filepath.VolumeName(value) != "" {
		return "", fmt.Errorf("snapshot %s must be relative: %q", label, value)
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("snapshot %s escapes root: %q", label, value)
	}
	canonical := filepath.ToSlash(clean)
	if canonical != value {
		return "", fmt.Errorf("snapshot %s is not canonical: %q", label, value)
	}
	return canonical, nil
}

func sameSnapshotRel(a, b string) bool {
	a, b = filepath.ToSlash(filepath.Clean(a)), filepath.ToSlash(filepath.Clean(b))
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
