package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"

	appconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/personal/store"
)

// seedSessions creates 12 sessions, one per hour back from now (s00 newest),
// the even ones under /work/a, s03 titled "schedule fix", s05 with "flaky
// ping test" in its conversation.
func seedSessions(t *testing.T) context.Context {
	t.Helper()
	t.Setenv("SUAG_HOME", shortCLIHome(t))
	t.Setenv("SUAG_HOOKS", "off")
	dbPath, err := appconfig.ResolveCoreStorePath()
	assert.NoErr(t, err)
	repo, err := store.Open(dbPath)
	assert.NoErr(t, err)
	defer repo.Close()
	ctx := context.Background()
	now := time.Now()
	for i := 0; i < 12; i++ {
		cwd := "/work/b"
		if i%2 == 0 {
			cwd = "/work/a/sub"
		}
		title := fmt.Sprintf("task %d", i)
		if i == 3 {
			title = "schedule fix"
		}
		at := now.Add(-time.Duration(i) * time.Hour)
		assert.NoErr(t, repo.CreateSession(ctx, store.Session{ID: fmt.Sprintf("s%02d", i), Title: title, Cwd: cwd,
			CreatedAt: at, UpdatedAt: at, Meta: map[string]any{"provider": "cc", "model": "flash", "suag_version": "0.14.1"}}))
	}
	_, err = repo.AppendEntries(ctx, "s05", store.NewEntry{Type: store.EntryUser,
		JSON: json.RawMessage(`{"Role":"user","Content":"why does the flaky ping test fail?"}`)})
	assert.NoErr(t, err)
	// Appending touched s05: put its activity time back.
	s05, err := repo.GetSession(ctx, "s05")
	assert.NoErr(t, err)
	s05.UpdatedAt = now.Add(-5 * time.Hour)
	assert.NoErr(t, repo.UpdateSession(ctx, s05))
	return ctx
}

func listed(t *testing.T, ctx context.Context, args ...string) []string {
	t.Helper()
	out, err := runCLI(t, ctx, append([]string{"sessions", "--json"}, args...)...)
	assert.Require(t, assert.NoErr(t, err))
	var rows []sessionRow
	assert.Require(t, assert.NoErr(t, json.Unmarshal([]byte(out), &rows)))
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids
}

func TestSessionsCommand(t *testing.T) {
	ctx := seedSessions(t)
	t.Run("the last 10, newest first", func(t *testing.T) {
		ids := listed(t, ctx)
		assert.Eq(t, 10, len(ids))
		assert.Eq(t, "s00", ids[0])
		assert.Eq(t, 12, len(listed(t, ctx, "-n", "0")))
		assert.Eq(t, []string{"s00", "s01", "s02"}, listed(t, ctx, "--limit", "3"))
	})
	t.Run("time range", func(t *testing.T) {
		assert.Eq(t, 3, len(listed(t, ctx, "--since", "3h")))  // 0h, 1h, 2h ago
		assert.Eq(t, 2, len(listed(t, ctx, "--until", "10h"))) // 10h, 11h ago
		_, err := runCLI(t, ctx, "sessions", "--since", "soon")
		assert.ErrMsgContains(t, err, "want 12h, 7d or a date")
	})
	t.Run("keywords in the fields or the conversation", func(t *testing.T) {
		assert.Eq(t, []string{"s03"}, listed(t, ctx, "-q", "schedule"))
		assert.Eq(t, []string{"s03"}, listed(t, ctx, "SCHEDULE", "fix")) // words after the flags, any case
		out, err := runCLI(t, ctx, "sessions", "--json", "-q", "flaky ping")
		assert.Require(t, assert.NoErr(t, err))
		var rows []sessionRow
		assert.NoErr(t, json.Unmarshal([]byte(out), &rows))
		assert.Require(t, assert.Len(t, rows, 1))
		assert.Eq(t, "s05", rows[0].ID)
		assert.StrContains(t, rows[0].Match, "flaky ping test")
		assert.Eq(t, []string{}, listed(t, ctx, "-q", "nothing-like-this"))
	})
	t.Run("directory prefix", func(t *testing.T) {
		ids := listed(t, ctx, "--cwd", "/WORK/A", "-n", "0") // case-insensitive, whole path elements
		assert.Eq(t, 6, len(ids))
		assert.Eq(t, 0, len(listed(t, ctx, "--cwd", "/work/a/su")))
		assert.True(t, pathHasPrefix(`D:\Work\x\ws`, "d:/work/x"))
		assert.Eq(t, "…/x/ws", shortDir(`D:\work\x\ws`))
	})
	t.Run("table", func(t *testing.T) {
		out, err := runCLI(t, ctx, "sessions", "-n", "2")
		assert.Require(t, assert.NoErr(t, err))
		assert.StrContains(t, out, "UPDATED")
		assert.StrContains(t, out, "cc/flash")
		assert.StrContains(t, out, "0.14.1")
		assert.StrContains(t, out, "2 of 12 sessions; -n 0 shows all")
		out, err = runCLI(t, ctx, "sessions", "flaky")
		assert.NoErr(t, err)
		assert.StrContains(t, out, "↳")
		assert.True(t, strings.Contains(out, "s05"))
	})
}

func TestSessionsCommandAgentSessions(t *testing.T) {
	t.Setenv("SUAG_HOME", shortCLIHome(t))
	t.Setenv("SUAG_HOOKS", "off")
	dbPath, err := appconfig.ResolveCoreStorePath()
	assert.Require(t, assert.NoErr(t, err))
	repo, err := store.Open(dbPath)
	assert.Require(t, assert.NoErr(t, err))
	ctx := context.Background()
	assert.NoErr(t, repo.CreateSession(ctx, store.Session{ID: "parent", Title: "main work"}))
	assert.NoErr(t, repo.CreateSession(ctx, store.Session{ID: "child", Title: "find callers",
		Meta: map[string]any{store.MetaKind: store.KindAgent, store.MetaParentSession: "parent"}}))
	assert.NoErr(t, repo.Close())

	assert.Eq(t, []string{"parent"}, listed(t, ctx))
	assert.Eq(t, 2, len(listed(t, ctx, "--agents")))
	out, err := runCLI(t, ctx, "sessions", "--agents")
	assert.Require(t, assert.NoErr(t, err))
	assert.StrContains(t, out, "[agent of parent] find callers")
}
