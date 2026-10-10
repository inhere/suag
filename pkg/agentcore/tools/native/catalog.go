package native

import (
	"time"

	"github.com/inhere/suag/pkg/agentcore/api"
	"github.com/inhere/suag/pkg/agentcore/tools"
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

// Spec is one entry of the native tool catalog. Whether a tool writes is its
// own tools.Meta (ToolMeta), not a catalog flag.
type Spec struct {
	Name string
	// New builds the tool, or returns nil when env lacks what it needs
	// (web_search without a key).
	New func(env Env) api.Tool
}

// Catalog lists every native tool. run, chat and ACP
// all register from this one list; update_plan and tool_search are wired
// separately because they depend on session state, not on Env.
func Catalog() []Spec {
	return []Spec{
		{Name: "file_read", New: func(env Env) api.Tool {
			return &FileReadTool{workspace: env.Workspace, maxOut: env.MaxOutputBytes}
		}},
		{Name: "file_list", New: func(env Env) api.Tool {
			return &FileListTool{workspace: env.Workspace, maxOut: env.MaxOutputBytes}
		}},
		{Name: "file_grep", New: func(env Env) api.Tool {
			return &FileGrepTool{workspace: env.Workspace, maxOut: env.MaxOutputBytes}
		}},
		{Name: "file_find", New: func(env Env) api.Tool {
			return &FileFindTool{workspace: env.Workspace, maxOut: env.MaxOutputBytes}
		}},
		{Name: "web_fetch", New: func(env Env) api.Tool {
			t, err := NewWebFetchTool(env.FetchAllowPrivate, env.FetchProxy)
			if err != nil {
				return nil // invalid proxy: rejected earlier by wiring
			}
			t.maxOut = env.MaxOutputBytes
			return t
		}},
		{Name: "file_write", New: func(env Env) api.Tool {
			return &FileWriteTool{workspace: env.Workspace}
		}},
		{Name: "file_edit", New: func(env Env) api.Tool {
			return &FileEditTool{workspace: env.Workspace}
		}},
		{Name: "shell_exec", New: func(env Env) api.Tool {
			t := NewShellExecTool(env.Workspace, 0)
			t.shell = resolveShell(env.Shell)
			t.maxOut = env.MaxOutputBytes
			if env.ShellTimeoutMax > 0 {
				t.maxTimeout = env.ShellTimeoutMax
				t.timeout = min(t.timeout, t.maxTimeout)
			}
			return t
		}},
		{Name: "web_search", New: func(env Env) api.Tool {
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
// Tools that are not read-only (tools.MetaOf) are skipped unless allowWrite
// is set.
func BuildTools(env Env, allowWrite bool) []api.Tool {
	var out []api.Tool
	for _, spec := range Catalog() {
		t := spec.New(env)
		if t == nil || (!allowWrite && !tools.MetaOf(t).ReadOnly) {
			continue
		}
		out = append(out, t)
	}
	return out
}
