// Package diffview builds what one file tool call changes on disk and renders
// it as a unified diff.
//
// The construction is shared: the ACP server turns a Change into the
// tool_call_diff it sends its client (internal/acpserver), and chat shows the
// same change under the tool result, before an approval question and again in
// full with its :diff command (internal/chat), so both present the same
// preview. Chat builds this package's Lines in a form the terminal can read;
// nothing here knows about a terminal, an ACP client or a tool registry.
//
// Nothing runs for a tool that only reads, and nothing is read outside the
// workspace: a call whose path is outside the workspace (or under a dangling
// symlink), a directory, a binary or an oversized file yields no change, and
// the call itself is not touched either way - the preview is read before the
// tool runs.
package diffview

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/inhere/suga/pkg/agentcore/tools/native"
)

// MaxBytes caps each side of one change: a larger file yields no change, the
// caller only points at the path.
const MaxBytes = 256 << 10

// The tools whose calls change one workspace file.
const (
	// ToolEdit is file_edit (old_string/new_string or edits).
	ToolEdit = "file_edit"
	// ToolWrite is file_write (content).
	ToolWrite = "file_write"
	// ToolRemove is file_remove (path).
	ToolRemove = "file_remove"
)

// Change is what one file tool call does to one file: the text before the call
// and the text the call writes, or the line count a removal takes away.
type Change struct {
	// Path is the absolute path of the file, as the file tools resolve it.
	Path string
	// Rel is that path relative to the workspace: the short form to show a
	// person (never empty, the path is always inside the workspace).
	Rel string
	// Tool is the call that makes the change: ToolEdit, ToolWrite or
	// ToolRemove.
	Tool string
	// Old is the file's text before the call; nil when the file does not exist
	// (file_write creating it).
	Old *string
	// New is the text the call writes; nil for a removal, which writes nothing.
	New *string
	// RemovedLines is how many lines the file ToolRemove takes away (0 for the
	// other tools, and for a target that is not a regular text file).
	RemovedLines int
}

// NewFile reports whether the call creates the file (file_write onto a path
// that did not exist).
func (c *Change) NewFile() bool { return c != nil && c.Old == nil && c.New != nil }

// Call returns the change of one file tool call (ToolEdit, ToolWrite or
// ToolRemove) by the arguments the model sent. ok is false for every other tool
// and whenever there is nothing to preview: a path the file tools would refuse,
// a target that is not a regular text file within MaxBytes, a file_edit that
// cannot apply, or a file_remove whose target is gone.
func Call(cwd, name string, args map[string]any) (*Change, bool) {
	switch name {
	case ToolEdit, ToolWrite, ToolRemove:
	default:
		return nil, false
	}
	path := argPath(args)
	if path == "" {
		return nil, false
	}
	resolved, err := native.ResolveSandboxedPath(cwd, path)
	if err != nil {
		return nil, false // the tool refuses this path; there is nothing to show
	}
	return At(cwd, name, resolved, args)
}

// At returns the change of one file tool call on an already resolved absolute
// path: the ACP server resolves the path for the call's location first and
// previews the same target. cwd is the workspace the path must stay inside.
func At(cwd, name, path string, args map[string]any) (*Change, bool) {
	rel := relPath(cwd, path)
	if rel == "" {
		return nil, false
	}
	switch name {
	case ToolEdit, ToolWrite, ToolRemove:
	default:
		return nil, false
	}
	root, err := os.OpenRoot(cwd)
	if err != nil {
		return nil, false
	}
	defer root.Close()
	old, ok := readOld(root, rel)
	if !ok {
		return nil, false
	}
	change := &Change{Path: path, Rel: rel, Tool: name, Old: old}
	switch name {
	case ToolRemove:
		if old == nil {
			return nil, false // nothing there: nothing to count
		}
		change.RemovedLines = len(splitLines(*old))
	default:
		newText, ok := proposed(old, args, name)
		if !ok {
			return nil, false
		}
		change.New = &newText
	}
	return change, true
}

// proposed is the text the call writes: file_write's content, or the file's
// text with file_edit's replacements applied by the very function the tool runs
// (native.ApplyEdit), so the preview is the exact result.
func proposed(old *string, args map[string]any, name string) (string, bool) {
	if name == ToolWrite {
		content, ok := args["content"].(string)
		if !ok || len(content) > MaxBytes || !textual(content) {
			return "", false
		}
		return content, true
	}
	if old == nil {
		return "", false // file_edit needs an existing file
	}
	edited, _, err := native.ApplyEdit(*old, args)
	if err != nil || len(edited) > MaxBytes {
		return "", false
	}
	return edited, true
}

// argPath is the path argument of a file tool call: the file tools agree on
// "path"; a caller that spells it "file_path" is read the same way.
func argPath(args map[string]any) string {
	if path, _ := args["path"].(string); path != "" {
		return path
	}
	path, _ := args["file_path"].(string)
	return path
}

// readOld reads the file's text before the call: old is nil for a file that
// does not exist (a new file), ok is false whenever the target cannot be
// previewed at all.
func readOld(root *os.Root, rel string) (old *string, ok bool) {
	// Open exactly once through os.Root (which follows only in-root symlinks and
	// rejects escapes), then judge the handle itself: no separate Lstat/Stat that
	// a swap could race with.
	f, err := openPreview(root, rel)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// An absent target (including a missing parent directory, which file_write
		// creates) is a new file. A dangling symlink also reports not-exist, so fail
		// closed on anything that is still present as a link.
		if _, lerr := root.Lstat(rel); lerr == nil {
			return nil, false
		}
		return nil, true
	case err != nil:
		// os.Root rejects ".." and symlink escapes with a non-NotExist error.
		return nil, false
	}
	defer f.Close()
	text, ok := readBounded(f)
	if !ok {
		return nil, false
	}
	return &text, true
}

// readBounded reads an opened regular file, giving up on non-regular files
// (FIFO/device/directory), files past MaxBytes, or binary content.
func readBounded(f *os.File) (string, bool) {
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxBytes {
		return "", false
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if err != nil || len(data) > MaxBytes || !textual(string(data)) {
		return "", false
	}
	return string(data), true
}

// textual reports whether s is valid UTF-8 without NUL bytes (a binary marker).
func textual(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

// relPath returns path relative to cwd, or "" when it is not inside cwd.
func relPath(cwd, path string) string {
	rel, err := filepath.Rel(cwd, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return rel
}
