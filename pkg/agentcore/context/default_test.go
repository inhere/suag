package context_test

import (
	stdctx "context"
	"errors"
	"fmt"
	"testing"

	"github.com/inhere/axon/pkg/agentcore"
	builderctx "github.com/inhere/axon/pkg/agentcore/context"
)

type contextTestMemory struct {
	context  []agentcore.Message
	buildErr error
}

func (m *contextTestMemory) Hydrate(stdctx.Context, *agentcore.RunState) error { return nil }

func (m *contextTestMemory) ObserveMessages(stdctx.Context, *agentcore.RunState, ...agentcore.Message) error {
	return nil
}

func (m *contextTestMemory) BuildContext(stdctx.Context, *agentcore.RunState) ([]agentcore.Message, error) {
	if m.buildErr != nil {
		return nil, m.buildErr
	}
	if len(m.context) == 0 {
		return nil, nil
	}
	msgs := make([]agentcore.Message, len(m.context))
	copy(msgs, m.context)
	return msgs, nil
}

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

func TestTranscriptWindow_TruncatesLatestMessages(t *testing.T) {
	builder := builderctx.TranscriptWindow(builderctx.WindowConfig{MaxMessages: 2})

	msgs, err := builder.BuildMessages(stdctx.Background(), stateWithMessages(4))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %#v", msgs)
	}
	if msgs[0].Content != "message-3" || msgs[1].Content != "message-4" {
		t.Fatalf("expected the last 2 messages, got %#v", msgs)
	}
}

func TestTranscriptWindow_ReturnsCopies(t *testing.T) {
	state := stateWithMessages(2)
	builder := builderctx.TranscriptWindow(builderctx.WindowConfig{})

	msgs, err := builder.BuildMessages(stdctx.Background(), state)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	msgs[0].Content = "mutated"

	if state.Messages[0].Content != "message-1" {
		t.Fatalf("expected original state to remain unchanged, got %#v", state.Messages)
	}
}

func TestMemoryFirst_UsesMemoryWhenPresent(t *testing.T) {
	mem := &contextTestMemory{
		context: []agentcore.Message{{Role: "system", Content: "from-memory"}},
	}
	builder := builderctx.MemoryFirst(mem, builderctx.MemoryFirstConfig{MaxMessages: 2})

	msgs, err := builder.BuildMessages(stdctx.Background(), stateWithMessages(4))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "from-memory" {
		t.Fatalf("expected memory output to win, got %#v", msgs)
	}
}

func TestMemoryFirst_FallsBackOnlyWhenMemoryNil(t *testing.T) {
	builder := builderctx.MemoryFirst(nil, builderctx.MemoryFirstConfig{MaxMessages: 2})

	msgs, err := builder.BuildMessages(stdctx.Background(), stateWithMessages(4))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected fallback transcript window, got %#v", msgs)
	}
	if msgs[0].Content != "message-3" || msgs[1].Content != "message-4" {
		t.Fatalf("expected latest transcript messages, got %#v", msgs)
	}
}

func TestMemoryFirst_DoesNotFallbackOnEmptyMemoryContext(t *testing.T) {
	mem := &contextTestMemory{}
	builder := builderctx.MemoryFirst(mem, builderctx.MemoryFirstConfig{MaxMessages: 2})

	msgs, err := builder.BuildMessages(stdctx.Background(), stateWithMessages(4))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if msgs != nil {
		t.Fatalf("expected empty memory result to be preserved, got %#v", msgs)
	}
}

func TestMemoryFirst_PropagatesMemoryErrors(t *testing.T) {
	mem := &contextTestMemory{buildErr: errors.New("memory failed")}
	builder := builderctx.MemoryFirst(mem, builderctx.MemoryFirstConfig{})

	_, err := builder.BuildMessages(stdctx.Background(), stateWithMessages(1))
	if err == nil || err.Error() != "memory failed" {
		t.Fatalf("expected memory error to be returned, got %v", err)
	}
}

