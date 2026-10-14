package native

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Interpreter is one script interpreter found on PATH and confirmed to work:
// Name is the command that was looked up (or the name the configuration gave),
// Path the resolved binary and Version the version string it printed for
// --version.
type Interpreter struct {
	Name    string
	Path    string
	Version string
}

// interpreterProbeTimeout bounds one "<path> --version" probe: an interpreter
// that does not answer quickly is treated as unusable, and the whole detection
// must not delay the first turn.
const interpreterProbeTimeout = 2 * time.Second

// InterpreterSetting is a parsed tools.interpreters value (see
// ParseInterpreterSetting).
type InterpreterSetting struct {
	// Enabled is false when detection is switched off: nothing is probed and no
	// hint is written, so the model stays with the system shell.
	Enabled bool
	// Auto probes the built-in candidate list; when false, Names holds the
	// commands and paths the configuration named.
	Auto  bool
	Names []string
}

// key identifies the setting in the probe cache: settings with the same key
// probe the same candidates.
func (s InterpreterSetting) key() string {
	switch {
	case !s.Enabled:
		return "off"
	case s.Auto:
		return "auto"
	default:
		return "list:" + strings.Join(s.Names, "\x00")
	}
}

// ParseInterpreterSetting reads a tools.interpreters config value:
//   - unset, true or "auto": probe the built-in candidates (default);
//   - false or "off": no detection;
//   - a command name, an absolute path, or a list of either: probe exactly
//     those (an absolute path is taken as the user's own choice, so the Windows
//     store-alias check does not apply to it).
//
// An empty list or an unusable value (a number, a map) keeps the default.
func ParseInterpreterSetting(raw any) InterpreterSetting {
	switch v := raw.(type) {
	case nil:
		return InterpreterSetting{Enabled: true, Auto: true}
	case bool:
		return InterpreterSetting{Enabled: v, Auto: v}
	case string:
		return interpreterSettingFromList([]string{v})
	case []string:
		return interpreterSettingFromList(v)
	case []any:
		names := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				names = append(names, s)
			}
		}
		return interpreterSettingFromList(names)
	default:
		return InterpreterSetting{Enabled: true, Auto: true}
	}
}

// interpreterSettingFromList turns configured entries into a setting. Entries
// are kept in order; "off"/"false" anywhere switches detection off.
func interpreterSettingFromList(entries []string) InterpreterSetting {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := strings.TrimSpace(entry)
		if name == "" {
			continue
		}
		switch strings.ToLower(name) {
		case "off", "false", "none":
			return InterpreterSetting{Enabled: false}
		case "auto", "true", "on":
			return InterpreterSetting{Enabled: true, Auto: true}
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return InterpreterSetting{Enabled: true, Auto: true}
	}
	return InterpreterSetting{Enabled: true, Names: names}
}

// defaultInterpreterNames are the candidates auto-detection probes, in order:
// python3 before python (python may be an older interpreter or the same
// binary), then node. bash is not probed: on Unix the shell already is POSIX sh,
// and on Windows the bash on PATH may be WSL's (see resolveShell) — name it in
// tools.interpreters to advertise it anyway.
func defaultInterpreterNames() []string {
	return []string{"python3", "python", "node"}
}

// interpreterProbe is the environment detection runs in: a PATH lookup and a
// version runner, injectable so tests need no real interpreter.
type interpreterProbe struct {
	// lookup resolves a bare command name to its path (exec.LookPath).
	lookup func(name string) (string, error)
	// version returns the output of "<path> --version" (stdout and stderr) and
	// whether the command succeeded.
	version func(ctx context.Context, path string) ([]byte, error)
	// windows switches on the Windows-only rules (drive paths, store aliases).
	windows bool
}

// realProbe runs against the host PATH.
func realProbe() interpreterProbe {
	return interpreterProbe{
		lookup:  exec.LookPath,
		version: runInterpreterVersion,
		windows: runtime.GOOS == "windows",
	}
}

// runInterpreterVersion runs "<path> --version" with a short timeout. stdout
// and stderr share one buffer: python prints the version to stdout, other tools
// to stderr.
func runInterpreterVersion(ctx context.Context, path string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, interpreterProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.Bytes(), err
}

// interpreterEntry memoises one setting's probe result.
type interpreterEntry struct {
	once sync.Once
	list []Interpreter
}

var (
	interpreterMu    sync.Mutex
	interpreterCache = map[string]*interpreterEntry{}
)

// DetectInterpreters returns the interpreters to advertise for a
// tools.interpreters value (see ParseInterpreterSetting). Every candidate is
// probed at most once per process: results are cached per setting, so a turn
// never pays for the probe again.
func DetectInterpreters(setting any) []Interpreter {
	return cachedInterpreters(realProbe(), ParseInterpreterSetting(setting))
}

// cachedInterpreters is DetectInterpreters over the process-wide cache. The key
// is the setting, so a second process or a changed configuration probes again.
func cachedInterpreters(p interpreterProbe, spec InterpreterSetting) []Interpreter {
	key := spec.key()
	interpreterMu.Lock()
	entry, ok := interpreterCache[key]
	if !ok {
		entry = &interpreterEntry{}
		interpreterCache[key] = entry
	}
	interpreterMu.Unlock()
	// The probe runs outside the lock (it starts processes); concurrent callers
	// for the same setting wait here instead of probing twice.
	entry.once.Do(func() { entry.list = detectInterpreters(context.Background(), p, spec) })
	return entry.list
}

