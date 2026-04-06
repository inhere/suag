package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/inhere/axon/pkg/agentcore"
	"github.com/inhere/axon/pkg/agentcore/files"
	memorypkg "github.com/inhere/axon/pkg/agentcore/memory"
	"github.com/inhere/axon/pkg/agentcore/mock"
)

type logMemoryContextHook struct {
	agentcore.NopHook
}

func (h logMemoryContextHook) BeforeModel(_ context.Context, event *agentcore.ModelEvent, _ *agentcore.Control) error {
	if event == nil || event.Request == nil {
		return nil
	}
	log.Printf("model context messages=%d", len(event.Request.Messages))
	for i, msg := range event.Request.Messages {
		log.Printf("  [%d] role=%s content=%q", i, msg.Role, msg.Content)
	}
	return nil
}

func main() {
	runID := "example-memory"
	storageDir := filepath.Join(os.TempDir(), "axon-agentcore-examples", "memory")

	store := files.NewStorage(storageDir)
	if err := store.SaveRun(context.Background(), &agentcore.RunState{
		RunID:   runID,
		AgentID: "memory-agent",
		Status:  agentcore.RunYielded,
		Step:    1,
	}); err != nil {
		log.Fatal(err)
	}
	if err := store.AppendMessages(context.Background(), runID,
		agentcore.Message{Role: "user", Content: "Summarize the current agentcore progress."},
		agentcore.Message{Role: "assistant", Content: "The kernel and docs are already in place."},
		agentcore.Message{Role: "user", Content: "Continue with the working memory milestone."},
	); err != nil {
		log.Fatal(err)
	}

	mem := memorypkg.New(memorypkg.Config{
		MaxRecentMessages:   2,
		IncludeToolMessages: true,
	})
	mem.SetSummary("The run is focused on agentcore evolution and the current milestone is default working memory.")

	runtime := agentcore.New(agentcore.Config{
		Provider: mock.NewProvider(mock.Final("working memory example completed")),
		Storage:  store,
		Memory:   mem,
		Hooks:    []agentcore.Hook{logMemoryContextHook{}},
	})

	result, err := runtime.Run(context.Background(), agentcore.RunInput{
		RunID:      runID,
		ResumeMode: agentcore.ResumeFromState,
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("status=%s output=%q steps=%d", result.Status, result.Output, result.Steps)
}
