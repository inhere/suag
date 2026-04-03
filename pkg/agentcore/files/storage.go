package files

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/inhere/axon/pkg/agentcore"
)

const (
	stateFileName    = "state.json"
	messagesFileName = "messages.jsonl"
	metaFileName     = "meta.json"
	artifactsDirName = "artifacts"
)

// Storage persists run state snapshots and append-only message history on disk.
type Storage struct {
	root string
}

// NewStorage creates a filesystem-backed agentcore storage rooted at root.
func NewStorage(root string) *Storage {
	return &Storage{root: root}
}

// LoadRun loads the latest run snapshot from state.json.
func (s *Storage) LoadRun(_ context.Context, runID string) (*agentcore.RunState, error) {
	if err := s.validateRunID(runID); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(s.statePath(runID))
	if err != nil {
		return nil, s.wrapError("load run snapshot", runID, err)
	}

	var state agentcore.RunState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, s.wrapError("decode run snapshot", runID, err)
	}
	return &state, nil
}

// LoadMessages loads the append-only authoritative message history from messages.jsonl.
func (s *Storage) LoadMessages(_ context.Context, runID string) ([]agentcore.Message, error) {
	if err := s.validateRunID(runID); err != nil {
		return nil, err
	}

	file, err := os.Open(s.messagesPath(runID))
	if err != nil {
		return nil, s.wrapError("load messages log", runID, err)
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	messages := make([]agentcore.Message, 0)
	lineNo := 0

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			lineNo++

			trimmed := strings.TrimSpace(string(line))
			if trimmed != "" {
				var msg agentcore.Message
				if unmarshalErr := json.Unmarshal([]byte(trimmed), &msg); unmarshalErr != nil {
					return nil, s.wrapError("decode messages log", runID, fmt.Errorf("line %d: %w", lineNo, unmarshalErr))
				}
				messages = append(messages, msg)
			}
		}

		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			break
		}
		return nil, s.wrapError("read messages log", runID, err)
	}

	return messages, nil
}

// SaveRun writes the current run snapshot to state.json and initializes the base layout.
func (s *Storage) SaveRun(_ context.Context, state *agentcore.RunState) error {
	if state == nil {
		return s.wrapError("save run snapshot", "", errors.New("run state is nil"))
	}
	if err := s.validateRunID(state.RunID); err != nil {
		return err
	}
	if err := s.ensureLayout(state.RunID); err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return s.wrapError("encode run snapshot", state.RunID, err)
	}
	if err := s.writeFileAtomically(state.RunID, s.statePath(state.RunID), data, 0o644); err != nil {
		return err
	}
	return nil
}

// AppendMessages appends new messages to the authoritative jsonl log.
func (s *Storage) AppendMessages(_ context.Context, runID string, msgs ...agentcore.Message) error {
	if err := s.validateRunID(runID); err != nil {
		return err
	}
	if err := s.ensureLayout(runID); err != nil {
		return err
	}
	if len(msgs) == 0 {
		return nil
	}

	file, err := os.OpenFile(s.messagesPath(runID), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return s.wrapError("open messages log", runID, err)
	}
	defer file.Close()

	enc := json.NewEncoder(file)
	for _, msg := range msgs {
		if err := enc.Encode(msg); err != nil {
			return s.wrapError("append messages log", runID, err)
		}
	}
	return nil
}

func (s *Storage) ensureLayout(runID string) error {
	runDir := s.runDir(runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return s.wrapError("create run directory", runID, err)
	}
	if err := os.MkdirAll(filepath.Join(runDir, artifactsDirName), 0o755); err != nil {
		return s.wrapError("create artifacts directory", runID, err)
	}
	if err := s.ensureJSONFile(runID, s.metaPath(runID)); err != nil {
		return err
	}
	if err := s.ensureFile(runID, s.messagesPath(runID)); err != nil {
		return err
	}
	return nil
}

func (s *Storage) ensureJSONFile(runID, path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return s.wrapError("stat file", runID, err)
	}

	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		return s.wrapError("initialize file", runID, err)
	}
	return nil
}

func (s *Storage) ensureFile(runID, path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return s.wrapError("stat file", runID, err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return s.wrapError("initialize file", runID, err)
	}
	return file.Close()
}

func (s *Storage) validateRunID(runID string) error {
	if strings.TrimSpace(runID) == "" {
		return s.wrapError("validate run id", runID, errors.New("run id is required"))
	}
	if filepath.IsAbs(runID) || filepath.VolumeName(runID) != "" {
		return s.wrapError("validate run id", runID, errors.New("run id must not be an absolute path"))
	}
	if runID == "." || runID == ".." {
		return s.wrapError("validate run id", runID, errors.New("run id must not reference the current or parent directory"))
	}
	if strings.ContainsAny(runID, `/\`) {
		return s.wrapError("validate run id", runID, errors.New("run id must not contain path separators"))
	}
	if cleaned := filepath.Clean(runID); cleaned != runID || cleaned == "." || cleaned == ".." {
		return s.wrapError("validate run id", runID, errors.New("run id must resolve to a single safe path segment"))
	}
	return nil
}

func (s *Storage) writeFileAtomically(runID, path string, data []byte, perm os.FileMode) error {
	tempFile, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return s.wrapError("create temp run snapshot", runID, err)
	}

	tempPath := tempFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()

	if err := tempFile.Chmod(perm); err != nil {
		_ = tempFile.Close()
		return s.wrapError("set temp run snapshot mode", runID, err)
	}
	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return s.wrapError("write temp run snapshot", runID, err)
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return s.wrapError("sync temp run snapshot", runID, err)
	}
	if err := tempFile.Close(); err != nil {
		return s.wrapError("close temp run snapshot", runID, err)
	}
	if err := replaceFile(tempPath, path); err != nil {
		return s.wrapError("replace run snapshot", runID, err)
	}

	cleanup = false
	return nil
}

func (s *Storage) runDir(runID string) string {
	return filepath.Join(s.root, runID)
}

func (s *Storage) statePath(runID string) string {
	return filepath.Join(s.runDir(runID), stateFileName)
}

func (s *Storage) messagesPath(runID string) string {
	return filepath.Join(s.runDir(runID), messagesFileName)
}

func (s *Storage) metaPath(runID string) string {
	return filepath.Join(s.runDir(runID), metaFileName)
}

func (s *Storage) wrapError(action, runID string, err error) error {
	if err == nil {
		return nil
	}
	if runID == "" {
		return &agentcore.StorageError{Err: fmt.Errorf("%s: %w", action, err)}
	}
	return &agentcore.StorageError{Err: fmt.Errorf("%s for run %q: %w", action, runID, err)}
}
