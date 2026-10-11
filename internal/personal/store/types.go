package store

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	// ErrSessionNotFound is returned when a session id does not exist.
	ErrSessionNotFound = errors.New("session not found")
	// ErrTurnNotFound is returned when a turn id does not exist.
	ErrTurnNotFound = errors.New("turn not found")
)

// Session lifecycle states stored in sessions.status.
const (
	SessionOpen     = "open"
	SessionRunning  = "running"
	SessionArchived = "archived"
)

// Turn lifecycle states stored in turns.status.
const (
	TurnRunning   = "running"
	TurnCompleted = "completed"
	TurnFailed    = "failed"
	TurnCancelled = "cancelled"
)

// Entry types stored in entries.type. context_update records are kept for
// trace audit only and are never replayed into later model context.
const (
	EntryUser          = "user"
	EntryAssistant     = "assistant"
	EntryTool          = "tool"
	EntryCompaction    = "compaction"
	EntryPlan          = "plan"
	EntryHandoff       = "handoff"
	EntryContextUpdate = "context_update"
)

// Event kinds stored in events.kind.
const (
	EventModel      = "model"
	EventTool       = "tool"
	EventDenial     = "denial"
	EventTurn       = "turn"
	EventCompaction = "compaction"
	EventToolSearch = "tool_search"
	// EventTurnContext records what a turn starts with (model, mode, tool
	// set, rules/memory snapshot hashes, taint); see D9.
	EventTurnContext = "turn_context"
)

// Session is one conversation. Meta holds free-form keys (rules hash, append
// prompt, effective model, loaded deferred tools, usage ...).
type Session struct {
	ID         string
	Title      string
	Cwd        string
	Status     string
	LastTurnID string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Meta       map[string]any
}

// Turn is one user request and its agentcore run (run_id == turn id).
type Turn struct {
	ID         string
	SessionID  string
	Status     string
	StopReason string
	StateJSON  []byte // agentcore RunState snapshot; empty until SaveRun
	StartedAt  time.Time
	EndedAt    time.Time
}

// Entry is one record of a session's linear log. Seq is assigned by the store.
type Entry struct {
	SessionID string
	Seq       int64
	TurnID    string
	Type      string
	JSON      json.RawMessage
	CreatedAt time.Time
}

// NewEntry is the input form of Entry for AppendEntries.
type NewEntry struct {
	TurnID string
	Type   string
	JSON   json.RawMessage
}

// EntryFilter narrows ListEntries; zero values mean no restriction.
type EntryFilter struct {
	SinceSeq int64 // only entries with seq > SinceSeq
	TurnID   string
	Types    []string
	Limit    int
}

// Event is one observability record (model call, tool call, turn ...).
type Event struct {
	ID               int64
	SessionID        string
	TurnID           string
	Kind             string
	Name             string
	Status           string
	StartedAt        time.Time
	DurationMs       int64
	TTFTMs           int64
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	Error            string
	Attrs            map[string]any
}

// EventFilter narrows ListEvents; zero values mean no restriction.
type EventFilter struct {
	SessionID string
	TurnID    string
	Kinds     []string
	Since     time.Time
	Limit     int
}

// Usage is the token total returned by SumUsage.
type Usage struct {
	Calls            int64
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	CostUSD          float64
}
