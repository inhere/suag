package native

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/pkg/agentcore/api"
)

const (
	// defaultReadLines is how many lines file_read returns without a limit.
	defaultReadLines = 2000
	// binarySniffBytes is how much of a file is checked for NUL bytes.
	binarySniffBytes = 8 << 10
)

// FileReadTool reads a file within the workspace, a window of lines at a time.
type FileReadTool struct {
	workspace string
	maxOut    int // result cap in bytes; 0 = DefaultMaxOutputBytes
}

func (t *FileReadTool) Name() string     { return "file_read" }
func (t *FileReadTool) Kind() tools.Kind { return tools.KindNative }
func (t *FileReadTool) Description() string {
	return fmt.Sprintf("Read a text file in the workspace. Lines are prefixed with their number and a tab "+
		"(the prefix is not part of the file). Returns at most %d lines per call; for large files pass "+
		"offset (1-based start line) and limit to read further. Binary files are reported, not dumped.", defaultReadLines)
}
func (t *FileReadTool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path within workspace (relative, or absolute inside it)",
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "1-based line to start from (default 1)",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": fmt.Sprintf("Maximum lines to return (default %d)", defaultReadLines),
				},
			},
			"required": []any{"path"},
		},
	}
}

func (t *FileReadTool) Execute(_ context.Context, call api.ToolCall) (*api.ToolResult, error) {
	path, err := sandboxedPath(t.workspace, call.Arguments, "path")
	if err != nil {
		return nil, err
	}
	offset, limit := 1, defaultReadLines
	if n, ok := numberArg(call.Arguments["offset"]); ok && n >= 1 {
		offset = int(n)
	}
	if n, ok := numberArg(call.Arguments["limit"]); ok && n >= 1 {
		limit = int(n)
	}
	content, err := withRoot(t.workspace, path, func(root *os.Root, rel string) (string, error) {
		f, err := root.Open(rel)
		if err != nil {
			return "", err
		}
		defer f.Close()
		return readLineWindow(f, offset, limit, outputLimit(t.maxOut))
	})
	if err != nil {
		return nil, fmt.Errorf("file_read: %w", err)
	}
	return &api.ToolResult{
		CallID:  call.CallID,
		Name:    t.Name(),
		Content: content,
	}, nil
}

// readLineWindow returns lines [offset, offset+limit) of f with line numbers,
// stopping early on a line boundary when maxBytes would be exceeded. When more
// lines follow it says which offset continues the read.
func readLineWindow(f *os.File, offset, limit, maxBytes int) (string, error) {
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory; use file_list", info.Name())
	}
	br := bufio.NewReaderSize(f, 64<<10)
	head, _ := br.Peek(binarySniffBytes)
	if bytes.IndexByte(head, 0) >= 0 {
		return fmt.Sprintf("[binary file: %s, %d bytes, type %s; not shown]",
			info.Name(), info.Size(), http.DetectContentType(head)), nil
	}
	if info.Size() == 0 {
		return "[empty file]", nil
	}

	var sb strings.Builder
	lineNo, shown := 0, 0
	more, cutBySize := false, false
	for {
		line, err := br.ReadString('\n')
		if line == "" && err != nil {
			if err != io.EOF {
				return "", err
			}
			break
		}
		lineNo++
		if lineNo < offset {
			continue
		}
		if shown == limit {
			more = true
			break
		}
		entry := fmt.Sprintf("%d\t%s", lineNo, strings.TrimRight(line, "\r\n")) + "\n"
		if sb.Len()+len(entry) > maxBytes {
			if shown == 0 { // a single huge line: return part of it
				entry = entry[:utf8Cut(entry, maxBytes)] + "\n"
				sb.WriteString(entry)
				shown = 1
			}
			more, cutBySize = true, true
			break
		}
		sb.WriteString(entry)
		shown++
		if err == io.EOF {
			break
		}
	}
	if shown == 0 {
		return fmt.Sprintf("[offset %d is past the end: the file has %d lines]", offset, lineNo), nil
	}
	if more {
		next := offset + shown
		reason := fmt.Sprintf("limit %d lines", limit)
		if cutBySize {
			reason = fmt.Sprintf("output cap %d bytes", maxBytes)
		}
		fmt.Fprintf(&sb, "[truncated at %s; continue with offset=%d]", reason, next)
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}
