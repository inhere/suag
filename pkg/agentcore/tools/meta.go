package tools

import "github.com/inhere/suag/pkg/agentcore/api"

// Category is a protocol-neutral class of what a tool does. Hosts map it onto
// their own vocabulary (e.g. ACP tool kinds) and approval prompts.
type Category string

const (
	CategoryRead    Category = "read"
	CategoryEdit    Category = "edit"
	CategoryExecute Category = "execute"
	CategorySearch  Category = "search"
	CategoryFetch   Category = "fetch"
	CategoryThink   Category = "think"
	// CategoryOther is the default for tools that declare nothing.
	CategoryOther Category = "other"
)

// Meta describes a tool for permission decisions.
type Meta struct {
	Category Category
	// ReadOnly marks tools without side effects outside the session (reading
	// files, searching, fetching, updating the plan): approval layers may run
	// them without asking, and read-only tool sets keep only them.
	ReadOnly bool
}

// Describer is the optional interface a tool implements to declare its Meta.
type Describer interface {
	ToolMeta() Meta
}

// MetaOf returns t's declared Meta. A tool without Describer, a nil tool and
// every MCP tool get {CategoryOther, false}: MCP tools are never trusted as
// read-only by name or declaration.
func MetaOf(t api.Tool) Meta {
	if t == nil || IsMCP(t) {
		return Meta{Category: CategoryOther}
	}
	if d, ok := t.(Describer); ok {
		meta := d.ToolMeta()
		if meta.Category == "" {
			meta.Category = CategoryOther
		}
		return meta
	}
	return Meta{Category: CategoryOther}
}

// IsMCP reports whether t is backed by an MCP server.
func IsMCP(t api.Tool) bool { return t != nil && KindOf(t) == KindMCP }

// MCPServerOf returns the configured server name of an MCP tool, or "".
func MCPServerOf(t api.Tool) string {
	if named, ok := t.(MCPServerNamer); ok {
		return named.MCPServer()
	}
	return ""
}
