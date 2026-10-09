package native

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestRelUnderRoot(t *testing.T) {
	sep := string(filepath.Separator)
	root := sep + "ws"
	cases := []struct {
		name, p string
		fold    bool
		want    string
		ok      bool
	}{
		{"root itself", root, false, ".", true},
		{"child", root + sep + "a" + sep + "b", false, filepath.Join("a", "b"), true},
		{"sibling prefix", root + "2" + sep + "a", false, "", false},
		{"parent", sep, false, "", false},
		{"case differs, exact", sep + "WS" + sep + "a", false, "", false},
		{"case differs, folded", sep + "WS" + sep + "A", true, "A", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := relUnderRoot(root, c.p, c.fold)
			assert.Eq(t, c.ok, ok)
			assert.Eq(t, c.want, got)
		})
	}
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
}

func TestFileTools_AbsolutePaths(t *testing.T) {
	base := t.TempDir()
	ws := filepath.Join(base, "ws")
	outside := filepath.Join(base, "outside")
	assert.NoErr(t, os.MkdirAll(filepath.Join(ws, "real"), 0o755))
	assert.NoErr(t, os.MkdirAll(outside, 0o755))
	assert.NoErr(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644))
	assert.NoErr(t, os.WriteFile(filepath.Join(ws, "real", "ok.txt"), []byte("ok"), 0o644))

	rd := &FileReadTool{workspace: ws}
	wr := &FileWriteTool{workspace: ws}
	ls := &FileListTool{workspace: ws}
	ctx := context.Background()

	t.Run("absolute inside is readable and writable", func(t *testing.T) {
		res, err := rd.Execute(ctx, call(map[string]any{"path": filepath.Join(ws, "real", "ok.txt")}))
		assert.NoErr(t, err)
		assert.Eq(t, "ok", res.Content)

		_, err = wr.Execute(ctx, call(map[string]any{"path": filepath.Join(ws, "new", "w.txt"), "content": "w"}))
		assert.NoErr(t, err)
		b, err := os.ReadFile(filepath.Join(ws, "new", "w.txt"))
		assert.NoErr(t, err)
		assert.Eq(t, "w", string(b))

		res, err = ls.Execute(ctx, call(map[string]any{"dir": filepath.Join(ws, "real")}))
		assert.NoErr(t, err)
		assert.StrContains(t, res.Content, "ok.txt")
	})

	t.Run("absolute outside is rejected", func(t *testing.T) {
		_, err := rd.Execute(ctx, call(map[string]any{"path": filepath.Join(outside, "secret.txt")}))
		assert.Err(t, err)
		assert.StrContains(t, err.Error(), "path escape")
		_, err = wr.Execute(ctx, call(map[string]any{"path": filepath.Join(outside, "n.txt"), "content": "x"}))
		assert.Err(t, err)
		_, statErr := os.Stat(filepath.Join(outside, "n.txt"))
		assert.True(t, os.IsNotExist(statErr))
	})
}

