package files

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/pkg/agentcore/memory"
)

var (
	ctx     = context.Background()
	user    = memory.UserScope()
	project = memory.ProjectScope("suag")
	fixed   = time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC)
)

func newStore(t *testing.T, layouts map[memory.Scope]Layout) *Store {
	t.Helper()
	s, err := New(layouts, WithClock(func() time.Time { return fixed }))
	assert.Require(t, assert.NoErr(t, err))
	return s
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	assert.Require(t, assert.NoErr(t, err))
	return string(data)
}

func add(title, body string, kind memory.Kind) memory.Op {
	return memory.Op{Action: memory.ActionAdd, Title: title, Body: body, Kind: kind,
		Prov: memory.Provenance{SessionID: "s1", Source: "tool"}}
}

func TestRoundTrip(t *testing.T) {
	cases := []struct {
		name      string
		layout    func(dir string) Layout
		entryFile func(dir, id string) string
		link      string
	}{
		{"dir layout", func(d string) Layout { return DirLayout{Dir: filepath.Join(d, "mem")} },
			func(d, id string) string { return filepath.Join(d, "mem", id+".md") }, ""},
		{"repo layout", func(d string) Layout { return RepoLayout{Root: d} },
			func(d, id string) string { return filepath.Join(d, ".suag", "memory", id+".md") }, ".suag/memory/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			layout := tc.layout(dir)
			s := newStore(t, map[memory.Scope]Layout{project: layout})
			index := filepath.Join(layout.paths().root, indexFile)

			// Empty scope reads as empty.
			ix, err := s.Index(ctx, project)
			assert.NoErr(t, err)
			assert.Empty(t, ix.Lines)

			e, err := s.Write(ctx, project, add("Prefer [table] tests", "Use table tests\nfor parsers.", memory.KindFeedback))
			assert.Require(t, assert.NoErr(t, err))
			assert.Eq(t, "prefer-table-tests", e.ID)
			assert.Eq(t, fixed, e.Updated)

			ix, err = s.Index(ctx, project)
			assert.NoErr(t, err)
			assert.Eq(t, []memory.IndexLine{{ID: e.ID, Title: "Prefer [table] tests", Kind: memory.KindFeedback}}, ix.Lines)
			wantIndex := "- [Prefer \\[table\\] tests](" + tc.link + "prefer-table-tests.md) — feedback\n"
			assert.Eq(t, wantIndex, readText(t, index))
			assert.Eq(t, len(wantIndex), ix.Bytes)

			wantFile := "---\nkind: feedback\ntitle: \"Prefer [table] tests\"\nupdated: 2026-10-11T08:00:00Z\n" +
				"source: \"tool\"\nsession: \"s1\"\n---\n\nUse table tests\nfor parsers.\n"
			assert.Eq(t, wantFile, readText(t, tc.entryFile(dir, e.ID)))

			got, err := s.Get(ctx, project, e.ID)
			assert.NoErr(t, err)
			assert.Eq(t, e.Title, got.Title)
			assert.Eq(t, e.Body, got.Body)
			assert.Eq(t, memory.KindFeedback, got.Kind)
			assert.Eq(t, fixed, got.Updated)
			assert.Eq(t, "s1", got.Prov.SessionID)

			// Same title again gets a sequence suffix.
			e2, err := s.Write(ctx, project, add("Prefer [table] tests", "other", memory.KindProject))
			assert.NoErr(t, err)
			assert.Eq(t, "prefer-table-tests-2", e2.ID)

			r, err := s.Write(ctx, project, memory.Op{Action: memory.ActionReplace, ID: e.ID, Title: "Table tests", Body: "Always."})
			assert.NoErr(t, err)
			assert.Eq(t, memory.KindFeedback, r.Kind, "empty kind keeps the stored one")
			got, _ = s.Get(ctx, project, e.ID)
			assert.Eq(t, "Always.", got.Body)
			ix, _ = s.Index(ctx, project)
			assert.Eq(t, "Table tests", ix.Lines[0].Title, "replace keeps the line position")
			assert.Eq(t, e2.ID, ix.Lines[1].ID)

			removed, err := s.Write(ctx, project, memory.Op{Action: memory.ActionRemove, ID: e.ID})
			assert.NoErr(t, err)
			assert.Eq(t, "Table tests", removed.Title)
			assert.Eq(t, "Always.", removed.Body)
			_, err = s.Get(ctx, project, e.ID)
			assert.True(t, errors.Is(err, memory.ErrNotFound))
			assert.False(t, fileExists(tc.entryFile(dir, e.ID)))
			ix, _ = s.Index(ctx, project)
			assert.Len(t, ix.Lines, 1)

			_, err = s.Write(ctx, project, memory.Op{Action: memory.ActionRemove, ID: e.ID})
			assert.True(t, errors.Is(err, memory.ErrNotFound))
			_, err = s.Write(ctx, project, memory.Op{Action: memory.ActionReplace, ID: "nope", Title: "a", Body: "b"})
			assert.True(t, errors.Is(err, memory.ErrNotFound))
		})
	}
}

func TestPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits")
	}
	dir := t.TempDir()
	mem := filepath.Join(dir, "a", "mem")
	s := newStore(t, map[memory.Scope]Layout{user: DirLayout{Dir: mem}, project: RepoLayout{Root: dir}})
	_, err := s.Write(ctx, user, add("x", "y", memory.KindUser))
	assert.NoErr(t, err)
	_, err = s.Write(ctx, project, add("x", "y", memory.KindUser))
	assert.NoErr(t, err)
	for path, want := range map[string]os.FileMode{
		mem:                                   0o700,
		filepath.Join(mem, "x.md"):            0o600,
		filepath.Join(mem, indexFile):         0o600,
		filepath.Join(dir, ".suag", "memory"): 0o700,
		filepath.Join(dir, ".suag", "memory", "x.md"): 0o600,
	} {
		fi, err := os.Stat(path)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, want, fi.Mode().Perm(), path)
	}
	// No temp files are left behind.
	names, _ := os.ReadDir(mem)
	assert.Len(t, names, 2)
}

func TestHandWrittenLines(t *testing.T) {
	dir := t.TempDir()
	original := strings.Join([]string{
		"# Project memory",
		"",
		"Notes kept by hand; suag must not touch them.",
		"- [Existing](.suag/memory/existing.md) — project",
		"- a plain list item",
		"- [Escapes the root](../outside.md) — project",
		"- [Unknown kind](.suag/memory/odd.md) — note",
		"- [Dup](.suag/memory/existing.md) — user",
		"",
		"## Footer",
	}, "\n") + "\n"
	index := filepath.Join(dir, indexFile)
	assert.NoErr(t, os.WriteFile(index, []byte(original), 0o600))
	s := newStore(t, map[memory.Scope]Layout{project: RepoLayout{Root: dir}})

	ix, err := s.Index(ctx, project)
	assert.NoErr(t, err)
	assert.Eq(t, []memory.IndexLine{{ID: "existing", Title: "Existing", Kind: memory.KindProject}}, ix.Lines)

	e, err := s.Write(ctx, project, add("New one", "body", memory.KindUser))
	assert.NoErr(t, err)
	assert.Eq(t, original+"- [New one](.suag/memory/new-one.md) — user\n", readText(t, index), "appended at the end")

	// Replacing an indexed entry whose file is missing creates the file in place.
	_, err = s.Write(ctx, project, memory.Op{Action: memory.ActionReplace, ID: "existing", Title: "Existing v2", Body: "now with a file"})
	assert.NoErr(t, err)
	want := strings.Replace(original, "- [Existing](.suag/memory/existing.md) — project", "- [Existing v2](.suag/memory/existing.md) — project", 1)
	assert.Eq(t, want+"- [New one](.suag/memory/new-one.md) — user\n", readText(t, index))

	_, err = s.Write(ctx, project, memory.Op{Action: memory.ActionRemove, ID: e.ID})
	assert.NoErr(t, err)
	_, err = s.Write(ctx, project, memory.Op{Action: memory.ActionRemove, ID: "existing"})
	assert.NoErr(t, err)
	wantAfter := strings.Replace(original, "- [Existing](.suag/memory/existing.md) — project\n", "", 1)
	assert.Eq(t, wantAfter, readText(t, index), "only managed lines changed")

	t.Run("crlf files keep crlf", func(t *testing.T) {
		d := t.TempDir()
		assert.NoErr(t, os.WriteFile(filepath.Join(d, indexFile), []byte("# Notes\r\n- keep\r\n"), 0o600))
		s := newStore(t, map[memory.Scope]Layout{user: DirLayout{Dir: d}})
		_, err := s.Write(ctx, user, add("A", "b", memory.KindUser))
		assert.NoErr(t, err)
		assert.Eq(t, "# Notes\r\n- keep\r\n- [A](a.md) — user\r\n", readText(t, filepath.Join(d, indexFile)))
	})
	t.Run("entry file without frontmatter", func(t *testing.T) {
		d := t.TempDir()
		assert.NoErr(t, os.WriteFile(filepath.Join(d, indexFile), []byte("- [Hand made](hand.md) — reference\n"), 0o600))
		assert.NoErr(t, os.WriteFile(filepath.Join(d, "hand.md"), []byte("just text\n"), 0o600))
		s := newStore(t, map[memory.Scope]Layout{user: DirLayout{Dir: d}})
		e, err := s.Get(ctx, user, "hand")
		assert.NoErr(t, err)
		assert.Eq(t, memory.Entry{ID: "hand", Title: "Hand made", Body: "just text", Kind: memory.KindReference}, e)
	})
}

