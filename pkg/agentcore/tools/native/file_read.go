package native

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/inhere/suag/pkg/agentcore/api"
	"github.com/inhere/suag/pkg/agentcore/tools"
)

const (
	// defaultReadLines is how many lines file_read returns without a limit.
	defaultReadLines = 2000
	// binarySniffBytes is how much of a file is checked for NUL bytes.
	binarySniffBytes = 8 << 10
	// maxReadPaths is how many files one file_read call may read via paths.
	maxReadPaths = 10
)

// FileReadTool reads files within the workspace (or a read root), a window of
// lines at a time.
type FileReadTool struct {
	workspace string
	readRoots []string // read-only directories outside the workspace
	maxOut    int      // page cap in bytes per file (inline threshold); 0 = DefaultMaxOutputBytes
}

func (t *FileReadTool) Name() string { return "file_read" }
func (t *FileReadTool) Description() string {
	return fmt.Sprintf("Read a text file in the workspace. Lines are prefixed with their number and a tab "+
		"(the prefix is not part of the file). Returns at most %d lines per call; for large files pass "+
		"offset (1-based start line) and limit to read further. To read several files in one call pass "+
		"paths (up to %d) instead of path; offset and limit then apply to each file. "+
		"Binary files are reported, not dumped.", defaultReadLines, maxReadPaths)
}

// ToolMeta marks the tool as a read-only read.
func (*FileReadTool) ToolMeta() tools.Meta {
	return tools.Meta{Category: tools.CategoryRead, ReadOnly: true, Rereadable: true}
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
					"description": "Path within workspace (relative, or absolute inside it or under a read-only dependency root)",
				},
				"paths": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": fmt.Sprintf("Several files to read in one call (up to %d), instead of path", maxReadPaths),
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
		},
	}
}

func (t *FileReadTool) Execute(_ context.Context, call api.ToolCall) (*api.ToolResult, error) {
	paths, err := ReadPathList(call.Arguments)
	if err != nil {
		return nil, fmt.Errorf("file_read: %w", err)
	}
	offset, limit := 1, defaultReadLines
	if n, ok := numberArg(call.Arguments["offset"]); ok && n >= 1 {
		offset = int(n)
	}
	if n, ok := numberArg(call.Arguments["limit"]); ok && n >= 1 {
		limit = int(n)
	}
	var content string
	if len(paths) == 1 {
		if content, err = t.readOne(paths[0], offset, limit); err != nil {
			return nil, err
		}
	} else {
		// Several files: each gets its own page cap, as if read one by one, and
		// a failing path is reported in place so the others still come back.
		var sb strings.Builder
		for i, p := range paths {
			if i > 0 {
				sb.WriteString("\n\n")
			}
			text, err := t.readOne(p, offset, limit)
			if err != nil {
				text = "[error: " + err.Error() + "]"
			}
			fmt.Fprintf(&sb, "==> %s <==\n%s", p, text)
		}
		content = sb.String()
	}
	return &api.ToolResult{
		CallID:  call.CallID,
		Name:    t.Name(),
		Content: content,
	}, nil
}

// readOne reads the line window of one path argument.
func (t *FileReadTool) readOne(arg string, offset, limit int) (string, error) {
	target, err := resolveRead(t.workspace, t.readRoots, arg)
	if err != nil {
		return "", err
	}
	content, err := withRoot(target.base, target.abs, func(root *os.Root, rel string) (string, error) {
		f, err := root.Open(rel)
		if err != nil {
			return "", err
		}
		defer f.Close()
		return readLineWindow(f, offset, limit, outputLimit(t.maxOut))
	})
	if err != nil {
		return "", fmt.Errorf("file_read: %w", err)
	}
	return content, nil
}

// ReadPathList returns file_read's paths: path, then the entries of paths
// (duplicates dropped). paths may arrive as a JSON-encoded string, as some
// models encode arrays that way, or as a single plain string.
func ReadPathList(args map[string]any) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	if p, _ := args["path"].(string); p != "" {
		add(p)
	}
	raw := args["paths"]
	if text, isText := raw.(string); isText {
		if strings.HasPrefix(strings.TrimSpace(text), "[") {
			if err := json.Unmarshal([]byte(text), &raw); err != nil {
				return nil, fmt.Errorf("paths must be an array of strings: %w", err)
			}
		} else {
			raw = []any{text}
		}
	}
	if raw != nil {
		items, isList := raw.([]any)
		if !isList {
			return nil, fmt.Errorf("paths must be an array of strings")
		}
		for i, item := range items {
			p, isText := item.(string)
			if !isText {
				return nil, fmt.Errorf("paths[%d] must be a string", i)
			}
			add(p)
		}
	}
	switch {
	case len(out) == 0:
		return nil, fmt.Errorf("%q arg is required", "path")
	case len(out) > maxReadPaths:
		return nil, fmt.Errorf("at most %d paths per call, got %d", maxReadPaths, len(out))
	}
	return out, nil
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
