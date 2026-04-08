package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/inhere/axon/internal/agentcoreadapters/internalllm"
	adapterinternaltools "github.com/inhere/axon/internal/agentcoreadapters/internaltools"
	"github.com/inhere/axon/internal/llm"
	internaltools "github.com/inhere/axon/internal/tools"
	"github.com/inhere/axon/pkg/agentcore"
	"github.com/inhere/axon/pkg/agentcore/files"
	"github.com/inhere/axon/pkg/agentcore/runtime"
)

type echoTool struct{}

func (echoTool) Name() string             { return "echo" }
func (echoTool) Description() string      { return "Echoes a message" }
func (echoTool) Kind() internaltools.Kind { return internaltools.KindNative }
func (echoTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{
		Name:        "echo",
		Description: "Echoes a message",
		Parameters: llm.SchemaProperty{
			Type: "object",
			Properties: map[string]*llm.SchemaProperty{
				"text": {Type: "string"},
			},
			Required: []string{"text"},
		},
	}
}
func (echoTool) Execute(_ context.Context, args map[string]any) (string, error) {
	return "tool:" + args["text"].(string), nil
}

func main() {
	reg := internaltools.NewRegistry()
	reg.MustRegister(echoTool{})

	provider := llm.NewMockProvider("internal-mock").
		QueueToolCall("echo", map[string]any{"text": "hello from bridge example"}).
		QueueText("bridge example completed")

	runtime := runtime.New(runtime.Config{
		Provider: internalllm.NewProvider(provider),
		Storage:  files.NewStorage(filepath.Join(os.TempDir(), "axon-agentcore-examples", "bridge")),
		Tools:    adapterinternaltools.NewRegistry(reg),
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
