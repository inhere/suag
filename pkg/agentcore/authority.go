package agentcore

import (
	"context"
	"errors"
)

// AuthorityBundle exposes the authoritative storage view as explicit parts.
type AuthorityBundle struct {
	State     *RunState
	Messages  []Message
	Artifacts []Artifact
}

// LastAssistantMessage returns the latest assistant message from the authority transcript.
func (b *AuthorityBundle) LastAssistantMessage() (Message, bool) {
	if b == nil {
		return Message{}, false
	}
	for i := len(b.Messages) - 1; i >= 0; i-- {
		if b.Messages[i].Role == "assistant" {
			return cloneMessagesValue([]Message{b.Messages[i]})[0], true
		}
	}
	return Message{}, false
}

// ToolMessages returns all authority transcript messages emitted by tools.
func (b *AuthorityBundle) ToolMessages() []Message {
	if b == nil || len(b.Messages) == 0 {
		return nil
	}
	out := make([]Message, 0, len(b.Messages))
	for _, msg := range b.Messages {
		if msg.Role == "tool" {
			out = append(out, msg)
		}
	}
	return cloneMessagesValue(out)
}

// FindArtifactByURI returns the first artifact with the given URI.
func (b *AuthorityBundle) FindArtifactByURI(uri string) (Artifact, bool) {
	if b == nil {
		return Artifact{}, false
	}
	for _, artifact := range b.Artifacts {
		if artifact.URI == uri {
			return cloneArtifactsValue([]Artifact{artifact})[0], true
		}
	}
	return Artifact{}, false
}

// HydratedState returns a run state rebuilt from the explicit authority parts.
func (b *AuthorityBundle) HydratedState() *RunState {
	if b == nil || b.State == nil {
		return nil
	}
	state := cloneRunStateValue(b.State)
	state.Messages = cloneMessagesValue(b.Messages)
	state.Artifacts = cloneArtifactsValue(b.Artifacts)
	return state
}

// LoadAuthorityBundle rebuilds the explicit authoritative run bundle from snapshot,
// transcript, and any optional artifact storage capability exposed by the storage backend.
func LoadAuthorityBundle(ctx context.Context, storage Storage, runID string) (*AuthorityBundle, error) {
	if storage == nil {
		return nil, &StorageError{Err: errors.New("storage is required")}
	}

	loaded, err := storage.LoadRun(ctx, runID)
	if err != nil {
		return nil, wrapStorageError(err)
	}
	if loaded == nil {
		return nil, nil
	}

	messages, err := storage.LoadMessages(ctx, runID)
	if err != nil {
		return nil, wrapStorageError(err)
	}

	artifacts := append([]Artifact(nil), loaded.Artifacts...)
	if artifactStore, ok := storage.(ArtifactStorage); ok {
		loadedArtifacts, err := artifactStore.LoadArtifacts(ctx, runID)
		if err != nil && !isStorageMiss(err) {
			return nil, wrapStorageError(err)
		}
		if err == nil {
			artifacts = append([]Artifact(nil), loadedArtifacts...)
		}
	}

	return &AuthorityBundle{
		State:     cloneRunStateValue(loaded),
		Messages:  cloneMessagesValue(messages),
		Artifacts: artifacts,
	}, nil
}

// LoadRunAuthority rebuilds the authoritative run view from snapshot, transcript,
// and any optional artifact storage capability exposed by the storage backend.
func LoadRunAuthority(ctx context.Context, storage Storage, runID string) (*RunState, error) {
	bundle, err := LoadAuthorityBundle(ctx, storage, runID)
	if err != nil {
		return nil, err
	}
	if bundle == nil {
		return nil, nil
	}
	return bundle.HydratedState(), nil
}
