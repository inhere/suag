package memory_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/inhere/suag/pkg/agentcore"
	memorypkg "github.com/inhere/suag/pkg/agentcore/memory"
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

func TestWorkingMemory_ObserveMessages_AppendsInOrderAndTruncates(t *testing.T) {
	mem := memorypkg.New(memorypkg.Config{
		MaxRecentMessages:   3,
		IncludeToolMessages: true,
	})

	if err := mem.Hydrate(context.Background(), &agentcore.RunState{
		Messages: []agentcore.Message{
			{Role: "user", Content: "one"},
			{Role: "assistant", Content: "two"},
		},
	}); err != nil {
		t.Fatalf("Hydrate() error = %v", err)
	}

	err := mem.ObserveMessages(context.Background(), nil,
		agentcore.Message{Role: "assistant", ToolCall: &agentcore.ToolCall{CallID: "call-1", Name: "lookup"}},
		agentcore.Message{Role: "tool", ToolResult: &agentcore.ToolResult{CallID: "call-1", Name: "lookup", Content: "result"}},
		agentcore.Message{Role: "assistant", Content: "final"},
	)
	if err != nil {
		t.Fatalf("ObserveMessages() error = %v", err)
	}

	msgs, err := mem.BuildContext(context.Background(), nil)
	if err != nil {
		t.Fatalf("BuildContext() error = %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("context messages = %#v, want last 3 observed messages", msgs)
	}
	if msgs[0].Role != "assistant" || msgs[0].ToolCall == nil || msgs[0].ToolCall.Name != "lookup" {
		t.Fatalf("first observed message = %#v, want assistant tool call", msgs[0])
	}
	if msgs[1].Role != "tool" || msgs[1].ToolResult == nil || msgs[1].ToolResult.Content != "result" {
		t.Fatalf("second observed message = %#v, want tool result", msgs[1])
	}
	if msgs[2].Role != "assistant" || msgs[2].Content != "final" {
		t.Fatalf("third observed message = %#v, want final assistant output", msgs[2])
	}
}

func TestWorkingMemory_ObserveMessages_MatchesHydrateSemantics(t *testing.T) {
	cfg := memorypkg.Config{
		MaxRecentMessages:     3,
		IncludeSystemMessages: false,
		IncludeToolMessages:   false,
	}
	base := []agentcore.Message{
		{Role: "system", Content: "internal"},
		{Role: "user", Content: "one"},
	}
	appended := []agentcore.Message{
		{Role: "assistant", ToolCall: &agentcore.ToolCall{CallID: "call-1", Name: "lookup"}},
		{Role: "tool", ToolResult: &agentcore.ToolResult{CallID: "call-1", Name: "lookup", Content: "result"}},
		{Role: "assistant", Content: "two"},
		{Role: "user", Content: "three"},
	}

	incremental := memorypkg.New(cfg)
	if err := incremental.Hydrate(context.Background(), &agentcore.RunState{Messages: base}); err != nil {
		t.Fatalf("incremental Hydrate() error = %v", err)
	}
	if err := incremental.ObserveMessages(context.Background(), nil, appended...); err != nil {
		t.Fatalf("ObserveMessages() error = %v", err)
	}
	incrementalContext, err := incremental.BuildContext(context.Background(), nil)
	if err != nil {
		t.Fatalf("incremental BuildContext() error = %v", err)
	}

	rebuilt := memorypkg.New(cfg)
	fullTranscript := append(append([]agentcore.Message{}, base...), appended...)
	if err := rebuilt.Hydrate(context.Background(), &agentcore.RunState{Messages: fullTranscript}); err != nil {
		t.Fatalf("rebuilt Hydrate() error = %v", err)
	}
	rebuiltContext, err := rebuilt.BuildContext(context.Background(), nil)
	if err != nil {
		t.Fatalf("rebuilt BuildContext() error = %v", err)
	}

	if !reflect.DeepEqual(incrementalContext, rebuiltContext) {
		t.Fatalf("observe/hydrate mismatch: incremental=%#v rebuilt=%#v", incrementalContext, rebuiltContext)
	}
}

func TestWorkingMemory_ObserveMessages_DoesNotOverwriteSummary(t *testing.T) {
	mem := memorypkg.New(memorypkg.Config{})
	mem.SetSummary("keep summary")

	if err := mem.ObserveMessages(context.Background(), nil,
		agentcore.Message{Role: "user", Content: "hello"},
	); err != nil {
		t.Fatalf("ObserveMessages() error = %v", err)
	}

	if got := mem.Summary(); got != "keep summary" {
		t.Fatalf("summary = %q, want preserved summary", got)
	}
}

func TestWorkingMemory_ToolCallMessagesKeepText(t *testing.T) {
	hydrate := func(t *testing.T, cfg memorypkg.Config) []agentcore.Message {
		t.Helper()
		mem := memorypkg.New(cfg)
		err := mem.Hydrate(context.Background(), &agentcore.RunState{Messages: []agentcore.Message{
			{Role: "user", Content: "q"},
			{Role: "assistant", Content: "let me look", ToolCall: &agentcore.ToolCall{CallID: "c1", Name: "file_read"}},
			{Role: "tool", ToolResult: &agentcore.ToolResult{CallID: "c1", Name: "file_read", Content: "data"}},
			{Role: "assistant", ToolCall: &agentcore.ToolCall{CallID: "c2", Name: "file_read"}},
			{Role: "assistant", Content: "  ", ToolCall: &agentcore.ToolCall{CallID: "c3", Name: "file_read"}},
			{Role: "assistant", Content: "done"},
		}})
		if err != nil {
			t.Fatalf("Hydrate() error = %v", err)
		}
		msgs, err := mem.BuildContext(context.Background(), nil)
		if err != nil {
			t.Fatalf("BuildContext() error = %v", err)
		}
		return msgs
	}

	t.Run("text is kept and the call is dropped", func(t *testing.T) {
		msgs := hydrate(t, memorypkg.Config{})
		var contents []string
		for _, m := range msgs {
			if m.ToolCall != nil || m.ToolResult != nil || m.Role == "tool" {
				t.Fatalf("orphan tool data in %#v", m)
			}
			contents = append(contents, m.Content)
		}
		if want := []string{"q", "let me look", "done"}; !reflect.DeepEqual(contents, want) {
			t.Fatalf("contents = %#v, want %#v (pure tool calls filtered)", contents, want)
		}
	})

	t.Run("IncludeToolMessages keeps everything", func(t *testing.T) {
		msgs := hydrate(t, memorypkg.Config{IncludeToolMessages: true})
		if len(msgs) != 6 || msgs[1].ToolCall == nil {
			t.Fatalf("msgs = %#v", msgs)
		}
	})
}
