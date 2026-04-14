// Package memory provides short-term and long-term memory abstractions for agents.
package memory

import (
	"context"
	"time"
)

// Message is a single conversation turn stored in memory.
type Message struct {
	Role      string // "user" | "assistant" | "tool" | "system"
	Content   string
	Timestamp time.Time
}

// Fragment is a distilled memory chunk stored for long-term semantic retrieval.
type Fragment struct {
	ID        string
	Content   string
	Embedding []float32
	Source    string // agent_id or task_id
	CreatedAt time.Time
}

// SearchResult is a fragment returned by a similarity search.
type SearchResult struct {
	Fragment *Fragment
	Score    float32
}

// Store is the richer backend memory interface used by internal agents.
// It intentionally remains broader than agentcore api.Memory, which only
// models the runtime-facing context layer.
type Store interface {
	// AddMessage appends a message to the conversation history.
	AddMessage(role, content string)

	// GetMessages returns the full raw message history.
	GetMessages() []Message

	// GetContext returns messages to include in the LLM prompt, respecting maxTokens.
	// It may summarise older messages to stay within budget.
	GetContext(ctx context.Context, maxTokens int) ([]Message, error)

	// SaveFragment persists a memory fragment for long-term retrieval.
	SaveFragment(ctx context.Context, content, source string) error

	// SearchSimilar returns the top-k most relevant fragments for the query.
	// Returns empty slice (not error) if vector search is unavailable.
	SearchSimilar(ctx context.Context, query string, topK int) ([]Fragment, error)

	// Close releases any resources (e.g. database connections).
	Close() error
}

// Summarizer produces a compressed summary of messages.
// Implementations may call an LLM or use extractive methods.
type Summarizer interface {
	Summarize(ctx context.Context, messages []Message) (string, error)
}
