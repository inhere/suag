# Axon - Agent OS

## Minimal Agentcore Embedding

```go
package main

import (
	"context"
	"log"

	"github.com/inhere/axon/pkg/agentcore"
	"github.com/inhere/axon/pkg/agentcore/files"
	"github.com/inhere/axon/pkg/agentcore/mock"
)

func main() {
	provider := mock.NewProvider(mock.Final("hello from agentcore"))

	runtime := agentcore.New(agentcore.Config{
		Provider: provider,
		Storage:  files.NewStorage(".agentcore-runs"),
	})

	result, err := runtime.Run(context.Background(), agentcore.RunInput{
		RunID:       "example-run",
		AgentID:     "example-agent",
		Instruction: "say hello",
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Println(result.Output)
}
```

If you want to bridge Axon's existing internal stacks into `agentcore`, use the adapters in:

- `pkg/agentcore/adapters/internalllm`
- `pkg/agentcore/adapters/internaltools`
