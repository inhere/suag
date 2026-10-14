package native

import (
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// AddDir is one additional directory of the session (--add-dir): the read
// tools may read it and, when Writable (:rw), the write tools may change it
// too. Path is absolute and cleaned.
type AddDir struct {
	Path     string
	Writable bool
}

// AddDirs is the additional-directory set of the session being served. The
// runtime and its tools are assembled before the session is opened, so the
// front end fills the set once the session is known and the tools read it at
// every call. A nil *AddDirs means "no additional directory" everywhere.
type AddDirs struct {
	mu   sync.RWMutex
	dirs []AddDir
}

// NewAddDirs returns an empty set.
func NewAddDirs() *AddDirs { return &AddDirs{} }

// Set replaces the set: paths are cleaned and a path named twice keeps its
// first entry (the front end resolves path conflicts before setting them).
func (a *AddDirs) Set(dirs []AddDir) {
	if a == nil {
		return
	}
	clean := make([]AddDir, 0, len(dirs))
	seen := make(map[string]bool, len(dirs))
	for _, d := range dirs {
		if d.Path == "" {
			continue
		}
		d.Path = filepath.Clean(d.Path)
		key := d.Path
		if foldPathCase {
			key = strings.ToLower(key)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		clean = append(clean, d)
	}
	a.mu.Lock()
	a.dirs = clean
	a.mu.Unlock()
}

// Snapshot returns the set in order.
func (a *AddDirs) Snapshot() []AddDir {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return slices.Clone(a.dirs)
}

// ReadPaths returns the directories the read tools may read: every added
// directory, the read-write ones included.
func (a *AddDirs) ReadPaths() []string { return a.paths(false) }

// WritePaths returns the directories the write tools may write: only :rw ones.
func (a *AddDirs) WritePaths() []string { return a.paths(true) }

// paths returns the paths of the set, or only the writable ones.
func (a *AddDirs) paths(writable bool) []string {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	var out []string
	for _, d := range a.dirs {
		if writable && !d.Writable {
			continue
		}
		out = append(out, d.Path)
	}
	return out
}

// List renders the set as "path:ro" / "path:rw" entries, in order. The
// turn_context event records it so a trace shows what the session could reach.
func (a *AddDirs) List() []string {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]string, len(a.dirs))
	for i, d := range a.dirs {
		out[i] = d.Path + ModeSuffix(d.Writable)
	}
	return out
}

// ModeSuffix names a directory mode as the --add-dir suffix: ":rw" when the
// directory is writable, ":ro" otherwise.
func ModeSuffix(writable bool) string {
	if writable {
		return ":rw"
	}
	return ":ro"
}