// detectInterpreters probes the candidates of spec in order and returns those
// that answered --version with a version. Candidates are deduplicated by
// resolved path, because python3 and python are often the same binary.
func detectInterpreters(ctx context.Context, p interpreterProbe, spec InterpreterSetting) []Interpreter {
	if !spec.Enabled {
		return nil
	}
	names := spec.Names
	if spec.Auto {
		names = defaultInterpreterNames()
	}
	var (
		out  []Interpreter
		seen = map[string]bool{}
	)
	for _, name := range names {
		path, explicit := name, isAbsInterpreterPath(name)
		if !explicit {
			found, err := p.lookup(name)
			if err != nil {
				continue
			}
			path = found
		}
		// A store alias found on PATH (%LOCALAPPDATA%\Microsoft\WindowsApps) is
		// the app-execution stub of the Microsoft Store: it either opens the
		// Store or prints an install hint instead of running an interpreter.
		// An absolute path from the configuration is the user's own choice and
		// is judged by --version alone.
		if !explicit && p.windows && isStoreAliasPath(path) {
			continue
		}
		key := resolveInterpreterPath(path)
		if p.windows {
			key = strings.ToLower(key)
		}
		if seen[key] {
			continue
		}
		output, err := p.version(ctx, path)
		version := interpreterVersion(string(output))
		if err != nil || version == "" {
			continue
		}
		seen[key] = true
		out = append(out, Interpreter{Name: name, Path: path, Version: version})
	}
	return out
}

// isAbsInterpreterPath reports an absolute path on any OS: detection may run
// with a configuration written on another platform (a Windows drive path in a
// shared config file), and the store-alias rule keys off it.
func isAbsInterpreterPath(p string) bool {
	if filepath.IsAbs(p) {
		return true
	}
	return len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/')
}

// isStoreAliasPath reports whether path is an entry of a Windows app-execution
// alias directory, i.e. a "WindowsApps" directory directly under "Microsoft"
// (%LOCALAPPDATA%\Microsoft\WindowsApps). The Microsoft Store package itself
// lives in "C:\Program Files\WindowsApps\<package>", which is a real install
// and stays usable.
func isStoreAliasPath(path string) bool {
	segments := strings.FieldsFunc(path, func(r rune) bool { return r == '\\' || r == '/' })
	for i := 1; i < len(segments); i++ {
		if strings.EqualFold(segments[i], "WindowsApps") && strings.EqualFold(segments[i-1], "Microsoft") {
			return true
		}
	}
	return false
}

// resolveInterpreterPath is the path two candidates are compared by: symlinks
// followed and made absolute (best effort: the original path when that fails),
// so a symlinked python3 and python count as one.
func resolveInterpreterPath(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return path
}

// versionPattern matches the version in an interpreter's --version output:
// "Python 3.12.4", "v22.6.0", "GNU bash, version 5.2.15(1)-release".
var versionPattern = regexp.MustCompile(`\d+(?:\.\d+)+`)

// interpreterVersion returns the first version-looking token of a --version
// output, or "" when it holds none (an interpreter that printed nothing, or
// only a help text, is not usable).
func interpreterVersion(output string) string {
	return versionPattern.FindString(output)
}

// InterpreterHint is the environment-section line that advertises the detected
// interpreters, e.g.
//
//	可用解释器：python 3.12.4（python）、node 22.6.0（node）。…
//
// and asks the model to write a script file instead of quoting complex text on
// one shell line. It is "" when no interpreter was found: the model then stays
// with the system shell.
//
// The text is generated per segment (the environment section travels in the
// segment context, not the stable system prompt), so a change here cannot
// invalidate the provider prefix cache.
func InterpreterHint(list []Interpreter) string {
	if len(list) == 0 {
		return ""
	}
	parts := make([]string, 0, len(list))
	for _, it := range list {
		parts = append(parts, fmt.Sprintf("%s %s（%s）", it.Name, it.Version, interpreterFileName(it.Path)))
	}
	return "可用解释器：" + strings.Join(parts, "、") + "。" + WriteScriptAdvice
}

// WriteScriptAdvice asks for a script file instead of a heavily quoted shell
// line. It is appended to the environment section and to shell_exec's
// description (PowerShell) whenever an interpreter is available.
const WriteScriptAdvice = "多行文本处理、复杂引号、JSON/正则处理时，先用 file_write 把脚本写到工作区 tmp/ 下再用解释器执行，" +
	"不要在 shell 一行里拼复杂引号。"

// PowerShellQuoteAdvice is appended to shell_exec's description when the shell
// is PowerShell: there a bare , = . @ inside an argument is syntax (a comma
// builds an array, an unquoted space splits it), so such an argument must be
// quoted as a whole (suga-cfeq: "-Dincludes=a,b" was split into an array).
const PowerShellQuoteAdvice = "参数含 , = . @ 时整体加引号，例如 \"-Dtest=A,B\"、\"-Dincludes=g:a\"。"

// interpreterFileName is the file name of a resolved interpreter path without
// its .exe suffix, for display. Both separators count: a configured Windows
// path keeps its backslashes, which filepath.Base on other systems would not
// split.
func interpreterFileName(path string) string {
	base := path
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	if strings.EqualFold(filepath.Ext(base), ".exe") {
		base = base[:len(base)-len(".exe")]
	}
	return base
}
