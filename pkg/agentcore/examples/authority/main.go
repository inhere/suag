package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/inhere/axon/pkg/agentcore"
	"github.com/inhere/axon/pkg/agentcore/files"
)

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

type artifactTool struct{}

func (artifactTool) Name() string { return "write_report" }

func (artifactTool) Description() string { return "Produces a final report artifact" }

func (artifactTool) Schema() agentcore.ToolSchema {
	return agentcore.ToolSchema{
		Name:        "write_report",
		Description: "Produces a final report artifact",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"topic": map[string]any{"type": "string"},
			},
			"required": []any{"topic"},
		},
	}
}

func (artifactTool) Execute(_ context.Context, call agentcore.ToolCall) (*agentcore.ToolResult, error) {
	topic, _ := call.Arguments["topic"].(string)
	return &agentcore.ToolResult{
		CallID:  call.CallID,
		Name:    call.Name,
		Content: "report generated for " + topic,
		Artifacts: []agentcore.Artifact{{
			Kind:    "file",
			URI:     "artifact://report.txt",
			Content: "report body for " + topic,
			Metadata: map[string]any{
				"topic": topic,
			},
		}},
	}, nil
}

type scriptedProvider struct {
	step int
}

func (p *scriptedProvider) Name() string { return "authority-demo" }

func (p *scriptedProvider) Generate(context.Context, *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	p.step++
	if p.step == 1 {
		return &agentcore.GenerateResult{
			ToolCalls: []agentcore.ToolCall{{
				CallID: "call-report",
				Name:   "write_report",
				Arguments: map[string]any{
					"topic": "authority bundle",
				},
			}},
			FinishReason: agentcore.FinishToolCalls,
		}, nil
	}
	return &agentcore.GenerateResult{
		Content:      "authority bundle demo completed",
		FinishReason: agentcore.FinishFinal,
	}, nil
}

func main() {
	ctx := context.Background()
	runID := "example-authority"
	root := filepath.Join(os.TempDir(), "axon-agentcore-examples", "authority")
	if err := os.RemoveAll(root); err != nil {
		log.Fatal(err)
	}

	tools := agentcore.NewToolRegistry()
	tools.Register(artifactTool{})
	store := files.NewStorage(root)

	runtime := agentcore.New(agentcore.Config{
		Provider:       &scriptedProvider{},
		Storage:        store,
		Tools:          tools,
		PromptBuilder:  staticPrompt("You are an authority bundle example agent."),
		ContextBuilder: passthroughContext{},
	})

	result, err := runtime.Run(ctx, agentcore.RunInput{
		RunID:       runID,
		AgentID:     "authority-agent",
		Instruction: "generate a report artifact and finish",
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("run result: status=%s output=%q", result.Status, result.Output)

	bundle, err := agentcore.LoadAuthorityBundle(ctx, store, runID)
	if err != nil {
		log.Fatal(err)
	}
	if bundle == nil {
		log.Fatal("expected authority bundle after completed run")
	}

	log.Printf("authority messages=%d artifacts=%d", len(bundle.Messages), len(bundle.Artifacts))
	if last, ok := bundle.LastAssistantMessage(); ok {
		log.Printf("last assistant message=%q", last.Content)
	}
	if artifact, ok := bundle.FindArtifactByURI("artifact://report.txt"); ok {
		log.Printf("artifact uri=%s topic=%v", artifact.URI, artifact.Metadata["topic"])
	}
}
