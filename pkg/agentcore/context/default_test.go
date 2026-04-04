package context_test

import (
	stdctx "context"
	"fmt"
	"testing"

	"github.com/inhere/axon/pkg/agentcore"
	builderctx "github.com/inhere/axon/pkg/agentcore/context"
)

func TestDefaultContextBuilder_Truncates(t *testing.T) {
	builder := builderctx.DefaultBuilder{MaxMessages: 3}

	msgs, err := builder.BuildMessages(stdctx.Background(), stateWithMessages(5))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "message-3" || msgs[2].Content != "message-5" {
		t.Fatalf("expected the latest messages, got %#v", msgs)
	}
}

func stateWithMessages(count int) *agentcore.RunState {
	state := &agentcore.RunState{
		RunID:    "run-1",
		AgentID:  "agent-1",
		Messages: make([]agentcore.Message, 0, count),
	}
	for i := 1; i <= count; i++ {
		state.Messages = append(state.Messages, agentcore.Message{
			Role:    "user",
			Content: fmt.Sprintf("message-%d", i),
		})
	}
	return state
}
