package memory

import (
	"context"
	"strings"
	"sync"
	"time"
)

const (
	defaultSummarizeThreshold = 20  // summarize when messages exceed this
	defaultKeepRecent         = 10  // keep last N messages after summarization
)

// InMemStore is a thread-safe in-process MemoryStore.
// It maintains a sliding window of messages and a running summary for older context.
type InMemStore struct {
	mu          sync.Mutex
	messages    []Message
	summary     string    // compressed summary of older messages
	summarizer  Summarizer
	maxMessages int
	keepRecent  int
}

// InMemConfig configures an InMemStore.
type InMemConfig struct {
	// Summarizer is called to compress old messages when the window overflows.
	// May be nil — in that case, old messages are dropped without summarisation.
	Summarizer  Summarizer
	MaxMessages int // default: defaultSummarizeThreshold
	KeepRecent  int // default: defaultKeepRecent
}

// NewInMemStore creates a new InMemStore.
func NewInMemStore(cfg InMemConfig) *InMemStore {
	if cfg.MaxMessages <= 0 {
		cfg.MaxMessages = defaultSummarizeThreshold
	}
	if cfg.KeepRecent <= 0 {
		cfg.KeepRecent = defaultKeepRecent
	}
	return &InMemStore{
		summarizer:  cfg.Summarizer,
		maxMessages: cfg.MaxMessages,
		keepRecent:  cfg.KeepRecent,
	}
}

// AddMessage implements Store.
func (s *InMemStore) AddMessage(role, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, Message{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	})
}

// GetMessages implements Store.
func (s *InMemStore) GetMessages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Message, len(s.messages))
	copy(out, s.messages)
	return out
}

// GetContext implements Store.
// Returns summary message (if any) followed by the most recent messages.
// If the window is over maxMessages it triggers auto-summarisation.
func (s *InMemStore) GetContext(ctx context.Context, maxTokens int) ([]Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Trigger summarisation when buffer overflows.
	if len(s.messages) > s.maxMessages {
		if err := s.compressLocked(ctx); err != nil {
			// Non-fatal: just keep all messages if summarisation fails.
		}
	}

	var out []Message
	if s.summary != "" {
		out = append(out, Message{Role: "system", Content: "[Memory summary]: " + s.summary})
	}

	recent := s.messages
	if len(recent) > s.keepRecent {
		recent = recent[len(recent)-s.keepRecent:]
	}
	out = append(out, recent...)
	return out, nil
}

// compressLocked summarises old messages. Must be called with s.mu held.
func (s *InMemStore) compressLocked(ctx context.Context) error {
	// Compress everything except the last keepRecent messages.
	cutoff := len(s.messages) - s.keepRecent
	if cutoff <= 0 {
		return nil
	}
	old := s.messages[:cutoff]

	if s.summarizer != nil {
		summary, err := s.summarizer.Summarize(ctx, old)
		if err != nil {
			return err
		}
		if s.summary != "" {
			s.summary = s.summary + "\n" + summary
		} else {
			s.summary = summary
		}
	} else {
		// Fallback: concatenate without LLM.
		var sb strings.Builder
		for _, m := range old {
			sb.WriteString(m.Role)
			sb.WriteString(": ")
			sb.WriteString(m.Content)
			sb.WriteString("\n")
		}
		if s.summary != "" {
			s.summary += "\n" + sb.String()
		} else {
			s.summary = sb.String()
		}
	}

	s.messages = s.messages[cutoff:]
	return nil
}

// SaveFragment implements Store — no-op for InMemStore (no persistence).
func (s *InMemStore) SaveFragment(_ context.Context, _, _ string) error { return nil }

// SearchSimilar implements Store — returns empty slice (no vector search in-memory).
func (s *InMemStore) SearchSimilar(_ context.Context, _ string, _ int) ([]Fragment, error) {
	return nil, nil
}

// Close implements Store — nothing to close.
func (s *InMemStore) Close() error { return nil }
