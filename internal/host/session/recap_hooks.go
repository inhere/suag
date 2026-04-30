package session

import (
	"context"
	"strings"
	"time"
)

type recapRepository interface {
	CreateRecap(ctx context.Context, recap Recap) error
	CreateMemoryCandidate(ctx context.Context, candidate MemoryCandidate) error
	GetMemoryCandidate(ctx context.Context, candidateID string) (MemoryCandidate, error)
	ListRecapsByTurn(ctx context.Context, sessionID string, turnID string) ([]Recap, error)
	ListMemoryCandidatesBySession(ctx context.Context, sessionID string) ([]MemoryCandidate, error)
	ListMemoryCandidatesByTurn(ctx context.Context, sessionID string, turnID string) ([]MemoryCandidate, error)
	UpdateMemoryCandidateStatus(ctx context.Context, candidateID string, status MemoryCandidateStatus) error
}

type StructuredRecapHook struct {
	store recapRepository
	now   func() time.Time
}

func NewStructuredRecapHook(repo Repository, now func() time.Time) *StructuredRecapHook {
	if now == nil {
		now = time.Now
	}
	return &StructuredRecapHook{
		store: repo,
		now:   now,
	}
}

func (h *StructuredRecapHook) OnTurnRecap(ctx context.Context, input RecapInput) error {
	if h == nil || h.store == nil {
		return nil
	}

	content := strings.TrimSpace(input.Summary)
	target := "assistant"
	if content == "" {
		content = strings.TrimSpace(input.Failure)
		target = "host"
	}
	if content == "" {
		content = string(input.Phase)
	}

	return h.store.CreateRecap(ctx, Recap{
		RecapID:   NewRecapID(),
		SessionID: input.Session.SessionID,
		TurnID:    input.TurnID,
		RunID:     input.RunID,
		Kind:      "turn_recap",
		Target:    target,
		Content:   content,
		CreatedAt: h.now().UTC(),
		Metadata: map[string]any{
			"phase":   string(input.Phase),
			"summary": strings.TrimSpace(input.Summary),
			"failure": strings.TrimSpace(input.Failure),
		},
	})
}

type MemoryCandidateHook struct {
	store recapRepository
	now   func() time.Time
}

func NewMemoryCandidateHook(repo Repository, now func() time.Time) *MemoryCandidateHook {
	if now == nil {
		now = time.Now
	}
	return &MemoryCandidateHook{
		store: repo,
		now:   now,
	}
}

func (h *MemoryCandidateHook) OnTurnRecap(ctx context.Context, input RecapInput) error {
	if h == nil || h.store == nil {
		return nil
	}

	recapID, err := latestRecapIDForTurn(ctx, h.store, input.Session.SessionID, input.TurnID)
	if err != nil {
		return err
	}
	if recapID == "" {
		return nil
	}

	for _, candidate := range buildMemoryCandidatesFromRecap(input, recapID, h.now().UTC()) {
		if err := h.store.CreateMemoryCandidate(ctx, candidate); err != nil {
			return err
		}
	}
	return nil
}

func latestRecapIDForTurn(ctx context.Context, store recapRepository, sessionID string, turnID string) (string, error) {
	recaps, err := store.ListRecapsByTurn(ctx, sessionID, turnID)
	if err != nil {
		return "", err
	}
	if len(recaps) == 0 {
		return "", nil
	}
	return strings.TrimSpace(recaps[len(recaps)-1].RecapID), nil
}

func buildMemoryCandidatesFromRecap(input RecapInput, sourceRecapID string, now time.Time) []MemoryCandidate {
	content := strings.TrimSpace(input.Summary)
	if content == "" {
		content = strings.TrimSpace(input.Failure)
	}
	if content == "" {
		return nil
	}

	candidates := make([]MemoryCandidate, 0, 2)
	baseMetadata := map[string]any{
		"phase": string(input.Phase),
	}

	normalized := strings.ToLower(content)
	// MEMORY 候选比 USER 候选更宽松。第一版只要 turn recap 有明确总结，
	// 就先生成可审查的草案，后续再叠加去重与审核策略。
	candidates = append(candidates, MemoryCandidate{
		CandidateID:   NewMemoryCandidateID(),
		SessionID:     input.Session.SessionID,
		TurnID:        input.TurnID,
		RunID:         input.RunID,
		SourceRecapID: sourceRecapID,
		Target:        "MEMORY",
		Kind:          "turn_learning",
		Content:       content,
		Score:         0.8,
		Status:        MemoryCandidateDraft,
		CreatedAt:     now,
		Metadata:      cloneMap(baseMetadata),
	})

	if strings.Contains(normalized, "user prefers") || strings.Contains(normalized, "prefers") {
		candidates = append(candidates, MemoryCandidate{
			CandidateID:   NewMemoryCandidateID(),
			SessionID:     input.Session.SessionID,
			TurnID:        input.TurnID,
			RunID:         input.RunID,
			SourceRecapID: sourceRecapID,
			Target:        "USER",
			Kind:          "user_preference",
			Content:       content,
			Score:         0.7,
			Status:        MemoryCandidateDraft,
			CreatedAt:     now,
			Metadata:      cloneMap(baseMetadata),
		})
	}

	return candidates
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
