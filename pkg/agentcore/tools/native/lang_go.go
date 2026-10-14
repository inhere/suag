package native

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// goLanguage is the Go declaration: the module cache and the standard library
// sources as read-only roots, `go doc` for a dependency's API, `go test` (and
// `-run` for one test) to run the tests.
var goLanguage = Language{
	Name:          "Go",
	Markers:       []string{"go.mod", "go.sum"},
	DependencyAPI: "`go doc <package>` (or a grep for the exported names) for Go",
	TestCommands:  `\bgo test\b`,
	FocusedFlags:  []string{"-run"},
	dependencyRoots: func(env langEnv, add *rootCollector) {
		// GOMODCACHE, else GOPATH/pkg/mod, else ~/go/pkg/mod, and GOROOT/src
		// (GOROOT from the environment or from the go binary in PATH).
		modCache := env.goVar("GOMODCACHE")
		if modCache == "" {
			if paths := filepath.SplitList(env.goVar("GOPATH")); len(paths) > 0 && paths[0] != "" {
				modCache = filepath.Join(paths[0], "pkg", "mod")
			} else if env.home != "" {
				modCache = filepath.Join(env.home, "go", "pkg", "mod")
			}
		}
		add.add("Go modules", modCache)
		if goRoot := goRootDir(env.goVar("GOROOT")); goRoot != "" {
			add.add("Go standard library", filepath.Join(goRoot, "src"))
		}
	},
}

// readGoEnvFile parses the file `go env -w` writes (GOENV, default
// <UserConfigDir>/go/env): KEY=VALUE lines. Missing or unreadable is empty.
// GOENV=off means no file: the process environment is then the only source.
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
