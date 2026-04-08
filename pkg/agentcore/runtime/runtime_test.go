package runtime

import (
	"context"
	"testing"

	"github.com/inhere/axon/pkg/agentcore"
	"github.com/inhere/axon/pkg/agentcore/mock"
)

func TestNew_DelegatesToAgentcoreRuntime(t *testing.T) {
	rt := New(Config{
		Provider: mock.NewProvider(mock.Final("ok")),
	})

	result, err := rt.Run(context.Background(), agentcore.RunInput{
		Instruction: "hello",
	})
	if err != nil {
		t.Fatalf("expected runtime run to succeed, got %v", err)
	}
	if result.Status != agentcore.RunCompleted {
		t.Fatalf("expected completed status, got %#v", result)
	}
	if result.Output != "ok" {
		t.Fatalf("expected output ok, got %#v", result)
	}
}
