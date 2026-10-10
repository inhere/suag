package native

import (
	"time"

	"github.com/inhere/suag/internal/platform/tools"
)

// Env carries the settings native tools are built with. Zero values fall back
// to each tool's defaults.
type Env struct {
	// Workspace confines file tools and is the shell working directory.
	Workspace string
	// MaxOutputBytes caps one tool result (tools.max_output_bytes).
	MaxOutputBytes int
	// SerpAPIKey enables web_search; empty leaves it unavailable.
	SerpAPIKey string
	// Shell overrides the shell_exec shell (tools.shell); empty = OS default.
	Shell string
	// ShellTimeoutMax caps a shell_exec timeout (tools.shell_timeout_max).
	ShellTimeoutMax time.Duration
	// FetchAllowPrivate lets web_fetch reach private and local addresses
	// (tools.web_fetch.allow_private).
	FetchAllowPrivate bool
	// FetchProxy routes web_fetch through a proxy (tools.web_fetch.proxy);
	// wiring validates it at startup with ParseFetchProxy.
	FetchProxy string
}

// Spec is one entry of the native tool catalog.
type Spec struct {
	Name string
	// Write marks tools that change files or run commands; they are only
	// registered when writing is allowed.
	Write bool
	// New builds the tool, or returns nil when env lacks what it needs
	// (web_search without a key).
	New func(env Env) tools.Tool
}

// Catalog lists every native tool with its read/write flag. run, chat and ACP
// all register from this one list; update_plan and tool_search are wired
// separately because they depend on session state, not on Env.
func Catalog() []Spec {
	return []Spec{
		{Name: "file_read", New: func(env Env) tools.Tool {
			return &FileReadTool{workspace: env.Workspace, maxOut: env.MaxOutputBytes}
		}},
		{Name: "file_list", New: func(env Env) tools.Tool {
			return &FileListTool{workspace: env.Workspace, maxOut: env.MaxOutputBytes}
		}},
		{Name: "file_grep", New: func(env Env) tools.Tool {
			return &FileGrepTool{workspace: env.Workspace, maxOut: env.MaxOutputBytes}
		}},
		{Name: "file_find", New: func(env Env) tools.Tool {
			return &FileFindTool{workspace: env.Workspace, maxOut: env.MaxOutputBytes}
		}},
		{Name: "web_fetch", New: func(env Env) tools.Tool {
			t, err := NewWebFetchTool(env.FetchAllowPrivate, env.FetchProxy)
			if err != nil {
				return nil // invalid proxy: rejected earlier by wiring
			}
			t.maxOut = env.MaxOutputBytes
			return t
		}},
		{Name: "file_write", Write: true, New: func(env Env) tools.Tool {
			return &FileWriteTool{workspace: env.Workspace}
		}},
		{Name: "file_edit", Write: true, New: func(env Env) tools.Tool {
			return &FileEditTool{workspace: env.Workspace}
		}},
		{Name: "shell_exec", Write: true, New: func(env Env) tools.Tool {
			t := NewShellExecTool(env.Workspace, 0)
			t.shell = resolveShell(env.Shell)
			t.maxOut = env.MaxOutputBytes
			if env.ShellTimeoutMax > 0 {
				t.maxTimeout = env.ShellTimeoutMax
				t.timeout = min(t.timeout, t.maxTimeout)
			}
			return t
		}},
		{Name: "web_search", New: func(env Env) tools.Tool {
			if env.SerpAPIKey == "" {
				return nil
			}
			t := NewWebSearchTool(env.SerpAPIKey)
			t.maxOut = env.MaxOutputBytes
			return t
		}},
	}
}

// BuildTools returns the catalog tools available in env, in catalog order.
// Write tools are skipped unless allowWrite is set.
func BuildTools(env Env, allowWrite bool) []tools.Tool {
	var out []tools.Tool
	for _, spec := range Catalog() {
		if spec.Write && !allowWrite {
			continue
		}
		if t := spec.New(env); t != nil {
			out = append(out, t)
		}
	}
	return out
}
