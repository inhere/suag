package agentcore

import (
	"context"
	"errors"
	"testing"
)

func TestLoadAuthorityBundle_RehydratesMessagesAndArtifacts(t *testing.T) {
	store := newRuntimeTestStorage()
	store.runs["run-authority-view"] = &RunState{
		RunID:   "run-authority-view",
		AgentID: "agent-1",
		Status:  RunYielded,
		Step:    1,
		Messages: []Message{
			{Role: "assistant", Content: "stale-snapshot"},
		},
	}
	store.messages["run-authority-view"] = []Message{
		{Role: "user", Content: "persisted question"},
		{Role: "assistant", Content: "persisted answer"},
	}
	store.artifacts["run-authority-view"] = []Artifact{{
		Kind: "file",
		URI:  "artifact://authority.txt",
	}}

	got, err := LoadAuthorityBundle(context.Background(), store, "run-authority-view")
	if err != nil {
		t.Fatalf("expected hydrated authority bundle, got %v", err)
	}
	if got == nil || got.State == nil || got.State.RunID != "run-authority-view" {
		t.Fatalf("expected loaded authority bundle, got %#v", got)
	}
	if len(got.Messages) != 2 || got.Messages[0].Content != "persisted question" || got.Messages[1].Content != "persisted answer" {
		t.Fatalf("expected messages to come from authoritative log, got %#v", got.Messages)
	}
	if len(got.Artifacts) != 1 || got.Artifacts[0].URI != "artifact://authority.txt" {
		t.Fatalf("expected artifacts to come from optional artifact capability, got %#v", got.Artifacts)
	}

	hydrated := got.HydratedState()
	if len(hydrated.Messages) != 2 || hydrated.Messages[1].Content != "persisted answer" {
		t.Fatalf("expected hydrated state to include authority messages, got %#v", hydrated.Messages)
	}
	if len(hydrated.Artifacts) != 1 || hydrated.Artifacts[0].URI != "artifact://authority.txt" {
		t.Fatalf("expected hydrated state to include authority artifacts, got %#v", hydrated.Artifacts)
	}
}

func TestLoadRunAuthority_ReusesAuthorityBundleHydration(t *testing.T) {
	store := &runtimeSnapshotOnlyStorage{
		runs: map[string]*RunState{
			"run-snapshot-artifacts": {
				RunID:   "run-snapshot-artifacts",
				AgentID: "agent-1",
				Status:  RunCompleted,
				Artifacts: []Artifact{{
					Kind: "file",
					URI:  "artifact://snapshot.txt",
				}},
			},
		},
		messages: map[string][]Message{
			"run-snapshot-artifacts": {
				{Role: "assistant", Content: "done"},
			},
		},
	}

	got, err := LoadRunAuthority(context.Background(), store, "run-snapshot-artifacts")
	if err != nil {
		t.Fatalf("expected snapshot-only authority load to succeed, got %v", err)
	}
	if len(got.Artifacts) != 1 || got.Artifacts[0].URI != "artifact://snapshot.txt" {
		t.Fatalf("expected snapshot artifacts fallback, got %#v", got.Artifacts)
	}
}

func TestLoadAuthorityBundle_PropagatesArtifactCapabilityErrors(t *testing.T) {
	store := newRuntimeTestStorage()
	store.runs["run-artifact-load-fail"] = &RunState{
		RunID:   "run-artifact-load-fail",
		AgentID: "agent-1",
		Status:  RunYielded,
	}
	store.messages["run-artifact-load-fail"] = []Message{{Role: "user", Content: "persisted"}}
	store.artifactLoadErr = &StorageError{Err: errors.New("artifact store unavailable")}

	_, err := LoadAuthorityBundle(context.Background(), store, "run-artifact-load-fail")
	if err == nil {
		t.Fatal("expected artifact capability error, got nil")
	}
	var storageErr *StorageError
	if !errors.As(err, &storageErr) {
		t.Fatalf("expected storage error wrapper, got %T %v", err, err)
	}
}

