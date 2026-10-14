// Package adddir resolves the additional directories of one session: the
// repeatable --add-dir flag and the SUGA_ADD_DIRS environment variable, both
// "<absolute directory>[:ro|:rw]" (default :ro), plus the set a session
// already carries in its metadata.
package adddir

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// EnvVar is the environment variable an outer tool (a job runner) uses instead
// of the flag: os.PathListSeparator separated entries in the value form of
// --add-dir.
const EnvVar = "SUGA_ADD_DIRS"

// MetaKey is the session metadata key holding the session's added directories:
// a list of {path, mode} objects.
const MetaKey = "add_dirs"

// foldCase reports whether path comparison ignores case (Windows), matching
// how the native tools compare paths.
var foldCase = runtime.GOOS == "windows"

// Dir is one additional directory of a session. Path is absolute and cleaned;
// Writable is the :rw mode (:ro, the default, is readable but not writable).
type Dir struct {
	Path     string
	Writable bool
}

// Entry parses one --add-dir value (or one SUGA_ADD_DIRS item):
// "<absolute directory>[:ro|:rw]", default :ro. Only a trailing ":ro"/":rw"
// counts as a mode, so a Windows drive letter ("D:\docs") keeps its colon. "~"
// is expanded like tools.read_roots does, and the directory must exist.
func Entry(value string) (Dir, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return Dir{}, fmt.Errorf("%q is empty", value)
	}
	path, writable := raw, false
	switch {
	case strings.HasSuffix(raw, ":rw"):
		path, writable = raw[:len(raw)-3], true
	case strings.HasSuffix(raw, ":ro"):
		path = raw[:len(raw)-3]
	}
	if strings.TrimSpace(path) == "" {
		return Dir{}, fmt.Errorf("%q names no directory", raw)
	}
	dir := filepath.Clean(expandHome(path))
	if !filepath.IsAbs(dir) {
		return Dir{}, fmt.Errorf("%q is not an absolute path", raw)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return Dir{}, fmt.Errorf("%q is not an existing directory", raw)
	}
	return Dir{Path: dir, Writable: writable}, nil
}

