package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

// TestOpenReadOnly: a read-only handle reads an existing store and never
// creates, migrates or writes one; a missing or uninitialized store is an
// error (the trace --web page must not touch the session store it shows).
func TestOpenReadOnly(t *testing.T) {
	ctx := context.Background()

	t.Run("a missing store is an error and no file is created", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "sub", "sessions.db")
		assert.Require(t, assert.NoErr(t, os.MkdirAll(filepath.Dir(path), 0o755)))
		_, err := OpenReadOnly(path)
		assert.Err(t, err)
		_, statErr := os.Stat(path)
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("a file without a store is not migrated", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "empty.db")
		assert.Require(t, assert.NoErr(t, os.WriteFile(path, nil, 0o600)))
		_, err := OpenReadOnly(path)
		assert.ErrMsgContains(t, err, "not initialized")
		info, statErr := os.Stat(path)
		assert.Require(t, assert.NoErr(t, statErr))
		assert.Eq(t, int64(0), info.Size()) // nothing was written into it
	})

	t.Run("an existing store reads and refuses writes", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "sessions.db")
		rw, err := Open(path)
		assert.Require(t, assert.NoErr(t, err))
		assert.NoErr(t, rw.CreateSession(ctx, Session{ID: "s1", Title: "read me", Cwd: "/w"}))
		_, err = rw.AppendEntries(ctx, "s1", NewEntry{Type: EntryUser, JSON: []byte(`{"Role":"user","Content":"hi"}`)})
		assert.Require(t, assert.NoErr(t, err))
		assert.NoErr(t, rw.Close())

		ro, err := OpenReadOnly(path)
		assert.Require(t, assert.NoErr(t, err))
		defer ro.Close()
		sess, err := ro.GetSession(ctx, "s1")
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "read me", sess.Title)
		entries, err := ro.ListEntries(ctx, "s1", EntryFilter{})
		assert.Require(t, assert.NoErr(t, err))
		assert.Len(t, entries, 1)
		if _, err := ro.DB().ExecContext(ctx, `CREATE TABLE probe(x int)`); err == nil {
			t.Fatal("a write on the read-only handle must fail")
		}
	})
}

// TestOpenReadOnlyReadsWhileWriting: a reader opened beside a writer keeps
// reading a WAL database while entries are appended (no SQLITE_BUSY, suag-5o5g).
func TestOpenReadOnlyReadsWhileWriting(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sessions.db")
	rw, err := Open(path)
	assert.Require(t, assert.NoErr(t, err))
	defer rw.Close()
	assert.NoErr(t, rw.CreateSession(ctx, Session{ID: "s1", Cwd: "/w"}))

	ro, err := OpenReadOnly(path)
	assert.Require(t, assert.NoErr(t, err))
	defer ro.Close()

	for i := range 20 {
		_, err := rw.AppendEntries(ctx, "s1", NewEntry{Type: EntryUser, JSON: []byte(`{"Role":"user","Content":"hi"}`)})
		assert.Require(t, assert.NoErr(t, err))
		entries, err := ro.ListEntries(ctx, "s1", EntryFilter{})
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, i+1, len(entries))
	}
}

// TestListEntryRefs: the link fields of every entry, bodies left behind; an
// entry that is not a message (no call id) still lists, a broken body does not
// fail the query.
func TestListEntryRefs(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	assert.NoErr(t, s.CreateSession(ctx, Session{ID: "s1", Cwd: "/w"}))
	_, err := s.AppendEntries(ctx, "s1",
		NewEntry{TurnID: "t1", Type: EntryUser, JSON: []byte(`{"Role":"user","Content":"hi"}`)},
		NewEntry{TurnID: "t1", Type: EntryAssistant, JSON: []byte(`{"Role":"assistant","ToolCall":{"CallID":"c1","Name":"file_read"}}`)},
		NewEntry{TurnID: "t1", Type: EntryTool, JSON: []byte(`{"Role":"tool","ToolResult":{"CallID":"c1","Name":"file_read","Content":"x"}}`)},
		NewEntry{TurnID: "t1", Type: EntryPlan, JSON: []byte(`{"steps":[]}`)},
	)
	assert.Require(t, assert.NoErr(t, err))

	refs, err := s.ListEntryRefs(ctx, "s1")
	assert.Require(t, assert.NoErr(t, err))
	assert.Len(t, refs, 4)
	assert.Eq(t, int64(1), refs[0].Seq)
	assert.Eq(t, "", refs[0].CallID)
	assert.Eq(t, EntryAssistant, refs[1].Type)
	assert.Eq(t, "t1", refs[1].TurnID)
	assert.Eq(t, "c1", refs[1].CallID)
	assert.Eq(t, "c1", refs[2].CallID) // the result carries the same call id
	assert.Eq(t, "", refs[3].CallID)   // a plan entry is no message
	assert.False(t, refs[1].CreatedAt.IsZero())
}
