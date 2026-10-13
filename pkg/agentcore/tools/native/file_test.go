package native

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/inhere/suga/pkg/agentcore/api"
)

func TestFileTools_ReturnToolResultsWithStableMetadata(t *testing.T) {
	workspace := t.TempDir()
	filePath := filepath.Join(workspace, "note.txt")
	if err := os.WriteFile(filePath, []byte("hello from file"), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}

	readTool := &FileReadTool{workspace: workspace}
	writeTool := &FileWriteTool{workspace: workspace}

	t.Run("file_read returns content in ToolResult", func(t *testing.T) {
		result, err := readTool.Execute(context.Background(), api.ToolCall{
			CallID: "call-read",
			Name:   "file_read",
			Arguments: map[string]any{
				"path": "note.txt",
			},
		})
		if err != nil {
			t.Fatalf("Execute error: %v", err)
		}
		if result.CallID != "call-read" || result.Name != "file_read" {
			t.Fatalf("result metadata = %#v, want preserved call metadata", result)
		}
		if result.Content != "1\thello from file" {
			t.Fatalf("content = %q, want file body", result.Content)
		}
	})

	t.Run("file_write returns write summary in ToolResult", func(t *testing.T) {
		result, err := writeTool.Execute(context.Background(), api.ToolCall{
			CallID: "call-write",
			Name:   "file_write",
			Arguments: map[string]any{
				"path":    "sub/out.txt",
				"content": "updated",
			},
		})
		if err != nil {
			t.Fatalf("Execute error: %v", err)
		}
		if result.CallID != "call-write" || result.Name != "file_write" {
			t.Fatalf("result metadata = %#v, want preserved call metadata", result)
		}
		if result.Content == "" {
			t.Fatalf("content = %q, want non-empty write summary", result.Content)
		}
	})
}
