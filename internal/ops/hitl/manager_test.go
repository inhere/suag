package hitl

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryManager_RequestInterventionUsesTicketContextFromContext(t *testing.T) {
	t.Parallel()

	manager := NewInMemoryManager(nil)

	ctx := WithTicketContext(context.Background(), TicketContext{
		SessionID: "sess_001",
		TurnID:    "turn_001",
		RunID:     "run_001",
	})

	done := make(chan error, 1)
	go func() {
		_, err := manager.RequestIntervention(ctx, "default", "task_001", "need approval", "ctx")
		done <- err
	}()

	var ticket *Ticket
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pending := manager.ListPending()
		if len(pending) > 0 {
			ticket = pending[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ticket == nil {
		t.Fatal("ticket was never created")
	}

	if ticket.SessionID != "sess_001" {
		t.Fatalf("ticket.SessionID = %q, want %q", ticket.SessionID, "sess_001")
	}
	if ticket.TurnID != "turn_001" {
		t.Fatalf("ticket.TurnID = %q, want %q", ticket.TurnID, "turn_001")
	}
	if ticket.RunID != "run_001" {
		t.Fatalf("ticket.RunID = %q, want %q", ticket.RunID, "run_001")
	}

	if err := manager.Respond(ticket.ID, "continue"); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RequestIntervention() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RequestIntervention() did not return after response")
	}
}
