package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/inhere/axon/pkg/agentcore"
	contextpkg "github.com/inhere/axon/pkg/agentcore/context"
	"github.com/inhere/axon/pkg/agentcore/files"
	memorypkg "github.com/inhere/axon/pkg/agentcore/memory"
	"github.com/inhere/axon/pkg/agentcore/mock"
	promptpkg "github.com/inhere/axon/pkg/agentcore/prompt"
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

func newRuntime(provider agentcore.Provider, storage agentcore.Storage, tools agentcore.ToolRegistry, hooks ...agentcore.Hook) agentcore.Runtime {
	mem := memorypkg.New(memorypkg.Config{
		MaxRecentMessages:   6,
		IncludeToolMessages: true,
	})
	mem.SetSummary("The host wraps agentcore with Start/Continue entrypoints and wants resumable, inspectable runtime behavior.")

	return agentcore.New(agentcore.Config{
		Provider: provider,
		Storage:  storage,
		Memory:   mem,
		Tools:    tools,
		PromptBuilder: promptpkg.Layered(promptpkg.Config{
			Base: "You are a host-integrated assistant.",
			Metadata: promptpkg.MetadataConfig{
				Enabled: true,
			},
			Sections: []string{
				"Keep runtime state easy to resume from the host.",
				"Prefer explicit transitions over hidden control flow.",
			},
		}),
		ContextBuilder: contextpkg.MemoryThenRecent(mem, contextpkg.MemoryThenRecentConfig{
			MaxMessages: 4,
		}),
		Hooks: hooks,
	})
}

func main() {
	ctx := context.Background()
	storageDir := filepath.Join(os.TempDir(), "axon-agentcore-examples", "host")
	if err := os.RemoveAll(storageDir); err != nil {
		log.Fatal(err)
	}

	store := files.NewStorage(storageDir)
	tools := agentcore.NewToolRegistry()
	tools.Register(mock.NewTool(
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
		tools,
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
		tools,
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
