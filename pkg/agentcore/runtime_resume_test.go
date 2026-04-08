package agentcore

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/inhere/axon/internal/llm"
	internaltoolsimpl "github.com/inhere/axon/internal/tools"
)

func TestRuntime_Run_ResumeMatrix(t *testing.T) {
	tests := []struct {
		name                 string
		runID                string
		resumeMode           ResumeMode
		instruction          string
		wantProviderMessages int
		wantLogMessages      int
		wantLastRole         string
		wantLastContent      string
	}{
		{
			name:                 "auto without instruction does not append user message",
			runID:                "run-auto-no-input",
			resumeMode:           ResumeAuto,
			instruction:          "",
			wantProviderMessages: 2,
			wantLogMessages:      3,
			wantLastRole:         "assistant",
			wantLastContent:      "done",
		},
		{
			name:                 "auto with instruction appends user message",
			runID:                "run-auto-with-input",
			resumeMode:           ResumeAuto,
			instruction:          "continue",
			wantProviderMessages: 3,
			wantLogMessages:      4,
			wantLastRole:         "assistant",
			wantLastContent:      "done",
		},
		{
			name:                 "from state does not append instruction",
			runID:                "run-from-state",
			resumeMode:           ResumeFromState,
			instruction:          "ignore me",
			wantProviderMessages: 2,
			wantLogMessages:      3,
			wantLastRole:         "assistant",
			wantLastContent:      "done",
		},
		{
			name:                 "with input appends user message",
			runID:                "run-with-input",
			resumeMode:           ResumeWithInput,
			instruction:          "continue",
			wantProviderMessages: 3,
			wantLogMessages:      4,
			wantLastRole:         "assistant",
			wantLastContent:      "done",
		},
		{
			name:                 "with input appends even empty instruction",
			runID:                "run-with-empty-input",
			resumeMode:           ResumeWithInput,
			instruction:          "",
			wantProviderMessages: 3,
			wantLogMessages:      4,
			wantLastRole:         "assistant",
			wantLastContent:      "done",
		},
		{
			name:                 "with input preserves whitespace only instruction",
			runID:                "run-with-whitespace-input",
			resumeMode:           ResumeWithInput,
			instruction:          "   ",
			wantProviderMessages: 3,
			wantLogMessages:      4,
			wantLastRole:         "assistant",
			wantLastContent:      "done",
		},
		{
			name:                 "zero resume mode defaults to auto semantics without instruction",
			runID:                "run-zero-mode-no-input",
			resumeMode:           "",
			instruction:          "",
			wantProviderMessages: 2,
			wantLogMessages:      3,
			wantLastRole:         "assistant",
			wantLastContent:      "done",
		},
		{
			name:                 "zero resume mode defaults to auto semantics with instruction",
			runID:                "run-zero-mode-with-input",
			resumeMode:           "",
			instruction:          "continue",
			wantProviderMessages: 3,
			wantLogMessages:      4,
			wantLastRole:         "assistant",
			wantLastContent:      "done",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newRuntimeTestStorage()
			store.runs[tc.runID] = &RunState{
				RunID:   tc.runID,
				AgentID: "agent-1",
				Status:  RunRunning,
				Step:    1,
			}
			store.messages[tc.runID] = []Message{
				{Role: "user", Content: "first"},
				{Role: "assistant", Content: "second"},
			}
			provider := &runtimeTestProvider{
				results: []*GenerateResult{{
					Content:      "done",
					FinishReason: FinishFinal,
				}},
			}

			rt := New(Config{
				Provider:       provider,
				Storage:        store,
				Tools:          NewToolRegistry(),
				PromptBuilder:  runtimeTestPromptBuilder{},
				ContextBuilder: runtimeTestContextBuilder{},
			})

			result, err := rt.Run(context.Background(), RunInput{
				RunID:       tc.runID,
				Instruction: tc.instruction,
				ResumeMode:  tc.resumeMode,
			})
			if err != nil {
				t.Fatalf("expected resume to succeed, got %v", err)
			}
			if result == nil || result.Steps != 2 {
				t.Fatalf("expected resumed run to increment steps, got %#v", result)
			}
			if len(provider.calls) != 1 || len(provider.calls[0].Messages) != tc.wantProviderMessages {
				t.Fatalf("expected %d provider messages, got %#v", tc.wantProviderMessages, provider.calls)
			}
			if provider.calls[0].Messages[0].Content != "first" || provider.calls[0].Messages[1].Content != "second" {
				t.Fatalf("expected persisted message order to be preserved, got %#v", provider.calls[0].Messages)
			}
			if tc.wantProviderMessages == 3 {
				last := provider.calls[0].Messages[2]
				if last.Role != "user" || last.Content != tc.instruction {
					t.Fatalf("expected appended user message to reach provider, got %#v", provider.calls[0].Messages)
				}
			}
			if len(store.messages[tc.runID]) != tc.wantLogMessages {
				t.Fatalf("expected %d persisted messages, got %#v", tc.wantLogMessages, store.messages[tc.runID])
			}
			last := store.messages[tc.runID][len(store.messages[tc.runID])-1]
			if last.Role != tc.wantLastRole || last.Content != tc.wantLastContent {
				t.Fatalf("unexpected final persisted message, got %#v", last)
			}
		})
	}
}

type runtimeAdapterEchoTool struct{}

