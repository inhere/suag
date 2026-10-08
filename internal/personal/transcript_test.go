package personal

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
