package agent

import (
	"fmt"
	"strings"

	"github.com/inhere/axon/internal/hitl"
	"github.com/inhere/axon/internal/tracing"
	"github.com/inhere/axon/pkg/agentcore"
)

// RuntimeKind selects which internal runner implementation should execute an agent task.
type RuntimeKind string

const (
	RuntimeAgentcore RuntimeKind = "agentcore"
	RuntimeReact     RuntimeKind = "react"
)

// RunnerFactoryConfig centralizes runner selection and common runtime wiring.
type RunnerFactoryConfig struct {
	RuntimeKind         RuntimeKind
	Storage             agentcore.Storage
	Tracer              tracing.Tracer
	HITL                hitl.Manager
	FailThreshold       int
	MaxSteps            int
	MaxToolCallsPerStep int
	PromptBuilder       agentcore.PromptBuilder
	ContextBuilder      agentcore.ContextBuilder
	ResumePolicy        agentcore.ResumePolicy
	ToolExecutionPolicy agentcore.ToolExecutionPolicy
	ErrorPolicy         agentcore.ErrorPolicy
	Hooks               []agentcore.Hook
}

// NewRunner chooses the active runner implementation for an agent.
func NewRunner(a *Agent, cfg RunnerFactoryConfig) (Runner, error) {
	switch resolveRuntimeKind(cfg.RuntimeKind) {
	case RuntimeReact:
		return NewReactRunnerWithConfig(a, ReactRunnerConfig{
			MaxSteps:            cfg.MaxSteps,
			FailThreshold:       cfg.FailThreshold,
			Tracer:              ensureTracer(cfg.Tracer),
			HITL:                cfg.HITL,
			ResumePolicy:        cfg.ResumePolicy,
			ToolExecutionPolicy: cfg.ToolExecutionPolicy,
			ErrorPolicy:         cfg.ErrorPolicy,
		}), nil
	case RuntimeAgentcore:
		return NewAgentcoreRunner(a, AgentcoreRunnerConfig{
			Storage:             cfg.Storage,
			PromptBuilder:       cfg.PromptBuilder,
			ContextBuilder:      cfg.ContextBuilder,
			ResumePolicy:        cfg.ResumePolicy,
			ToolExecutionPolicy: cfg.ToolExecutionPolicy,
			ErrorPolicy:         cfg.ErrorPolicy,
			Hooks:               cfg.Hooks,
			Tracer:              ensureTracer(cfg.Tracer),
			HITL:                cfg.HITL,
			FailThreshold:       cfg.FailThreshold,
			MaxSteps:            cfg.MaxSteps,
			MaxToolCallsPerStep: cfg.MaxToolCallsPerStep,
		}), nil
	default:
		return nil, fmt.Errorf("unsupported runtime kind %q", cfg.RuntimeKind)
	}
}

func resolveRuntimeKind(kind RuntimeKind) RuntimeKind {
	switch RuntimeKind(strings.ToLower(strings.TrimSpace(string(kind)))) {
	case "", RuntimeAgentcore:
		return RuntimeAgentcore
	case RuntimeReact:
		return RuntimeReact
	default:
		return kind
	}
}

func ensureTracer(tracer tracing.Tracer) tracing.Tracer {
	if tracer != nil {
		return tracer
	}
	return tracing.NewInMemoryTracer(nil)
}
