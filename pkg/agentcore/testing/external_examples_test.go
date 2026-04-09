package agentcoretesting_test

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/inhere/axon/pkg/agentcore"
	agentcoretesting "github.com/inhere/axon/pkg/agentcore/testing"
)

func TestExternalStorageContractExample(t *testing.T) {
	agentcoretesting.RunStorageSuite(t, agentcoretesting.NewStorageFactoryFromPath(func(t *testing.T, root string) agentcore.Storage {
		t.Helper()
		_ = root // Example in-memory storage ignores the filesystem root.
		return newExampleStorage()
	}))
}

func TestExternalToolRegistryContractExample(t *testing.T) {
	agentcoretesting.RunToolRegistrySuite(t, func(t *testing.T) agentcoretesting.ToolRegistryFixture {
		t.Helper()

		registry := &exampleToolRegistry{
			schema: agentcore.ToolSchema{
				Name:        "echo",
				Description: "echo text",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"text": map[string]any{"type": "string"},
					},
				},
			},
		}

		return agentcoretesting.NewToolRegistryFixture(
			registry,
			registry.schema,
			agentcore.ToolCall{
				CallID:    "call-1",
				Name:      "echo",
				Arguments: map[string]any{"text": "hello"},
			},
			&agentcore.ToolResult{
				CallID:  "call-1",
				Name:    "echo",
				Content: "hello",
			},
		)
	})
}

func TestExternalProviderContractExample(t *testing.T) {
	agentcoretesting.RunProviderSuite(t, func(t *testing.T) agentcoretesting.ProviderFixture {
		t.Helper()

		return agentcoretesting.NewProviderFixture(
			&exampleProvider{
				generateResult: &agentcore.GenerateResult{
					Content:      "done",
					FinishReason: agentcore.FinishFinal,
				},
				streamResult: &agentcore.GenerateResult{
					Content:      "stream done",
					FinishReason: agentcore.FinishFinal,
				},
				streamEvents: []agentcore.StreamEvent{
					{Kind: agentcore.StreamEventTextDelta, Delta: "stream "},
					{Kind: agentcore.StreamEventTextDelta, Delta: "done"},
					{Kind: agentcore.StreamEventMessageStop},
				},
			},
			agentcoretesting.NewGenerateCase(
				"final_result",
				&agentcore.GenerateRequest{System: "system"},
				&agentcore.GenerateResult{
					Content:      "done",
					FinishReason: agentcore.FinishFinal,
				},
			),
		).WithStreaming(agentcoretesting.StreamingCase{
			Request: &agentcore.GenerateRequest{System: "system"},
			WantEvents: []agentcore.StreamEvent{
				{Kind: agentcore.StreamEventTextDelta, Delta: "stream "},
				{Kind: agentcore.StreamEventTextDelta, Delta: "done"},
				{Kind: agentcore.StreamEventMessageStop},
			},
			WantResult: &agentcore.GenerateResult{
				Content:      "stream done",
				FinishReason: agentcore.FinishFinal,
			},
		})
	})
}

func TestExternalContractChecklistExample(t *testing.T) {
	items := agentcoretesting.ContractChecklist()
	if len(items) == 0 {
		t.Fatal("expected contract checklist to expose public conformance metadata")
	}

	var foundStorage bool
	for _, item := range items {
		if item.Surface == "Storage authority contract" {
			foundStorage = true
			if len(item.RecommendedSuites) == 0 {
				t.Fatal("expected storage checklist entry to recommend at least one suite")
			}
			break
		}
	}
	if !foundStorage {
		t.Fatal("expected storage checklist entry")
	}
}

func TestExternalOnboardingChecklistExample(t *testing.T) {
	items := agentcoretesting.OnboardingChecklist()
	if len(items) == 0 {
		t.Fatal("expected onboarding checklist to expose navigation metadata")
	}

	var foundProvider bool
	for _, item := range items {
		if item.Kind == "Provider" {
			foundProvider = true
			if len(item.RecommendedSuites) == 0 || len(item.Docs) == 0 || len(item.Examples) == 0 {
				t.Fatalf("expected provider onboarding item to include suites, docs, and examples, got %#v", item)
			}
			break
		}
	}
	if !foundProvider {
		t.Fatal("expected provider onboarding entry")
	}
}

type exampleStorage struct {
	mu       sync.Mutex
	runs     map[string]*agentcore.RunState
	messages map[string][]agentcore.Message
}

func newExampleStorage() *exampleStorage {
	return &exampleStorage{
		runs:     make(map[string]*agentcore.RunState),
		messages: make(map[string][]agentcore.Message),
	}
}

func (s *exampleStorage) LoadRun(_ context.Context, runID string) (*agentcore.RunState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.runs[runID]
	if !ok {
		return nil, nil
	}
	return cloneRunState(state), nil
}