func (runtimeAdapterEchoTool) Name() string        { return "echo" }
func (runtimeAdapterEchoTool) Description() string { return "Echo input text" }
func (runtimeAdapterEchoTool) Kind() internaltoolsimpl.Kind {
	return internaltoolsimpl.KindNative
}
func (runtimeAdapterEchoTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{
		Name:        "echo",
		Description: "Echo input text",
		Parameters: llm.SchemaProperty{
			Type: "object",
			Properties: map[string]*llm.SchemaProperty{
				"text": {Type: "string", Description: "text to echo"},
			},
			Required: []string{"text"},
		},
	}
}
func (runtimeAdapterEchoTool) Execute(_ context.Context, args map[string]any) (string, error) {
	text, _ := args["text"].(string)
	return "tool:" + text, nil
}

type runtimeInternalProviderAdapter struct {
	inner llm.Provider
}

func (p runtimeInternalProviderAdapter) Name() string {
	if p.inner == nil {
		return ""
	}
	return p.inner.Name()
}

func (p runtimeInternalProviderAdapter) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResult, error) {
	resp, err := p.inner.Complete(ctx, &llm.CompletionRequest{
		Model:       req.Model,
		Messages:    runtimeToInternalMessages(req.Messages),
		Tools:       runtimeToInternalTools(req.Tools),
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		System:      req.System,
	})
	if err != nil {
		return nil, &ProviderError{Err: err}
	}

	result := &GenerateResult{
		Content:   resp.Content,
		ToolCalls: runtimeToAgentcoreToolCalls(resp.ToolCalls),
		Usage: Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		},
	}
	switch resp.FinishReason {
	case "", "stop":
		result.FinishReason = FinishFinal
	case "tool_calls":
		result.FinishReason = FinishToolCalls
	case "length":
		return nil, &ProviderError{Err: fmt.Errorf("unsupported finish reason: %s", resp.FinishReason)}
	default:
		return nil, &ProviderError{Err: fmt.Errorf("unknown finish reason: %s", resp.FinishReason)}
	}
	if err := result.Validate(); err != nil {
		return nil, &ProviderError{Err: err}
	}
	return result, nil
}

type runtimeInternalToolsAdapter struct {
	inner *internaltoolsimpl.Registry
}

func (r runtimeInternalToolsAdapter) ListTools(context.Context) ([]ToolSchema, error) {
	if r.inner == nil {
		return nil, nil
	}
	schemas := r.inner.Schemas()
	out := make([]ToolSchema, len(schemas))
	for i, schema := range schemas {
		out[i] = ToolSchema{
			Name:        schema.Name,
			Description: schema.Description,
			InputSchema: runtimeFromSchemaProperty(schema.Parameters),
		}
	}
	return out, nil
}

func (r runtimeInternalToolsAdapter) ExecuteTool(ctx context.Context, call ToolCall) (*ToolResult, error) {
	content, err := r.inner.Execute(ctx, call.Name, cloneMetadata(call.Arguments))
	if err != nil {
		return nil, err
	}
	return &ToolResult{
		CallID:  call.CallID,
		Name:    call.Name,
		Content: content,
	}, nil
}

func runtimeToInternalMessages(messages []Message) []llm.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]llm.Message, len(messages))
	for i, msg := range messages {
		out[i] = llm.Message{
			Role:    msg.Role,
			Content: msg.Content,
		}
		if msg.ToolCall != nil {
			out[i].ToolCall = &llm.ToolCall{
				ID:        msg.ToolCall.CallID,
				Name:      msg.ToolCall.Name,
				Arguments: cloneMetadata(msg.ToolCall.Arguments),
			}
		}
		if msg.ToolResult != nil {
			callID := msg.ToolResult.CallID
			if callID == "" && msg.ToolCall != nil {
				callID = msg.ToolCall.CallID
			}
			out[i].ToolResult = &llm.ToolResult{
				ToolCallID: callID,
				Content:    msg.ToolResult.Content,
			}
		}
	}
	return out
}

func runtimeToInternalTools(tools []ToolSchema) []llm.ToolSchema {
	if len(tools) == 0 {
		return nil
	}
	out := make([]llm.ToolSchema, len(tools))
	for i, tool := range tools {
		out[i] = llm.ToolSchema{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  runtimeToSchemaProperty(tool.InputSchema),
		}
	}
	return out
}

func runtimeToSchemaProperty(schema map[string]any) llm.SchemaProperty {
	prop := llm.SchemaProperty{}
	if len(schema) == 0 {
		return prop
	}
	if value, ok := schema["type"].(string); ok {
		prop.Type = value
	}
	if value, ok := schema["description"].(string); ok {
		prop.Description = value
	}
	switch enum := schema["enum"].(type) {
	case []string:
		prop.Enum = append([]string(nil), enum...)
	case []any:
		prop.Enum = make([]string, 0, len(enum))
		for _, item := range enum {
			if text, ok := item.(string); ok {
				prop.Enum = append(prop.Enum, text)
			}
		}
	}
	switch required := schema["required"].(type) {
	case []string:
		prop.Required = append([]string(nil), required...)
	case []any:
		prop.Required = make([]string, 0, len(required))
		for _, item := range required {
			if text, ok := item.(string); ok {
				prop.Required = append(prop.Required, text)
			}
		}
	}
	if properties, ok := schema["properties"].(map[string]any); ok {
		prop.Properties = make(map[string]*llm.SchemaProperty, len(properties))
		for name, raw := range properties {
			if nested, ok := raw.(map[string]any); ok {
				child := runtimeToSchemaProperty(nested)
				prop.Properties[name] = &child
			}
		}
	}
	return prop
}