func TestInvalidIDs(t *testing.T) {
	dir := t.TempDir()
	assert.NoErr(t, os.WriteFile(filepath.Join(dir, "secret.md"), []byte("outside"), 0o600))
	s := newStore(t, map[memory.Scope]Layout{user: DirLayout{Dir: filepath.Join(dir, "mem")}})
	_, err := s.Write(ctx, user, add("seed", "x", memory.KindUser))
	assert.NoErr(t, err)
	for _, id := range []string{"../secret", "a/b", `a\b`, filepath.Join(dir, "secret"), "/etc/passwd", "MEMORY", "memory", ".."} {
		t.Run(id, func(t *testing.T) {
			_, err := s.Get(ctx, user, id)
			assert.True(t, errors.Is(err, memory.ErrInvalid), id)
			_, err = s.Write(ctx, user, memory.Op{Action: memory.ActionReplace, ID: id, Title: "t", Body: "b"})
			assert.True(t, errors.Is(err, memory.ErrInvalid), id)
			_, err = s.Write(ctx, user, memory.Op{Action: memory.ActionRemove, ID: id})
			assert.True(t, errors.Is(err, memory.ErrInvalid), id)
		})
	}
	assert.Eq(t, "outside", readText(t, filepath.Join(dir, "secret.md")))
}

func TestSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside")
	assert.NoErr(t, os.MkdirAll(outside, 0o700))
	assert.NoErr(t, os.WriteFile(filepath.Join(outside, "evil.md"), []byte("---\nkind: user\ntitle: \"stolen\"\n---\n\nsecret\n"), 0o600))

	t.Run("entry symlink cannot be read", func(t *testing.T) {
		mem := filepath.Join(dir, "mem")
		assert.NoErr(t, os.MkdirAll(mem, 0o700))
		if err := os.Symlink(filepath.Join(outside, "evil.md"), filepath.Join(mem, "evil.md")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		s := newStore(t, map[memory.Scope]Layout{user: DirLayout{Dir: mem}})
		_, err := s.Get(ctx, user, "evil")
		assert.Err(t, err)
		assert.False(t, errors.Is(err, memory.ErrNotFound))
		hits, err := s.Search(ctx, user, "secret", 5)
		assert.NoErr(t, err)
		assert.Empty(t, hits, "escaping files are skipped by search")
		_, err = s.Write(ctx, user, memory.Op{Action: memory.ActionRemove, ID: "evil"})
		assert.Err(t, err)
		assert.True(t, fileExists(filepath.Join(outside, "evil.md")))
	})
	t.Run("entry dir symlink cannot be written through", func(t *testing.T) {
		repo := filepath.Join(dir, "repo")
		assert.NoErr(t, os.MkdirAll(repo, 0o700))
		if err := os.Symlink(outside, filepath.Join(repo, ".suag")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		s := newStore(t, map[memory.Scope]Layout{project: RepoLayout{Root: repo}})
		_, err := s.Write(ctx, project, add("Leak", "x", memory.KindUser))
		assert.Err(t, err)
		assert.False(t, fileExists(filepath.Join(outside, "memory", "leak.md")))
	})
	t.Run("index symlink cannot be read", func(t *testing.T) {
		mem := filepath.Join(dir, "mem2")
		assert.NoErr(t, os.MkdirAll(mem, 0o700))
		if err := os.Symlink(filepath.Join(outside, "evil.md"), filepath.Join(mem, indexFile)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		s := newStore(t, map[memory.Scope]Layout{user: DirLayout{Dir: mem}})
		_, err := s.Index(ctx, user)
		assert.Err(t, err)
	})
}

func TestSearch(t *testing.T) {
	dir := t.TempDir()
	clock := fixed
	s, err := New(map[memory.Scope]Layout{user: DirLayout{Dir: dir}}, WithClock(func() time.Time {
		clock = clock.Add(time.Minute)
		return clock
	}))
	assert.Require(t, assert.NoErr(t, err))
	_, _ = s.Write(ctx, user, add("Go testing", "Use table tests with t.Run for every method.", memory.KindFeedback))
	_, _ = s.Write(ctx, user, add("Python testing", "Use pytest; tables via parametrize.", memory.KindFeedback))
	_, _ = s.Write(ctx, user, add("Release flow", "Push from the host, never from the container.", memory.KindProject))
	_, _ = s.Write(ctx, user, add("中文笔记", "提交前运行全量测试", memory.KindUser))

	t.Run("multi term AND, case insensitive", func(t *testing.T) {
		hits, err := s.Search(ctx, user, "TABLE testing", 0)
		assert.NoErr(t, err)
		assert.Len(t, hits, 2)
		assert.Eq(t, "python-testing", hits[0].ID, "newest first")
		hits, err = s.Search(ctx, user, "table go", 0)
		assert.NoErr(t, err)
		assert.Len(t, hits, 1)
		assert.Eq(t, "go-testing", hits[0].ID)
		assert.Contains(t, hits[0].Snippet, "Go testing Use table tests")
	})
	t.Run("limit and no match", func(t *testing.T) {
		hits, _ := s.Search(ctx, user, "use", 1)
		assert.Len(t, hits, 1)
		hits, _ = s.Search(ctx, user, "rust", 5)
		assert.Empty(t, hits)
	})
	t.Run("non ascii", func(t *testing.T) {
		hits, err := s.Search(ctx, user, "全量", 5)
		assert.NoErr(t, err)
		assert.Len(t, hits, 1)
		assert.Eq(t, memory.KindUser, hits[0].Kind)
	})
	t.Run("long body snippet is cut", func(t *testing.T) {
		_, _ = s.Write(ctx, user, add("Long", strings.Repeat("filler ", 100)+"needle "+strings.Repeat("tail ", 100), memory.KindUser))
		hits, _ := s.Search(ctx, user, "needle", 5)
		assert.Len(t, hits, 1)
		assert.True(t, strings.HasPrefix(hits[0].Snippet, "…"))
		assert.True(t, strings.HasSuffix(hits[0].Snippet, "…"))
		assert.Contains(t, hits[0].Snippet, "needle")
		assert.Lt(t, len(hits[0].Snippet), 200)
	})
	t.Run("empty query", func(t *testing.T) {
		_, err := s.Search(ctx, user, " ", 5)
		assert.True(t, errors.Is(err, memory.ErrInvalid))
	})
	t.Run("missing dir", func(t *testing.T) {
		s := newStore(t, map[memory.Scope]Layout{user: DirLayout{Dir: filepath.Join(dir, "none")}})
		hits, err := s.Search(ctx, user, "x", 5)
		assert.NoErr(t, err)
		assert.Empty(t, hits)
	})
}

func TestConcurrentAdds(t *testing.T) {
	dir := t.TempDir()
	s := newStore(t, map[memory.Scope]Layout{user: DirLayout{Dir: dir}})
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Write(ctx, user, add(fmt.Sprintf("Note %d", i), "body", memory.KindUser))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		assert.NoErr(t, err)
	}
	ix, err := s.Index(ctx, user)
	assert.NoErr(t, err)
	assert.Len(t, ix.Lines, 8)
}

func TestNewAndScopes(t *testing.T) {
	t.Run("relative root rejected", func(t *testing.T) {
		_, err := New(map[memory.Scope]Layout{user: DirLayout{Dir: "rel/dir"}})
		assert.True(t, errors.Is(err, memory.ErrInvalid))
	})
	t.Run("invalid scope rejected", func(t *testing.T) {
		_, err := New(map[memory.Scope]Layout{{Kind: "team"}: DirLayout{Dir: t.TempDir()}})
		assert.True(t, errors.Is(err, memory.ErrInvalid))
	})
	t.Run("unknown scope", func(t *testing.T) {
		s := newStore(t, map[memory.Scope]Layout{user: DirLayout{Dir: t.TempDir()}})
		_, err := s.Index(ctx, memory.ProjectScope("other"))
		assert.True(t, errors.Is(err, memory.ErrNoBackend))
		assert.Eq(t, "files", s.BackendName())
	})
	t.Run("repo root is never created", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "no-repo")
		s := newStore(t, map[memory.Scope]Layout{project: RepoLayout{Root: missing}})
		_, err := s.Write(ctx, project, add("a", "b", memory.KindUser))
		assert.Err(t, err)
		assert.False(t, fileExists(missing))
		ix, err := s.Index(ctx, project)
		assert.NoErr(t, err)
		assert.Empty(t, ix.Lines)
	})
	t.Run("governed over files", func(t *testing.T) {
		s := newStore(t, map[memory.Scope]Layout{user: DirLayout{Dir: t.TempDir()}})
		g := memory.NewGoverned(s, memory.Policy{})
		e, err := g.Write(ctx, user, add("Key note​", "token=abc123", memory.KindUser))
		assert.NoErr(t, err)
		got, err := s.Get(ctx, user, e.ID)
		assert.NoErr(t, err)
		assert.Eq(t, "Key note", got.Title)
		assert.Eq(t, "token=[REDACTED]", got.Body)
	})
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
