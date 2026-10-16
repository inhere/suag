package store

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestFTSWorkspaceAndCurrentScope(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	root := filepath.Join(string(filepath.Separator), "repo")
	for _, tc := range []struct {
		id    string
		cwd   string
		agent bool
	}{
		{"current", "", false},
		{"exact", root, false},
		{"trailing", root + string(filepath.Separator), false},
		{"sub", filepath.Join(root, "sub"), false},
		{"sibling", root + "2", false},
		{"dash", root + "-旧", false},
		{"nocwd", "", false},
		{"case", filepath.Join(string(filepath.Separator)+"REPO", "case"), false},
		{"agent", filepath.Join(root, "agent"), true},
	} {
		meta := map[string]any(nil)
		if tc.agent {
			meta = agentMeta("current")
		}
		assert.Require(t, assert.NoErr(t, s.CreateSession(ctx, Session{ID: tc.id, Cwd: tc.cwd, Meta: meta})))
		appendAll(t, s, tc.id, user(t, "workspace token in "+tc.id))
	}

	t.Run("workspace uses SQL path boundaries and keeps current without cwd", func(t *testing.T) {
		got := hitKeys(search(t, s, EntrySearch{Query: "workspace token", WorkspaceDir: root, CurrentSessionID: "current"}))
		assert.Eq(t, []string{"current/1", "exact/1", "sub/1", "trailing/1"}, got)
	})
	t.Run("workspace excludes current only when it is not requested", func(t *testing.T) {
		got := hitKeys(search(t, s, EntrySearch{Query: "workspace token", WorkspaceDir: root}))
		assert.Eq(t, []string{"exact/1", "sub/1", "trailing/1"}, got)
	})
	t.Run("current without workspace is SQL restricted", func(t *testing.T) {
		got := hitKeys(search(t, s, EntrySearch{Query: "workspace token", CurrentSessionID: "current"}))
		assert.Eq(t, []string{"current/1"}, got)
	})
	t.Run("IncludeAgents applies with the workspace condition", func(t *testing.T) {
		got := hitKeys(search(t, s, EntrySearch{Query: "workspace token", WorkspaceDir: root, CurrentSessionID: "current", IncludeAgents: true}))
		assert.Eq(t, []string{"agent/1", "current/1", "exact/1", "sub/1", "trailing/1"}, got)
	})
	t.Run("FoldCase uses SQLite lower for ASCII paths", func(t *testing.T) {
		got := hitKeys(search(t, s, EntrySearch{Query: "workspace token", WorkspaceDir: root, CurrentSessionID: "current", FoldCase: true}))
		assert.Eq(t, []string{"case/1", "current/1", "exact/1", "sub/1", "trailing/1"}, got)
	})

	t.Run("legacy slash spellings are measured on the host path separator", func(t *testing.T) {
		for _, tc := range []struct {
			id  string
			cwd string
		}{
			{"drive-slash", `C:/Repo`},
			{"drive-backslash", `C:\Repo`},
		} {
			assert.Require(t, assert.NoErr(t, s.CreateSession(ctx, Session{ID: tc.id, Cwd: tc.cwd})))
			appendAll(t, s, tc.id, user(t, "drive token in "+tc.id))
		}
		got := hitKeys(search(t, s, EntrySearch{Query: "drive token", WorkspaceDir: filepath.Clean(`C:/Repo`), FoldCase: true}))
		want := []string{"drive-slash/1"}
		if runtime.GOOS == "windows" {
			// Windows SQL normalizes legacy slash spellings before applying
			// the approved ASCII lower() fold.
			want = []string{"drive-backslash/1", "drive-slash/1"}
		}
		assert.Eq(t, want, got)
	})
}