func runtimeFromSchemaProperty(prop llm.SchemaProperty) map[string]any {
	out := map[string]any{}
	if prop.Type != "" {
		out["type"] = prop.Type
	}
	if prop.Description != "" {
		out["description"] = prop.Description
	}
	if len(prop.Enum) > 0 {
		enum := make([]any, len(prop.Enum))
		for i, item := range prop.Enum {
			enum[i] = item
		}
		out["enum"] = enum
	}
	if len(prop.Required) > 0 {
		required := make([]any, len(prop.Required))
		for i, item := range prop.Required {
			required[i] = item
		}
		out["required"] = required
	}
	if len(prop.Properties) > 0 {
		properties := make(map[string]any, len(prop.Properties))
		for name, child := range prop.Properties {
			if child == nil {
				continue
			}
			properties[name] = runtimeFromSchemaProperty(*child)
		}
		out["properties"] = properties
	}
	return out
}

func runtimeToAgentcoreToolCalls(calls []llm.ToolCall) []ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]ToolCall, len(calls))
	for i, call := range calls {
		out[i] = ToolCall{
			CallID:    call.ID,
			Name:      call.Name,
			Arguments: cloneMetadata(call.Arguments),
		}
	}
	return out
}

func TestRuntime_Run_ResumeFromState_WithFilesStorageAndAdapters(t *testing.T) {
	runID := "resume-with-adapters"
	store := newDefaultFileStorage(t.TempDir())

	reg := internaltoolsimpl.NewRegistry()
	reg.MustRegister(runtimeAdapterEchoTool{})

	yieldAfterSecondModel := &runtimeTestHook{}
	beforeModelCalls := 0
	yieldAfterSecondModel.beforeModel = func(ctrl *Control) {
		beforeModelCalls++
		if beforeModelCalls == 2 {
			ctrl.Yield("await_resume", map[string]any{"step": "after_tool"})
		}
	}

	firstRuntime := New(Config{
		Provider: runtimeInternalProviderAdapter{
			inner: llm.NewMockProvider("internal-mock").
				QueueToolCall("echo", map[string]any{"text": "persisted"}),
		},
		Storage:        store,
		Tools:          runtimeInternalToolsAdapter{inner: reg},
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{yieldAfterSecondModel},
	})

	result, err := firstRuntime.Run(context.Background(), RunInput{
		RunID:       runID,
		AgentID:     "agent-1",
		Instruction: "use a tool and pause",
	})
	if err != nil {
		t.Fatalf("expected first run to yield cleanly, got %v", err)
	}
	if result.Status != RunYielded || result.Yield == nil || result.Yield.Reason != "await_resume" {
		t.Fatalf("expected yielded result before second model call, got %#v", result)
	}

	state, err := store.LoadRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("expected persisted run state, got %v", err)
	}
	if state.Status != RunYielded || state.Step != 1 {
		t.Fatalf("expected yielded state after one model step, got %#v", state)
	}

	messages, err := store.LoadMessages(context.Background(), runID)
	if err != nil {
		t.Fatalf("expected persisted messages, got %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("expected user, assistant tool-call, and tool messages after pause, got %#v", messages)
	}
	if messages[1].Role != "assistant" || messages[1].ToolCall == nil || messages[1].ToolCall.CallID != "call-echo" {
		t.Fatalf("expected persisted assistant tool-call message from adapter chain, got %#v", messages[1])
	}
	if messages[2].Role != "tool" || messages[2].ToolResult == nil || messages[2].ToolResult.Content != "tool:persisted" {
		t.Fatalf("expected persisted tool result content, got %#v", messages[2])
	}

	resumeProvider := llm.NewMockProvider("internal-mock").QueueText("final after resume")
	resumeRuntime := New(Config{
		Provider:       runtimeInternalProviderAdapter{inner: resumeProvider},
		Storage:        store,
		Tools:          runtimeInternalToolsAdapter{inner: reg},
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
	})

	result, err = resumeRuntime.Run(context.Background(), RunInput{
		RunID:      runID,
		ResumeMode: ResumeFromState,
	})
	if err != nil {
		t.Fatalf("expected resume to complete, got %v", err)
	}
	if result.Status != RunCompleted || result.Output != "final after resume" {
		t.Fatalf("expected completed resumed result, got %#v", result)
	}
	if result.Steps != 2 {
		t.Fatalf("expected resumed run to continue from step 1 to 2, got %#v", result)
	}

	providerCalls := resumeProvider.Calls()
	if len(providerCalls) != 1 {
		t.Fatalf("expected one resumed provider call, got %d", len(providerCalls))
	}
	if len(providerCalls[0].Messages) != 3 {
		t.Fatalf("expected resumed provider to receive prior user, assistant tool-call, and tool messages, got %#v", providerCalls[0].Messages)
	}
	if providerCalls[0].Messages[1].Role != "assistant" || providerCalls[0].Messages[1].ToolCall == nil || providerCalls[0].Messages[1].ToolCall.ID != "call-echo" {
		t.Fatalf("expected resumed provider to receive assistant tool-call message, got %#v", providerCalls[0].Messages[1])
	}
	if providerCalls[0].Messages[2].ToolResult == nil || providerCalls[0].Messages[2].ToolResult.Content != "tool:persisted" {
		t.Fatalf("expected resumed provider to receive persisted tool result, got %#v", providerCalls[0].Messages[2])
	}
	if len(providerCalls[0].Tools) != 1 || providerCalls[0].Tools[0].Name != "echo" {
		t.Fatalf("expected resumed provider to receive adapted tool schema, got %#v", providerCalls[0].Tools)
	}

	finalState, err := store.LoadRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("expected final persisted state, got %v", err)
	}
	if finalState.Status != RunCompleted || finalState.Step != 2 {
		t.Fatalf("expected completed final state, got %#v", finalState)
	}
	finalMessages, err := store.LoadMessages(context.Background(), runID)
	if err != nil {
		t.Fatalf("expected final persisted messages, got %v", err)
	}
	if len(finalMessages) != 4 || finalMessages[3].Role != "assistant" || finalMessages[3].Content != "final after resume" {
		t.Fatalf("expected assistant output appended after resume, got %#v", finalMessages)
	}
}

