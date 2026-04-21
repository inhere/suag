package runtimecaps

import "context"

const CapabilitySetSessionMetadataKey = "capability_set"

type TurnToolMode string

const (
	TurnToolModeAuto     TurnToolMode = "auto"
	TurnToolModeFull     TurnToolMode = "full"
	TurnToolModeReadOnly TurnToolMode = "read_only"
)

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

// CapabilityPolicy is the session-stable tool governance baseline that can be
// refined again at turn time using the current message.
type CapabilityPolicy struct {
	ToolMode      TurnToolMode
	ToolAllowlist []string
	ToolDenylist  []string
}

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
	ToolMode  TurnToolMode
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
	RecallMetadata  map[string]any
}

type staticToolProvider struct {
	id    string
	tools []ToolRef
}
