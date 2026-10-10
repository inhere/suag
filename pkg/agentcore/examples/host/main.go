package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/mock"
	"github.com/inhere/suag/pkg/agentcore/storage/files"
	"github.com/inhere/suag/pkg/agentcore/tools"
)

type StartRequest struct {
	SessionID string
	AgentID   string
	Message   string
}

type ContinueRequest struct {
	SessionID string
	Message   string
}

type AgentHost struct {
	runtime agentcore.Runtime
}

func NewAgentHost(runtime agentcore.Runtime) *AgentHost {
	return &AgentHost{runtime: runtime}
}

func (h *AgentHost) Start(ctx context.Context, req StartRequest) (*agentcore.RunResult, error) {
	return h.runtime.Run(ctx, agentcore.RunInput{
		RunID:       req.SessionID,
		AgentID:     req.AgentID,
		Instruction: req.Message,
	})
}

func (h *AgentHost) Continue(ctx context.Context, req ContinueRequest) (*agentcore.RunResult, error) {
	return h.runtime.Run(ctx, agentcore.RunInput{
		RunID:       req.SessionID,
		Instruction: req.Message,
		ResumeMode:  agentcore.ResumeWithInput,
		Stream: func(_ context.Context, event agentcore.StreamEvent) error {
			switch event.Kind {
			case agentcore.StreamEventTextDelta:
				log.Printf("ui delta=%q", event.Delta)
			case agentcore.StreamEventMessageStop:
				log.Printf("ui flush")
			}
			return nil
		},
	})
}

type yieldBeforeFinalHook struct {
	agentcore.NopHook
	beforeModelCalls int
}

func (h *yieldBeforeFinalHook) BeforeModel(_ context.Context, _ *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	h.beforeModelCalls++
	if h.beforeModelCalls == 2 {
		ctrl.Yield("await_operator_input", map[string]any{
			"phase": "before_final_answer",
		})
	}
	return nil
}

// staticPrompt is the simplest PromptBuilder: one fixed system prompt.
type staticPrompt string

func (p staticPrompt) BuildSystemPrompt(context.Context, *agentcore.RunState) (string, error) {
	return string(p), nil
}

func newRuntime(provider agentcore.Provider, storage agentcore.Storage, tools agentcore.ToolRegistry, hooks ...agentcore.Hook) agentcore.Runtime {
	return agentcore.New(agentcore.Config{
		Provider:      provider,
		Storage:       storage,
		Tools:         tools,
		PromptBuilder: staticPrompt("You are a host-integrated assistant. Keep runtime state easy to resume from the host."),
		Hooks:         hooks,
	})
}

func main() {
	ctx := context.Background()
	storageDir := filepath.Join(os.TempDir(), "suag-agentcore-examples", "host")
	if err := os.RemoveAll(storageDir); err != nil {
		log.Fatal(err)
	}

	store := files.NewStorage(storageDir)
	reg := tools.NewRegistry()
	reg.Register(mock.NewTool(
		"echo",
		mock.WithDescription("Echoes the provided text"),
		mock.WithSchema(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string"},
			},
			"required": []any{"text"},
		}),
	))

	startRuntime := newRuntime(
		mock.NewProvider(
			mock.ToolCall("call-echo", "echo", map[string]any{"text": "prepare a host-facing summary"}),
		),
		store,
		reg,
		&yieldBeforeFinalHook{},
	)

	startHost := NewAgentHost(startRuntime)
	first, err := startHost.Start(ctx, StartRequest{
		SessionID: "session-host-example",
		AgentID:   "host-agent",
		Message:   "Use the tool, then pause so the host can ask for the final answer.",
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("first status=%s yield=%q", first.Status, first.Yield.Reason)

	resumeRuntime := newRuntime(
		mock.NewProvider(
			mock.Stream(
				mock.Final("host wrapper completed the resumed run"),
				agentcore.StreamEvent{Kind: agentcore.StreamEventTextDelta, Delta: "host "},
				agentcore.StreamEvent{Kind: agentcore.StreamEventTextDelta, Delta: "wrapper "},
				agentcore.StreamEvent{Kind: agentcore.StreamEventTextDelta, Delta: "completed "},
				agentcore.StreamEvent{Kind: agentcore.StreamEventTextDelta, Delta: "the resumed run"},
				agentcore.StreamEvent{Kind: agentcore.StreamEventMessageStop},
			),
		),
		store,
		reg,
	)

	host := NewAgentHost(resumeRuntime)
	resumed, err := host.Continue(ctx, ContinueRequest{
		SessionID: "session-host-example",
		Message:   "Continue and send the final answer to the UI.",
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("resumed status=%s output=%q steps=%d", resumed.Status, resumed.Output, resumed.Steps)
}