func TestMemoryThenRecent_AppendsRecentTranscriptAfterMemory(t *testing.T) {
	mem := &contextTestMemory{
		context: []agentcore.Message{{Role: "system", Content: "from-memory"}},
	}
	state := &agentcore.RunState{
		Messages: []agentcore.Message{
			{Role: "user", Content: "message-1"},
			{Role: "assistant", Content: "message-2"},
			{Role: "user", Content: "message-3"},
			{Role: "assistant", Content: "message-4"},
		},
	}
	builder := builderctx.MemoryThenRecent(mem, builderctx.MemoryThenRecentConfig{MaxMessages: 2})

	msgs, err := builder.BuildMessages(stdctx.Background(), state)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected memory + 2 recent transcript messages, got %#v", msgs)
	}
	if msgs[0].Content != "from-memory" {
		t.Fatalf("expected memory message first, got %#v", msgs)
	}
	if msgs[1].Content != "message-3" || msgs[2].Content != "message-4" {
		t.Fatalf("expected recent transcript window after memory output, got %#v", msgs)
	}
}

func TestMemoryThenRecent_UsesTranscriptWindowWhenMemoryNil(t *testing.T) {
	builder := builderctx.MemoryThenRecent(nil, builderctx.MemoryThenRecentConfig{MaxMessages: 2})

	msgs, err := builder.BuildMessages(stdctx.Background(), stateWithMessages(4))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected transcript fallback, got %#v", msgs)
	}
	if msgs[0].Content != "message-3" || msgs[1].Content != "message-4" {
		t.Fatalf("expected transcript fallback to preserve latest messages, got %#v", msgs)
	}
}

func TestMemoryThenRecent_DoesNotDeduplicateMessages(t *testing.T) {
	mem := &contextTestMemory{
		context: []agentcore.Message{{Role: "user", Content: "message-4"}},
	}
	state := &agentcore.RunState{
		Messages: []agentcore.Message{
			{Role: "user", Content: "message-1"},
			{Role: "assistant", Content: "message-2"},
			{Role: "user", Content: "message-3"},
			{Role: "user", Content: "message-4"},
		},
	}
	builder := builderctx.MemoryThenRecent(mem, builderctx.MemoryThenRecentConfig{MaxMessages: 2})

	msgs, err := builder.BuildMessages(stdctx.Background(), state)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected duplicate content to be preserved, got %#v", msgs)
	}
	if msgs[0].Content != "message-4" || msgs[2].Content != "message-4" {
		t.Fatalf("expected duplicate content from memory and transcript to remain, got %#v", msgs)
	}
}

func TestArtifactsThenRecent_AppendsProjectedArtifactsAfterTranscript(t *testing.T) {
	state := &agentcore.RunState{
		Messages: []agentcore.Message{
			{Role: "user", Content: "message-1"},
			{Role: "assistant", Content: "message-2"},
		},
		Artifacts: []agentcore.Artifact{
			{Kind: "file", URI: "artifact://one.txt", Content: "first artifact"},
			{Kind: "file", URI: "artifact://two.txt", Content: "second artifact"},
		},
	}
	builder := builderctx.ArtifactsThenRecent(builderctx.ArtifactContextConfig{
		MaxMessages: 2,
	})

	msgs, err := builder.BuildMessages(stdctx.Background(), state)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(msgs) != 4 {
		t.Fatalf("expected transcript plus two projected artifact messages, got %#v", msgs)
	}
	if msgs[0].Content != "message-1" || msgs[1].Content != "message-2" {
		t.Fatalf("expected transcript to stay first, got %#v", msgs)
	}
	if msgs[2].Role != "system" || msgs[2].Content != "Artifact(file): artifact://one.txt\nfirst artifact" {
		t.Fatalf("expected first projected artifact message, got %#v", msgs[2])
	}
	if msgs[3].Role != "system" || msgs[3].Content != "Artifact(file): artifact://two.txt\nsecond artifact" {
		t.Fatalf("expected second projected artifact message, got %#v", msgs[3])
	}
}

