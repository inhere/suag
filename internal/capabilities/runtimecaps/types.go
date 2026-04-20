package runtimecaps

import "context"

// ToolRef describes a runtime tool exposed by a provider.
type ToolRef struct {
	Name       string
	Source     string
	ProviderID string
}

// ToolProvider exposes tool references that can participate in a capability set.
type ToolProvider interface {
	ID() string
	ListToolRefs(ctx context.Context) ([]ToolRef, error)
}

// CapabilityPolicy reserves space for future allow/deny rules.
type CapabilityPolicy struct{}

// CapabilityScope reserves space for future workspace/session scoping.
type CapabilityScope struct{}

// CapabilitySet is the session-stable capability baseline.
type CapabilitySet struct {
	Tools    []ToolRef
	Policies CapabilityPolicy
	Scope    CapabilityScope
}

// ResolvedTurnCapabilities is the per-turn capability snapshot.
type ResolvedTurnCapabilities struct {
	ToolRefs  []ToolRef
	ToolNames []string
}

// BuildSetInput carries future session-scoped capability inputs.
type BuildSetInput struct{}

type staticToolProvider struct {
	id    string
	tools []ToolRef
}
