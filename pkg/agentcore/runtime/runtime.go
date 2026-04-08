package runtime

import "github.com/inhere/axon/pkg/agentcore"

// Runtime re-exports the default runtime contract from the root package.
type Runtime = agentcore.Runtime

// Config re-exports the default runtime config from the root package.
type Config = agentcore.Config

// DefaultRuntime re-exports the default runtime implementation from the root package.
type DefaultRuntime = agentcore.DefaultRuntime

// New constructs the default runtime using the root package implementation.
func New(cfg Config) *DefaultRuntime {
	return agentcore.New(cfg)
}
