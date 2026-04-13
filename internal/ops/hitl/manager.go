package hitl

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Manager handles the lifecycle of HITL tickets.
type Manager interface {
	// RequestIntervention creates a ticket and suspends until a human responds.
	// Blocks until ctx is cancelled or a human calls Respond/Dismiss.
	RequestIntervention(ctx context.Context, agentID, taskID, reason, ctxMsg string) (*Response, error)

	// Respond delivers a human instruction to the waiting agent.
	Respond(ticketID, instruction string) error

	// Dismiss marks the ticket as dismissed (task will be failed).
	Dismiss(ticketID, reason string) error

	// ListPending returns all tickets currently awaiting response.
	ListPending() []*Ticket
}

// InMemoryManager is a channel-based HITL manager for single-process use.
type InMemoryManager struct {
	mu      sync.Mutex
	tickets map[string]*Ticket
	chans   map[string]chan *Response
	notify  func(*Ticket) // optional hook: called when a new ticket is created
}

// NewInMemoryManager creates a new InMemoryManager.
// Pass a notify func to receive new-ticket events (e.g. for SSE push); nil disables it.
func NewInMemoryManager(notify func(*Ticket)) *InMemoryManager {
	return &InMemoryManager{
		tickets: make(map[string]*Ticket),
		chans:   make(map[string]chan *Response),
		notify:  notify,
	}
}

// RequestIntervention implements Manager.
func (m *InMemoryManager) RequestIntervention(ctx context.Context, agentID, taskID, reason, ctxMsg string) (*Response, error) {
	ticketCtx, _ := TicketContextFromContext(ctx)
	ticket := &Ticket{
		ID:        newTicketID(),
		AgentID:   agentID,
		TaskID:    taskID,
		SessionID: ticketCtx.SessionID,
		TurnID:    ticketCtx.TurnID,
		RunID:     ticketCtx.RunID,
		Reason:    reason,
		Context:   ctxMsg,
		Status:    TicketWaiting,
		CreatedAt: time.Now(),
	}

	respCh := make(chan *Response, 1)

	m.mu.Lock()
	m.tickets[ticket.ID] = ticket
	m.chans[ticket.ID] = respCh
	m.mu.Unlock()

	if m.notify != nil {
		m.notify(ticket)
	}

	// Block until human responds or context is cancelled.
	select {
	case <-ctx.Done():
		m.mu.Lock()
		ticket.Status = TicketDismissed
		delete(m.chans, ticket.ID)
		m.mu.Unlock()
		return nil, ctx.Err()
	case resp := <-respCh:
		return resp, nil
	}
}

// Respond implements Manager.
func (m *InMemoryManager) Respond(ticketID, instruction string) error {
	m.mu.Lock()
	ticket, ok := m.tickets[ticketID]
	ch, chOk := m.chans[ticketID]
	m.mu.Unlock()

	if !ok || !chOk {
		return fmt.Errorf("ticket %q not found or already closed", ticketID)
	}

	ticket.Status = TicketResponded
	ch <- &Response{TicketID: ticketID, Instruction: instruction, Action: ActionContinue}
	close(ch)

	m.mu.Lock()
	delete(m.chans, ticketID)
	m.mu.Unlock()
	return nil
}

// Dismiss implements Manager.
func (m *InMemoryManager) Dismiss(ticketID, reason string) error {
	m.mu.Lock()
	ticket, ok := m.tickets[ticketID]
	ch, chOk := m.chans[ticketID]
	m.mu.Unlock()

	if !ok || !chOk {
		return fmt.Errorf("ticket %q not found or already closed", ticketID)
	}

	ticket.Status = TicketDismissed
	ch <- &Response{TicketID: ticketID, Instruction: reason, Action: ActionAbandon}
	close(ch)

	m.mu.Lock()
	delete(m.chans, ticketID)
	m.mu.Unlock()
	return nil
}

// ListPending implements Manager.
func (m *InMemoryManager) ListPending() []*Ticket {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Ticket, 0)
	for _, t := range m.tickets {
		if t.Status == TicketWaiting {
			out = append(out, t)
		}
	}
	return out
}

func newTicketID() string {
	return fmt.Sprintf("hitl-%d", time.Now().UnixNano())
}