// expandHome turns a leading "~" into the user's home directory.
func expandHome(dir string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return dir
	}
	if dir == "~" {
		return home
	}
	if strings.HasPrefix(dir, "~/") || strings.HasPrefix(dir, `~\`) {
		return filepath.Join(home, dir[1:])
	}
	return dir
}

// Parse parses --add-dir values: every entry must be valid, because the user
// typed it just now.
func Parse(values []string) ([]Dir, error) {
	dirs := make([]Dir, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		dir, err := Entry(value)
		if err != nil {
			return nil, fmt.Errorf("--add-dir: %w", err)
		}
		dirs = append(dirs, dir)
	}
	return dirs, nil
}

// ParseEnv parses SUGA_ADD_DIRS: entries separated by os.PathListSeparator.
// A bad entry cannot be fixed by the user of this process, so it is reported
// as a warning and skipped instead of failing the start.
func ParseEnv(value string) ([]Dir, []string) {
	var dirs []Dir
	var warnings []string
	for _, item := range splitEnvList(value, string(os.PathListSeparator)) {
		if strings.TrimSpace(item) == "" {
			continue
		}
		dir, err := Entry(item)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: skipping %v", EnvVar, err))
			continue
		}
		dirs = append(dirs, dir)
	}
	return dirs, warnings
}

// splitEnvList splits an environment list. On Unix the separator is ":" and
// collides with the ":ro"/":rw" suffix ("/a:ro:/b"), so a bare "ro"/"rw"
// segment glues back onto the entry before it; on Windows the separator is ";"
// and the suffix never splits.
func splitEnvList(value, sep string) []string {
	var out []string
	for _, item := range strings.Split(value, sep) {
		if (item == "ro" || item == "rw") && len(out) > 0 {
			out[len(out)-1] += ":" + item
			continue
		}
		out = append(out, item)
	}
	return out
}

// Resolve parses the --add-dir values and SUGA_ADD_DIRS of this invocation
// into the directories to record for the session, flag entries first. A bad
// flag value is an error. The read-only run mode is reported as a warning:
// the session keeps the requested mode and Effective makes the run read-only.
func Resolve(values []string, env string, readOnly bool) ([]Dir, []string, error) {
	flags, err := Parse(values)
	if err != nil {
		return nil, nil, err
	}
	envDirs, warnings := ParseEnv(env)
	// Flag entries come first and win a path named twice: the flag is what the
	// user typed now, the environment is what an outer tool set.
	dirs := make([]Dir, 0, len(flags)+len(envDirs))
	seen := make(map[string]bool, len(flags)+len(envDirs))
	for _, list := range [][]Dir{flags, envDirs} {
		for _, dir := range list {
			key := pathKey(dir.Path)
			if seen[key] {
				continue
			}
			seen[key] = true
			dirs = append(dirs, dir)
		}
	}
	if readOnly {
		for _, dir := range dirs {
			if dir.Writable {
				warnings = append(warnings, fmt.Sprintf("--read-only: %s is readable only in this run (:rw ignored)", dir.Path))
				break
			}
		}
	}
	return dirs, warnings, nil
}

// Merge combines the directories a session already carries with the ones given
// now: the stored order is kept, a stored path given again takes the new mode,
// and paths only given now are appended in order.
func Merge(stored, added []Dir) []Dir {
	out := make([]Dir, 0, len(stored)+len(added))
	at := make(map[string]int, len(stored)+len(added))
	for _, list := range [][]Dir{stored, added} {
		for _, dir := range list {
			if dir.Path == "" {
				continue
			}
			key := pathKey(dir.Path)
			if i, seen := at[key]; seen {
				out[i] = dir
				continue
			}
			at[key] = len(out)
			out = append(out, dir)
		}
	}
	return out
}

// Effective applies a run mode to the set: a read-only run has no write tool at
// all, so every directory is reported read-only.
func Effective(dirs []Dir, readOnly bool) []Dir {
	if !readOnly {
		return dirs
	}
	out := make([]Dir, len(dirs))
	for i, dir := range dirs {
		out[i] = Dir{Path: dir.Path}
	}
	return out
}

// ToMeta renders the set for the session metadata (MetaKey).
func ToMeta(dirs []Dir) []map[string]any {
	out := make([]map[string]any, len(dirs))
	for i, dir := range dirs {
		mode := "ro"
		if dir.Writable {
			mode = "rw"
		}
		out[i] = map[string]any{"path": dir.Path, "mode": mode}
	}
	return out
}

// FromMeta reads the stored set back: the list ToMeta wrote, as the JSON store
// hands it over ([]any of objects). An absent value is an empty set; a
// malformed one is an error the caller reports and then ignores.
func FromMeta(value any) ([]Dir, error) {
	if value == nil {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok {
		typed, isTyped := value.([]map[string]any)
		if !isTyped {
			return nil, fmt.Errorf("%s: expected a list, got %T", MetaKey, value)
		}
		items = make([]any, len(typed))
		for i, item := range typed {
			items[i] = item
		}
	}
	dirs := make([]Dir, 0, len(items))
	for i, item := range items {
		obj, isObj := item.(map[string]any)
		if !isObj {
			return nil, fmt.Errorf("%s[%d]: expected a {path, mode} object, got %T", MetaKey, i, item)
		}
		path, _ := obj["path"].(string)
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("%s[%d]: %q is not an absolute path", MetaKey, i, path)
		}
		switch mode, _ := obj["mode"].(string); mode {
		case "", "ro":
			dirs = append(dirs, Dir{Path: filepath.Clean(path)})
		case "rw":
			dirs = append(dirs, Dir{Path: filepath.Clean(path), Writable: true})
		default:
			return nil, fmt.Errorf("%s[%d]: unknown mode %q", MetaKey, i, mode)
		}
	}
	return dirs, nil
}

// Equal reports whether two sets name the same paths in the same order with
// the same modes, i.e. whether the session metadata has to be rewritten.
func Equal(a, b []Dir) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if pathKey(a[i].Path) != pathKey(b[i].Path) || a[i].Writable != b[i].Writable {
			return false
		}
	}
	return true
}

// pathKey normalises a path for the duplicate check (case-insensitive on
// Windows, where two spellings name one directory).
func pathKey(path string) string {
	path = filepath.Clean(path)
	if foldCase {
		return strings.ToLower(path)
	}
	return path
}
