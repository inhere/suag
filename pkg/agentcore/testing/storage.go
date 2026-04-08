package agentcoretesting

import (
	"context"
	"reflect"
	stdtesting "testing"

	"github.com/inhere/axon/pkg/agentcore"
	"github.com/inhere/axon/pkg/agentcore/mock"
)

// StorageFactory creates an isolated storage implementation for one test case.
type StorageFactory func(t *stdtesting.T) agentcore.Storage

// StoragePathFactory creates a storage rooted at an isolated directory path.
type StoragePathFactory func(t *stdtesting.T, root string) agentcore.Storage

// NewStorageFactoryFromPath adapts a path-based storage constructor into a
// StorageFactory backed by t.TempDir().
func NewStorageFactoryFromPath(factory StoragePathFactory) StorageFactory {
	return func(t *stdtesting.T) agentcore.Storage {
		t.Helper()
		if factory == nil {
			t.Fatal("agentcoretesting: storage path factory is nil")
		}
		return factory(t, t.TempDir())
	}
}

// RunStorageSuite runs the minimal public storage conformance checks.
func RunStorageSuite(t *stdtesting.T, factory StorageFactory) {
	t.Helper()
	if factory == nil {
		t.Fatal("agentcoretesting: storage factory is nil")
	}

	t.Run("save_and_load_run_round_trip", func(t *stdtesting.T) {
		t.Helper()
		store := factory(t)
		if store == nil {
			t.Fatal("agentcoretesting: storage factory returned nil")
		}

		state := &agentcore.RunState{
			RunID:   "run-state-round-trip",
			AgentID: "agent-1",
			Status:  agentcore.RunYielded,
			Step:    2,
			Artifacts: []agentcore.Artifact{{
				Kind: "file",
				URI:  "artifact://note.txt",
			}},
			Metadata: map[string]any{
				"scope": "storage-suite",
			},
			Scratch: map[string]any{
				"phase": "round-trip",
			},
			Usage:     agentcore.Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5},
			LastError: "recoverable",
		}

		if err := store.SaveRun(context.Background(), state); err != nil {
			t.Fatalf("expected SaveRun to succeed, got %v", err)
		}

		got, err := store.LoadRun(context.Background(), state.RunID)
		if err != nil {
			t.Fatalf("expected LoadRun to succeed, got %v", err)
		}
		if got == nil {
			t.Fatal("expected LoadRun to return a run state")
		}
		if got.RunID != state.RunID || got.AgentID != state.AgentID || got.Status != state.Status || got.Step != state.Step {
			t.Fatalf("expected run identity fields to round-trip, got %#v", got)
		}
		if !reflect.DeepEqual(got.Artifacts, state.Artifacts) {
			t.Fatalf("expected artifacts to round-trip, got %#v", got.Artifacts)
		}
		if !reflect.DeepEqual(got.Metadata, state.Metadata) {
			t.Fatalf("expected metadata to round-trip, got %#v", got.Metadata)
		}
		if !reflect.DeepEqual(got.Scratch, state.Scratch) {
			t.Fatalf("expected scratch to round-trip, got %#v", got.Scratch)
		}
		if got.Usage != state.Usage || got.LastError != state.LastError {
			t.Fatalf("expected usage and last error to round-trip, got %#v", got)
		}
	})

	t.Run("append_and_load_messages_preserves_order", func(t *stdtesting.T) {
		t.Helper()
		store := factory(t)
		if store == nil {
			t.Fatal("agentcoretesting: storage factory returned nil")
		}

		runID := "run-message-round-trip"
		if err := store.SaveRun(context.Background(), &agentcore.RunState{
			RunID:   runID,
			AgentID: "agent-1",
			Status:  agentcore.RunRunning,
		}); err != nil {
			t.Fatalf("expected SaveRun to initialize storage layout, got %v", err)
		}

		want := []agentcore.Message{
			{Role: "user", Content: "hello"},
			{
				Role: "assistant",
				ToolCall: &agentcore.ToolCall{
					CallID:    "call-1",
					Name:      "echo",
					Arguments: map[string]any{"text": "hello"},
				},
			},
			{
				Role:    "tool",
				Content: "tool:hello",
				ToolResult: &agentcore.ToolResult{
					CallID:  "call-1",
					Name:    "echo",
					Content: "tool:hello",
				},
			},
		}

		if err := store.AppendMessages(context.Background(), runID, want...); err != nil {
			t.Fatalf("expected AppendMessages to succeed, got %v", err)
		}

		got, err := store.LoadMessages(context.Background(), runID)
		if err != nil {
			t.Fatalf("expected LoadMessages to succeed, got %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("expected messages to preserve append order and shape, got %#v", got)
		}
	})

	t.Run("runtime_resume_replays_authority_transcript", func(t *stdtesting.T) {
		t.Helper()
		store := factory(t)
		if store == nil {
			t.Fatal("agentcoretesting: storage factory returned nil")
		}

		runID := "run-storage-runtime-resume"
		tools := agentcore.NewToolRegistry()
		tools.Register(mock.NewTool("echo"))

		yieldHook := &yieldOnSecondModelHook{}
		firstRuntime := agentcore.New(agentcore.Config{
			Provider: mock.NewProvider(mock.ToolCall("call-1", "echo", map[string]any{"text": "persisted"})),
			Storage:  store,
			Tools:    tools,
			Hooks:    []agentcore.Hook{yieldHook},
		})

		firstResult, err := firstRuntime.Run(context.Background(), agentcore.RunInput{
			RunID:       runID,
			AgentID:     "agent-1",
			Instruction: "use the tool and pause",
		})
		if err != nil {
			t.Fatalf("expected first run to yield cleanly, got %v", err)
		}
		if firstResult == nil || firstResult.Status != agentcore.RunYielded {
			t.Fatalf("expected yielded first result, got %#v", firstResult)
		}

		resumeRuntime := agentcore.New(agentcore.Config{
			Provider: mock.NewProvider(mock.Final("done after resume")),
			Storage:  store,
			Tools:    tools,
		})

		resumed, err := resumeRuntime.Run(context.Background(), agentcore.RunInput{
			RunID:      runID,
			ResumeMode: agentcore.ResumeFromState,
		})
		if err != nil {
			t.Fatalf("expected resumed run to complete, got %v", err)
		}
		if resumed == nil || resumed.Status != agentcore.RunCompleted || resumed.Output != "done after resume" {
			t.Fatalf("expected completed resumed result, got %#v", resumed)
		}

		state, err := store.LoadRun(context.Background(), runID)
		if err != nil {
			t.Fatalf("expected final LoadRun to succeed, got %v", err)
		}
		if state == nil || state.Status != agentcore.RunCompleted {
			t.Fatalf("expected completed final state, got %#v", state)
		}

		messages, err := store.LoadMessages(context.Background(), runID)
		if err != nil {
			t.Fatalf("expected final LoadMessages to succeed, got %v", err)
		}
		if len(messages) != 4 {
			t.Fatalf("expected user, assistant tool-call, tool result, and final assistant output, got %#v", messages)
		}
		if messages[0].Role != "user" || messages[0].Content != "use the tool and pause" {
			t.Fatalf("expected original user instruction to remain authoritative, got %#v", messages[0])
		}
		if messages[1].Role != "assistant" || messages[1].ToolCall == nil || messages[1].ToolCall.CallID != "call-1" {
			t.Fatalf("expected persisted assistant tool-call message, got %#v", messages[1])
		}
		if messages[2].Role != "tool" || messages[2].ToolResult == nil || messages[2].ToolResult.Content != "persisted" {
			t.Fatalf("expected persisted tool result message, got %#v", messages[2])
		}
		if messages[3].Role != "assistant" || messages[3].Content != "done after resume" {
			t.Fatalf("expected final assistant answer appended after resume, got %#v", messages[3])
		}
	})
}

type yieldOnSecondModelHook struct {
	agentcore.NopHook
	beforeModelCalls int
}

func (h *yieldOnSecondModelHook) BeforeModel(_ context.Context, _ *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	h.beforeModelCalls++
	if h.beforeModelCalls == 2 {
		ctrl.Yield("await_resume", map[string]any{"source": "storage-suite"})
	}
	return nil
}
