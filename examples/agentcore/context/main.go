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
)

type logContextHook struct {
	agentcore.NopHook
	label string
}

func (h logContextHook) BeforeModel(_ context.Context, event *agentcore.ModelEvent, _ *agentcore.Control) error {
	if event == nil || event.Request == nil {
		return nil
	}
	log.Printf("[%s] model context messages=%d", h.label, len(event.Request.Messages))
	for i, msg := range event.Request.Messages {
		log.Printf("[%s]   [%d] role=%s content=%q", h.label, i, msg.Role, msg.Content)
	}
	return nil
}

func main() {
	transcript := []agentcore.Message{
		{Role: "user", Content: "Summarize the current agentcore state."},
		{Role: "assistant", Content: "The kernel runtime and docs are already in place."},
		{Role: "user", Content: "Show me the newest context presets."},
		{Role: "assistant", Content: "TranscriptWindow and MemoryFirst are already shipped."},
	}

	log.Println("running TranscriptWindow preset")
	runPreset("context-transcript-window", transcript, nil,
		contextpkg.TranscriptWindow(contextpkg.WindowConfig{MaxMessages: 2}),
		logContextHook{label: "TranscriptWindow"},
	)

	log.Println("running MemoryFirst preset")
	memFirst := memorypkg.New(memorypkg.Config{MaxRecentMessages: 2})
	memFirst.SetSummary("The run is focused on agentcore kernel evolution and the user wants to inspect context presets.")
	runPreset("context-memory-first", transcript, memFirst,
		contextpkg.MemoryFirst(memFirst, contextpkg.MemoryFirstConfig{MaxMessages: 2}),
		logContextHook{label: "MemoryFirst"},
	)

	log.Println("running MemoryThenRecent preset")
	memThenRecent := memorypkg.New(memorypkg.Config{MaxRecentMessages: 2})
	memThenRecent.SetSummary("The run is focused on agentcore kernel evolution and the user wants to inspect context presets.")
	runPreset("context-memory-then-recent", transcript, memThenRecent,
		contextpkg.MemoryThenRecent(memThenRecent, contextpkg.MemoryThenRecentConfig{MaxMessages: 2}),
		logContextHook{label: "MemoryThenRecent"},
	)
}

func runPreset(runID string, transcript []agentcore.Message, mem agentcore.Memory, builder agentcore.ContextBuilder, hook agentcore.Hook) {
	store := files.NewStorage(filepath.Join(os.TempDir(), "axon-agentcore-examples", runID))
	if err := store.SaveRun(context.Background(), &agentcore.RunState{
		RunID:   runID,
		AgentID: "context-agent",
		Status:  agentcore.RunYielded,
		Step:    1,
	}); err != nil {
		log.Fatal(err)
	}
	if err := store.AppendMessages(context.Background(), runID, transcript...); err != nil {
		log.Fatal(err)
	}

	runtime := agentcore.New(agentcore.Config{
		Provider:       mock.NewProvider(mock.Final("context preset example completed")),
		Storage:        store,
		Memory:         mem,
		ContextBuilder: builder,
		Hooks:          []agentcore.Hook{hook},
	})

	result, err := runtime.Run(context.Background(), agentcore.RunInput{
		RunID:      runID,
		ResumeMode: agentcore.ResumeFromState,
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("[%s] status=%s output=%q steps=%d", runID, result.Status, result.Output, result.Steps)
}
