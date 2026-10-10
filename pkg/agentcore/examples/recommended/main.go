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

// staticPrompt is the simplest PromptBuilder: one fixed system prompt.
type staticPrompt string

func (p staticPrompt) BuildSystemPrompt(context.Context, *agentcore.RunState) (string, error) {
	return string(p), nil
}

type logModelShapeHook struct {
	agentcore.NopHook
}

func (h logModelShapeHook) BeforeModel(_ context.Context, event *agentcore.ModelEvent, _ *agentcore.Control) error {
	if event == nil || event.Request == nil {
		return nil
	}

	log.Printf("system-bytes=%d context-messages=%d tools=%d", len(event.Request.System), len(event.Request.Messages), len(event.Request.Tools))
	return nil
}

type yieldAfterToolHook struct {
	agentcore.NopHook
	beforeModelCalls int
}

func (h *yieldAfterToolHook) BeforeModel(_ context.Context, _ *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	h.beforeModelCalls++
	if h.beforeModelCalls == 2 {
		ctrl.Yield("await_user_confirmation", map[string]any{
			"stage": "after_tool",
		})
	}
	return nil
}

func main() {
	ctx := context.Background()
	runID := "example-recommended"
	storageDir := filepath.Join(os.TempDir(), "suag-agentcore-examples", "recommended")
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

	promptBuilder := staticPrompt("You are a focused embedding-host assistant. Use tools only when needed.")

	firstRuntime := agentcore.New(agentcore.Config{
		Provider: mock.NewProvider(
			mock.ToolCall("call-echo", "echo", map[string]any{"text": "draft a release-readiness summary"}),
		),
		Storage:       store,
		Tools:         reg,
		PromptBuilder: promptBuilder,
		Hooks: []agentcore.Hook{
			logModelShapeHook{},
			&yieldAfterToolHook{},
		},
	})

	firstResult, err := firstRuntime.Run(ctx, agentcore.RunInput{
		RunID:       runID,
		AgentID:     "recommended-agent",
		Instruction: "Use the tool, then pause so the host can decide whether to continue.",
		Metadata: map[string]any{
			"mode":    "recommended",
			"project": "agentcore",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("first run: status=%s yield=%q", firstResult.Status, firstResult.Yield.Reason)

	resumeRuntime := agentcore.New(agentcore.Config{
		Provider: mock.NewProvider(
			mock.Stream(
				mock.Final("release-readiness summary delivered after explicit resume"),
				agentcore.StreamEvent{Kind: agentcore.StreamEventTextDelta, Delta: "release-readiness "},
				agentcore.StreamEvent{Kind: agentcore.StreamEventTextDelta, Delta: "summary "},
				agentcore.StreamEvent{Kind: agentcore.StreamEventTextDelta, Delta: "delivered "},
				agentcore.StreamEvent{Kind: agentcore.StreamEventTextDelta, Delta: "after explicit resume"},
				agentcore.StreamEvent{Kind: agentcore.StreamEventMessageStop},
			),
		),
		Storage:       store,
		Tools:         reg,
		PromptBuilder: promptBuilder,
		Hooks:         []agentcore.Hook{logModelShapeHook{}},
	})

	resumedResult, err := resumeRuntime.Run(ctx, agentcore.RunInput{
		RunID:       runID,
		ResumeMode:  agentcore.ResumeWithInput,
		Instruction: "Continue and stream the final answer back to the host.",
		Stream: func(_ context.Context, event agentcore.StreamEvent) error {
			switch event.Kind {
			case agentcore.StreamEventTextDelta:
				log.Printf("stream delta=%q", event.Delta)
			case agentcore.StreamEventMessageStop:
				log.Printf("stream message stop")
			}
			return nil
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("resumed run: status=%s output=%q steps=%d", resumedResult.Status, resumedResult.Output, resumedResult.Steps)
}