func (s *exampleStorage) LoadMessages(_ context.Context, runID string) ([]agentcore.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return cloneMessages(s.messages[runID]), nil
}

func (s *exampleStorage) SaveRun(_ context.Context, state *agentcore.RunState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if state == nil {
		return fmt.Errorf("state is nil")
	}
	s.runs[state.RunID] = cloneRunState(state)
	return nil
}

func (s *exampleStorage) AppendMessages(_ context.Context, runID string, msgs ...agentcore.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages[runID] = append(s.messages[runID], cloneMessages(msgs)...)
	return nil
}

type exampleToolRegistry struct {
	schema agentcore.ToolSchema
}

func (r *exampleToolRegistry) ListTools(context.Context) ([]agentcore.ToolSchema, error) {
	return []agentcore.ToolSchema{cloneToolSchema(r.schema)}, nil
}

func (r *exampleToolRegistry) ExecuteTool(_ context.Context, call agentcore.ToolCall) (*agentcore.ToolResult, error) {
	return &agentcore.ToolResult{
		CallID:  call.CallID,
		Name:    call.Name,
		Content: call.Arguments["text"].(string),
	}, nil
}

type exampleProvider struct {
	generateCalls  int
	generateResult *agentcore.GenerateResult
	streamResult   *agentcore.GenerateResult
	streamEvents   []agentcore.StreamEvent
}

func (p *exampleProvider) Name() string {
	return "example"
}

func (p *exampleProvider) Generate(context.Context, *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	p.generateCalls++
	if p.generateCalls > 1 {
		return nil, fmt.Errorf("unexpected extra Generate call")
	}
	return cloneGenerateResult(p.generateResult), nil
}

func (p *exampleProvider) Stream(ctx context.Context, _ *agentcore.GenerateRequest, emit agentcore.StreamFunc) (*agentcore.GenerateResult, error) {
	for _, event := range p.streamEvents {
		if emit != nil {
			if err := emit(ctx, cloneStreamEvent(event)); err != nil {
				return nil, err
			}
		}
	}
	return cloneGenerateResult(p.streamResult), nil
}

func cloneRunState(state *agentcore.RunState) *agentcore.RunState {
	if state == nil {
		return nil
	}
	cloned := *state
	cloned.Messages = cloneMessages(state.Messages)
	cloned.Artifacts = append([]agentcore.Artifact(nil), state.Artifacts...)
	cloned.Metadata = cloneMap(state.Metadata)
	cloned.Scratch = cloneMap(state.Scratch)
	return &cloned
}

func cloneMessages(messages []agentcore.Message) []agentcore.Message {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]agentcore.Message, len(messages))
	for i, msg := range messages {
		cloned[i] = agentcore.Message{
			Role:       msg.Role,
			Content:    msg.Content,
			Metadata:   cloneMap(msg.Metadata),
			ToolCall:   cloneToolCall(msg.ToolCall),
			ToolResult: cloneToolResult(msg.ToolResult),
		}
	}
	return cloned
}

func cloneToolCall(call *agentcore.ToolCall) *agentcore.ToolCall {
	if call == nil {
		return nil
	}
	cloned := *call
	cloned.Arguments = cloneMap(call.Arguments)
	return &cloned
}

func cloneToolResult(result *agentcore.ToolResult) *agentcore.ToolResult {
	if result == nil {
		return nil
	}
	cloned := *result
	cloned.Artifacts = append([]agentcore.Artifact(nil), result.Artifacts...)
	cloned.Metadata = cloneMap(result.Metadata)
	return &cloned
}

func cloneGenerateResult(result *agentcore.GenerateResult) *agentcore.GenerateResult {
	if result == nil {
		return nil
	}
	cloned := &agentcore.GenerateResult{
		Content:      result.Content,
		FinishReason: result.FinishReason,
		Usage:        result.Usage,
		Artifacts:    append([]agentcore.Artifact(nil), result.Artifacts...),
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
	return cloned
}

func cloneToolSchema(schema agentcore.ToolSchema) agentcore.ToolSchema {
	return agentcore.ToolSchema{
		Name:        schema.Name,
		Description: schema.Description,
		InputSchema: cloneMap(schema.InputSchema),
	}
}

func cloneStreamEvent(event agentcore.StreamEvent) agentcore.StreamEvent {
	return agentcore.StreamEvent{
		Kind:     event.Kind,
		Delta:    event.Delta,
		Metadata: cloneMap(event.Metadata),
	}
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

func cloneValue(v any) any {
	switch typed := v.(type) {
	case nil:
		return nil
	case map[string]any:
		return cloneMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cloneValue(item)
		}
		return out
	default:
		value := reflect.ValueOf(v)
		if value.IsValid() && value.Kind() == reflect.Slice {
			out := make([]any, value.Len())
			for i := 0; i < value.Len(); i++ {
				out[i] = cloneValue(value.Index(i).Interface())
			}
			return out
		}
		return v
	}
}