func TestRuntime_Run_ResumeAutoAfterYieldWithoutInstruction_ReusesState(t *testing.T) {
	runID := "resume-auto-after-yield"
	store := newRuntimeTestStorage()
	hook := &runtimeTestHook{}
	beforeModelCalls := 0
	hook.beforeModel = func(ctrl *Control) {
		beforeModelCalls++
		if beforeModelCalls == 2 {
			ctrl.Yield("await_resume", map[string]any{"step": "after_tool"})
		}
	}

	firstProvider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls: []ToolCall{{
				CallID:    "call-1",
				Name:      "echo",
				Arguments: map[string]any{"msg": "ping"},
			}},
			FinishReason: FinishToolCalls,
		}},
	}
	registry := NewToolRegistry()
	registry.Register(&runtimeTestTool{
		name:   "echo",
		result: &ToolResult{Content: "tool:ping"},
	})

	firstResult, err := New(Config{
		Provider:       firstProvider,
		Storage:        store,
		Tools:          registry,
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{hook},
	}).Run(context.Background(), RunInput{
		RunID:       runID,
		AgentID:     "agent-1",
		Instruction: "resume me later",
	})
	if err != nil {
		t.Fatalf("expected first run to yield, got %v", err)
	}
	if firstResult.Status != RunYielded || firstResult.Yield == nil || firstResult.Yield.Reason != "await_resume" {
		t.Fatalf("expected yielded first result, got %#v", firstResult)
	}

	resumeProvider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done after auto resume",
			FinishReason: FinishFinal,
		}},
	}
	resumed, err := New(Config{
		Provider:       resumeProvider,
		Storage:        store,
		Tools:          registry,
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:      runID,
		ResumeMode: ResumeAuto,
	})
	if err != nil {
		t.Fatalf("expected auto resume to complete, got %v", err)
	}
	if resumed.Status != RunCompleted || resumed.Output != "done after auto resume" {
		t.Fatalf("expected completed resumed result, got %#v", resumed)
	}

	if len(resumeProvider.calls) != 1 {
		t.Fatalf("resume provider calls = %d, want 1", len(resumeProvider.calls))
	}
	if len(resumeProvider.calls[0].Messages) != 3 {
		t.Fatalf("expected auto resume to reuse prior transcript only, got %#v", resumeProvider.calls[0].Messages)
	}
	if resumeProvider.calls[0].Messages[0].Role != "user" || resumeProvider.calls[0].Messages[0].Content != "resume me later" {
		t.Fatalf("expected original user instruction to remain authoritative, got %#v", resumeProvider.calls[0].Messages[0])
	}

	finalMessages := store.messages[runID]
	if len(finalMessages) != 4 {
		t.Fatalf("expected yielded transcript plus final assistant output, got %#v", finalMessages)
	}
	userCount := 0
	for _, msg := range finalMessages {
		if msg.Role == "user" && msg.Content == "resume me later" {
			userCount++
		}
	}
	if userCount != 1 {
		t.Fatalf("expected auto resume not to append a duplicate user message, got %#v", finalMessages)
	}
}

func TestRuntime_Run_ResumeFromStateCanYieldAgainWithoutForkingTranscript(t *testing.T) {
	runID := "resume-from-state-yield-again"
	store := newRuntimeTestStorage()

	firstHook := &runtimeTestHook{}
	firstBeforeModelCalls := 0
	firstHook.beforeModel = func(ctrl *Control) {
		firstBeforeModelCalls++
		if firstBeforeModelCalls == 2 {
			ctrl.Yield("await_resume", map[string]any{"step": "after_tool"})
		}
	}

	registry := NewToolRegistry()
	registry.Register(&runtimeTestTool{
		name:   "echo",
		result: &ToolResult{Content: "tool:ping"},
	})

	firstProvider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls: []ToolCall{{
				CallID:    "call-1",
				Name:      "echo",
				Arguments: map[string]any{"msg": "ping"},
			}},
			FinishReason: FinishToolCalls,
		}},
	}

	firstResult, err := New(Config{
		Provider:       firstProvider,
		Storage:        store,
		Tools:          registry,
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{firstHook},
	}).Run(context.Background(), RunInput{
		RunID:       runID,
		AgentID:     "agent-1",
		Instruction: "resume me twice",
	})
	if err != nil {
		t.Fatalf("expected first run to yield, got %v", err)
	}
	if firstResult.Status != RunYielded || firstResult.Yield == nil || firstResult.Yield.Reason != "await_resume" {
		t.Fatalf("expected yielded first result, got %#v", firstResult)
	}

	secondHook := &runtimeTestHook{}
	secondHook.beforeModel = func(ctrl *Control) {
		ctrl.Yield("await_resume_again", map[string]any{"step": "before_model"})
	}
	secondProvider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "should-not-run",
			FinishReason: FinishFinal,
		}},
	}

	secondResult, err := New(Config{
		Provider:       secondProvider,
		Storage:        store,
		Tools:          registry,
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{secondHook},
	}).Run(context.Background(), RunInput{
		RunID:      runID,
		ResumeMode: ResumeFromState,
	})
	if err != nil {
		t.Fatalf("expected second resume to yield again, got %v", err)
	}
	if secondResult.Status != RunYielded || secondResult.Yield == nil || secondResult.Yield.Reason != "await_resume_again" {
		t.Fatalf("expected second yielded result, got %#v", secondResult)
	}
	if len(secondProvider.calls) != 0 {
		t.Fatalf("expected provider not to run on second yield, got %#v", secondProvider.calls)
	}

	resumeProvider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done after repeated resume",
			FinishReason: FinishFinal,
		}},
	}
	resumed, err := New(Config{
		Provider:       resumeProvider,
		Storage:        store,
		Tools:          registry,
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:      runID,
		ResumeMode: ResumeFromState,
	})
	if err != nil {
		t.Fatalf("expected final resume to complete, got %v", err)
	}
	if resumed.Status != RunCompleted || resumed.Output != "done after repeated resume" {
		t.Fatalf("expected completed result after repeated resume, got %#v", resumed)
	}
	if len(resumeProvider.calls) != 1 || len(resumeProvider.calls[0].Messages) != 3 {
		t.Fatalf("expected final resume to reuse yielded transcript only, got %#v", resumeProvider.calls)
	}

	finalMessages := store.messages[runID]
	if len(finalMessages) != 4 {
		t.Fatalf("expected yielded transcript plus final answer without duplicates, got %#v", finalMessages)
	}
	if finalMessages[0].Role != "user" || finalMessages[1].Role != "assistant" || finalMessages[2].Role != "tool" || finalMessages[3].Role != "assistant" {
		t.Fatalf("unexpected repeated-resume transcript shape, got %#v", finalMessages)
	}
}

