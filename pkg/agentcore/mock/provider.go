package mock

import (
	"context"
	"fmt"
	"sync"

	"github.com/inhere/axon/pkg/agentcore"
)

// Step is a deterministic provider script step.
type Step struct {
	result *agentcore.GenerateResult
	err    error
}

// Final scripts a final model response.
func Final(content string) Step {
	return Step{
		result: &agentcore.GenerateResult{
			Content:      content,
			FinishReason: agentcore.FinishFinal,
		},
	}
}

// ToolCall scripts a single tool call response.
func ToolCall(callID, name string, arguments map[string]any) Step {
	return Step{
		result: &agentcore.GenerateResult{
			ToolCalls: []agentcore.ToolCall{{
				CallID:    callID,
				Name:      name,
				Arguments: cloneMap(arguments),
			}},
			FinishReason: agentcore.FinishToolCalls,
		},
	}
}

// Error scripts a provider failure.
func Error(err error) Step {
	return Step{err: err}
}

// Provider returns scripted responses in order.
type Provider struct {
	mu    sync.Mutex
	name  string
	steps []Step
	index int
}

// NewProvider creates a deterministic scripted provider.
func NewProvider(steps ...Step) *Provider {
	cloned := make([]Step, len(steps))
	for i, step := range steps {
		cloned[i] = cloneStep(step)
	}
	return &Provider{
		name:  "mock",
		steps: cloned,
	}
}

// Name returns the provider identifier.
func (p *Provider) Name() string {
	if p == nil || p.name == "" {
		return "mock"
	}
	return p.name
}

// Generate returns the next scripted result.
func (p *Provider) Generate(context.Context, *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	if p == nil {
		return nil, fmt.Errorf("mock provider exhausted")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.index >= len(p.steps) {
		return nil, fmt.Errorf("mock provider exhausted")
	}

	step := cloneStep(p.steps[p.index])
	p.index++
	if step.err != nil {
		return nil, step.err
	}
	if err := step.result.Validate(); err != nil {
		return nil, err
	}
	return step.result, nil
}

func cloneStep(step Step) Step {
	return Step{
		result: cloneResult(step.result),
		err:    step.err,
	}
}

func cloneResult(result *agentcore.GenerateResult) *agentcore.GenerateResult {
	if result == nil {
		return nil
	}

	cloned := &agentcore.GenerateResult{
		Content:      result.Content,
		FinishReason: result.FinishReason,
		Usage:        result.Usage,
		Metadata:     cloneMap(result.Metadata),
	}

	if len(result.ToolCalls) > 0 {
		cloned.ToolCalls = make([]agentcore.ToolCall, len(result.ToolCalls))
		for i, call := range result.ToolCalls {
			cloned.ToolCalls[i] = agentcore.ToolCall{
				CallID:    call.CallID,
				Name:      call.Name,
				Arguments: cloneMap(call.Arguments),
			}
		}
	}

	if len(result.Artifacts) > 0 {
		cloned.Artifacts = make([]agentcore.Artifact, len(result.Artifacts))
		copy(cloned.Artifacts, result.Artifacts)
	}

	return cloned
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = cloneValue(v)
	}
	return dst
}

func cloneSlice(src []any) []any {
	if len(src) == 0 {
		return nil
	}
	dst := make([]any, len(src))
	for i, v := range src {
		dst[i] = cloneValue(v)
	}
	return dst
}

func cloneValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		return cloneSlice(typed)
	default:
		return v
	}
}
