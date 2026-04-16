package session

import (
	"context"
	"strings"
	"time"
)

type recapStore interface {
	CreateRecap(ctx context.Context, recap Recap) error
}

type StructuredRecapHook struct {
	store recapStore
	now   func() time.Time
}

func NewStructuredRecapHook(store Store, now func() time.Time) *StructuredRecapHook {
	if now == nil {
		now = time.Now
	}
	return &StructuredRecapHook{
		store: store,
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
