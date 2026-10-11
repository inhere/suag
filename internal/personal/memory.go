package personal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"
	"time"

	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/memory"
)

// metaMemoryIndexContent is the session meta key of the frozen memory index
// section (its hash is metaMemoryIndexHash).
const metaMemoryIndexContent = "memory_index_content"

// memoryIndexNote is the fixed explanation put above the index entries.
const memoryIndexNote = "## 记忆索引\n\n" +
	"以下是跨会话记忆的索引快照：会话开始时冻结，只列标题，本会话内的写入不会更新它（压缩后才刷新）。" +
	"需要正文时用 memory 工具 action=get（id 为行首标识，scope 为 project 或 user）；" +
	"新增、改写、删除用 add / replace / remove。" +
	"条目四类：user（用户本人）、feedback（对工作方式的纠正与确认）、project（代码和 git 看不出的目标、约束、进展）、" +
	"reference（外部资源的位置）。不要记：能从代码或 git 查到的、规则文件里已有的、本次会话的临时信息、密钥。"

// memoryIndexEmpty is shown when no scope has entries yet.
const memoryIndexEmpty = "（暂无记忆条目）"

// renderMemorySection wraps the rendered scope indexes into the prompt section.
func renderMemorySection(index string) string {
	index = strings.TrimSpace(index)
	if index == "" {
		index = memoryIndexEmpty
	}
	return memoryIndexNote + "\n\n" + index
}

// memorySnapshot reads the memory index through Config.MemoryIndex and
// renders the prompt section with its sha256. ok is false when there is no
// memory source or nothing could be read; a failure is only logged and never
// affects the turn (the prompt then has no memory section).
func (r *Runner) memorySnapshot(ctx context.Context, sessionID string) (content, hash string, ok bool) {
	if r.cfg.MemoryIndex == nil {
		return "", "", false
	}
	index, err := r.cfg.MemoryIndex(ctx)
	if err != nil {
		slog.Warn("read memory index", "session_id", sessionID, "err", err)
		if strings.TrimSpace(index) == "" {
			return "", "", false
		}
		// Partial index (one scope failed): keep what could be read.
	}
	content = renderMemorySection(index)
	sum := sha256.Sum256([]byte(content))
	return content, hex.EncodeToString(sum[:]), true
}

// memorySnapshotPatch fills patch with a snapshot for a session that has
// none yet (sessions created before memory existed, or whose first snapshot
// failed). Sessions with a snapshot keep it: writes during the session never
// change the prompt (design D6).
func (r *Runner) memorySnapshotPatch(ctx context.Context, model Session, patch map[string]any) {
	if _, has := model.Meta[metaMemoryIndexHash]; has {
		return
	}
	if content, hash, ok := r.memorySnapshot(ctx, model.ID); ok {
		patch[metaMemoryIndexContent], patch[metaMemoryIndexHash] = content, hash
	}
}

// storeMemorySnapshot stores a memory index snapshot taken at a segment break
// (compaction; the prompt cache is rebuilt anyway) in the session meta and
// updates meta (the in-memory metadata of the running prompt builder; may be
// nil). It reports whether the section changed. A failure keeps the old
// snapshot.
func (r *Runner) storeMemorySnapshot(ctx context.Context, sessionID string, meta map[string]any, content, hash string) bool {
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := r.cfg.Repository.PatchSessionMeta(wctx, sessionID, map[string]any{
		metaMemoryIndexContent: content, metaMemoryIndexHash: hash,
	}); err != nil {
		slog.Warn("store memory index snapshot", "session_id", sessionID, "err", err)
		return false
	}
	if meta == nil {
		return false
	}
	changed := metaString(meta, metaMemoryIndexHash) != hash
	meta[metaMemoryIndexContent], meta[metaMemoryIndexHash] = content, hash
	return changed
}

// memoryPromptHook makes the model call right after a compaction use the
// refreshed memory index: agentcore builds the system prompt of a step before
// the context (where compaction happens), so the step's request is rebuilt
// here once when the context builder marked the prompt stale.
type memoryPromptHook struct {
	agentcore.NopHook
	b *contextBuilder
}

func (h memoryPromptHook) BeforeModel(ctx context.Context, event *agentcore.ModelEvent, _ *agentcore.Control) error {
	b := h.b
	if b == nil || !b.promptStale || event == nil || event.Request == nil || b.prompt == nil {
		return nil
	}
	b.promptStale = false
	system, err := b.prompt.BuildSystemPrompt(ctx, b.lastState)
	if err != nil {
		slog.Warn("rebuild system prompt after compaction", "session_id", b.sessionID, "err", err)
		return nil
	}
	event.Request.System = system
	return nil
}

// SessionIDFromContext returns the session of the turn running in ctx ("" outside a turn).
func SessionIDFromContext(ctx context.Context) string { return observeScopeFrom(ctx).sessionID }

// MemoryProvenance is the provenance of a memory write made by a tool in
// the turn running in ctx.
func MemoryProvenance(ctx context.Context) memory.Provenance {
	scope := observeScopeFrom(ctx)
	return memory.Provenance{SessionID: scope.sessionID, TurnID: scope.turnID, Source: "tool"}
}

// SessionTainted reports whether the session of the turn in ctx has seen
// external content (tainted_at in its meta, design D7). Outside a turn or
// when the session cannot be read it reports true (fail closed: the write
// then needs a confirmation).
func (r *Runner) SessionTainted(ctx context.Context) bool {
	sessionID := observeScopeFrom(ctx).sessionID
	if sessionID == "" {
		return true
	}
	if m := r.rec.taint; m != nil {
		if _, done := m.marked.Load(sessionID); done {
			return true
		}
	}
	model, err := r.cfg.Repository.GetSession(context.WithoutCancel(ctx), sessionID)
	if err != nil {
		slog.Warn("read session taint", "session_id", sessionID, "err", err)
		return true
	}
	_, tainted := model.Meta[metaTaintedAt]
	return tainted
}

// RecordMemoryEvent writes a memory_write / memory_denied event for the turn
// in ctx (memory.Policy.Observe); best-effort like every event.
func (r *Runner) RecordMemoryEvent(ctx context.Context, ev memory.Event) {
	status := eventOK
	if ev.Type == memory.EventDenied {
		status = eventDenied
	}
	attrs := map[string]any{"scope": ev.Scope.String(), "backend": ev.Backend, "action": string(ev.Action),
		"tainted": ev.Tainted}
	for key, value := range map[string]string{"id": ev.ID, "kind": string(ev.Kind), "source": ev.Source, "reason": ev.Reason} {
		if value != "" {
			attrs[key] = value
		}
	}
	r.rec.record(ctx, store.Event{Kind: ev.Type, Name: string(ev.Action), Status: status, Error: ev.Detail,
		StartedAt: time.Now(), Attrs: attrs})
}
