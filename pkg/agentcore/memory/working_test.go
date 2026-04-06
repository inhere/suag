package memory_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/inhere/axon/pkg/agentcore"
	memorypkg "github.com/inhere/axon/pkg/agentcore/memory"
)

type memoryTestInjector struct{}

func (memoryTestInjector) InjectSummary(summary string, _ *agentcore.RunState) []agentcore.Message {
	if summary == "" {
		return nil
	}
	return []agentcore.Message{{
		Role:    "assistant",
		Content: "custom: " + summary,
	}}
}

func TestWorkingMemory_ImplementsAgentcoreMemory(t *testing.T) {
	var _ agentcore.Memory = (*memorypkg.WorkingMemory)(nil)
}

func TestWorkingMemory_BuildContext_IncludesSummaryThenRecentMessages(t *testing.T) {
	mem := memorypkg.New(memorypkg.Config{MaxRecentMessages: 2})
	mem.SetSummary("repo summary")

	err := mem.Hydrate(context.Background(), &agentcore.RunState{
		Messages: []agentcore.Message{
			{Role: "user", Content: "first"},
			{Role: "assistant", Content: "second"},
			{Role: "user", Content: "third"},
		},
	})
	if err != nil {
		t.Fatalf("Hydrate() error = %v", err)
	}

	msgs, err := mem.BuildContext(context.Background(), nil)
	if err != nil {
		t.Fatalf("BuildContext() error = %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("context messages = %#v, want summary + last 2 recent messages", msgs)
	}
	if msgs[0].Role != "system" {
		t.Fatalf("summary role = %q, want system", msgs[0].Role)
	}
	if msgs[0].Content != "[Memory summary]\nrepo summary" {
		t.Fatalf("summary content = %q, want default summary prefix", msgs[0].Content)
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "second" {
		t.Fatalf("first recent message = %#v, want second assistant message", msgs[1])
	}
	if msgs[2].Role != "user" || msgs[2].Content != "third" {
		t.Fatalf("second recent message = %#v, want trailing user message", msgs[2])
	}
}

func TestWorkingMemory_Hydrate_IsIdempotent(t *testing.T) {
	state := &agentcore.RunState{
		Messages: []agentcore.Message{
			{Role: "user", Content: "one"},
			{Role: "assistant", Content: "two"},
		},
	}
	mem := memorypkg.New(memorypkg.Config{})

	if err := mem.Hydrate(context.Background(), state); err != nil {
		t.Fatalf("first Hydrate() error = %v", err)
	}
	first, err := mem.BuildContext(context.Background(), nil)
	if err != nil {
		t.Fatalf("first BuildContext() error = %v", err)
	}

	if err := mem.Hydrate(context.Background(), state); err != nil {
		t.Fatalf("second Hydrate() error = %v", err)
	}
	second, err := mem.BuildContext(context.Background(), nil)
	if err != nil {
		t.Fatalf("second BuildContext() error = %v", err)
	}

	if len(first) != len(second) {
		t.Fatalf("context lengths differ: first=%d second=%d", len(first), len(second))
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("context drift after repeated hydrate: first=%#v second=%#v", first, second)
	}
}

func TestWorkingMemory_BuildContext_FiltersSystemAndToolMessages(t *testing.T) {
	mem := memorypkg.New(memorypkg.Config{
		IncludeSystemMessages: false,
		IncludeToolMessages:   false,
	})

	err := mem.Hydrate(context.Background(), &agentcore.RunState{
		Messages: []agentcore.Message{
			{Role: "system", Content: "internal"},
			{Role: "user", Content: "question"},
			{Role: "assistant", ToolCall: &agentcore.ToolCall{CallID: "call-1", Name: "lookup"}},
			{Role: "tool", ToolResult: &agentcore.ToolResult{CallID: "call-1", Name: "lookup", Content: "result"}},
			{Role: "assistant", Content: "answer"},
		},
	})
	if err != nil {
		t.Fatalf("Hydrate() error = %v", err)
	}

	msgs, err := mem.BuildContext(context.Background(), nil)
	if err != nil {
		t.Fatalf("BuildContext() error = %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("context messages = %#v, want only user and assistant text messages", msgs)
	}
	if msgs[0].Role != "user" || msgs[0].Content != "question" {
		t.Fatalf("first kept message = %#v, want user question", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "answer" {
		t.Fatalf("second kept message = %#v, want assistant answer", msgs[1])
	}
}

func TestWorkingMemory_Hydrate_DoesNotClearSummary(t *testing.T) {
	mem := memorypkg.New(memorypkg.Config{})
	mem.SetSummary("keep me")

	if err := mem.Hydrate(context.Background(), &agentcore.RunState{
		Messages: []agentcore.Message{{Role: "user", Content: "hello"}},
	}); err != nil {
		t.Fatalf("Hydrate() error = %v", err)
	}

	if got := mem.Summary(); got != "keep me" {
		t.Fatalf("summary = %q, want preserved value", got)
	}
}

func TestWorkingMemory_BuildContext_UsesCustomSummaryInjector(t *testing.T) {
	mem := memorypkg.New(memorypkg.Config{SummaryInjector: memoryTestInjector{}})
	mem.SetSummary("custom")

	msgs, err := mem.BuildContext(context.Background(), nil)
	if err != nil {
		t.Fatalf("BuildContext() error = %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("context messages = %#v, want custom injected summary only", msgs)
	}
	if msgs[0].Role != "assistant" || msgs[0].Content != "custom: custom" {
		t.Fatalf("custom summary message = %#v, want custom injector output", msgs[0])
	}
}