func TestRuntime_Run_ResumeWithInputThenYieldAgainKeepsAppendedInputAuthoritative(t *testing.T) {
	runID := "resume-with-input-yield-again"
	store := newRuntimeTestStorage()

	firstHook := &runtimeTestHook{}
	firstBeforeModelCalls := 0
	firstHook.beforeModel = func(ctrl *Control) {
		firstBeforeModelCalls++
		if firstBeforeModelCalls == 2 {
			ctrl.Yield("await_human", map[string]any{"step": "after_tool"})
		}
	}

	registry := NewToolRegistry()
	registry.Register(&runtimeTestTool{
		name:   "echo",
		result: &ToolResult{Content: "tool:ping"},
	})

	firstProvider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls: []ToolCall{{
				CallID:    "call-1",
				Name:      "echo",
				Arguments: map[string]any{"msg": "ping"},
			}},
			FinishReason: FinishToolCalls,
		}},
	}

	_, err := New(Config{
		Provider:       firstProvider,
		Storage:        store,
		Tools:          registry,
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{firstHook},
	}).Run(context.Background(), RunInput{
		RunID:       runID,
		AgentID:     "agent-1",
		Instruction: "wait for my follow-up",
	})
	if err != nil {
		t.Fatalf("expected first run to yield, got %v", err)
	}

	secondHook := &runtimeTestHook{}
	secondHook.beforeModel = func(ctrl *Control) {
		ctrl.Yield("await_confirmation", map[string]any{"step": "before_model"})
	}
	secondProvider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "should-not-run",
			FinishReason: FinishFinal,
		}},
	}

	secondResult, err := New(Config{
		Provider:       secondProvider,
		Storage:        store,
		Tools:          registry,
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{secondHook},
	}).Run(context.Background(), RunInput{
		RunID:       runID,
		Instruction: "continue with my approved input",
		ResumeMode:  ResumeWithInput,
	})
	if err != nil {
		t.Fatalf("expected second run to yield again, got %v", err)
	}
	if secondResult.Status != RunYielded || secondResult.Yield == nil || secondResult.Yield.Reason != "await_confirmation" {
		t.Fatalf("expected second yielded result, got %#v", secondResult)
	}
	if len(secondProvider.calls) != 0 {
		t.Fatalf("expected provider not to run before second yield, got %#v", secondProvider.calls)
	}

	resumeProvider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done after human follow-up",
			FinishReason: FinishFinal,
		}},
	}
	resumed, err := New(Config{
		Provider:       resumeProvider,
		Storage:        store,
		Tools:          registry,
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:      runID,
		ResumeMode: ResumeFromState,
	})
	if err != nil {
		t.Fatalf("expected final resume to complete, got %v", err)
	}
	if resumed.Status != RunCompleted || resumed.Output != "done after human follow-up" {
		t.Fatalf("expected final completed result, got %#v", resumed)
	}
	if len(resumeProvider.calls) != 1 || len(resumeProvider.calls[0].Messages) != 4 {
		t.Fatalf("expected final resume to include appended human input exactly once, got %#v", resumeProvider.calls)
	}
	lastInput := resumeProvider.calls[0].Messages[3]
	if lastInput.Role != "user" || lastInput.Content != "continue with my approved input" {
		t.Fatalf("expected appended human input to remain authoritative before final answer, got %#v", resumeProvider.calls[0].Messages)
	}

	finalMessages := store.messages[runID]
	if len(finalMessages) != 5 {
		t.Fatalf("expected transcript to keep tool transcript, appended user input, and final answer, got %#v", finalMessages)
	}
	if finalMessages[3].Role != "user" || finalMessages[3].Content != "continue with my approved input" {
		t.Fatalf("expected appended user input to remain in authority transcript, got %#v", finalMessages)
	}
	if finalMessages[4].Role != "assistant" || finalMessages[4].Content != "done after human follow-up" {
		t.Fatalf("expected final assistant answer after appended user input, got %#v", finalMessages)
	}
}