func TestArtifactsThenRecent_FiltersArtifacts(t *testing.T) {
	state := &agentcore.RunState{
		Messages: []agentcore.Message{
			{Role: "user", Content: "message-1"},
		},
		Artifacts: []agentcore.Artifact{
			{Kind: "file", URI: "artifact://keep.txt", Content: "keep"},
			{Kind: "image", URI: "artifact://skip.png", Content: "skip"},
		},
	}
	builder := builderctx.ArtifactsThenRecent(builderctx.ArtifactContextConfig{
		Filter: func(artifact agentcore.Artifact) bool {
			return artifact.Kind == "file"
		},
	})

	msgs, err := builder.BuildMessages(stdctx.Background(), state)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected transcript plus one filtered artifact, got %#v", msgs)
	}
	if msgs[1].Content != "Artifact(file): artifact://keep.txt\nkeep" {
		t.Fatalf("expected only filtered artifact to be projected, got %#v", msgs[1])
	}
}

func TestArtifactsThenRecent_UsesCustomFormatterAndReturnsCopies(t *testing.T) {
	state := &agentcore.RunState{
		Messages: []agentcore.Message{
			{Role: "user", Content: "message-1"},
		},
		Artifacts: []agentcore.Artifact{
			{Kind: "file", URI: "artifact://report.txt", Metadata: map[string]any{"topic": "authority"}},
		},
	}
	builder := builderctx.ArtifactsThenRecent(builderctx.ArtifactContextConfig{
		Formatter: func(artifact agentcore.Artifact) agentcore.Message {
			return agentcore.Message{
				Role:    "system",
				Content: fmt.Sprintf("Artifact topic=%v uri=%s", artifact.Metadata["topic"], artifact.URI),
				Metadata: map[string]any{
					"source": "artifact_projection",
				},
			}
		},
	})

	msgs, err := builder.BuildMessages(stdctx.Background(), state)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected transcript plus one formatted artifact, got %#v", msgs)
	}
	if msgs[1].Content != "Artifact topic=authority uri=artifact://report.txt" || msgs[1].Metadata["source"] != "artifact_projection" {
		t.Fatalf("expected custom artifact formatting, got %#v", msgs[1])
	}

	msgs[1].Content = "mutated"
	msgs[1].Metadata["source"] = "changed"
	if state.Artifacts[0].URI != "artifact://report.txt" || state.Artifacts[0].Metadata["topic"] != "authority" {
		t.Fatalf("expected artifact state to remain unchanged, got %#v", state.Artifacts[0])
	}
}

func TestDefaultArtifactFormatter_IncludesContent(t *testing.T) {
	msg := builderctx.DefaultArtifactFormatter(agentcore.Artifact{
		Kind:    "file",
		URI:     "artifact://report.txt",
		Content: "report body",
	})

	if msg.Role != "system" {
		t.Fatalf("expected system role, got %#v", msg)
	}
	if msg.Content != "Artifact(file): artifact://report.txt\nreport body" {
		t.Fatalf("unexpected formatted content: %#v", msg)
	}
}

func TestCompactArtifactFormatter_OmitsContent(t *testing.T) {
	msg := builderctx.CompactArtifactFormatter(agentcore.Artifact{
		Kind:    "file",
		URI:     "artifact://report.txt",
		Content: "report body",
	})

	if msg.Content != "Artifact(file): artifact://report.txt" {
		t.Fatalf("expected compact formatter to omit artifact content, got %#v", msg)
	}
}

func TestArtifactKindFilter_MatchesConfiguredKinds(t *testing.T) {
	filter := builderctx.ArtifactKindFilter("file", "text")

	if !filter(agentcore.Artifact{Kind: "file"}) {
		t.Fatal("expected file artifact to match filter")
	}
	if !filter(agentcore.Artifact{Kind: "text"}) {
		t.Fatal("expected text artifact to match filter")
	}
	if filter(agentcore.Artifact{Kind: "image"}) {
		t.Fatal("expected image artifact to be filtered out")
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
