package llm

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// Pool manages multiple LLM providers with automatic failover.
// It tries each provider in order until one succeeds.
type Pool struct {
	mu        sync.RWMutex
	providers []Provider
	embedder  Provider // dedicated provider for embeddings
}

// NewPool creates a Pool from an ordered list of providers.
// The first provider is the primary; subsequent ones are fallbacks.
// embedProvider (may be nil) is used exclusively for Embed calls;
// falls back to the primary if nil.
func NewPool(providers []Provider, embedProvider Provider) *Pool {
	return &Pool{providers: providers, embedder: embedProvider}
}

// Name implements Provider.
func (p *Pool) Name() string { return "pool" }

// Complete tries each provider in turn and returns the first success.
func (p *Pool) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	p.mu.RLock()
	providers := p.providers
	p.mu.RUnlock()

	var lastErr error
	for _, prov := range providers {
		resp, err := prov.Complete(ctx, req)
		if err == nil {
			return resp, nil
		}
		slog.Warn("llm provider failed, trying next", "provider", prov.Name(), "err", err)
		lastErr = err
	}
	return nil, fmt.Errorf("all providers failed: %w", lastErr)
}

// Stream tries each provider in turn.
func (p *Pool) Stream(ctx context.Context, req *CompletionRequest) (<-chan string, error) {
	p.mu.RLock()
	providers := p.providers
	p.mu.RUnlock()

	var lastErr error
	for _, prov := range providers {
		ch, err := prov.Stream(ctx, req)
		if err == nil {
			return ch, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("all providers failed for stream: %w", lastErr)
}

// Embed uses the dedicated embed provider if set, otherwise tries the pool.
func (p *Pool) Embed(ctx context.Context, text string) ([]float32, error) {
	p.mu.RLock()
	embedder := p.embedder
	providers := p.providers
	p.mu.RUnlock()

	if embedder != nil {
		vec, err := embedder.Embed(ctx, text)
		if err == nil {
			return vec, nil
		}
		slog.Warn("embed provider failed, trying pool", "err", err)
	}

	var lastErr error
	for _, prov := range providers {
		vec, err := prov.Embed(ctx, text)
		if err == nil {
			return vec, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("all providers failed for embed: %w", lastErr)
}

// AddProvider appends a provider to the pool (thread-safe).
func (p *Pool) AddProvider(prov Provider) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.providers = append(p.providers, prov)
}
