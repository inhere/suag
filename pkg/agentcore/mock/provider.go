package mock

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/inhere/axon/pkg/agentcore"
)

// Step is a deterministic provider script step.
type Step struct {
	result       *agentcore.GenerateResult
	err          error
	streamEvents []agentcore.StreamEvent
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

// Stream attaches streaming events to a scripted step.
func Stream(step Step, events ...agentcore.StreamEvent) Step {
	step.streamEvents = cloneStreamEvents(events)
	return step
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

// Stream emits scripted streaming events before returning the final scripted result.
func (p *Provider) Stream(ctx context.Context, req *agentcore.GenerateRequest, emit agentcore.StreamFunc) (*agentcore.GenerateResult, error) {
	_ = req

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

	for _, event := range step.streamEvents {
		if emit == nil {
			continue
		}
		if err := emit(ctx, cloneStreamEvent(event)); err != nil {
			return nil, err
		}
	}

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
		result:       cloneResult(step.result),
		err:          step.err,
		streamEvents: cloneStreamEvents(step.streamEvents),
	}
}

func cloneStreamEvents(events []agentcore.StreamEvent) []agentcore.StreamEvent {
	if len(events) == 0 {
		return nil
	}
	cloned := make([]agentcore.StreamEvent, len(events))
	for i, event := range events {
		cloned[i] = cloneStreamEvent(event)
	}
	return cloned
}

func cloneStreamEvent(event agentcore.StreamEvent) agentcore.StreamEvent {
	return agentcore.StreamEvent{
		Kind:     event.Kind,
		Delta:    event.Delta,
		Metadata: cloneMap(event.Metadata),
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
		for i, artifact := range result.Artifacts {
			cloned.Artifacts[i] = agentcore.Artifact{
				Kind:     artifact.Kind,
				URI:      artifact.URI,
				Content:  artifact.Content,
				Metadata: cloneMap(artifact.Metadata),
			}
		}
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
	if v == nil {
		return nil
	}

	switch typed := v.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		return cloneSlice(typed)
	}

	value := reflect.ValueOf(v)
	switch value.Kind() {
	case reflect.Slice:
		if value.IsNil() {
			return v
		}

		cloned := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			cloned.Index(i).Set(reflect.ValueOf(cloneValue(value.Index(i).Interface())))
		}
		return cloned.Interface()
	case reflect.Map:
		if value.IsNil() {
			return v
		}

		cloned := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			cloned.SetMapIndex(iter.Key(), reflect.ValueOf(cloneValue(iter.Value().Interface())))
		}
		return cloned.Interface()
	default:
		return v
	}
}
