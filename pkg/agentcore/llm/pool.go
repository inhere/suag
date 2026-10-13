package llm

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	agentcoreapi "github.com/inhere/suga/pkg/agentcore/api"
)

// Pool manages multiple LLM providers with automatic failover.
// It tries each provider in order until one succeeds.
type Pool struct {
	mu        sync.RWMutex
	providers []Provider
	observer  AttemptObserver // optional; sees every provider attempt
}

// NewPool creates a Pool from an ordered list of providers.
// The first provider is the primary; subsequent ones are fallbacks.
func NewPool(providers []Provider) *Pool {
	return &Pool{providers: providers}
}

// SetObserver installs a callback invoked once per provider attempt (fallbacks
// included). Pass nil to remove it.
func (p *Pool) SetObserver(obs AttemptObserver) {
	p.mu.Lock()
	p.observer = obs
	p.mu.Unlock()
}

// Name implements Provider.
func (p *Pool) Name() string { return "pool" }

// Generate tries each provider in turn and returns the first success.
func (p *Pool) Generate(ctx context.Context, req *agentcoreapi.GenerateRequest) (*agentcoreapi.GenerateResult, error) {
	p.mu.RLock()
	providers, obs := p.providers, p.observer
	p.mu.RUnlock()

	var lastErr error
	for i, prov := range providers {
		resp, err := GenerateObserved(ctx, prov, req, i+1, obs)
		if err == nil {
			return resp, nil
		}
		slog.Warn("llm provider failed, trying next", "provider", prov.Name(), "err", err)
		lastErr = err
	}
	return nil, fmt.Errorf("all providers failed: %w", lastErr)
}

// Stream implements the public streaming contract.
func (p *Pool) Stream(ctx context.Context, req *agentcoreapi.GenerateRequest, emit agentcoreapi.StreamFunc) (*agentcoreapi.GenerateResult, error) {
	p.mu.RLock()
	providers, obs := p.providers, p.observer
	p.mu.RUnlock()

	// Once a provider has emitted text the caller has shown it, so a later
	// failure is returned as is: falling back would repeat or mix the answer.
	emitted := false
	tracked := func(ctx context.Context, ev agentcoreapi.StreamEvent) error {
		if ev.Kind == agentcoreapi.StreamEventTextDelta && ev.Delta != "" {
			emitted = true
		}
		if emit == nil {
			return nil
		}
		return emit(ctx, ev)
	}
	var lastErr error
	for i, prov := range providers {
		streaming, ok := prov.(agentcoreapi.StreamingProvider)
		if !ok {
			lastErr = fmt.Errorf("provider %s does not support streaming", prov.Name())
			continue
		}
		resp, err := StreamObserved(ctx, streaming, req, tracked, i+1, obs)
		if err == nil {
			return resp, nil
		}
		if emitted || ctx.Err() != nil {
			return nil, err
		}
		slog.Warn("llm provider stream failed, trying next", "provider", prov.Name(), "err", err)
		lastErr = err
	}
	return nil, fmt.Errorf("all providers failed for stream: %w", lastErr)
}

// AddProvider appends a provider to the pool (thread-safe).
func (p *Pool) AddProvider(prov Provider) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.providers = append(p.providers, prov)
}
