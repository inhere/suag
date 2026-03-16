// Package hitl implements the Human-In-The-Loop manager for agent intervention.
package hitl

import "time"

// TicketStatus represents the current state of an HITL intervention request.
type TicketStatus string

const (
	TicketWaiting   TicketStatus = "waiting"
	TicketResponded TicketStatus = "responded"
	TicketDismissed TicketStatus = "dismissed"
)

// Ticket is a single HITL intervention request.
type Ticket struct {
	ID        string
	AgentID   string
	TaskID    string
	Reason    string       // why HITL was triggered
	Context   string       // relevant context (e.g. failed tool call + args)
	Status    TicketStatus
	CreatedAt time.Time
}

// Response carries the human operator's instruction back to the agent.
type Response struct {
	TicketID    string
	Instruction string // e.g. "retry with different args" or "abandon task"
	Action      Action
}

// Action classifies the operator's intent.
type Action string

const (
	ActionContinue Action = "continue" // resume with the given instruction
	ActionAbandon  Action = "abandon"  // mark task as failed
)
