package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/inhere/axon/pkg/agentcore"
	"github.com/inhere/axon/pkg/agentcore/files"
	"github.com/inhere/axon/pkg/agentcore/mock"
)

type auditHook struct {
	agentcore.NopHook
}

func (h auditHook) BeforeModel(_ context.Context, event *agentcore.ModelEvent, _ *agentcore.Control) error {
	if event != nil && event.Request != nil {
		log.Printf("before model: messages=%d tools=%d", len(event.Request.Messages), len(event.Request.Tools))
	}
	return nil
}

func (h auditHook) AfterRun(_ context.Context, event *agentcore.RunEvent, _ *agentcore.Control) error {
	if event != nil && event.Result != nil {
		log.Printf("after run: status=%s output=%q", event.Result.Status, event.Result.Output)
	}
	return nil
}

func main() {
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

	runtime := agentcore.New(agentcore.Config{
		Provider: mock.NewProvider(
			mock.ToolCall("call-echo", "echo", map[string]any{"text": "hello from custom example"}),
			mock.Final("tool finished successfully"),
		),
		Storage: files.NewStorage(filepath.Join(os.TempDir(), "axon-agentcore-examples", "custom")),
		Tools:   tools,
		Hooks:   []agentcore.Hook{auditHook{}},
	})

	result, err := runtime.Run(context.Background(), agentcore.RunInput{
		RunID:       "example-custom",
		AgentID:     "custom-agent",
		Instruction: "use the echo tool",
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("final result: status=%s output=%q steps=%d", result.Status, result.Output, result.Steps)
}
