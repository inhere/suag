package native

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// searchTimeout bounds one file_grep / file_find walk; what was found by
// then is returned and marked incomplete. A var so tests can shorten it.
var searchTimeout = 10 * time.Second

// maxSearchFileBytes: larger files are skipped by file_grep.
const maxSearchFileBytes = 1 << 20

// errSearchFull stops a walk once enough results were collected.
var errSearchFull = errors.New("search result limit reached")

// gitignore holds the simple patterns of the workspace root .gitignore. It is
// best effort: negation (!) and nested .gitignore files are not supported.
type gitignore struct {
	patterns []ignorePattern
}

type ignorePattern struct {
	glob     string // slash-separated, without leading/trailing slash
	dirOnly  bool   // pattern ended with "/"
	anchored bool   // pattern contains a "/" before its end: match the full path
}

// loadGitignore reads <root>/.gitignore through root; a missing file is an
// empty matcher.
func loadGitignore(root *os.Root) *gitignore {
	g := &gitignore{}
	f, err := root.Open(".gitignore")
	if err != nil {
		return g
	}
	defer f.Close()
	sc := bufio.NewScanner(io.LimitReader(f, 256<<10))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		p := ignorePattern{}
		if strings.HasSuffix(line, "/") {
			p.dirOnly = true
			line = strings.TrimRight(line, "/")
		}
		line = strings.TrimPrefix(line, "**/")
		if strings.Contains(line, "/") {
			p.anchored = true
			line = strings.TrimPrefix(line, "/")
		}
		if line != "" {
			p.glob = line
			g.patterns = append(g.patterns, p)
		}
	}
	return g
}

// ignored reports whether rel (slash-separated, relative to the workspace)
// matches a pattern.
func (g *gitignore) ignored(rel string, isDir bool) bool {
	for _, p := range g.patterns {
		if p.dirOnly && !isDir {
			continue
		}
		target := path.Base(rel)
		if p.anchored {
			target = rel
		}
		if ok, _ := path.Match(p.glob, target); ok {
			return true
		}
	}
	return false
}

// walkWorkspace visits the regular files under start (a path inside workspace)
// in lexical order, calling fn with the slash-separated path relative to the
// workspace. Every open goes through os.Root and symlinks are never followed,
// so nothing outside the workspace is read. ignoredDirs and the root
// .gitignore are skipped. fn may return errSearchFull to stop early. The walk
// ends at ctx's deadline; timedOut then reports an incomplete result.
func walkWorkspace(ctx context.Context, workspace, start string, fn func(root *os.Root, rel string, d fs.DirEntry) error) (timedOut bool, err error) {
	_, err = withRoot(workspace, start, func(root *os.Root, relStart string) (struct{}, error) {
		ign := loadGitignore(root)
		fsStart := filepath.ToSlash(relStart)
		info, err := root.Lstat(relStart)
		if err != nil {
			return struct{}{}, err
		}
		if info.Mode().IsRegular() { // a single file
			return struct{}{}, fn(root, fsStart, fs.FileInfoToDirEntry(info))
		}
		werr := fs.WalkDir(root.FS(), fsStart, func(rel string, d fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				timedOut = true
				return fs.SkipAll
			}
			if err != nil {
				if rel == fsStart {
					return err
				}
				return nil // unreadable entry: skip it
			}
			if d.IsDir() {
				if rel != fsStart && (ignoredDirs[d.Name()] || ign.ignored(rel, true)) {
					return fs.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() || ign.ignored(rel, false) {
				return nil // symlinks and special files are not followed
			}
			return fn(root, rel, d)
		})
		if errors.Is(werr, errSearchFull) {
			werr = nil
		}
		return struct{}{}, werr
	})
	return timedOut, err
}

// isBinaryHead reports whether head (the start of a file) looks binary.
func isBinaryHead(head []byte) bool { return bytes.IndexByte(head, 0) >= 0 }

// globMatch matches a slash-separated rel path against pattern. "**" matches
// any number of path segments; a pattern without "/" matches the base name
// at any depth ("*.go").
func globMatch(pattern, rel string) bool {
	pattern = strings.TrimPrefix(filepath.ToSlash(pattern), "./")
	if !strings.Contains(pattern, "/") {
		ok, _ := path.Match(pattern, path.Base(rel))
		return ok
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(rel, "/"))
}

func matchSegments(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for i := 0; i <= len(segs); i++ {
				if matchSegments(pat[1:], segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], segs[0]); !ok {
			return false
		}
		pat, segs = pat[1:], segs[1:]
	}
	return len(segs) == 0
}
