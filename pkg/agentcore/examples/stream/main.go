package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/inhere/axon/pkg/agentcore"
	"github.com/inhere/axon/pkg/agentcore/mock"
	"github.com/inhere/axon/pkg/agentcore/storage/files"
)

func main() {
	runtime := agentcore.New(agentcore.Config{
		Provider: mock.NewProvider(
			mock.Stream(
				mock.Final("hello from streaming example"),
				agentcore.StreamEvent{Kind: agentcore.StreamEventTextDelta, Delta: "hello "},
				agentcore.StreamEvent{Kind: agentcore.StreamEventTextDelta, Delta: "from "},
				agentcore.StreamEvent{Kind: agentcore.StreamEventTextDelta, Delta: "streaming example"},
				agentcore.StreamEvent{Kind: agentcore.StreamEventMessageStop},
			),
		),
		Storage: files.NewStorage(filepath.Join(os.TempDir(), "axon-agentcore-examples", "stream")),
	})

	result, err := runtime.Run(context.Background(), agentcore.RunInput{
		RunID:       "example-stream",
		AgentID:     "stream-agent",
		Instruction: "stream a greeting",
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

	log.Printf("status=%s output=%q steps=%d", result.Status, result.Output, result.Steps)
}
