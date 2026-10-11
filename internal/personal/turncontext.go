package personal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/inhere/suag/internal/personal/store"
)

// Run modes recorded in turn_context (Config.Mode).
const (
	ModeReadOnly   = "read_only"
	ModeAllowWrite = "allow_write"
)

// metaMemoryIndexHash is the session meta key of the frozen memory index
// snapshot hash (see memory.go; "" without a memory source).
const metaMemoryIndexHash = "memory_index_hash"

// toolSetHashLen is how many hex digits of the tool set hash are kept.
const toolSetHashLen = 12

// recordTurnContext writes the turn_context event (D9): enough to answer
// "what did the model see when this turn started" without storing the prompt
// itself. model is the session as of the turn start (patched metadata).
func (r *Runner) recordTurnContext(ctx context.Context, model Session) {
	var names []string
	if r.cfg.Tools != nil {
		schemas, err := r.cfg.Tools.ListTools(ctx)
		if err != nil {
			slog.Warn("list tools for turn_context", "session_id", model.ID, "err", err)
		}
		for _, s := range schemas {
			names = append(names, s.Name)
		}
	}
	mode := r.cfg.Mode
	if mode == "" {
		mode = "unknown"
	}
	_, tainted := model.Meta[metaTaintedAt]
	r.rec.record(ctx, store.Event{
		Kind: store.EventTurnContext, Name: store.EventTurnContext, Status: eventOK, StartedAt: time.Now(),
		Attrs: map[string]any{
			"model":       r.cfg.Model,
			"provider":    r.cfg.ProviderName,
			"mode":        mode,
			"tools":       toolSetHash(names),
			"tools_count": len(names),
			"rules_hash":  metaString(model.Meta, "personal_rules_hash"),
			// Always present (empty when no memory snapshot is recorded).
			"memory_hash": metaString(model.Meta, metaMemoryIndexHash),
			"tainted":     tainted,
		},
	})
}

// toolSetHash is the first toolSetHashLen hex digits of sha256 over the
// sorted tool names (one per line), so the hash ignores listing order.
func toolSetHash(names []string) string {
	sorted := slices.Clone(names)
	slices.Sort(sorted)
	sum := sha256.Sum256([]byte(strings.Join(sorted, "\n")))
	return hex.EncodeToString(sum[:])[:toolSetHashLen]
}

func metaString(meta map[string]any, key string) string {
	s, _ := meta[key].(string)
	return s
}
