package native

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

// interpreterFake is an interpreterProbe over maps: paths resolves a looked-up
// command name, outputs holds each resolved path's --version output (a missing
// path stands for a probe that failed, e.g. the exit code 9009 of the Windows
// Store stub).
type interpreterFake struct {
	paths   map[string]string
	outputs map[string]string
	windows bool
	probes  int
}

func (f *interpreterFake) probe() interpreterProbe {
	return interpreterProbe{
		windows: f.windows,
		lookup: func(name string) (string, error) {
			if path, ok := f.paths[name]; ok {
				return path, nil
			}
			return "", exec.ErrNotFound
		},
		version: func(_ context.Context, path string) ([]byte, error) {
			f.probes++
			out, ok := f.outputs[path]
			if !ok {
				return []byte(""), fmt.Errorf("exit status 9009")
			}
			return []byte(out), nil
		},
	}
}

// windowsPaths is a Windows-like set: python3 resolves to the same file as
// python (deduplicated), node to its own, plus a Store alias path.
func windowsFake() *interpreterFake {
	const (
		pythonExe = `C:\Python312\python.exe`
		nodeExe   = `C:\Program Files\nodejs\node.exe`
		storeExe  = `C:\Users\me\AppData\Local\Microsoft\WindowsApps\python.exe`
	)
	return &interpreterFake{
		windows: true,
		paths: map[string]string{
			"python3": pythonExe,
			"python":  pythonExe,
			"node":    nodeExe,
			"store":   storeExe,
		},
		outputs: map[string]string{
			pythonExe: "Python 3.12.4\n",
			nodeExe:   "v22.6.0\n",
			storeExe:  "Python 3.12.4\n",
		},
	}
}

func TestDetectInterpreters(t *testing.T) {
	ctx := context.Background()
	auto := InterpreterSetting{Enabled: true, Auto: true}

	t.Run("all candidates, duplicates by resolved path", func(t *testing.T) {
		fake := windowsFake()
		got := detectInterpreters(ctx, fake.probe(), auto)
		assert.Eq(t, 2, len(got))
		assert.Eq(t, Interpreter{Name: "python3", Path: `C:\Python312\python.exe`, Version: "3.12.4"}, got[0])
		assert.Eq(t, Interpreter{Name: "node", Path: `C:\Program Files\nodejs\node.exe`, Version: "22.6.0"}, got[1])
		// python3 and python are one binary: probed once.
		assert.Eq(t, 2, fake.probes)
	})

	t.Run("only python", func(t *testing.T) {
		fake := &interpreterFake{
			paths:   map[string]string{"python": "/usr/bin/python"},
			outputs: map[string]string{"/usr/bin/python": "Python 3.11.9\n"},
		}
		got := detectInterpreters(ctx, fake.probe(), auto)
		assert.Eq(t, 1, len(got))
		assert.Eq(t, "python", got[0].Name)
		assert.Eq(t, "3.11.9", got[0].Version)
	})

	t.Run("store alias is skipped even when it answers", func(t *testing.T) {
		// The app-execution alias of the Microsoft Store prints a version but
		// only forwards to the Store package: an auto-detected one is skipped.
		fake := windowsFake()
		spec := InterpreterSetting{Enabled: true, Names: []string{"store"}}
		assert.Empty(t, detectInterpreters(ctx, fake.probe(), spec))
	})

	t.Run("store alias answers nothing and fails", func(t *testing.T) {
		fake := windowsFake()
		delete(fake.outputs, `C:\Users\me\AppData\Local\Microsoft\WindowsApps\python.exe`)
		spec := InterpreterSetting{Enabled: true, Names: []string{"store"}}
		assert.Empty(t, detectInterpreters(ctx, fake.probe(), spec))
	})

	t.Run("store package install is not an alias", func(t *testing.T) {
		// "C:\Program Files\WindowsApps\<package>" is the real install of a
		// Store python, only "%LOCALAPPDATA%\Microsoft\WindowsApps" is the alias.
		const real = `C:\Program Files\WindowsApps\PythonSoftwareFoundation.Python.3.12_qbz5\python.exe`
		fake := &interpreterFake{
			windows: true,
			paths:   map[string]string{"python3": real},
			outputs: map[string]string{real: "Python 3.12.4\n"},
		}
		got := detectInterpreters(ctx, fake.probe(), auto)
		assert.Eq(t, 1, len(got))
		assert.Eq(t, real, got[0].Path)
	})

	t.Run("explicit absolute path bypasses the alias check", func(t *testing.T) {
		const storeExe = `C:\Users\me\AppData\Local\Microsoft\WindowsApps\python.exe`
		fake := windowsFake()
		spec := InterpreterSetting{Enabled: true, Names: []string{storeExe}}
		got := detectInterpreters(ctx, fake.probe(), spec)
		assert.Eq(t, 1, len(got))
		assert.Eq(t, storeExe, got[0].Path)
		// A configured path is not looked up, only probed.
		assert.Eq(t, 1, fake.probes)
	})

	t.Run("failed probe is dropped", func(t *testing.T) {
		fake := &interpreterFake{
			paths:   map[string]string{"python3": "/usr/bin/python3"},
			outputs: map[string]string{"/usr/bin/python3": "Python was not found\n"},
		}
		assert.Empty(t, detectInterpreters(ctx, fake.probe(), auto))
	})

	t.Run("none found", func(t *testing.T) {
		fake := &interpreterFake{}
		assert.Empty(t, detectInterpreters(ctx, fake.probe(), auto))
	})

	t.Run("disabled probes nothing", func(t *testing.T) {
		fake := windowsFake()
		assert.Empty(t, detectInterpreters(ctx, fake.probe(), ParseInterpreterSetting(false)))
		assert.Eq(t, 0, fake.probes)
	})

	t.Run("configured names probe those only", func(t *testing.T) {
		fake := windowsFake()
		got := detectInterpreters(ctx, fake.probe(), ParseInterpreterSetting([]any{"node"}))
		assert.Eq(t, 1, len(got))
		assert.Eq(t, "node", got[0].Name)
	})
}

