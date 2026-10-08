package main

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"

	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/mock"
	"github.com/inhere/suag/pkg/agentcore/storage/files"
)

type StartRequest struct {
	SessionID string
	AgentID   string
	Message   string
}

type ContinueRequest struct {
	SessionID    string
	OperatorNote string
}

type PolicyAwareHost struct {
	storage agentcore.Storage
	tools   agentcore.ToolRegistry
}

func NewPolicyAwareHost(storage agentcore.Storage, tools agentcore.ToolRegistry) *PolicyAwareHost {
	return &PolicyAwareHost{
		storage: storage,
		tools:   tools,
	}
}

func (h *PolicyAwareHost) Start(ctx context.Context, req StartRequest) (*agentcore.RunResult, error) {
	runtime := h.newRuntime(mock.NewProvider(
		mock.Error(errors.New("provider temporarily unavailable")),
	))

	return runtime.Run(ctx, agentcore.RunInput{
		RunID:       req.SessionID,
		AgentID:     req.AgentID,
		Instruction: req.Message,
		Metadata: map[string]any{
			"entrypoint": "start",
		},
	})
}

func (h *PolicyAwareHost) Continue(ctx context.Context, req ContinueRequest) (*agentcore.RunResult, error) {
	runtime := h.newRuntime(mock.NewProvider(
		mock.ToolCall("call-profile", "profile_lookup", map[string]any{"customer_id": "cust-123"}),
		mock.ToolCall("call-risk", "risk_check", map[string]any{"customer_id": "cust-123"}),
		mock.Final("policy-aware host resumed the run and returned a final answer"),
	))

	return runtime.Run(ctx, agentcore.RunInput{
		RunID:       req.SessionID,
		Instruction: req.OperatorNote,
		ResumeMode:  agentcore.ResumeWithInput,
		Metadata: map[string]any{
			"entrypoint":             "continue",
			"suppress_resume_append": true,
			"operator_note":          req.OperatorNote,
		},
	})
}

func (h *PolicyAwareHost) newRuntime(provider agentcore.Provider) agentcore.Runtime {
	return agentcore.New(agentcore.Config{
		Provider:            provider,
		Storage:             h.storage,
		Tools:               h.tools,
		PromptBuilder:       staticPrompt("You are a policy-aware host assistant."),
		ContextBuilder:      passthroughContext{},
		ResumePolicy:        hostResumePolicy{},
		ToolExecutionPolicy: hostToolPolicy{},
		ErrorPolicy:         hostErrorPolicy{},
		MaxToolCallsPerStep: 1,
	})
}

type hostResumePolicy struct {
	agentcore.DefaultResumePolicy
}

func (hostResumePolicy) ShouldAppendInstruction(input agentcore.RunInput, _ *agentcore.RunState) bool {
	if suppress, ok := input.Metadata["suppress_resume_append"].(bool); ok && suppress {
		return false
	}
	return agentcore.DefaultResumePolicy{}.ShouldAppendInstruction(input, nil)
}

type hostToolPolicy struct{}

func (hostToolPolicy) ValidateToolCalls(_ context.Context, _ *agentcore.RunState, _ []agentcore.ToolCall) error {
	return nil
}

func (hostToolPolicy) HandleToolError(_ context.Context, _ *agentcore.RunState, call agentcore.ToolCall, err error) agentcore.ToolErrorDecision {
	return agentcore.ToolErrorDecision{
		Result: &agentcore.ToolResult{
			CallID:  call.CallID,
			Name:    call.Name,
			Content: "host fallback: " + err.Error(),
			Metadata: map[string]any{
				"source": "host_tool_policy",
			},
		},
	}
}

type hostErrorPolicy struct{}

func (hostErrorPolicy) HandleRecoverable(_ context.Context, _ *agentcore.RunState, event *agentcore.ErrorEvent) agentcore.ErrorDecision {
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
				Reason:   "await_host_retry",
				ResumeOK: true,
				Metadata: map[string]any{"stage": event.Stage},
			},
		},
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

func main() {
	ctx := context.Background()
	root := filepath.Join(os.TempDir(), "suag-agentcore-examples", "policyhost")
	if err := os.RemoveAll(root); err != nil {
		log.Fatal(err)
	}

	store := files.NewStorage(root)
	tools := agentcore.NewToolRegistry()
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

	host := NewPolicyAwareHost(store, tools)

	first, err := host.Start(ctx, StartRequest{
		SessionID: "session-policy-host",
		AgentID:   "host-agent",
		Message:   "Fetch the profile and risk state.",
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("start: status=%s yield=%q", first.Status, first.Yield.Reason)

	second, err := host.Continue(ctx, ContinueRequest{
		SessionID:    "session-policy-host",
		OperatorNote: "Continue after retry, but keep this note out of the transcript.",
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("continue: status=%s output=%q", second.Status, second.Output)

	bundle, err := agentcore.LoadAuthorityBundle(ctx, store, "session-policy-host")
	if err != nil {
		log.Fatal(err)
	}
	if bundle == nil {
		log.Fatal("expected authority bundle after policy host run")
	}
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
