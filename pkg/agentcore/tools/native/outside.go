package native

import (
	"os"
	"path/filepath"
)

// OutsideDir is a directory a file tool call wants to reach that lies outside
// the workspace, the configured read roots and the session's added directories.
// The chat approver asks the person to authorize such a directory before the
// call runs (chat.ask_outside_dirs).
type OutsideDir struct {
	// Path is the canonical absolute path of the directory to authorize: the
	// target itself when it is an existing directory, the directory of an
	// existing file, else the nearest existing ancestor. It is derived from the
	// path the tool would really resolve (symlinks followed), never from words
	// the model wrote about the call.
	Path string
	// Target is the call's path argument, cleaned but otherwise as given, for
	// the question's detail line.
	Target string
	// Writable reports that the call would change files under Path (a write
	// tool), so authorizing it read-only would not be enough.
	Writable bool
}

// OutsideOf reports the directory a file tool call reaches outside the
// workspace, the read roots and dirs. Only the absolute path arguments of the
// file tools count: a relative path can only ever name a place inside the
// workspace, and a path that is already reachable needs no authorization. ok is
// false when nothing has to be authorized (an unknown tool, no path argument,
// a reachable path, or a target with no authorizable directory such as the
// volume root).
//
// readRoots are the tool's static read-only roots (native.RootPaths); dirs is
// the session's live added-directory set. A write call is checked against the
// read-write directories only, exactly like the write tools themselves, so a
// write into a read-only added directory is offered as an authorization too.
func OutsideOf(workspace string, readRoots []string, dirs *AddDirs, tool string, args map[string]any) (OutsideDir, bool) {
	paths, writable, ok := fileCallPaths(tool, args)
	if !ok {
		return OutsideDir{}, false
	}
	roots := readRoots
	if writable {
		roots = dirs.WritePaths()
	} else {
		roots = withAddDirs(readRoots, dirs)
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			continue
		}
		if insideAnyRoot(workspace, path) || insideAnyRoots(roots, path) {
			continue
		}
		dir, ok := authorizableDir(path)
		if !ok {
			continue
		}
		return OutsideDir{Path: dir, Target: filepath.Clean(path), Writable: writable}, true
	}
	return OutsideDir{}, false
}

// fileCallPaths returns the path arguments of a file tool call and whether the
// call would write there. ok is false for a tool that does not touch files and
// for a call whose arguments the tool itself will refuse (a malformed path
// list, a missing path).
func fileCallPaths(tool string, args map[string]any) (paths []string, writable, ok bool) {
	switch tool {
	case "file_read":
		list, err := ReadPathList(args)
		if err != nil {
			return nil, false, false
		}
		return list, false, true
	case "file_list", "file_grep", "file_find":
		return singlePathArg(args, false)
	case "file_write", "file_edit", "file_remove":
		return singlePathArg(args, true)
	}
	return nil, false, false
}

// singlePathArg returns the "path" argument of a one-path file tool.
func singlePathArg(args map[string]any, writable bool) ([]string, bool, bool) {
	path, _ := args["path"].(string)
	if path == "" {
		return nil, writable, false
	}
	return []string{path}, writable, true
}

// insideAnyRoot reports whether path lies under root (symlink-resolved roots
// included, like the tools' own resolution).
func insideAnyRoot(root, path string) bool {
	if root == "" {
		return false
	}
	_, ok := absUnderWorkspace(root, path, foldPathCase)
	return ok
}

// insideAnyRoots reports whether path lies under one of roots.
func insideAnyRoots(roots []string, path string) bool {
	for _, root := range roots {
		if insideAnyRoot(root, path) {
			return true
		}
	}
	return false
}

// authorizableDir returns the directory to authorize for path. The path itself
// is resolved first (symlinks followed), so a link is asked about where it
// really points; when it is an existing file its directory is authorized
// instead. A path that does not exist yet walks up to its nearest existing
// ancestor, so authorizing a directory also covers a file about to be created
// in it. The volume root is never offered: authorizing it would hand over the
// whole filesystem, and it is not a directory a person means to grant.
func authorizableDir(path string) (string, bool) {
	candidate := filepath.Clean(path)
	for {
		real, err := filepath.EvalSymlinks(candidate)
		if err == nil {
			real = filepath.Clean(real)
			if info, statErr := os.Stat(real); statErr == nil && !info.IsDir() {
				real = filepath.Dir(real)
			}
			if filepath.Dir(real) == real {
				return "", false
			}
			return real, true
		}
		// Anything but "missing" (permissions, a link loop) is resolved the same
		// way: the nearest ancestor that does resolve is the best offer.
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return "", false
		}
		candidate = parent
	}
}