func TestParseInterpreterSetting(t *testing.T) {
	t.Run("defaults to auto", func(t *testing.T) {
		for _, raw := range []any{nil, true, "auto", "on", "", []any{}, []string{}, 42} {
			assert.Eq(t, InterpreterSetting{Enabled: true, Auto: true}, ParseInterpreterSetting(raw))
		}
	})
	t.Run("off", func(t *testing.T) {
		for _, raw := range []any{false, "off", "false", "none", []any{"off"}} {
			assert.Eq(t, InterpreterSetting{Enabled: false}, ParseInterpreterSetting(raw))
		}
	})
	t.Run("names and paths", func(t *testing.T) {
		want := InterpreterSetting{Enabled: true, Names: []string{"python3", `C:\Python312\python.exe`}}
		assert.Eq(t, want, ParseInterpreterSetting([]any{"python3", `C:\Python312\python.exe`}))
		assert.Eq(t, want, ParseInterpreterSetting([]string{"python3", `C:\Python312\python.exe`}))
		assert.Eq(t, InterpreterSetting{Enabled: true, Names: []string{"node"}}, ParseInterpreterSetting("node"))
	})
}

func TestCachedInterpretersProbesOnce(t *testing.T) {
	fake := windowsFake()
	// A name of its own keeps this setting out of the cache of other tests.
	spec := InterpreterSetting{Enabled: true, Names: []string{"python3", "cached-probe-test"}}
	fake.paths["cached-probe-test"] = "/usr/bin/cached-probe-test"
	fake.outputs["/usr/bin/cached-probe-test"] = "1.2.3\n"

	first := cachedInterpreters(fake.probe(), spec)
	second := cachedInterpreters(fake.probe(), spec)
	assert.Eq(t, first, second)
	assert.Eq(t, 2, len(first))
	// The second call is served from the cache: no new probe.
	assert.Eq(t, 2, fake.probes)
}

func TestInterpreterHint(t *testing.T) {
	t.Run("lists interpreters and the advice", func(t *testing.T) {
		list := []Interpreter{
			{Name: "python", Path: `C:\Python312\python.exe`, Version: "3.12.4"},
			{Name: "node", Path: `C:\Program Files\nodejs\node.exe`, Version: "22.6.0"},
		}
		hint := InterpreterHint(list)
		assert.StrContains(t, hint, "可用解释器：python 3.12.4（python）、node 22.6.0（node）。")
		assert.StrContains(t, hint, "先用 file_write 把脚本写到工作区 tmp/ 下再用解释器执行")
		assert.Eq(t, false, strings.Contains(hint, "\n"))
	})
	t.Run("empty without interpreters", func(t *testing.T) {
		assert.Eq(t, "", InterpreterHint(nil))
	})
}

func TestIsStoreAliasPath(t *testing.T) {
	t.Run("alias directory", func(t *testing.T) {
		assert.True(t, isStoreAliasPath(`C:\Users\me\AppData\Local\Microsoft\WindowsApps\python.exe`))
		assert.True(t, isStoreAliasPath(`C:\Users\me\AppData\Local\Microsoft\WindowsApps\python3.exe`))
		assert.True(t, isStoreAliasPath(`/mnt/c/Users/me/AppData/Local/Microsoft/WindowsApps/python.exe`))
	})
	t.Run("real installs", func(t *testing.T) {
		assert.False(t, isStoreAliasPath(`C:\Program Files\WindowsApps\PythonSoftwareFoundation.Python.3.12_qbz5\python.exe`))
		assert.False(t, isStoreAliasPath(`C:\Python312\python.exe`))
		assert.False(t, isStoreAliasPath(`C:\Microsoft\python.exe`))
		assert.False(t, isStoreAliasPath("/usr/bin/python3"))
	})
}
