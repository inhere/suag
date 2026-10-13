package personal

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/inhere/suag/pkg/agentcore/redact"
)

type Transcript struct {
	mu   sync.Mutex
	file *os.File
	path string
}

type transcriptEntry struct {
	Version   int    `json:"v"`
	Type      string `json:"type"`
	Timestamp string `json:"ts"`
	Text      string `json:"text,omitempty"`
	Injected  *bool  `json:"injected,omitempty"`
	Name      string `json:"name,omitempty"`
	Summary   string `json:"summary,omitempty"`
}

// OpenTranscript opens the per-session jsonl file in append mode with private permissions.
func OpenTranscript(root, sessionID string) (*Transcript, error) {
	if err := validateSessionID(sessionID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("transcript root is empty")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create transcript directory: %w", err)
	}
	path := filepath.Join(root, sessionID+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open transcript: %w", err)
	}
	return &Transcript{file: f, path: path}, nil
}

func (t *Transcript) Path() string {
	if t == nil {
		return ""
	}
	return t.path
}

func (t *Transcript) AppendUser(text string, injected bool) error {
	return t.append(transcriptEntry{Type: "user", Text: redact.Secrets(text), Injected: &injected})
}

func (t *Transcript) AppendAssistant(text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return t.append(transcriptEntry{Type: "assistant", Text: redact.Secrets(text)})
}

func (t *Transcript) AppendTool(name, content string) error {
	return t.AppendToolCall(name, nil, content)
}

// AppendToolCall records a tool call as "<name> key=value ..." built from the call
// arguments (never the tool output), so a transcript reader can tell what was done.
// The summary is redacted and rune-bounded; values are cut on UTF-8 boundaries.
func (t *Transcript) AppendToolCall(name string, args map[string]any, _ string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "tool"
	}
	summary := name
	if brief := summarizeToolArgs(args); brief != "" {
		summary += " " + brief
	} else {
		summary += " result; output omitted"
	}
	summary = truncateRunes(redact.Secrets(summary), 200)
	return t.append(transcriptEntry{Type: "tool", Name: name, Summary: summary})
}

const toolArgValueLimit = 120

// summaryArgKeys are the argument names that best identify what a tool call did,
// in display priority order.
var summaryArgKeys = []string{"path", "paths", "file_path", "filepath", "file", "command", "cmd", "pattern", "query", "url",
	"description"} // the agent tool: its short label, not the whole prompt

func summarizeToolArgs(args map[string]any) string {
	var parts []string
	used := map[string]bool{}
	for _, key := range summaryArgKeys {
		if v, ok := args[key]; ok {
			if text := toolArgText(v); text != "" {
				parts = append(parts, key+"="+text)
			}
			used[key] = true
		}
	}
	if len(parts) == 0 {
		// Unknown tool shape: fall back to the first few scalar args in stable order.
		keys := make([]string, 0, len(args))
		for k := range args {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if used[k] || len(parts) >= 3 {
				continue
			}
			switch args[k].(type) {
			case string, bool, float64, int, int64:
				if text := toolArgText(args[k]); text != "" {
					parts = append(parts, k+"="+text)
				}
			}
		}
	}
	return strings.Join(parts, " ")
}

func toolArgText(v any) string {
	text := strings.Join(strings.Fields(fmt.Sprint(v)), " ")
	return truncateRunes(text, toolArgValueLimit)
}

func (t *Transcript) append(entry transcriptEntry) error {
	if t == nil || t.file == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	entry.Version = 1
	entry.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	writer := bufio.NewWriter(t.file)
	if _, err := writer.Write(append(data, '\n')); err != nil {
		return err
	}
	return writer.Flush()
}

// Close flushes the final session transcript to disk.
func (t *Transcript) Close() error {
	if t == nil || t.file == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	err := t.file.Sync()
	return errors.Join(err, t.file.Close())
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit-1]) + "…"
}
