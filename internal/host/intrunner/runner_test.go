package intrunner

import (
	"context"
	"testing"

	"github.com/gookit/goutil/testutil/assert"

	"github.com/inhere/axon/internal/platform/llm"
	"github.com/inhere/axon/internal/platform/tools"
	"github.com/inhere/axon/internal/platform/tracing"
	"github.com/inhere/axon/pkg/agentcore/api"
)

func TestRunner_RunReusesSessionMemoryAcrossTurns(t *testing.T) {
	t.Parallel()

	mock := llm.NewMockProvider("mock").
		QueueText("first answer").
		QueueText("second answer")

	runner, err := New(Config{
		Provider: mock,
		Registry: tools.NewRegistry(),
		Tracer:   tracing.NewInMemoryTracer(nil),
		RunRoot:  t.TempDir(),
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	_, err = runner.Run(context.Background(), Request{
		SessionID:   "sess_001",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "first task",
	})
	assert.NoErr(t, err)

	_, err = runner.Run(context.Background(), Request{
		SessionID:   "sess_001",
		TurnID:      "turn_002",
		RunID:       "run_002",
		AgentID:     "default",
		Instruction: "second task",
	})
	assert.NoErr(t, err)

	calls := mock.Calls()
	assert.Eq(t, 2, len(calls))
	assert.True(t, containsMessage(calls[1].Messages, "user", "first task"))
	assert.True(t, containsMessage(calls[1].Messages, "assistant", "first answer"))
}

func TestRunner_RunEmitsPreparingAndThinkingStates(t *testing.T) {
	t.Parallel()

	mock := llm.NewMockProvider("mock").QueueText("done")

	runner, err := New(Config{
		Provider: mock,
		Registry: tools.NewRegistry(),
		Tracer:   tracing.NewInMemoryTracer(nil),
		RunRoot:  t.TempDir(),
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	var states []string
	_, err = runner.Run(context.Background(), Request{
		SessionID:   "sess_001",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "do something",
		Emit: func(event Event) error {
			if event.EventType != "run.status" {
				return nil
			}
			state, _ := event.Payload["state"].(string)
			states = append(states, state)
			return nil
		},
	})
	assert.NoErr(t, err)
	assert.Eq(t, 2, len(states))
	assert.Eq(t, "PREPARING", states[0])
	assert.Eq(t, "THINKING", states[1])
}

func containsMessage(messages []api.Message, role, content string) bool {
	for _, msg := range messages {
		if msg.Role == role && msg.Content == content {
			return true
		}
	}
	return false
}