func TestRuntime_Run_RejectsResumeForCompletedOrFailed(t *testing.T) {
	tests := []struct {
		name   string
		runID  string
		status RunStatus
	}{
		{name: "completed", runID: "run-complete", status: RunCompleted},
		{name: "failed", runID: "run-failed", status: RunFailed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newRuntimeTestStorage()
			store.runs[tc.runID] = &RunState{
				RunID:   tc.runID,
				AgentID: "agent-1",
				Status:  tc.status,
			}
			store.messages[tc.runID] = []Message{{Role: "assistant", Content: "terminal"}}
			provider := &runtimeTestProvider{
				results: []*GenerateResult{{
					Content:      "should-not-run",
					FinishReason: FinishFinal,
				}},
			}

			rt := New(Config{
				Provider:       provider,
				Storage:        store,
				Tools:          NewToolRegistry(),
				PromptBuilder:  runtimeTestPromptBuilder{},
				ContextBuilder: runtimeTestContextBuilder{},
			})

			result, err := rt.Run(context.Background(), RunInput{
				RunID:       tc.runID,
				Instruction: "continue",
				ResumeMode:  ResumeFromState,
			})
			if !errors.Is(err, ErrRunNotResumable) {
				t.Fatalf("expected terminal resume to be rejected, got result=%#v err=%v", result, err)
			}
			if len(provider.calls) != 0 {
				t.Fatalf("expected provider not to be called for terminal resume, got %#v", provider.calls)
			}
		})
	}
}

func TestRuntime_Run_RejectsTerminalResumeAcrossResumeModes(t *testing.T) {
	tests := []struct {
		name       string
		status     RunStatus
		resumeMode ResumeMode
	}{
		{name: "completed_auto", status: RunCompleted, resumeMode: ResumeAuto},
		{name: "completed_with_input", status: RunCompleted, resumeMode: ResumeWithInput},
		{name: "failed_auto", status: RunFailed, resumeMode: ResumeAuto},
		{name: "failed_with_input", status: RunFailed, resumeMode: ResumeWithInput},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runID := "run-terminal-" + tc.name
			store := newRuntimeTestStorage()
			store.runs[runID] = &RunState{
				RunID:   runID,
				AgentID: "agent-1",
				Status:  tc.status,
			}
			store.messages[runID] = []Message{{Role: "assistant", Content: "terminal"}}
			provider := &runtimeTestProvider{
				results: []*GenerateResult{{
					Content:      "should-not-run",
					FinishReason: FinishFinal,
				}},
			}

			result, err := New(Config{
				Provider:       provider,
				Storage:        store,
				Tools:          NewToolRegistry(),
				PromptBuilder:  runtimeTestPromptBuilder{},
				ContextBuilder: runtimeTestContextBuilder{},
			}).Run(context.Background(), RunInput{
				RunID:       runID,
				Instruction: "try to continue terminal run",
				ResumeMode:  tc.resumeMode,
			})
			if !errors.Is(err, ErrRunNotResumable) {
				t.Fatalf("expected terminal resume to be rejected, got result=%#v err=%v", result, err)
			}
			if len(provider.calls) != 0 {
				t.Fatalf("expected provider not to be called for terminal resume, got %#v", provider.calls)
			}
			if len(store.messages[runID]) != 1 || store.messages[runID][0].Content != "terminal" {
				t.Fatalf("expected authority transcript to remain unchanged, got %#v", store.messages[runID])
			}
		})
	}
}

func TestRuntime_Run_ResumeMergesInputMetadataIntoStateAndProviderRequest(t *testing.T) {
	runID := "run-resume-metadata-merge"
	store := newRuntimeTestStorage()
	store.runs[runID] = &RunState{
		RunID:   runID,
		AgentID: "agent-1",
		Status:  RunYielded,
		Step:    1,
		Metadata: map[string]any{
			"existing": "keep",
			"override": "old",
		},
	}
	store.messages[runID] = []Message{
		{Role: "user", Content: "persisted question"},
		{Role: "assistant", Content: "persisted draft"},
	}
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done with merged metadata",
			FinishReason: FinishFinal,
		}},
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          NewToolRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:      runID,
		ResumeMode: ResumeFromState,
		Metadata: map[string]any{
			"override": "new",
			"added":    "fresh",
		},
	})
	if err != nil {
		t.Fatalf("expected resumed run to succeed, got %v", err)
	}
	if result.Status != RunCompleted || result.Output != "done with merged metadata" {
		t.Fatalf("expected completed result, got %#v", result)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(provider.calls))
	}
	if got := provider.calls[0].Metadata["existing"]; got != "keep" {
		t.Fatalf("expected provider request to keep existing metadata, got %#v", provider.calls[0].Metadata)
	}
	if got := provider.calls[0].Metadata["override"]; got != "new" {
		t.Fatalf("expected provider request to use overridden metadata, got %#v", provider.calls[0].Metadata)
	}
	if got := provider.calls[0].Metadata["added"]; got != "fresh" {
		t.Fatalf("expected provider request to include added metadata, got %#v", provider.calls[0].Metadata)
	}

	saved := store.runs[runID]
	if saved == nil {
		t.Fatal("expected saved state after resume")
	}
	if got := saved.Metadata["existing"]; got != "keep" {
		t.Fatalf("expected saved state to keep existing metadata, got %#v", saved.Metadata)
	}
	if got := saved.Metadata["override"]; got != "new" {
		t.Fatalf("expected saved state to override metadata, got %#v", saved.Metadata)
	}
	if got := saved.Metadata["added"]; got != "fresh" {
		t.Fatalf("expected saved state to include added metadata, got %#v", saved.Metadata)
	}
}

