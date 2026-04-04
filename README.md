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

- `internal/agentcoreadapters/internalllm`
- `internal/agentcoreadapters/internaltools`

## Agentcore Docs

- Quickstart: `docs/usage/agentcore-quickstart.md`
- API and extensibility guide: `docs/usage/agentcore-api.md`
- Implementation-oriented design note: `docs/usage/agentcore.md`
- Package README: `pkg/agentcore/README.md`
- Runnable examples: `examples/agentcore/`
- Long-term v1 agent core blueprint: `docs/v1/01-agent-core.md`
