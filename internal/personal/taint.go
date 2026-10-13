package personal

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/tools"
)

// Session meta keys of the taint flag (D7): written once, never cleared.
const (
	metaTaintedAt = "tainted_at"
	metaTaintedBy = "tainted_by"
)

// sessionMetaOnce is the slice of *store.Store the taint marker needs.
type sessionMetaOnce interface {
	PatchSessionMetaIfAbsent(ctx context.Context, id, guard string, patch map[string]any) (bool, error)
}

// taintMarker flags a session as tainted the first time external content
// enters its context (design "污染置位"). The flag is per session and sticky:
// compaction summaries may still carry the external text.
type taintMarker struct {
	meta sessionMetaOnce
	// always lists tools that taint on every result (memory.taint_tools).
	always map[string]bool
	// onSpill lists tools that taint when their output was externalised
	// (memory.taint_on_spill): a huge shell output is most likely a dump
	// of something fetched, a small one usually is not.
	onSpill map[string]bool
	// marked caches the sessions known to be tainted so later external
	// results skip the store; the flag is never cleared, so it cannot go
	// stale.
	marked sync.Map
}

func newTaintMarker(meta sessionMetaOnce, always, onSpill []string) *taintMarker {
	set := func(names []string) map[string]bool {
		m := make(map[string]bool, len(names))
		for _, n := range names {
			m[n] = true
		}
		return m
	}
	return &taintMarker{meta: meta, always: set(always), onSpill: set(onSpill)}
}

// taints reports whether a result of tool (looked up by name; nil when the
// lookup failed) brings external content into the context: the tool declares
// External (all MCP tools do), the result declares metadata external=true
// (per-call sources such as a memory backend), the tool is configured in
// memory.taint_tools, or its output was externalised and the tool is in
// memory.taint_on_spill.
func (m *taintMarker) taints(tool agentcore.Tool, name string, res *agentcore.ToolResult) bool {
	if tools.MetaOf(tool).External || m.always[name] {
		return true
	}
	if ext, _ := res.Metadata["external"].(bool); ext {
		return true
	}
	if m.onSpill[name] {
		if info, ok := tools.OutputInfoOf(res.Metadata); ok && info.Ref != "" {
			return true
		}
	}
	return false
}

// observe sets tainted_at/tainted_by once for the turn's session when res
// taints it. Error results count too: an error from an external source may
// carry injected text just the same. A failed write is only logged.
func (m *taintMarker) observe(ctx context.Context, tool agentcore.Tool, name string, res *agentcore.ToolResult) {
	if m == nil || m.meta == nil || res == nil {
		return
	}
	sessionID := observeScopeFrom(ctx).sessionID
	if sessionID == "" {
		return
	}
	if _, done := m.marked.Load(sessionID); done || !m.taints(tool, name, res) {
		return
	}
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	// Write-once in one transaction: a concurrent call cannot overwrite
	// tainted_by with a later tool.
	if _, err := m.meta.PatchSessionMetaIfAbsent(wctx, sessionID, metaTaintedAt, map[string]any{
		metaTaintedAt: time.Now().UTC().Format(time.RFC3339),
		metaTaintedBy: name,
	}); err != nil {
		slog.Warn("mark session tainted", "session_id", sessionID, "tool", name, "err", err)
		return
	}
	m.marked.Store(sessionID, struct{}{})
}