func TestRuntime_Run_TerminalResumeDoesNotMergeInputMetadata(t *testing.T) {
	runID := "run-terminal-resume-metadata"
	store := newRuntimeTestStorage()
	store.runs[runID] = &RunState{
		RunID:   runID,
		AgentID: "agent-1",
		Status:  RunCompleted,
		Metadata: map[string]any{
			"existing": "keep",
		},
	}
	store.messages[runID] = []Message{{Role: "assistant", Content: "terminal"}}
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "should-not-run",
			FinishReason: FinishFinal,
		}},
	}

	_, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          NewToolRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:      runID,
		ResumeMode: ResumeWithInput,
		Metadata: map[string]any{
			"override": "should-not-stick",
		},
		Instruction: "try to continue",
	})
	if !errors.Is(err, ErrRunNotResumable) {
		t.Fatalf("expected terminal resume to be rejected, got %v", err)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("expected provider not to be called, got %#v", provider.calls)
	}
	saved := store.runs[runID]
	if saved == nil {
		t.Fatal("expected saved terminal state to stay present")
	}
	if len(saved.Metadata) != 1 || saved.Metadata["existing"] != "keep" {
		t.Fatalf("expected terminal state metadata to stay unchanged, got %#v", saved.Metadata)
	}
}

func TestRuntime_Run_ResumePreservesMutatedToolResultMetadataAndArtifacts(t *testing.T) {
	runID := "run-resume-mutated-tool-result"
	store := newRuntimeTestStorage()
	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{
		name: "lookup",
		result: &ToolResult{
			Content: "tool-output",
		},
	})

	yieldHook := &runtimeTestHook{}
	beforeModelCalls := 0
	yieldHook.beforeModel = func(ctrl *Control) {
		beforeModelCalls++
		if beforeModelCalls == 2 {
			ctrl.Yield("await_resume", map[string]any{"step": "after_tool"})
		}
	}
	yieldHook.afterToolEvent = func(event *ToolEvent, _ *Control) {
		event.Result = &ToolResult{
			CallID:  "call-1",
			Name:    "lookup",
			Content: "mutated-output",
			Artifacts: []Artifact{{
				Kind: "file",
				URI:  "artifact://mutated.txt",
			}},
			Metadata: map[string]any{
				"source": "after_tool",
			},
		}
	}

	firstProvider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls: []ToolCall{{
				CallID: "call-1",
				Name:   "lookup",
				Arguments: map[string]any{
					"query": "original",
				},
			}},
			FinishReason: FinishToolCalls,
		}},
	}

	firstResult, err := New(Config{
		Provider:       firstProvider,
		Storage:        store,
		Tools:          tools,
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{yieldHook},
	}).Run(context.Background(), RunInput{
		RunID:       runID,
		AgentID:     "agent-1",
		Instruction: "collect mutated artifact and resume later",
	})
	if err != nil {
		t.Fatalf("expected first run to yield, got %v", err)
	}
	if firstResult.Status != RunYielded || firstResult.Yield == nil || firstResult.Yield.Reason != "await_resume" {
		t.Fatalf("expected yielded first result, got %#v", firstResult)
	}

	persisted := store.messages[runID]
	if len(persisted) != 3 {
		t.Fatalf("expected yielded transcript with user, assistant tool-call, and tool result, got %#v", persisted)
	}
	if persisted[2].ToolResult == nil || persisted[2].ToolResult.Metadata["source"] != "after_tool" {
		t.Fatalf("expected persisted tool result metadata to survive before resume, got %#v", persisted[2].ToolResult)
	}
	if len(persisted[2].ToolResult.Artifacts) != 1 || persisted[2].ToolResult.Artifacts[0].URI != "artifact://mutated.txt" {
		t.Fatalf("expected persisted tool result artifacts to survive before resume, got %#v", persisted[2].ToolResult)
	}

	resumeProvider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done after resume with mutated tool result",
			FinishReason: FinishFinal,
		}},
	}

	resumed, err := New(Config{
		Provider:       resumeProvider,
		Storage:        store,
		Tools:          tools,
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:      runID,
		ResumeMode: ResumeFromState,
	})
	if err != nil {
		t.Fatalf("expected resumed run to complete, got %v", err)
	}
	if resumed.Status != RunCompleted || resumed.Output != "done after resume with mutated tool result" {
		t.Fatalf("expected completed resumed result, got %#v", resumed)
	}
	if len(resumeProvider.calls) != 1 || len(resumeProvider.calls[0].Messages) != 3 {
		t.Fatalf("expected resumed provider to receive yielded transcript, got %#v", resumeProvider.calls)
	}
	toolMsg := resumeProvider.calls[0].Messages[2]
	if toolMsg.ToolResult == nil || toolMsg.ToolResult.Content != "mutated-output" {
		t.Fatalf("expected resumed provider to receive mutated tool result, got %#v", toolMsg)
	}
	if toolMsg.ToolResult.Metadata["source"] != "after_tool" {
		t.Fatalf("expected resumed provider to receive mutated tool result metadata, got %#v", toolMsg.ToolResult)
	}
	if len(toolMsg.ToolResult.Artifacts) != 1 || toolMsg.ToolResult.Artifacts[0].URI != "artifact://mutated.txt" {
		t.Fatalf("expected resumed provider to receive mutated tool artifacts, got %#v", toolMsg.ToolResult)
	}

	saved := store.runs[runID]
	if saved == nil || len(saved.Artifacts) != 1 || saved.Artifacts[0].URI != "artifact://mutated.txt" {
		t.Fatalf("expected run state artifacts to remain authoritative across resume, got %#v", saved)
	}
}

