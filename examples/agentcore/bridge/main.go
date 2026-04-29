package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/inhere/axon/internal/platform/llm"
	"github.com/inhere/axon/internal/platform/tools"
	"github.com/inhere/axon/pkg/agentcore"
	"github.com/inhere/axon/pkg/agentcore/api"
	"github.com/inhere/axon/pkg/agentcore/storage/files"
)

type echoTool struct{}

func (echoTool) Name() string        { return "echo" }
func (echoTool) Description() string { return "Echoes a message" }
func (echoTool) Kind() tools.Kind    { return tools.KindNative }
func (echoTool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        "echo",
		Description: "Echoes a message",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{
					"type": "string",
				},
			},
			"required": []any{"text"},
		},
	}
}
func (echoTool) Execute(_ context.Context, args api.ToolCall) (*api.ToolResult, error) {
	return &api.ToolResult{
		CallID:  args.CallID,
		Name:    args.Name,
		Content: "tool:" + args.Name,
	}, nil
}

func main() {
	reg := tools.NewRegistry()
	reg.MustRegister(echoTool{})

	provider := llm.NewMockProvider("internal-mock").
		QueueToolCall("echo", map[string]any{"text": "hello from bridge example"}).
		QueueText("bridge example completed")

	runtime := agentcore.New(agentcore.Config{
		Provider: llm.NewProvider(provider),
		Storage:  files.NewStorage(filepath.Join(os.TempDir(), "axon-agentcore-examples", "bridge")),
		Tools:    reg,
	})

	result, err := runtime.Run(context.Background(), agentcore.RunInput{
		RunID:       "example-bridge-" + time.Now().Format("20060102150405"),
		AgentID:     "bridge-agent",
		Instruction: "use the internal echo tool",
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("status=%s output=%q steps=%d", result.Status, result.Output, result.Steps)
}
