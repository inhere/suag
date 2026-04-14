package llm

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	agentcoreapi "github.com/inhere/axon/pkg/agentcore/api"
)

// Pool manages multiple LLM providers with automatic failover.
// It tries each provider in order until one succeeds.
type Pool struct {
	mu        sync.RWMutex
	providers []Provider
	embedder  EmbeddingProvider // dedicated provider for embeddings
}

// NewPool creates a Pool from an ordered list of providers.
// The first provider is the primary; subsequent ones are fallbacks.
// embedProvider (may be nil) is used exclusively for Embed calls;
// falls back to the primary if nil.
func NewPool(providers []Provider, embedProvider EmbeddingProvider) *Pool {
	return &Pool{providers: providers, embedder: embedProvider}
}

// Name implements Provider.
func (p *Pool) Name() string { return "pool" }

// Generate tries each provider in turn and returns the first success.
func (p *Pool) Generate(ctx context.Context, req *agentcoreapi.GenerateRequest) (*agentcoreapi.GenerateResult, error) {
	p.mu.RLock()
	providers := p.providers
	p.mu.RUnlock()

	var lastErr error
	for _, prov := range providers {
		resp, err := prov.Generate(ctx, req)
		if err == nil {
			return resp, nil
		}
		slog.Warn("llm provider failed, trying next", "provider", prov.Name(), "err", err)
		lastErr = err
	}
	return nil, fmt.Errorf("all providers failed: %w", lastErr)
}

// Complete keeps the legacy completion facade for migration callers.
func (p *Pool) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	resp, err := p.Generate(ctx, toGenerateRequest(req))
	if err != nil {
		return nil, err
	}
	return fromGenerateResult(resp), nil
}

// Stream implements the public streaming contract.
func (p *Pool) Stream(ctx context.Context, req *agentcoreapi.GenerateRequest, emit agentcoreapi.StreamFunc) (*agentcoreapi.GenerateResult, error) {
	p.mu.RLock()
	providers := p.providers
	p.mu.RUnlock()

	var lastErr error
	for _, prov := range providers {
		streaming, ok := prov.(agentcoreapi.StreamingProvider)
		if !ok {
			lastErr = fmt.Errorf("provider %s does not support streaming", prov.Name())
			continue
		}
		resp, err := streaming.Stream(ctx, req, emit)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("all providers failed for stream: %w", lastErr)
}

// StreamText keeps the legacy channel-based streaming helper.
func (p *Pool) StreamText(ctx context.Context, req *CompletionRequest) (<-chan string, error) {
	ch := make(chan string, 1)
	go func() {
		defer close(ch)
		resp, err := p.Complete(ctx, req)
		if err != nil || resp == nil {
			return
		}
		ch <- resp.Content
	}()
	return ch, nil
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
		embedProv, ok := prov.(EmbeddingProvider)
		if !ok {
			lastErr = fmt.Errorf("provider %s does not support embeddings", prov.Name())
			continue
		}
		vec, err := embedProv.Embed(ctx, text)
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
