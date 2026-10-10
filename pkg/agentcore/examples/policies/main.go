package main

import (
	"context"
	"errors"
	"github.com/inhere/suag/pkg/agentcore/tools"
	"log"
	"os"
	"path/filepath"

	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/mock"
	"github.com/inhere/suag/pkg/agentcore/storage/files"
)

type resumeWithoutAppendingPolicy struct {
	agentcore.DefaultResumePolicy
}

func (resumeWithoutAppendingPolicy) ShouldAppendInstruction(agentcore.RunInput, *agentcore.RunState) bool {
	return false
}

func (resumeWithoutAppendingPolicy) MergeMetadata(stateMetadata, inputMetadata map[string]any) map[string]any {
	merged := cloneMetadata(stateMetadata)
	if merged == nil {
		merged = make(map[string]any)
	}
	for k, v := range inputMetadata {
		merged[k] = v
	}
	merged["resume_policy"] = "suppress_append"
	return merged
}

type permissiveBatchToolPolicy struct{}

func (permissiveBatchToolPolicy) ValidateToolCalls(_ context.Context, _ *agentcore.RunState, _ []agentcore.ToolCall) error {
	return nil
}

func (permissiveBatchToolPolicy) HandleToolError(_ context.Context, _ *agentcore.RunState, call agentcore.ToolCall, err error) agentcore.ToolErrorDecision {
	return agentcore.ToolErrorDecision{
		Result: &agentcore.ToolResult{
			CallID:  call.CallID,
			Name:    call.Name,
			Content: "tool policy fallback: " + err.Error(),
			Metadata: map[string]any{
				"policy": "fallback",
			},
		},
	}
}

type yieldProviderErrorPolicy struct{}

func (yieldProviderErrorPolicy) HandleRecoverable(_ context.Context, _ *agentcore.RunState, event *agentcore.ErrorEvent) agentcore.ErrorDecision {
	if event == nil || event.Stage != "model" {
		return agentcore.ErrorDecision{}
	}
	var providerErr *agentcore.ProviderError
	if !errors.As(event.Err, &providerErr) {
		return agentcore.ErrorDecision{}
	}
	return agentcore.ErrorDecision{
		Result: &agentcore.RunResult{
			Status: agentcore.RunYielded,
			Yield: &agentcore.Yield{
				Reason:   "retry_after_provider_error",
				ResumeOK: true,
				Metadata: map[string]any{
					"stage": event.Stage,
				},
			},
		},
	}
}

func main() {
	ctx := context.Background()
	runID := "example-policies"
	root := filepath.Join(os.TempDir(), "suag-agentcore-examples", "policies")
	if err := os.RemoveAll(root); err != nil {
		log.Fatal(err)
	}

	store := files.NewStorage(root)
	tools := tools.NewRegistry()
	tools.Register(mock.NewTool(
		"profile_lookup",
		mock.WithDescription("Looks up a customer profile"),
		mock.WithHandler(func(_ context.Context, call agentcore.ToolCall) (*agentcore.ToolResult, error) {
			return &agentcore.ToolResult{
				CallID:  call.CallID,
				Name:    call.Name,
				Content: "customer: apollo",
			}, nil
		}),
	))
	tools.Register(mock.NewTool(
		"risk_check",
		mock.WithDescription("Checks account risk"),
		mock.WithHandler(func(_ context.Context, _ agentcore.ToolCall) (*agentcore.ToolResult, error) {
			return nil, errors.New("risk backend unavailable")
		}),
	))

	firstRuntime := agentcore.New(agentcore.Config{
		Provider: mock.NewProvider(
			mock.Error(errors.New("provider temporarily unavailable")),
		),
		Storage:        store,
		Tools:          tools,
		PromptBuilder:  staticPrompt("You are a policy demonstration agent."),
		ContextBuilder: passthroughContext{},
		ErrorPolicy:    yieldProviderErrorPolicy{},
	})

	firstResult, err := firstRuntime.Run(ctx, agentcore.RunInput{
		RunID:       runID,
		AgentID:     "policy-agent",
		Instruction: "Fetch the customer profile and risk state.",
		Metadata: map[string]any{
			"customer_id": "cust-123",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("first run: status=%s yield=%q", firstResult.Status, firstResult.Yield.Reason)

	resumeRuntime := agentcore.New(agentcore.Config{
		Provider: mock.NewProvider(
			mock.ToolCall("call-profile", "profile_lookup", map[string]any{"customer_id": "cust-123"}),
			mock.ToolCall("call-risk", "risk_check", map[string]any{"customer_id": "cust-123"}),
			mock.Final("profile gathered, risk fallback applied, and run completed after explicit resume"),
		),
		Storage:             store,
		Tools:               tools,
		PromptBuilder:       staticPrompt("You are a policy demonstration agent."),
		ContextBuilder:      passthroughContext{},
		ResumePolicy:        resumeWithoutAppendingPolicy{},
		ToolExecutionPolicy: permissiveBatchToolPolicy{},
		MaxToolCallsPerStep: 1,
	})

	secondResult, err := resumeRuntime.Run(ctx, agentcore.RunInput{
		RunID:       runID,
		ResumeMode:  agentcore.ResumeWithInput,
		Instruction: "Continue, but do not append this instruction to transcript.",
		Metadata: map[string]any{
			"resume_requested_by": "host",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("second run: status=%s output=%q", secondResult.Status, secondResult.Output)

	bundle, err := agentcore.LoadAuthorityBundle(ctx, store, runID)
	if err != nil {
		log.Fatal(err)
	}
	if bundle == nil || bundle.State == nil {
		log.Fatal("expected authority bundle after completed run")
	}

	log.Printf("persisted metadata resume_policy=%v resume_requested_by=%v", bundle.State.Metadata["resume_policy"], bundle.State.Metadata["resume_requested_by"])
	log.Printf("persisted messages=%d", len(bundle.Messages))
	for i, msg := range bundle.Messages {
		switch {
		case msg.ToolCall != nil && msg.ToolResult == nil:
			log.Printf("message[%d]: role=%s tool_call=%s", i, msg.Role, msg.ToolCall.Name)
		case msg.ToolResult != nil:
			log.Printf("message[%d]: role=%s tool_result=%q metadata=%v", i, msg.Role, msg.ToolResult.Content, msg.ToolResult.Metadata)
		default:
			log.Printf("message[%d]: role=%s content=%q", i, msg.Role, msg.Content)
		}
	}
}

type staticPrompt string

func (p staticPrompt) BuildSystemPrompt(context.Context, *agentcore.RunState) (string, error) {
	return string(p), nil
}

type passthroughContext struct{}

func (passthroughContext) BuildMessages(_ context.Context, state *agentcore.RunState) ([]agentcore.Message, error) {
	if state == nil || len(state.Messages) == 0 {
		return nil, nil
	}
	msgs := make([]agentcore.Message, len(state.Messages))
	copy(msgs, state.Messages)
	return msgs, nil
}

func cloneMetadata(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
