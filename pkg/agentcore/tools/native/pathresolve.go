package native

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

// foldPathCase reports whether path comparison is case-insensitive on this OS.
var foldPathCase = runtime.GOOS == "windows"

// splitAbs splits a cleaned absolute path into its volume name and the
// non-empty separator-delimited elements after the root.
func splitAbs(p string) (vol string, elems []string) {
	vol = filepath.VolumeName(p)
	for _, e := range strings.Split(p[len(vol):], string(filepath.Separator)) {
		if e != "" {
			elems = append(elems, e)
		}
	}
	return vol, elems
}

// relUnderRoot returns the elements of the cleaned absolute path p below the
// cleaned absolute root, comparing volume and elements case-insensitively when
// fold is set (Windows). ok is false when p is not root or a descendant of it.
// It is purely lexical, so it can be unit-tested for either case mode on any OS.
func relUnderRoot(root, p string, fold bool) (rel string, ok bool) {
	eq := func(a, b string) bool {
		if fold {
			return strings.EqualFold(a, b)
		}
		return a == b
	}
	rv, re := splitAbs(root)
	pv, pe := splitAbs(p)
	if !eq(rv, pv) || len(pe) < len(re) {
		return "", false
	}
	for i := range re {
		if !eq(re[i], pe[i]) {
			return "", false
		}
	}
	rest := pe[len(re):]
	if len(rest) == 0 {
		return ".", true
	}
	return filepath.Join(rest...), true
}

// absUnderWorkspace maps an absolute path argument to a path under workspace
// when it lies inside it. A path under the workspace's symlink-resolved real
// location is accepted too, so a model quoting the real path of a symlinked
// workspace root is not rejected. Both sides are compared in their canonical
// spelling (CanonicalPath: a Windows 8.3 short name versus the long one), while
// the result keeps the workspace's own spelling.
func absUnderWorkspace(workspace, arg string, fold bool) (string, bool) {
	p := CanonicalPath(arg)
	if rel, ok := relUnderRoot(CanonicalPath(workspace), p, fold); ok {
		return filepath.Join(workspace, rel), true
	}
	if real, err := filepath.EvalSymlinks(workspace); err == nil && real != workspace {
		if rel, ok := relUnderRoot(filepath.Clean(real), p, fold); ok {
			return filepath.Join(workspace, rel), true
		}
	}
	return "", false
}

// errEscapeViaSymlink marks realRelInRoot's confirmation that the fully
// resolved location is outside the workspace.
var errEscapeViaSymlink = errors.New("path escapes workspace via symlink")

// isRootEscape reports whether err might be os.Root refusing a path that leaves
// the root. os.Root's escape and structural errors (not a directory,
// unsupported file mode, empty path) are *os.PathError values whose cause is
// not a syscall.Errno, whereas real OS failures (EISDIR, ENOENT, EACCES, ...)
// always carry an Errno. A true result only selects the candidates for the
// realRelInRoot second look; it never decides on its own that a path escaped.
func isRootEscape(err error) bool {
	var pe *os.PathError
	if !errors.As(err, &pe) {
		return false
	}
	var errno syscall.Errno
	return !errors.As(pe.Err, &errno)
}

// realRelInRoot is the second-chance check used when os.Root rejected abs
// (typically a symlink whose target is absolute, which os.Root never follows).
// It fully resolves the deepest existing ancestor of abs plus the workspace
// root itself, and returns the path relative to the real root only when the
// real location is inside it. Intermediate links that leave the workspace and
// come back are fine: only the final real location matters, and the caller
// still opens it through os.Root, which keeps confining the access.
func realRelInRoot(workspace, abs string) (string, error) {
	realRoot, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return "", err
	}
	realRoot, err = filepath.Abs(realRoot)
	if err != nil {
		return "", err
	}
	// abs is lexically clean (no ".." elements), so peel off missing trailing
	// elements (a file about to be created) and resolve what exists.
	existing, tail := abs, []string(nil)
	var real string
	for {
		real, err = filepath.EvalSymlinks(existing)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", err
		}
		tail = append([]string{filepath.Base(existing)}, tail...)
		existing = parent
	}
	real = filepath.Join(append([]string{real}, tail...)...)
	rel, ok := relUnderRoot(realRoot, filepath.Clean(real), foldPathCase)
	if !ok {
		return "", fmt.Errorf("%w: %q is outside workspace", errEscapeViaSymlink, abs)
	}
	return rel, nil
}

// withRoot runs op on the workspace-relative form of abs through os.Root. If
// os.Root rejects it as an escape, abs is re-resolved once via realRelInRoot
// and op is retried on the resolved relative path (still through os.Root).
func withRoot[T any](workspace, abs string, op func(root *os.Root, rel string) (T, error)) (T, error) {
	var zero T
	workspace = filepath.Clean(workspace)
	rel, err := relInWorkspace(workspace, abs)
	if err != nil {
		return zero, err
	}
	root, err := openWorkspaceRoot(workspace)
	if err != nil {
		return zero, err
	}
	defer root.Close()

	v, err := op(root, rel)
	// os.Root.MkdirAll reports a symlink-to-directory with an absolute target as
	// "file exists" rather than as an escape, so retry on that too (an existing
	// directory is a normal idempotent MkdirAll outcome; the retry is a no-op
	// there). The retry is still confined by realRelInRoot and os.Root.
	if !isRootEscape(err) && !errors.Is(err, os.ErrExist) {
		return v, err
	}
	rel2, ferr := realRelInRoot(workspace, abs)
	if ferr != nil {
		// Only a confirmed escape is reworded; any other error is returned as is.
		if errors.Is(ferr, errEscapeViaSymlink) {
			return zero, wrapRootErr(err, abs)
		}
		return zero, err
	}
	if v, err = op(root, rel2); err != nil {
		return zero, err
	}
	return v, nil
}
