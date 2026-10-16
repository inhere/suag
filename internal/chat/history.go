package chat

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	appconfig "github.com/inhere/suga/internal/config"
	"github.com/inhere/suga/pkg/agentcore/redact"
)

const defaultHistorySize = 1000

type historyEntry struct {
	TS        time.Time `json:"ts"`
	Workspace string    `json:"workspace"`
	Text      string    `json:"text"`
}

// inputHistory is a process-local snapshot plus append-only persistence. The
// append file is deliberately opened with O_APPEND so concurrent chats cannot
// overwrite one another's entries.
type inputHistory struct {
	mu         sync.Mutex
	path       string
	workspace  string
	entries    []historyEntry
	enabled    bool
	maxEntries int
}

func newInputHistory(workspace string, enabled bool, maxEntries int) (*inputHistory, error) {
	if maxEntries < 1 {
		maxEntries = defaultHistorySize
	}
	h := &inputHistory{workspace: workspace, enabled: enabled, maxEntries: maxEntries}
	if !enabled {
		return h, nil
	}
	home, err := appconfig.ResolveSugaHome()
	if err != nil {
		return nil, err
	}
	h.path = filepath.Join(home, "history.jsonl")
	f, err := os.Open(h.path)
	if os.IsNotExist(err) {
		return h, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		var e historyEntry
		if json.Unmarshal(s.Bytes(), &e) == nil && e.Text != "" {
			h.entries = append(h.entries, e)
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	h.entries = trimHistory(h.entries, maxEntries)
	return h, nil
}

func trimHistory(entries []historyEntry, max int) []historyEntry {
	if len(entries) <= max {
		return entries
	}
	return entries[len(entries)-max:]
}

func (h *inputHistory) add(text string) error {
	if h == nil || !h.enabled || strings.TrimSpace(text) == "" {
		return nil
	}
	e := historyEntry{TS: time.Now().UTC(), Workspace: h.workspace, Text: redact.Secrets(text)}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(h.path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(h.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := fmt.Fprintf(f, "%s\n", data)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	needsTrim := len(h.entries)+1 > h.maxEntries
	h.entries = trimHistory(append(h.entries, e), h.maxEntries)
	if needsTrim {
		if err := h.rewriteLocked(); err != nil {
			return err
		}
	}
	return nil
}

func (h *inputHistory) rewriteLocked() error {
	data := make([]byte, 0, len(h.entries)*64)
	for _, entry := range h.entries {
		line, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		data = append(data, line...)
		data = append(data, '\n')
	}
	tmp := h.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, h.path)
}

func (h *inputHistory) lines() []string {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.entries))
	// The editor expects oldest-to-newest entries. Keep the current workspace
	// group first so Up reaches it before entries from other workspaces.
	for pass := 0; pass < 2; pass++ {
		for _, entry := range h.entries {
			same := entry.Workspace == h.workspace
			if (pass == 0) != same {
				continue
			}
			out = append(out, entry.Text)
		}
	}
	return out
}
