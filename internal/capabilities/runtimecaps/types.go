package runtimecaps

import "context"

const CapabilitySetSessionMetadataKey = "capability_set"

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
type BuildSetInput struct {
	SessionMetadata map[string]any
}

// ResolveTurnInput carries the minimum turn-time inputs used to refine the
// session-stable capability baseline into a per-turn snapshot.
type ResolveTurnInput struct {
	SessionMetadata map[string]any
	UserMessage     string
	AgentID         string
	IsContinue      bool
}

type staticToolProvider struct {
	id    string
	tools []ToolRef
}