func TestFileTools_AbsoluteSymlinkTargets(t *testing.T) {
	base := t.TempDir()
	ws := filepath.Join(base, "ws")
	outside := filepath.Join(base, "outside")
	assert.NoErr(t, os.MkdirAll(filepath.Join(ws, "real"), 0o755))
	assert.NoErr(t, os.MkdirAll(outside, 0o755))
	assert.NoErr(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644))
	assert.NoErr(t, os.WriteFile(filepath.Join(ws, "real", "ok.txt"), []byte("ok"), 0o644))

	symlinkOrSkip(t, filepath.Join(ws, "real"), filepath.Join(ws, "absin"))         // absolute -> inside
	symlinkOrSkip(t, filepath.Join(ws, "real", "ok.txt"), filepath.Join(ws, "f"))   // absolute file -> inside
	symlinkOrSkip(t, outside, filepath.Join(ws, "absout"))                          // absolute -> outside
	symlinkOrSkip(t, "real", filepath.Join(ws, "rel"))                              // relative -> inside
	symlinkOrSkip(t, filepath.Join(outside, "secret.txt"), filepath.Join(ws, "sf")) // file -> outside
	// chain: hop1 -> outside, outside/back -> ws/real (leaves, then re-enters)
	symlinkOrSkip(t, filepath.Join(ws, "real"), filepath.Join(outside, "back"))
	symlinkOrSkip(t, filepath.Join(outside, "back"), filepath.Join(ws, "hop"))
	// chain that ends outside: ws/hop2 -> ws/absout -> outside
	symlinkOrSkip(t, filepath.Join(ws, "absout"), filepath.Join(ws, "hop2"))
	// ".." segment: link inside ws whose target climbs out and back in
	symlinkOrSkip(t, filepath.Join(ws, "real", "..", "..", "outside"), filepath.Join(ws, "dotdot"))

	rd := &FileReadTool{workspace: ws}
	wr := &FileWriteTool{workspace: ws}
	ls := &FileListTool{workspace: ws}
	ctx := context.Background()
	read := func(p string) (string, error) {
		res, err := rd.Execute(ctx, call(map[string]any{"path": p}))
		if err != nil {
			return "", err
		}
		return res.Content, nil
	}

	t.Run("absolute link to inside is readable, writable, listable", func(t *testing.T) {
		got, err := read("absin/ok.txt")
		assert.NoErr(t, err)
		assert.Eq(t, "ok", got)
		got, err = read("f")
		assert.NoErr(t, err)
		assert.Eq(t, "ok", got)

		_, err = wr.Execute(ctx, call(map[string]any{"path": "absin/sub/w.txt", "content": "w"}))
		assert.NoErr(t, err)
		b, err := os.ReadFile(filepath.Join(ws, "real", "sub", "w.txt"))
		assert.NoErr(t, err)
		assert.Eq(t, "w", string(b))

		res, err := ls.Execute(ctx, call(map[string]any{"dir": "absin"}))
		assert.NoErr(t, err)
		assert.StrContains(t, res.Content, "ok.txt")
	})

	t.Run("chain leaving and re-entering ends inside", func(t *testing.T) {
		got, err := read("hop/ok.txt")
		assert.NoErr(t, err)
		assert.Eq(t, "ok", got)
	})

	t.Run("relative link still works", func(t *testing.T) {
		got, err := read("rel/ok.txt")
		assert.NoErr(t, err)
		assert.Eq(t, "ok", got)
	})

	t.Run("links ending outside are rejected", func(t *testing.T) {
		for _, p := range []string{"absout/secret.txt", "sf", "hop2/secret.txt", "dotdot/secret.txt"} {
			_, err := read(p)
			assert.Err(t, err)
			assert.StrContains(t, err.Error(), "path escape")
		}
		_, err := wr.Execute(ctx, call(map[string]any{"path": "absout/new.txt", "content": "x"}))
		assert.Err(t, err)
		_, err = wr.Execute(ctx, call(map[string]any{"path": "hop2/sub/new.txt", "content": "x"}))
		assert.Err(t, err)
		_, err = ls.Execute(ctx, call(map[string]any{"dir": "absout"}))
		assert.Err(t, err)
		for _, p := range []string{"new.txt", "sub"} {
			_, statErr := os.Stat(filepath.Join(outside, p))
			assert.True(t, os.IsNotExist(statErr))
		}
	})
}

