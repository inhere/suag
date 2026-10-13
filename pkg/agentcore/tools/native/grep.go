package native

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"strings"

	"github.com/inhere/suga/pkg/agentcore/api"
	"github.com/inhere/suga/pkg/agentcore/tools"
)

const (
	defaultGrepResults = 100
	maxGrepResults     = 1000
	// maxGrepLineBytes trims very long matching lines (minified files).
	maxGrepLineBytes = 300
)

// FileGrepTool searches file contents in the workspace with a regular
// expression, in pure Go (no external rg).
type FileGrepTool struct {
	workspace string
	readRoots []string // read-only directories outside the workspace
	maxOut    int      // raw result cap in bytes; 0 = DefaultRawOutputBytes
}

func (t *FileGrepTool) Name() string { return "file_grep" }
func (t *FileGrepTool) Description() string {
	return "Search file contents in the workspace with a Go (RE2) regular expression. " +
		"Returns path:line: text (paths relative to the workspace, / separated), or only paths with output=files. " +
		"Skips .git, node_modules, vendor, dist, .gitignore matches (any directory's .gitignore; the result says what was skipped, " +
		"no_ignore includes it), binary files and files over 1MB. Prefer this over shell grep."
}

// ToolMeta marks the tool as a read-only search.
func (*FileGrepTool) ToolMeta() tools.Meta {
	return tools.Meta{Category: tools.CategorySearch, ReadOnly: true, Prefetchable: true}
}
func (t *FileGrepTool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern":     map[string]any{"type": "string", "description": "RE2 regular expression"},
				"path":        map[string]any{"type": "string", "description": "File or directory to search (default: workspace root); may be absolute under a read-only dependency root"},
				"glob":        map[string]any{"type": "string", "description": "Only files matching this glob, e.g. *.go or internal/**/*.go (relative to path or to the workspace)"},
				"ignore_case": map[string]any{"type": "boolean", "description": "Case-insensitive match"},
				"output":      map[string]any{"type": "string", "enum": []any{"content", "files"}, "description": "content (default): matching lines; files: matching file paths"},
				"max_results": map[string]any{"type": "integer", "description": fmt.Sprintf("Maximum lines or files (default %d, max %d)", defaultGrepResults, maxGrepResults)},
				"no_ignore":   map[string]any{"type": "boolean", "description": "Also search .gitignore matches and node_modules/vendor/dist (.git is always skipped)"},
			},
			"required": []any{"pattern"},
		},
	}
}

func (t *FileGrepTool) Execute(ctx context.Context, call api.ToolCall) (*api.ToolResult, error) {
	args := call.Arguments
	pattern, _ := args["pattern"].(string)
	if pattern == "" {
		return nil, fmt.Errorf("file_grep: pattern is required")
	}
	if ic, _ := args["ignore_case"].(bool); ic {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("file_grep: invalid pattern: %w", err)
	}
	start, err := searchStart(t.workspace, t.readRoots, args)
	if err != nil {
		return nil, err
	}
	glob, _ := args["glob"].(string)
	filesOnly := args["output"] == "files"
	limit := defaultGrepResults
	if n, ok := numberArg(args["max_results"]); ok && n >= 1 {
		limit = min(int(n), maxGrepResults)
	}

	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()
	var out []string
	full := false
	noIgnore, _ := args["no_ignore"].(bool)
	walk, err := walkWorkspace(ctx, start.base, start.abs, noIgnore, func(root *os.Root, rel string, d fs.DirEntry) error {
		if glob != "" && !start.matchUnder(glob, rel) {
			return nil
		}
		if info, err := d.Info(); err != nil || info.Size() > maxSearchFileBytes {
			return nil
		}
		matches, err := grepFile(root, rel, start.show(rel), re, filesOnly, limit-len(out))
		if err != nil {
			return nil // unreadable file: skip
		}
		out = append(out, matches...)
		if len(out) >= limit {
			full = true
			return errSearchFull
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("file_grep: %w", err)
	}
	content := strings.Join(out, "\n")
	if len(out) == 0 {
		content = "no matches"
	}
	if full {
		content += fmt.Sprintf("\n[stopped at %d results; narrow path/glob or raise max_results]", limit)
	}
	if walk.timedOut {
		content += fmt.Sprintf("\n[已超时（%s），结果不完整]", searchTimeout)
	}
	if walk.ignoreNote != "" {
		content += "\n" + walk.ignoreNote
	}
	return &api.ToolResult{CallID: call.CallID, Name: t.Name(), Content: TruncateHeadTail(content, rawLimit(t.maxOut))}, nil
}

// grepFile returns up to limit matches in one file: "name:line: text" lines,
// or just name when filesOnly; name is how the file is shown (rel, or its
// absolute path under a read root). Binary files yield nothing.
func grepFile(root *os.Root, rel, name string, re *regexp.Regexp, filesOnly bool, limit int) ([]string, error) {
	f, err := root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	br := bufio.NewReaderSize(f, 64<<10)
	head, _ := br.Peek(binarySniffBytes)
	if isBinaryHead(head) {
		return nil, nil
	}
	var out []string
	lineNo := 0
	for len(out) < limit {
		line, err := br.ReadString('\n')
		if line == "" && err != nil {
			if err == io.EOF {
				break
			}
			return out, err
		}
		lineNo++
		line = strings.TrimRight(line, "\r\n")
		if !re.MatchString(line) {
			continue
		}
		if filesOnly {
			return []string{name}, nil
		}
		if len(line) > maxGrepLineBytes {
			line = line[:utf8Cut(line, maxGrepLineBytes)] + "…"
		}
		out = append(out, fmt.Sprintf("%s:%d: %s", name, lineNo, line))
	}
	return out, nil
}

// searchStart resolves the optional path argument (default: the workspace),
// which may also be an absolute path under one of the read roots.
func searchStart(workspace string, roots []string, args map[string]any) (readTarget, error) {
	p, _ := args["path"].(string)
	if p == "" {
		p = "."
	}
	return resolveRead(workspace, roots, p)
}
