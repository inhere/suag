# Suag - SupAgent OS

## Minimal Agentcore Embedding

```go
package main

import (
	"context"
	"log"

	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/storage/files"
	"github.com/inhere/suag/pkg/agentcore/mock"
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

If you want to bridge Suag's existing internal stacks into `agentcore`, use the adapters in:

- `internal/acadapters/internalllm`
- `internal/acadapters/internaltools`

## Agentcore Docs

- Package README: `pkg/agentcore/README.md`
- Docs index: `pkg/agentcore/docs/README.md`
- Getting started: `pkg/agentcore/docs/getting-started.md`
- API and extensibility guide: `pkg/agentcore/docs/api.md`
- Runnable examples: `pkg/agentcore/examples/`
- Internal bridge example: `examples/agentcore/bridge`
- Long-term v1 agent core blueprint: `docs/v1/01-agent-core.md`