func TestAuthorityBundle_LastAssistantMessageReturnsCopy(t *testing.T) {
	bundle := &AuthorityBundle{
		Messages: []Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "draft"},
			{Role: "tool", Content: "tool-output"},
			{Role: "assistant", Content: "final", Metadata: map[string]any{"stage": "final"}},
		},
	}

	msg, ok := bundle.LastAssistantMessage()
	if !ok {
		t.Fatal("expected last assistant message to be found")
	}
	if msg.Content != "final" || msg.Metadata["stage"] != "final" {
		t.Fatalf("expected final assistant message, got %#v", msg)
	}

	msg.Content = "mutated"
	msg.Metadata["stage"] = "changed"
	if bundle.Messages[3].Content != "final" || bundle.Messages[3].Metadata["stage"] != "final" {
		t.Fatalf("expected returned message to be cloned, got %#v", bundle.Messages[3])
	}
}

func TestAuthorityBundle_ToolMessagesReturnsCopiesInOrder(t *testing.T) {
	bundle := &AuthorityBundle{
		Messages: []Message{
			{Role: "user", Content: "hello"},
			{
				Role:    "tool",
				Content: "tool-1",
				ToolResult: &ToolResult{
					CallID:  "call-1",
					Name:    "lookup",
					Content: "tool-1",
					Metadata: map[string]any{
						"source": "first",
					},
				},
			},
			{Role: "assistant", Content: "done"},
			{
				Role:    "tool",
				Content: "tool-2",
				ToolResult: &ToolResult{
					CallID:  "call-2",
					Name:    "search",
					Content: "tool-2",
				},
			},
		},
	}

	msgs := bundle.ToolMessages()
	if len(msgs) != 2 {
		t.Fatalf("expected two tool messages, got %#v", msgs)
	}
	if msgs[0].ToolResult == nil || msgs[0].ToolResult.CallID != "call-1" || msgs[1].ToolResult == nil || msgs[1].ToolResult.CallID != "call-2" {
		t.Fatalf("expected tool messages in order, got %#v", msgs)
	}

	msgs[0].ToolResult.Metadata["source"] = "changed"
	if bundle.Messages[1].ToolResult.Metadata["source"] != "first" {
		t.Fatalf("expected returned tool messages to be cloned, got %#v", bundle.Messages[1].ToolResult)
	}
}

func TestAuthorityBundle_FindArtifactByURIReturnsCopy(t *testing.T) {
	bundle := &AuthorityBundle{
		Artifacts: []Artifact{
			{Kind: "file", URI: "artifact://one.txt"},
			{Kind: "file", URI: "artifact://two.txt", Metadata: map[string]any{"stage": "final"}},
		},
	}

	artifact, ok := bundle.FindArtifactByURI("artifact://two.txt")
	if !ok {
		t.Fatal("expected artifact to be found")
	}
	if artifact.URI != "artifact://two.txt" || artifact.Metadata["stage"] != "final" {
		t.Fatalf("unexpected artifact result: %#v", artifact)
	}

	artifact.Metadata["stage"] = "changed"
	if bundle.Artifacts[1].Metadata["stage"] != "final" {
		t.Fatalf("expected returned artifact to be cloned, got %#v", bundle.Artifacts[1])
	}
}

type runtimeSnapshotOnlyStorage struct {
	runs     map[string]*RunState
	messages map[string][]Message
}

func (s *runtimeSnapshotOnlyStorage) LoadRun(_ context.Context, runID string) (*RunState, error) {
	return cloneRunState(s.runs[runID]), nil
}

func (s *runtimeSnapshotOnlyStorage) LoadMessages(_ context.Context, runID string) ([]Message, error) {
	return cloneMessages(s.messages[runID]), nil
}

func (s *runtimeSnapshotOnlyStorage) SaveRun(context.Context, *RunState) error { return nil }

func (s *runtimeSnapshotOnlyStorage) AppendMessages(context.Context, string, ...Message) error {
	return nil
}
