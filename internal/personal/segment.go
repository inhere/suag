package personal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/llm"
)

// Segment start reasons (compactionRecord.Reason). A record without a reason
// was written by an older binary and is an ordinary compaction. "idle" (chat
// idle consolidation) and "handoff" (--from) join with T10/T7.
const (
	segmentReasonStart     = "start"     // session start: snapshot only, no handoff package
	segmentReasonThreshold = "threshold" // automatic: the context crossed the threshold
	segmentReasonManual    = "manual"    // chat :compact
)

// metaSegmentContext marks the segment context message in Message.Metadata.
// The message is rendered by compose for every request: it is not a stored
// entry, not a ctxItem, never a cut candidate and never part of items().
const metaSegmentContext = llm.SegmentContextMetaKey

// segmentContext is the frozen context snapshot of one segment (design
// "段起始记录"): what the segment sees besides its history. It is stored in
// the segment start record; session meta only caches the same values.
//
// Environment and Skills stay empty until their sources are injected (T6);
// today the system prompt is still built from session meta, so the snapshot
// mirrors those meta values.
type segmentContext struct {
	Environment   string `json:"environment"`
	Rules         string `json:"rules"`
	RulesHash     string `json:"rules_hash,omitempty"`
	MemoryIndex   string `json:"memory_index"`
	MemoryHash    string `json:"memory_hash,omitempty"`
	Skills        string `json:"skills"`
	AppendPrompt  string `json:"append_prompt"`
	DeferredTools string `json:"deferred_tools"`
	// Hash is sha256 over all the fields above (see seal).
	Hash string `json:"hash"`
}

// seal computes Hash over the snapshot content (Hash itself excluded).
func (c *segmentContext) seal() {
	c.Hash = ""
	raw, _ := json.Marshal(c)
	sum := sha256.Sum256(raw)
	c.Hash = hex.EncodeToString(sum[:])
}

// segmentSnapshot builds the context snapshot from session metadata (rules,
// memory index, append prompt) and the current deferred tool listing. It is
// both how a new segment record is filled today and the compatibility view of
// a session that has no segment record with a context yet.
func (r *Runner) segmentSnapshot(meta map[string]any) *segmentContext {
	c := &segmentContext{
		Rules:         metaString(meta, "personal_rules_content"),
		RulesHash:     metaString(meta, "personal_rules_hash"),
		MemoryIndex:   metaString(meta, metaMemoryIndexContent),
		MemoryHash:    metaString(meta, metaMemoryIndexHash),
		AppendPrompt:  metaString(meta, "personal_append_prompt"),
		DeferredTools: r.deferredToolsPrompt(),
	}
	c.seal()
	return c
}

// currentSegment is the segment context in effect: the latest adopted segment
// record's snapshot, or - for sessions written before segment records (or
// whose latest record came from an older binary) - the meta snapshot.
func (r *Runner) currentSegment(view historyView, meta map[string]any) *segmentContext {
	if view.segment != nil {
		return view.segment
	}
	return r.segmentSnapshot(meta)
}

// writeStartSegment appends the reason=start record of a new session. It
// belongs to no turn, so it is adopted immediately; first_kept_seq 0 keeps
// every later message, which an older binary reads as "empty summary, keep
// everything" - the same view as before.
func (r *Runner) writeStartSegment(ctx context.Context, model Session) error {
	rec := compactionRecord{Reason: segmentReasonStart, Context: r.segmentSnapshot(model.Meta)}
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if _, err := r.cfg.Repository.AppendEntries(ctx, model.ID, store.NewEntry{Type: store.EntryCompaction, JSON: raw}); err != nil {
		return fmt.Errorf("write segment start record: %w", err)
	}
	return nil
}

// segmentContextMessage renders the segment context message of a view whose
// current segment carries the handoff package pkg ("" = none; no message).
// fromHandoff marks a --from handoff (segment 0), which keeps its own wording.
// For now the message only carries the handoff package; the snapshot moves in
// with T6.
func segmentContextMessage(pkg string, fromHandoff bool) (agentcore.Message, bool) {
	if pkg == "" {
		return agentcore.Message{}, false
	}
	content := pkg
	if !fromHandoff {
		content = summaryMessage(pkg).Content
	}
	return agentcore.Message{Role: "user", Content: content, Metadata: map[string]any{metaSegmentContext: true}}, true
}

// isSegmentContext reports whether m is a rendered segment context message.
func isSegmentContext(m agentcore.Message) bool {
	v, _ := m.Metadata[metaSegmentContext].(bool)
	return v
}
