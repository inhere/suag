package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/mock"
	"github.com/inhere/suga/pkg/agentcore/storage/files"
	"github.com/inhere/suga/pkg/agentcore/tools"
)

type yieldAfterToolHook struct {
	agentcore.NopHook
	beforeModelCalls int
}

func (h *yieldAfterToolHook) BeforeModel(_ context.Context, _ *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	h.beforeModelCalls++
	if h.beforeModelCalls == 2 {
		ctrl.Yield("await_resume", map[string]any{
			"step": "after_tool",
		})
	}
	return nil
}

func main() {
	runID := "example-resume"
	storageDir := filepath.Join(os.TempDir(), "suga-agentcore-examples", "resume")

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

	firstRuntime := agentcore.New(agentcore.Config{
		Provider: mock.NewProvider(
			mock.ToolCall("call-echo", "echo", map[string]any{"text": "hello from yielded run"}),
		),
		Storage: files.NewStorage(storageDir),
		Tools:   reg,
		Hooks:   []agentcore.Hook{&yieldAfterToolHook{}},
	})

	firstResult, err := firstRuntime.Run(context.Background(), agentcore.RunInput{
		RunID:       runID,
		AgentID:     "resume-agent",
		Instruction: "use echo and then pause",
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("first run: status=%s yield=%q", firstResult.Status, firstResult.Yield.Reason)

	resumeRuntime := agentcore.New(agentcore.Config{
		Provider: mock.NewProvider(
			mock.Final("completed after resume"),
		),
		Storage: files.NewStorage(storageDir),
		Tools:   reg,
	})

	resumedResult, err := resumeRuntime.Run(context.Background(), agentcore.RunInput{
		RunID:      runID,
		ResumeMode: agentcore.ResumeAuto,
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf(
		"resumed run: status=%s output=%q steps=%d",
		resumedResult.Status,
		resumedResult.Output,
		resumedResult.Steps,
	)
}
