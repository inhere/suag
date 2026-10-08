package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/mock"
	promptpkg "github.com/inhere/suag/pkg/agentcore/prompt"
	"github.com/inhere/suag/pkg/agentcore/storage/files"
)

type logPromptHook struct {
	agentcore.NopHook
}

func (h logPromptHook) BeforeModel(_ context.Context, event *agentcore.ModelEvent, _ *agentcore.Control) error {
	if event == nil || event.Request == nil {
		return nil
	}

	log.Printf("system prompt:\n%s", event.Request.System)
	return nil
}

func main() {
	runtime := agentcore.New(agentcore.Config{
		Provider: mock.NewProvider(mock.Final("prompt example completed")),
		Storage:  files.NewStorage(filepath.Join(os.TempDir(), "suag-agentcore-examples", "prompt")),
		PromptBuilder: promptpkg.Layered(promptpkg.Config{
			Base: "You are a focused repository assistant.",
			Metadata: promptpkg.MetadataConfig{
				Enabled: true,
			},
			Sections: []string{
				"Prefer grounded answers based on the current run state.",
				"Keep tool usage minimal and explicit.",
			},
		}),
		Hooks: []agentcore.Hook{logPromptHook{}},
	})

	result, err := runtime.Run(context.Background(), agentcore.RunInput{
		RunID:       "example-prompt",
		AgentID:     "prompt-agent",
		Instruction: "Explain how the layered prompt is composed.",
		Metadata: map[string]any{
			"mode":    "demo",
			"project": "agentcore",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("status=%s output=%q steps=%d", result.Status, result.Output, result.Steps)
}
