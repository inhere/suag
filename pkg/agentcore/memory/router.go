package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Router dispatches each call to the backend registered for the scope kind
// (user, project, people). The usual wiring is NewGoverned(router, policy):
// governance runs once, then the router picks the backend.
type Router struct {
	mu     sync.RWMutex
	routes map[string]Store
}

var (
	_ Store    = (*Router)(nil)
	_ Resolver = (*Router)(nil)
)

// NewRouter returns an empty router.
func NewRouter() *Router { return &Router{routes: map[string]Store{}} }

// Register sets the backend for a scope kind, replacing any previous one.
func (r *Router) Register(scopeKind string, s Store) error {
	switch scopeKind {
	case ScopeUser, ScopeProject, ScopePeople:
	default:
		return fmt.Errorf("%w: unknown scope kind %q", ErrInvalid, scopeKind)
	}
	if s == nil {
		return fmt.Errorf("%w: nil backend for scope %q", ErrInvalid, scopeKind)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routes[scopeKind] = s
	return nil
}

// Route returns the backend for s or an ErrNoBackend error.
func (r *Router) Route(s Scope) (Store, error) {
	r.mu.RLock()
	b, ok := r.routes[s.Kind]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %q (no memory backend is configured for this scope)", ErrNoBackend, s.String())
	}
	return b, nil
}

// RouteInfo names the backend serving one scope kind.
type RouteInfo struct {
	Scope   string // scope kind
	Backend string // backend name (Named), "unknown" when not reported
}

// Backends lists the registered scope kinds and their backend names, sorted
// by scope kind.
func (r *Router) Backends() []RouteInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RouteInfo, 0, len(r.routes))
	for k, s := range r.routes {
		out = append(out, RouteInfo{Scope: k, Backend: BackendName(s)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Scope < out[j].Scope })
	return out
}

// Index implements Store.
func (r *Router) Index(ctx context.Context, s Scope) (Index, error) {
	b, err := r.Route(s)
	if err != nil {
		return Index{}, err
	}
	return b.Index(ctx, s)
}

// Get implements Store.
func (r *Router) Get(ctx context.Context, s Scope, id string) (Entry, error) {
	b, err := r.Route(s)
	if err != nil {
		return Entry{}, err
	}
	return b.Get(ctx, s, id)
}

// Write implements Store.
func (r *Router) Write(ctx context.Context, s Scope, op Op) (Entry, error) {
	b, err := r.Route(s)
	if err != nil {
		return Entry{}, err
	}
	return b.Write(ctx, s, op)
}

// Search implements Store.
func (r *Router) Search(ctx context.Context, s Scope, q string, limit int) ([]Hit, error) {
	b, err := r.Route(s)
	if err != nil {
		return nil, err
	}
	return b.Search(ctx, s, q, limit)
}
