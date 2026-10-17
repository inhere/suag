package files

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/inhere/suga/pkg/agentcore/memory"
)

// Entries enumerates readable entry files using the same id and root safety
// rules as Search. Missing layouts are empty; unreadable or escaping entries
// are skipped so one bad file cannot hide the rest of the scope.
func (s *Store) Entries(_ context.Context, sc memory.Scope) ([]memory.Entry, error) {
	root, lp, err := s.open(sc, false)
	if err != nil || root == nil {
		return nil, err
	}
	defer root.Close()

	dir := lp.entryDir
	if dir == "" {
		dir = "."
	}
	f, err := root.Open(filepath.FromSlash(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names, err := f.Readdirnames(-1)
	f.Close()
	if err != nil {
		return nil, err
	}

	entries := make([]memory.Entry, 0, len(names))
	for _, name := range names {
		id, ok := strings.CutSuffix(name, ".md")
		if !ok || memory.ValidateID(id) != nil {
			continue
		}
		e, err := readEntry(root, lp, id)
		if err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, nil
}
