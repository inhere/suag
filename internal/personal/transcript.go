package personal

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	transcriptSecretValue = regexp.MustCompile(`(?i)([a-z0-9_.-]*(?:api[_-]?key|token|secret|password|authorization)[a-z0-9_.-]*)(\s*[:=]\s*)([^\s,;]+)`)
	transcriptBearerValue = regexp.MustCompile(`(?i)(authorization\s*:\s*bearer\s+)[^\s,;]+`)
	transcriptJSONSecret  = regexp.MustCompile(`(?i)(["'][^"']*(?:api[_-]?key|token|secret|password|authorization)[^"']*["']\s*:\s*["'])([^"']*)(["'])`)
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
	return t.append(transcriptEntry{Type: "user", Text: RedactSecretValues(text), Injected: &injected})
}

func (t *Transcript) AppendAssistant(text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return t.append(transcriptEntry{Type: "assistant", Text: RedactSecretValues(text)})
}

func (t *Transcript) AppendTool(name, _ string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "tool"
	}
	summary := truncateRunes(name+" result; output omitted", 200)
	return t.append(transcriptEntry{Type: "tool", Name: name, Summary: summary})
}

// RedactSecretValues removes credential-like values before tool data leaves the runtime.
func RedactSecretValues(value string) string {
	value = transcriptJSONSecret.ReplaceAllString(value, "$1[REDACTED]$3")
	value = transcriptBearerValue.ReplaceAllString(value, "$1[REDACTED]")
	return transcriptSecretValue.ReplaceAllStringFunc(value, func(match string) string {
		groups := transcriptSecretValue.FindStringSubmatch(match)
		if len(groups) == 4 && strings.EqualFold(groups[1], "authorization") && strings.EqualFold(groups[3], "Bearer") {
			return match
		}
		return groups[1] + "=[REDACTED]"
	})
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