func TestFileTools_WorkspaceRootIsSymlink(t *testing.T) {
	base := t.TempDir()
	realWs := filepath.Join(base, "realws")
	assert.NoErr(t, os.MkdirAll(filepath.Join(realWs, "d"), 0o755))
	assert.NoErr(t, os.WriteFile(filepath.Join(realWs, "d", "ok.txt"), []byte("ok"), 0o644))
	assert.NoErr(t, os.WriteFile(filepath.Join(base, "secret.txt"), []byte("secret"), 0o644))
	ws := filepath.Join(base, "link")
	symlinkOrSkip(t, realWs, ws)
	// absolute link inside the (symlinked) workspace pointing at its real path
	symlinkOrSkip(t, filepath.Join(realWs, "d"), filepath.Join(realWs, "absin"))
	symlinkOrSkip(t, base, filepath.Join(realWs, "up"))

	rd := &FileReadTool{workspace: ws}
	wr := &FileWriteTool{workspace: ws}
	ctx := context.Background()
	read := func(p string) (string, error) {
		res, err := rd.Execute(ctx, call(map[string]any{"path": p}))
		if err != nil {
			return "", err
		}
		return res.Content, nil
	}

	t.Run("relative, lexical-absolute and real-absolute paths", func(t *testing.T) {
		for _, p := range []string{"d/ok.txt", filepath.Join(ws, "d", "ok.txt"), filepath.Join(realWs, "d", "ok.txt"), "absin/ok.txt"} {
			got, err := read(p)
			assert.NoErr(t, err)
			assert.Eq(t, "ok", got)
		}
		_, err := wr.Execute(ctx, call(map[string]any{"path": filepath.Join(realWs, "absin", "w.txt"), "content": "w"}))
		assert.NoErr(t, err)
		b, err := os.ReadFile(filepath.Join(realWs, "d", "w.txt"))
		assert.NoErr(t, err)
		assert.Eq(t, "w", string(b))
	})

	t.Run("escape via link to parent is rejected", func(t *testing.T) {
		_, err := read("up/secret.txt")
		assert.Err(t, err)
		_, err = read(filepath.Join(base, "secret.txt"))
		assert.Err(t, err)
	})
}

func TestWithRoot_ErrorClassification(t *testing.T) {
	base := t.TempDir()
	ws := filepath.Join(base, "ws")
	outside := filepath.Join(base, "outside")
	assert.NoErr(t, os.MkdirAll(filepath.Join(ws, "dir"), 0o755))
	assert.NoErr(t, os.MkdirAll(outside, 0o755))
	assert.NoErr(t, os.WriteFile(filepath.Join(ws, "file.txt"), []byte("x"), 0o644))
	assert.NoErr(t, os.WriteFile(filepath.Join(ws, "ok.txt"), []byte("ok"), 0o644))

	t.Run("EISDIR does not trigger a second execution", func(t *testing.T) {
		runs := 0
		err := writeFileInRootCounting(ws, filepath.Join(ws, "dir"), &runs)
		assert.Err(t, err)
		assert.Eq(t, 1, runs)
		assert.StrNotContains(t, err.Error(), "path escape")
	})

	t.Run("a file as intermediate path is not reported as an escape", func(t *testing.T) {
		err := writeFileInRoot(ws, filepath.Join(ws, "file.txt", "sub", "a.txt"), []byte("x"))
		assert.Err(t, err)
		assert.StrNotContains(t, err.Error(), "path escape")
		_, err = readFileInRoot(ws, filepath.Join(ws, "file.txt", "a.txt"))
		assert.Err(t, err)
		assert.StrNotContains(t, err.Error(), "path escape")
	})

	t.Run("escape through a symlink is still rejected", func(t *testing.T) {
		symlinkOrSkip(t, outside, filepath.Join(ws, "out"))
		err := writeFileInRoot(ws, filepath.Join(ws, "out", "a.txt"), []byte("x"))
		assert.Err(t, err)
		assert.StrContains(t, err.Error(), "path escape")
	})

	t.Run("absolute symlink to inside the workspace still works", func(t *testing.T) {
		symlinkOrSkip(t, filepath.Join(ws, "dir"), filepath.Join(ws, "in"))
		assert.NoErr(t, writeFileInRoot(ws, filepath.Join(ws, "in", "a.txt"), []byte("a")))
		b, err := readFileInRoot(ws, filepath.Join(ws, "in", "a.txt"))
		assert.NoErr(t, err)
		assert.Eq(t, "a", string(b))
	})
}

// writeFileInRootCounting counts how many times withRoot runs the operation.
func writeFileInRootCounting(ws, abs string, runs *int) error {
	_, err := withRoot(ws, abs, func(root *os.Root, rel string) (struct{}, error) {
		*runs++
		f, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return struct{}{}, err
		}
		return struct{}{}, f.Close()
	})
	return err
}
