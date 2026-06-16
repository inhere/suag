package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/mock"
	"github.com/inhere/suag/pkg/agentcore/storage/files"
)

func main() {
	runtime := agentcore.New(agentcore.Config{
		Provider: mock.NewProvider(mock.Final("hello from minimal example")),
		Storage:  files.NewStorage(filepath.Join(os.TempDir(), "suag-agentcore-examples", "minimal")),
	})

	result, err := runtime.Run(context.Background(), agentcore.RunInput{
		RunID:       "example-minimal",
		AgentID:     "example-agent",
		Instruction: "say hello",
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("status=%s output=%q steps=%d", result.Status, result.Output, result.Steps)
}
