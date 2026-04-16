package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMemoryDraftHook_WritesDraftFilesFromCandidates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	root := t.TempDir()
	now := time.Date(2026, 4, 16, 13, 0, 0, 0, time.UTC)

	recap := Recap{
		RecapID:   "recap_001",
		SessionID: "sess_001",
		TurnID:    "turn_001",
		RunID:     "run_001",
		Kind:      "turn_recap",
		Target:    "assistant",
		Content:   "Prefer concise status updates. User prefers concise responses.",
		CreatedAt: now,
		Metadata: map[string]any{
			"phase": "completed",
		},
	}
	if err := store.CreateRecap(ctx, recap); err != nil {
		t.Fatalf("CreateRecap() error = %v", err)
	}
	if err := store.CreateMemoryCandidate(ctx, MemoryCandidate{
		CandidateID:   "mcand_001",
		SessionID:     "sess_001",
		TurnID:        "turn_001",
		RunID:         "run_001",
		SourceRecapID: "recap_001",
		Target:        "MEMORY",
		Kind:          "turn_learning",
		Content:       "Prefer concise status updates.",
		Score:         0.8,
		Status:        MemoryCandidateDraft,
		CreatedAt:     now,
		Metadata:      map[string]any{"phase": "completed"},
	}); err != nil {
		t.Fatalf("CreateMemoryCandidate() MEMORY error = %v", err)
	}
	if err := store.CreateMemoryCandidate(ctx, MemoryCandidate{
		CandidateID:   "mcand_002",
		SessionID:     "sess_001",
		TurnID:        "turn_001",
		RunID:         "run_001",
		SourceRecapID: "recap_001",
		Target:        "USER",
		Kind:          "user_preference",
		Content:       "User prefers concise responses.",
		Score:         0.7,
		Status:        MemoryCandidateDraft,
		CreatedAt:     now,
		Metadata:      map[string]any{"phase": "completed"},
	}); err != nil {
		t.Fatalf("CreateMemoryCandidate() USER error = %v", err)
	}

	hook := NewMemoryDraftHook(store, root, func() time.Time { return now })
	if err := hook.OnTurnRecap(ctx, RecapInput{
		Session: Session{SessionID: "sess_001"},
		TurnID:  "turn_001",
		RunID:   "run_001",
		Phase:   RecapPhaseCompleted,
		Summary: recap.Content,
	}); err != nil {
		t.Fatalf("OnTurnRecap() error = %v", err)
	}

	memoryDraftPath := filepath.Join(root, "memory", "MEMORY.draft.md")
	userDraftPath := filepath.Join(root, "memory", "USER.draft.md")

	memoryDraft, err := os.ReadFile(memoryDraftPath)
	if err != nil {
		t.Fatalf("os.ReadFile(MEMORY.draft.md) error = %v", err)
	}
	if !strings.Contains(string(memoryDraft), "Prefer concise status updates.") {
		t.Fatalf("MEMORY.draft.md = %q, want memory candidate content", string(memoryDraft))
	}

	userDraft, err := os.ReadFile(userDraftPath)
	if err != nil {
		t.Fatalf("os.ReadFile(USER.draft.md) error = %v", err)
	}
	if !strings.Contains(string(userDraft), "User prefers concise responses.") {
		t.Fatalf("USER.draft.md = %q, want user candidate content", string(userDraft))
	}

	candidates, err := store.ListMemoryCandidatesByTurn(ctx, "sess_001", "turn_001")
	if err != nil {
		t.Fatalf("ListMemoryCandidatesByTurn() error = %v", err)
	}
	for _, candidate := range candidates {
		if candidate.Status != MemoryCandidateRendered {
			t.Fatalf("candidate %q status = %q, want %q", candidate.CandidateID, candidate.Status, MemoryCandidateRendered)
		}
	}
}

