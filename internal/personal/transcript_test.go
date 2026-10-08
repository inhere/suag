package personal

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTranscriptAppendsTypedEntriesAndRedactsToolSummaries(t *testing.T) {
	root := t.TempDir()
	first, err := OpenTranscript(root, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.AppendUser("[gofer web 回复] continue", true); err != nil {
		t.Fatal(err)
	}
	if err := first.AppendTool("shell_exec", `Authorization: Bearer canary-credential-4a2b {"api_key":"canary-credential-4a2b"}`); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopening a resumed session appends to its existing transcript.
	resumed, err := OpenTranscript(root, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := resumed.AppendAssistant("done"); err != nil {
		t.Fatal(err)
	}
	if err := resumed.Close(); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(filepath.Join(root, "session-1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var entries []map[string]any
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var entry map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected appended entries, got %d", len(entries))
	}
	if entries[0]["v"] != float64(1) || entries[0]["type"] != "user" || entries[0]["injected"] != true {
		t.Fatalf("unexpected user entry: %#v", entries[0])
	}
	tool := entries[1]["summary"].(string)
	if len([]rune(tool)) > 200 || strings.Contains(tool, "canary-credential-4a2b") || entries[1]["name"] != "shell_exec" {
		t.Fatalf("tool summary was not bounded and redacted: %#v", entries[1])
	}
	if entries[2]["type"] != "assistant" || entries[2]["text"] != "done" {
		t.Fatalf("unexpected resumed assistant entry: %#v", entries[2])
	}
}

func TestTranscriptToolSummaryIncludesNameAndKeyArgs(t *testing.T) {
	read := func(t *testing.T, root string) map[string]any {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, "s.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		var entry map[string]any
		if err := json.Unmarshal([]byte(lines[len(lines)-1]), &entry); err != nil {
			t.Fatal(err)
		}
		return entry
	}
	t.Run("path", func(t *testing.T) {
		root := t.TempDir()
		tr, _ := OpenTranscript(root, "s")
		defer tr.Close()
		_ = tr.AppendToolCall("read_file", map[string]any{"path": "a/b.go"}, "body")
		if got := read(t, root)["summary"]; got != "read_file path=a/b.go" {
			t.Fatalf("summary = %v", got)
		}
	})
	t.Run("long command truncated on rune boundary", func(t *testing.T) {
		root := t.TempDir()
		tr, _ := OpenTranscript(root, "s")
		defer tr.Close()
		_ = tr.AppendToolCall("shell_exec", map[string]any{"command": strings.Repeat("工", 300)}, "")
		got := read(t, root)["summary"].(string)
		if !utf8.ValidString(got) || !strings.HasPrefix(got, "shell_exec command=工") || strings.Count(got, "工") > 120 {
			t.Fatalf("summary = %q", got)
		}
	})
	t.Run("secrets redacted", func(t *testing.T) {
		root := t.TempDir()
		tr, _ := OpenTranscript(root, "s")
		defer tr.Close()
		_ = tr.AppendToolCall("shell_exec", map[string]any{"command": "curl -H token=canary-xyz1 http://x"}, "")
		if got := read(t, root)["summary"].(string); strings.Contains(got, "canary-xyz1") {
			t.Fatalf("leaked: %q", got)
		}
	})
}
