package native

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ReadRoot is a directory outside the workspace that the read-only file tools
// (file_read, file_list, file_grep, file_find) may read. Writes stay confined
// to the workspace.
type ReadRoot struct {
	Label string // what the directory holds, e.g. "Go modules"
	Path  string // absolute and cleaned
}

// DependencyRoots detects the local dependency source caches: the Go module
// cache, the Go standard library sources and the Rust crate sources. Only
// source directories are returned, never a whole tool home (~/.cargo holds
// credentials). Directories that do not exist are left out. It runs no
// external command: the go env file, environment variables and the go binary
// location in PATH are enough.
func DependencyRoots() []ReadRoot {
	home, _ := os.UserHomeDir()
	goEnv := readGoEnvFile()
	goVar := func(key string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return goEnv[key]
	}

	var out []ReadRoot
	add := func(label, dir string) {
		if dir == "" || !filepath.IsAbs(dir) {
			return
		}
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			out = append(out, ReadRoot{Label: label, Path: filepath.Clean(dir)})
		}
	}

	modCache := goVar("GOMODCACHE")
	if modCache == "" {
		if paths := filepath.SplitList(goVar("GOPATH")); len(paths) > 0 && paths[0] != "" {
			modCache = filepath.Join(paths[0], "pkg", "mod")
		} else if home != "" {
			modCache = filepath.Join(home, "go", "pkg", "mod")
		}
	}
	add("Go modules", modCache)
	if goRoot := goRootDir(goVar("GOROOT")); goRoot != "" {
		add("Go standard library", filepath.Join(goRoot, "src"))
	}

	cargoHome := os.Getenv("CARGO_HOME")
	if cargoHome == "" && home != "" {
		cargoHome = filepath.Join(home, ".cargo")
	}
	if cargoHome != "" {
		add("Rust crates", filepath.Join(cargoHome, "registry", "src"))
	}
	return out
}

// readGoEnvFile parses the file `go env -w` writes (GOENV, default
// <UserConfigDir>/go/env): KEY=VALUE lines. Missing or unreadable is empty.
func readGoEnvFile() map[string]string {
	path := os.Getenv("GOENV")
	if path == "off" {
		return nil
	}
	if path == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return nil
		}
		path = filepath.Join(dir, "go", "env")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	vars := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if ok && key != "" && !strings.HasPrefix(key, "#") {
			vars[key] = value
		}
	}
	return vars
}

// goRootDir returns GOROOT: the explicit value, or the directory two levels
// above the go binary found in PATH (<goroot>/bin/go).
func goRootDir(explicit string) string {
	if explicit != "" {
		return explicit
	}
	bin, err := exec.LookPath("go")
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(bin); err == nil {
		bin = real
	}
	bin, err = filepath.Abs(bin)
	if err != nil {
		return ""
	}
	return filepath.Dir(filepath.Dir(bin))
}

// ConfiguredReadRoots turns tools.read_roots entries into read roots: "~" is
// expanded, the path must be absolute and an existing directory. Rejected
// entries are returned as errors so the caller can report them.
func ConfiguredReadRoots(entries []string) ([]ReadRoot, []error) {
	home, _ := os.UserHomeDir()
	var out []ReadRoot
	var errs []error
	for _, entry := range entries {
		dir := strings.TrimSpace(entry)
		if dir == "" {
			continue
		}
		if home != "" && (dir == "~" || strings.HasPrefix(dir, "~/") || strings.HasPrefix(dir, `~\`)) {
			dir = filepath.Join(home, dir[1:])
		}
		if !filepath.IsAbs(dir) {
			errs = append(errs, fmt.Errorf("tools.read_roots: %q is not an absolute path", entry))
			continue
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			errs = append(errs, fmt.Errorf("tools.read_roots: %q is not an existing directory", entry))
			continue
		}
		out = append(out, ReadRoot{Label: "configured", Path: filepath.Clean(dir)})
	}
	return out, errs
}

// RootPaths returns the paths of roots, in order.
func RootPaths(roots []ReadRoot) []string {
	paths := make([]string, len(roots))
	for i, r := range roots {
		paths[i] = r.Path
	}
	return paths
}

// readTarget is where a read-only tool's path argument points: base is the
// directory that confines the I/O (the workspace or a read root) and abs the
// absolute path under it.
type readTarget struct {
	base, abs string
	outside   bool // base is a read root, not the workspace
}

// show turns a slash-separated path relative to base into what the tool
// prints: unchanged inside the workspace, absolute under a read root so the
// model can pass it straight back to another tool.
func (r readTarget) show(rel string) string {
	if !r.outside {
		return rel
	}
	return filepath.Join(r.base, filepath.FromSlash(rel))
}

// matchUnder reports whether the glob matches rel (slash-separated, relative
// to base) or rel taken relative to the search start: models naturally write
// a glob relative to the path they search (path=goutil@v0.8.2, glob=x/assert/*.go),
// while the walk reports paths relative to the workspace or read root.
func (r readTarget) matchUnder(glob, rel string) bool {
	if globMatch(glob, rel) {
		return true
	}
	start, err := filepath.Rel(r.base, r.abs)
	if err != nil || start == "." {
		return false
	}
	prefix := filepath.ToSlash(start) + "/"
	return strings.HasPrefix(rel, prefix) && globMatch(glob, strings.TrimPrefix(rel, prefix))
}

// resolveRead resolves a read-only tool's path argument: inside the workspace
// as ResolveSandboxedPath does, otherwise an absolute path under one of roots.
func resolveRead(workspace string, roots []string, arg string) (readTarget, error) {
	abs, err := ResolveSandboxedPath(workspace, arg)
	if err == nil {
		return readTarget{base: workspace, abs: abs}, nil
	}
	if !filepath.IsAbs(arg) || len(roots) == 0 {
		return readTarget{}, err
	}
	for _, root := range roots {
		if mapped, ok := absUnderWorkspace(root, arg, foldPathCase); ok {
			return readTarget{base: root, abs: mapped, outside: true}, nil
		}
	}
	return readTarget{}, fmt.Errorf("path escape: %q is outside the workspace and the read-only dependency roots", arg)
}
