package agentcore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/inhere/suga/pkg/agentcore/internal/runid"
)

// MemoryStorage is the in-process Storage and ArtifactStorage used when
// Config.Storage is nil. Nothing survives the process, so resuming a run from
// another Runtime instance needs a persistent storage such as storage/files.
//
// Values are copied through JSON on save and load, giving callers the same
// isolation and value shapes as the file storage.
type MemoryStorage struct {
	mu   sync.Mutex
	runs map[string]*memoryRun
}

// memoryRun holds the encoded data of one run. A run exists once any save or
// append touched it; state stays nil until SaveRun.
type memoryRun struct {
	state     []byte
	messages  [][]byte
	artifacts []byte
}

var (
	_ Storage         = (*MemoryStorage)(nil)
	_ ArtifactStorage = (*MemoryStorage)(nil)
)

// NewMemoryStorage creates an empty in-memory storage.
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{runs: map[string]*memoryRun{}}
}

// LoadRun returns the saved run snapshot; a missing one is a storage miss.
func (s *MemoryStorage) LoadRun(_ context.Context, runID string) (*RunState, error) {
	if err := validateMemoryRunID(runID); err != nil {
		return nil, err
	}
	s.mu.Lock()
	run := s.runs[runID]
	var data []byte
	if run != nil {
		data = run.state
	}
	s.mu.Unlock()
	if data == nil {
		return nil, memoryMiss("load run snapshot", runID)
	}
	var state RunState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, memoryError("decode run snapshot", runID, err)
	}
	return &state, nil
}

// LoadMessages returns the appended messages in order; an unknown run is a
// storage miss, a known run without messages returns an empty slice.
func (s *MemoryStorage) LoadMessages(_ context.Context, runID string) ([]Message, error) {
	if err := validateMemoryRunID(runID); err != nil {
		return nil, err
	}
	s.mu.Lock()
	run := s.runs[runID]
	var encoded [][]byte
	if run != nil {
		encoded = append(encoded, run.messages...)
	}
	s.mu.Unlock()
	if run == nil {
		return nil, memoryMiss("load messages log", runID)
	}
	messages := make([]Message, 0, len(encoded))
	for i, data := range encoded {
		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, memoryError("decode messages log", runID, fmt.Errorf("message %d: %w", i+1, err))
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

// SaveRun replaces the run snapshot.
func (s *MemoryStorage) SaveRun(_ context.Context, state *RunState) error {
	if state == nil {
		return memoryError("save run snapshot", "", errors.New("run state is nil"))
	}
	if err := validateMemoryRunID(state.RunID); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return memoryError("encode run snapshot", state.RunID, err)
	}
	s.mu.Lock()
	s.run(state.RunID).state = data
	s.mu.Unlock()
	return nil
}

// AppendMessages appends messages to the run's authoritative log.
func (s *MemoryStorage) AppendMessages(_ context.Context, runID string, msgs ...Message) error {
	if err := validateMemoryRunID(runID); err != nil {
		return err
	}
	encoded := make([][]byte, 0, len(msgs))
	for _, msg := range msgs {
		data, err := json.Marshal(msg)
		if err != nil {
			return memoryError("append messages log", runID, err)
		}
		encoded = append(encoded, data)
	}
	s.mu.Lock()
	run := s.run(runID)
	run.messages = append(run.messages, encoded...)
	s.mu.Unlock()
	return nil
}

// SaveArtifacts replaces the run's artifact index.
func (s *MemoryStorage) SaveArtifacts(_ context.Context, runID string, artifacts []Artifact) error {
	if err := validateMemoryRunID(runID); err != nil {
		return err
	}
	data, err := json.Marshal(artifacts)
	if err != nil {
		return memoryError("encode artifacts index", runID, err)
	}
	s.mu.Lock()
	s.run(runID).artifacts = data
	s.mu.Unlock()
	return nil
}

// LoadArtifacts returns the run's artifact index; an unknown run is a storage
// miss, a known run without saved artifacts returns an empty slice.
func (s *MemoryStorage) LoadArtifacts(_ context.Context, runID string) ([]Artifact, error) {
	if err := validateMemoryRunID(runID); err != nil {
		return nil, err
	}
	s.mu.Lock()
	run := s.runs[runID]
	var data []byte
	if run != nil {
		data = run.artifacts
	}
	s.mu.Unlock()
	if run == nil {
		return nil, memoryMiss("load artifacts index", runID)
	}
	var artifacts []Artifact
	if data != nil {
		if err := json.Unmarshal(data, &artifacts); err != nil {
			return nil, memoryError("decode artifacts index", runID, err)
		}
	}
	if artifacts == nil {
		return []Artifact{}, nil
	}
	return artifacts, nil
}

// run returns the entry of runID, creating it; the caller holds s.mu.
func (s *MemoryStorage) run(runID string) *memoryRun {
	if s.runs == nil {
		s.runs = map[string]*memoryRun{}
	}
	run := s.runs[runID]
	if run == nil {
		run = &memoryRun{}
		s.runs[runID] = run
	}
	return run
}

func validateMemoryRunID(runID string) error {
	if err := runid.Validate(runID); err != nil {
		return memoryError("validate run id", runID, err)
	}
	return nil
}

// memoryMiss reports missing data with os.ErrNotExist, which the runtime
// treats as "start a fresh run" (see isStorageMiss).
func memoryMiss(action, runID string) error {
	return memoryError(action, runID, os.ErrNotExist)
}

func memoryError(action, runID string, err error) error {
	if runID == "" {
		return &StorageError{Err: fmt.Errorf("%s: %w", action, err)}
	}
	return &StorageError{Err: fmt.Errorf("%s for run %q: %w", action, runID, err)}
}
