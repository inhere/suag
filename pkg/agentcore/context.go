package agentcore

import "context"

// ContextInput is the portable input for context assembly.
type ContextInput struct {
	Run       RunInput
	Messages  []Message
	Artifacts []Artifact
	Metadata  map[string]any
}

// ContextSnapshot is the concrete context representation exposed to hooks and builders.
type ContextSnapshot struct {
	Messages  []Message
	Artifacts []Artifact
	Metadata  map[string]any
}

// ContextBuilder assembles the runtime context presented to prompt construction.
type ContextBuilder interface {
	BuildContext(ctx context.Context, input ContextInput) (*ContextSnapshot, error)
}