func TestRuntime_Run_ResumeFromOnErrorYieldReusesAuthoritativeTranscript(t *testing.T) {
	store := newRuntimeTestStorage()
	firstProvider := &runtimeTestProvider{
		errs: []error{errors.New("provider boom")},
	}
	hook := &runtimeTestHook{
		onError: func(ctrl *Control) {
			ctrl.Yield("recoverable_error", map[string]any{"stage": "on_error"})
		},
	}

	firstResult, err := New(Config{
		Provider:       firstProvider,
		Storage:        store,
		Tools:          NewToolRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{hook},
	}).Run(context.Background(), RunInput{
		RunID:       "run-on-error-yield-resume",
		AgentID:     "agent-1",
		Instruction: "recover if provider fails",
	})
	if err != nil {
		t.Fatalf("expected on-error yield to override provider failure, got %v", err)
	}
	if firstResult.Status != RunYielded || firstResult.Yield == nil || firstResult.Yield.Reason != "recoverable_error" {
		t.Fatalf("expected yielded result from on-error control, got %#v", firstResult)
	}

	resumeProvider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done after recovering from provider failure",
			FinishReason: FinishFinal,
		}},
	}

	resumed, err := New(Config{
		Provider:       resumeProvider,
		Storage:        store,
		Tools:          NewToolRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:      "run-on-error-yield-resume",
		ResumeMode: ResumeFromState,
	})
	if err != nil {
		t.Fatalf("expected resume after on-error yield to complete, got %v", err)
	}
	if resumed.Status != RunCompleted || resumed.Output != "done after recovering from provider failure" {
		t.Fatalf("expected completed resumed result, got %#v", resumed)
	}
	if len(resumeProvider.calls) != 1 || len(resumeProvider.calls[0].Messages) != 1 {
		t.Fatalf("expected resume to reuse only the authoritative user transcript, got %#v", resumeProvider.calls)
	}
	if resumeProvider.calls[0].Messages[0].Role != "user" || resumeProvider.calls[0].Messages[0].Content != "recover if provider fails" {
		t.Fatalf("expected resume to reuse original user input only, got %#v", resumeProvider.calls[0].Messages)
	}

	finalMessages := store.messages["run-on-error-yield-resume"]
	if len(finalMessages) != 2 {
		t.Fatalf("expected resumed authority log to contain user plus final answer, got %#v", finalMessages)
	}
	if finalMessages[0].Role != "user" || finalMessages[1].Role != "assistant" {
		t.Fatalf("unexpected resumed authority transcript shape, got %#v", finalMessages)
	}
}

func TestRuntime_Run_ResumeFromPersistOutputOnErrorYieldDoesNotExposePhantomAssistant(t *testing.T) {
	store := newRuntimeTestStorage()
	store.appendErr = errors.New("append failed")
	store.appendErrOnCall = 2
	firstProvider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done",
			FinishReason: FinishFinal,
		}},
	}
	hook := &runtimeTestHook{
		onError: func(ctrl *Control) {
			ctrl.Yield("recover_after_persist_output", map[string]any{"stage": "on_error"})
		},
	}

	firstResult, err := New(Config{
		Provider:       firstProvider,
		Storage:        store,
		Tools:          NewToolRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{hook},
	}).Run(context.Background(), RunInput{
		RunID:       "run-persist-output-yield-resume",
		AgentID:     "agent-1",
		Instruction: "trigger append failure",
	})
	if err != nil {
		t.Fatalf("expected on-error yield to recover persist-output failure, got %v", err)
	}
	if firstResult.Status != RunYielded || firstResult.Yield == nil || firstResult.Yield.Reason != "recover_after_persist_output" {
		t.Fatalf("expected yielded result after persist-output failure, got %#v", firstResult)
	}

	store.appendErr = nil
	store.appendErrOnCall = 0
	resumeProvider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done after retry",
			FinishReason: FinishFinal,
		}},
	}

	resumed, err := New(Config{
		Provider:       resumeProvider,
		Storage:        store,
		Tools:          NewToolRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:      "run-persist-output-yield-resume",
		ResumeMode: ResumeFromState,
	})
	if err != nil {
		t.Fatalf("expected resume after persist-output on-error yield to complete, got %v", err)
	}
	if resumed.Status != RunCompleted || resumed.Output != "done after retry" {
		t.Fatalf("expected completed resumed result, got %#v", resumed)
	}
	if len(resumeProvider.calls) != 1 || len(resumeProvider.calls[0].Messages) != 1 {
		t.Fatalf("expected resume to reuse only authoritative user transcript, got %#v", resumeProvider.calls)
	}
	if resumeProvider.calls[0].Messages[0].Role != "user" || resumeProvider.calls[0].Messages[0].Content != "trigger append failure" {
		t.Fatalf("expected resume to avoid phantom assistant output, got %#v", resumeProvider.calls[0].Messages)
	}

	finalMessages := store.messages["run-persist-output-yield-resume"]
	if len(finalMessages) != 2 {
		t.Fatalf("expected final authority log to contain user plus retried assistant answer, got %#v", finalMessages)
	}
	if finalMessages[1].Role != "assistant" || finalMessages[1].Content != "done after retry" {
		t.Fatalf("expected retried assistant answer to be the only persisted final output, got %#v", finalMessages)
	}
}