func TestMemoryDraftHook_AggregatesAndDeduplicatesDraftCandidates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	root := t.TempDir()
	now := time.Date(2026, 4, 16, 14, 0, 0, 0, time.UTC)

	candidates := []MemoryCandidate{
		{
			CandidateID:   "mcand_101",
			SessionID:     "sess_agg",
			TurnID:        "turn_001",
			RunID:         "run_001",
			SourceRecapID: "recap_001",
			Target:        "MEMORY",
			Kind:          "turn_learning",
			Content:       "Keep status updates concise.",
			Score:         0.8,
			Status:        MemoryCandidateDraft,
			CreatedAt:     now,
		},
		{
			CandidateID:   "mcand_102",
			SessionID:     "sess_agg",
			TurnID:        "turn_002",
			RunID:         "run_002",
			SourceRecapID: "recap_002",
			Target:        "MEMORY",
			Kind:          "turn_learning",
			Content:       "Keep status updates concise.",
			Score:         0.8,
			Status:        MemoryCandidateRendered,
			CreatedAt:     now.Add(time.Minute),
		},
		{
			CandidateID:   "mcand_103",
			SessionID:     "sess_agg",
			TurnID:        "turn_003",
			RunID:         "run_003",
			SourceRecapID: "recap_003",
			Target:        "USER",
			Kind:          "user_preference",
			Content:       "User prefers bullet summaries.",
			Score:         0.7,
			Status:        MemoryCandidateDraft,
			CreatedAt:     now.Add(2 * time.Minute),
		},
		{
			CandidateID:   "mcand_104",
			SessionID:     "sess_agg",
			TurnID:        "turn_004",
			RunID:         "run_004",
			SourceRecapID: "recap_004",
			Target:        "USER",
			Kind:          "user_preference",
			Content:       "User prefers bullet summaries.",
			Score:         0.7,
			Status:        MemoryCandidateRejected,
			CreatedAt:     now.Add(3 * time.Minute),
		},
	}
	for _, candidate := range candidates {
		if err := store.CreateMemoryCandidate(ctx, candidate); err != nil {
			t.Fatalf("CreateMemoryCandidate(%s) error = %v", candidate.CandidateID, err)
		}
	}

	hook := NewMemoryDraftHook(store, root, func() time.Time { return now.Add(4 * time.Minute) })
	if err := hook.OnTurnRecap(ctx, RecapInput{
		Session: Session{SessionID: "sess_agg"},
		TurnID:  "turn_004",
		RunID:   "run_004",
		Phase:   RecapPhaseCompleted,
		Summary: "aggregate drafts",
	}); err != nil {
		t.Fatalf("OnTurnRecap() error = %v", err)
	}

	memoryDraft, err := os.ReadFile(filepath.Join(root, "memory", "MEMORY.draft.md"))
	if err != nil {
		t.Fatalf("os.ReadFile(MEMORY.draft.md) error = %v", err)
	}
	if strings.Count(string(memoryDraft), "Keep status updates concise.") != 1 {
		t.Fatalf("MEMORY.draft.md = %q, want deduplicated memory candidate", string(memoryDraft))
	}

	userDraft, err := os.ReadFile(filepath.Join(root, "memory", "USER.draft.md"))
	if err != nil {
		t.Fatalf("os.ReadFile(USER.draft.md) error = %v", err)
	}
	if strings.Count(string(userDraft), "User prefers bullet summaries.") != 1 {
		t.Fatalf("USER.draft.md = %q, want filtered and deduplicated user candidate", string(userDraft))
	}
	if strings.Contains(string(userDraft), "rejected") {
		t.Fatalf("USER.draft.md = %q, should not include rejected candidate markers", string(userDraft))
	}
}

func TestMemoryDraftHook_RefreshesAfterCandidateStatusChange(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	root := t.TempDir()
	now := time.Date(2026, 4, 16, 15, 0, 0, 0, time.UTC)

	for _, candidate := range []MemoryCandidate{
		{
			CandidateID:   "mcand_apply_001",
			SessionID:     "sess_apply",
			TurnID:        "turn_001",
			RunID:         "run_001",
			SourceRecapID: "recap_001",
			Target:        "MEMORY",
			Kind:          "turn_learning",
			Content:       "Keep summaries concise.",
			Score:         0.8,
			Status:        MemoryCandidateDraft,
			CreatedAt:     now,
		},
		{
			CandidateID:   "mcand_apply_002",
			SessionID:     "sess_apply",
			TurnID:        "turn_002",
			RunID:         "run_002",
			SourceRecapID: "recap_002",
			Target:        "USER",
			Kind:          "user_preference",
			Content:       "User prefers direct answers.",
			Score:         0.7,
			Status:        MemoryCandidateDraft,
			CreatedAt:     now.Add(time.Minute),
		},
	} {
		if err := store.CreateMemoryCandidate(ctx, candidate); err != nil {
			t.Fatalf("CreateMemoryCandidate(%s) error = %v", candidate.CandidateID, err)
		}
	}

	hook := NewMemoryDraftHook(store, root, func() time.Time { return now.Add(2 * time.Minute) })
	if err := hook.OnTurnRecap(ctx, RecapInput{
		Session: Session{SessionID: "sess_apply"},
		TurnID:  "turn_002",
		RunID:   "run_002",
		Phase:   RecapPhaseCompleted,
		Summary: "render drafts",
	}); err != nil {
		t.Fatalf("OnTurnRecap() initial error = %v", err)
	}

	if err := store.UpdateMemoryCandidateStatus(ctx, "mcand_apply_001", MemoryCandidateApplied); err != nil {
		t.Fatalf("UpdateMemoryCandidateStatus(applied) error = %v", err)
	}
	if err := store.UpdateMemoryCandidateStatus(ctx, "mcand_apply_002", MemoryCandidateRejected); err != nil {
		t.Fatalf("UpdateMemoryCandidateStatus(rejected) error = %v", err)
	}

	if err := hook.OnTurnRecap(ctx, RecapInput{
		Session: Session{SessionID: "sess_apply"},
		TurnID:  "turn_002",
		RunID:   "run_002",
		Phase:   RecapPhaseCompleted,
		Summary: "refresh drafts",
	}); err != nil {
		t.Fatalf("OnTurnRecap() refresh error = %v", err)
	}

	memoryDraft, err := os.ReadFile(filepath.Join(root, "memory", "MEMORY.draft.md"))
	if err != nil {
		t.Fatalf("os.ReadFile(MEMORY.draft.md) error = %v", err)
	}
	if strings.Contains(string(memoryDraft), "Keep summaries concise.") {
		t.Fatalf("MEMORY.draft.md = %q, should not include applied candidate", string(memoryDraft))
	}

	userDraft, err := os.ReadFile(filepath.Join(root, "memory", "USER.draft.md"))
	if err != nil {
		t.Fatalf("os.ReadFile(USER.draft.md) error = %v", err)
	}
	if strings.Contains(string(userDraft), "User prefers direct answers.") {
		t.Fatalf("USER.draft.md = %q, should not include rejected candidate", string(userDraft))
	}
}
