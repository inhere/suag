package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type MemoryProjectionSync interface {
	RefreshSessionProjection(ctx context.Context, sessionID string) error
}

// MemoryMarkdownProjector renders reviewed memory candidates into the local
// MEMORY.md / USER.md projection files consumed by the host runner.
type MemoryMarkdownProjector struct {
	store recapRepository
	root  string
	now   func() time.Time
}

func NewMemoryMarkdownProjector(repo Repository, root string, now func() time.Time) *MemoryMarkdownProjector {
	if now == nil {
		now = time.Now
	}
	return &MemoryMarkdownProjector{
		store: repo,
		root:  strings.TrimSpace(root),
		now:   now,
	}
}

func (w *MemoryMarkdownProjector) RefreshSessionProjection(ctx context.Context, sessionID string) error {
	if w == nil || w.store == nil || strings.TrimSpace(w.root) == "" || strings.TrimSpace(sessionID) == "" {
		return nil
	}

	candidates, err := w.store.ListMemoryCandidatesBySession(ctx, sessionID)
	if err != nil {
		return err
	}

	memoryDir := filepath.Join(w.root, "memory")
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		return err
	}

	now := w.now().UTC()
	if err := writeMemoryDraft(filepath.Join(memoryDir, "MEMORY.draft.md"), "MEMORY", candidates, now); err != nil {
		return err
	}
	if err := writeMemoryDraft(filepath.Join(memoryDir, "USER.draft.md"), "USER", candidates, now); err != nil {
		return err
	}
	if err := writeMemoryFormal(filepath.Join(memoryDir, "MEMORY.md"), "MEMORY", candidates, now); err != nil {
		return err
	}
	if err := writeMemoryFormal(filepath.Join(memoryDir, "USER.md"), "USER", candidates, now); err != nil {
		return err
	}
	return nil
}

type MemoryProjectionHook struct {
	writer *MemoryMarkdownProjector
}

// NewMemoryProjectionHook returns a recap hook that refreshes markdown memory
// projections and marks freshly rendered draft candidates as rendered.
func NewMemoryProjectionHook(repo Repository, root string, now func() time.Time) *MemoryProjectionHook {
	return &MemoryProjectionHook{
		writer: NewMemoryMarkdownProjector(repo, root, now),
	}
}

func (h *MemoryProjectionHook) OnTurnRecap(ctx context.Context, input RecapInput) error {
	if h == nil || h.writer == nil {
		return nil
	}

	candidates, err := h.writer.store.ListMemoryCandidatesBySession(ctx, input.Session.SessionID)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return nil
	}
	if err := h.writer.RefreshSessionProjection(ctx, input.Session.SessionID); err != nil {
		return err
	}

	for _, candidate := range candidates {
		if candidate.SessionID != input.Session.SessionID {
			continue
		}
		if candidate.Status != MemoryCandidateDraft {
			continue
		}
		if err := h.writer.store.UpdateMemoryCandidateStatus(ctx, candidate.CandidateID, MemoryCandidateRendered); err != nil {
			return err
		}
	}
	return h.writer.RefreshSessionProjection(ctx, input.Session.SessionID)
}

func writeMemoryDraft(path string, target string, candidates []MemoryCandidate, now time.Time) error {
	lines := []string{
		"# " + target + ".draft",
		"",
		"Updated: " + now.Format(time.RFC3339),
		"",
	}

	count := appendMemoryLines(&lines, target, candidates, func(candidate MemoryCandidate) bool {
		return candidate.Status == MemoryCandidateDraft || candidate.Status == MemoryCandidateRendered
	})
	if count == 0 {
		lines = append(lines, "_No draft candidates yet._")
	}
	lines = append(lines, "")

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}

func writeMemoryFormal(path string, target string, candidates []MemoryCandidate, now time.Time) error {
	lines := []string{
		"# " + target,
		"",
		"Updated: " + now.Format(time.RFC3339),
		"",
	}

	count := appendMemoryLines(&lines, target, candidates, func(candidate MemoryCandidate) bool {
		return candidate.Status == MemoryCandidateApplied
	})
	if count == 0 {
		lines = append(lines, "_No applied memory entries yet._")
	}
	lines = append(lines, "")

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}

func appendMemoryLines(lines *[]string, target string, candidates []MemoryCandidate, include func(candidate MemoryCandidate) bool) int {
	seen := make(map[string]struct{}, len(candidates))
	count := 0
	for _, candidate := range candidates {
		if candidate.Target != target {
			continue
		}
		if include != nil && !include(candidate) {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(candidate.Kind) + "\x00" + strings.TrimSpace(candidate.Content))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		*lines = append(*lines, "- ["+candidate.Kind+"] "+candidate.Content)
		count++
	}
	return count
}
