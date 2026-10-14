package wiring

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/internal/adddir"
	"github.com/inhere/suga/pkg/agentcore/api"
	"github.com/inhere/suga/pkg/agentcore/tools"
	"github.com/inhere/suga/pkg/agentcore/tools/native"
)

// outsideCase is one workspace with a file outside it, a session and the chat
// approval layer over the real tool registry.
type outsideCase struct {
	rt        *Runtime
	sessionID string
	ws        string
	outside   string
	approval  *tools.Approval
}

// outsideCaseOptions shapes a case before its runtime and approver are built.
type outsideCaseOptions struct {
	// readOnly assembles the runtime (and the approver) read-only: no write
	// tool, only the read-only answer.
	readOnly bool
	// askOutside turns the outside-directory question off (chat.ask_outside_dirs:
	// false); nil keeps the default (on).
	askOutside *bool
	// auto sets tools.approval: auto on the approver.
	auto bool
	// readRoots are the approver's static read-only roots.
	readRoots []string
}

// newOutsideCase builds the case; the asker is answered by the calling test.
func newOutsideCase(t *testing.T, asker tools.Asker, o outsideCaseOptions) outsideCase {
	t.Helper()
	ws, outside := t.TempDir(), t.TempDir()
	assert.NoErr(t, os.WriteFile(filepath.Join(ws, "in.txt"), []byte("inside\n"), 0o644))
	assert.NoErr(t, os.WriteFile(filepath.Join(outside, "b.txt"), []byte("outside\n"), 0o644))
	addDirsSessionConfig(t, ws)
	rt, sessionID := openAddDirsChatSession(t, Options{AllowWrite: !o.readOnly, ReadOnly: o.readOnly})
	askOutside := true
	if o.askOutside != nil {
		askOutside = *o.askOutside
	}
	approver := newChatApprover(chatApproverOptions{asker: asker, workspace: ws, dirs: rt.AddDirs,
		readRoots: o.readRoots, readOnly: o.readOnly, askOutside: askOutside, auto: o.auto})
	approver.SetAddDirs(rt.SessionAddDirs())
	return outsideCase{rt: rt, sessionID: sessionID, ws: ws, outside: outside,
		approval: tools.NewApproval(rt.Registry, approver)}
}

// read runs one file_read call through the approval layer.
func (c outsideCase) read(ctx context.Context, path string) (*api.ToolResult, error) {
	return c.approval.ExecuteTool(ctx, api.ToolCall{CallID: "read", Name: "file_read", Arguments: map[string]any{"path": path}})
}

// TestChatApproverOutsideDirs covers the outside-directory question
// (chat.ask_outside_dirs): a file call that reaches outside the workspace and the
// session's directories asks the person first, and the answer authorizes the
// directory for the whole session at once.
func TestChatApproverOutsideDirs(t *testing.T) {
	ctx := context.Background()

	t.Run("a read outside asks, and the answer authorizes the directory", func(t *testing.T) {
		asker := &scriptedAsker{answers: []string{"r"}}
		c := newOutsideCase(t, asker, outsideCaseOptions{})
		res, err := c.read(ctx, filepath.Join(c.outside, "b.txt"))
		assert.NoErr(t, err)
		assert.StrContains(t, res.Content, "outside")

		assert.Require(t, assert.Len(t, asker.questions, 1))
		q := asker.questions[0]
		assert.Eq(t, "file_read", q.Tool)
		assert.Eq(t, "授权访问 "+c.outside+"？[r] 只读 [w] 读写 [n] 拒绝：", q.Prompt)
		details := strings.Join(q.Details, "\n")
		assert.StrContains(t, details, "工具：file_read")
		assert.StrContains(t, details, "要访问："+filepath.Join(c.outside, "b.txt"))

		// The authorization is live and recorded, outside any turn.
		assert.Eq(t, []string{c.outside + ":ro"}, c.rt.AddDirs.List())
		assert.Eq(t, []adddir.Dir{{Path: c.outside, Source: adddir.SourceAsk}}, storedAddDirs(t, c.rt, c.sessionID))
		events := addDirEvents(t, c.rt, c.sessionID)
		assert.Require(t, assert.Len(t, events, 1))
		assert.Eq(t, "add", events[0].Name)
		assert.Eq(t, "", events[0].TurnID)
		assert.Eq(t, c.outside, events[0].Attrs["path"])
		assert.Eq(t, "ro", events[0].Attrs["mode"])
		assert.Eq(t, "ask", events[0].Attrs["from"])

		// A second file in the same directory needs no question.
		res, err = c.read(ctx, filepath.Join(c.outside, "b.txt"))
		assert.NoErr(t, err)
		assert.StrContains(t, res.Content, "outside")
		assert.Len(t, asker.questions, 1)
	})

	t.Run("writing answers w and the write runs", func(t *testing.T) {
		asker := &scriptedAsker{answers: []string{"w", "y"}}
		c := newOutsideCase(t, asker, outsideCaseOptions{})
		out := filepath.Join(c.outside, "new.txt")
		_, err := c.approval.ExecuteTool(ctx, api.ToolCall{CallID: "w", Name: "file_write",
			Arguments: map[string]any{"path": out, "content": "written\n"}})
		assert.NoErr(t, err)
		assert.Eq(t, []string{c.outside + ":rw"}, c.rt.AddDirs.List())
		body, readErr := os.ReadFile(out)
		assert.NoErr(t, readErr)
		assert.Eq(t, "written\n", string(body))
		// The write itself was confirmed afterwards, as every write tool is.
		assert.Require(t, assert.Len(t, asker.questions, 2))
		assert.StrContains(t, asker.questions[1].Prompt, "允许执行 file_write")
	})

	t.Run("n and an empty answer refuse and authorize nothing", func(t *testing.T) {
		for _, answer := range []string{"n", ""} {
			asker := &scriptedAsker{answers: []string{answer}}
			c := newOutsideCase(t, asker, outsideCaseOptions{})
			_, err := c.read(ctx, filepath.Join(c.outside, "b.txt"))
			assert.True(t, errors.Is(err, tools.ErrDenied), "answer %q", answer)
			assert.ErrMsgContains(t, err, "用户拒绝授权访问 "+c.outside)
			assert.Empty(t, c.rt.AddDirs.List())
			assert.Len(t, addDirEvents(t, c.rt, c.sessionID), 0)
		}
	})

	t.Run("without a terminal the tool keeps refusing", func(t *testing.T) {
		asker := &scriptedAsker{err: tools.ErrCannotAsk}
		c := newOutsideCase(t, asker, outsideCaseOptions{})
		_, err := c.read(ctx, filepath.Join(c.outside, "b.txt"))
		assert.Err(t, err)
		assert.False(t, errors.Is(err, tools.ErrDenied))
		assert.ErrMsgContains(t, err, "outside workspace")
		assert.Empty(t, c.rt.AddDirs.List())
	})

	t.Run("the question can be turned off", func(t *testing.T) {
		off := false
		asker := &scriptedAsker{answers: []string{"r"}}
		c := newOutsideCase(t, asker, outsideCaseOptions{askOutside: &off})
		_, err := c.read(ctx, filepath.Join(c.outside, "b.txt"))
		assert.ErrMsgContains(t, err, "outside workspace")
		assert.Len(t, asker.questions, 0)
		assert.Empty(t, c.rt.AddDirs.List())
	})

	t.Run("a read-only session offers the read-only answer only", func(t *testing.T) {
		asker := &scriptedAsker{answers: []string{"w"}}
		c := newOutsideCase(t, asker, outsideCaseOptions{readOnly: true})
		_, err := c.read(ctx, filepath.Join(c.outside, "b.txt"))
		assert.True(t, errors.Is(err, tools.ErrDenied))
		assert.Require(t, assert.Len(t, asker.questions, 1))
		assert.Eq(t, "授权访问 "+c.outside+"？[r] 只读 [n] 拒绝（本会话为 --read-only）：", asker.questions[0].Prompt)
		assert.Empty(t, c.rt.AddDirs.List())
		assert.Len(t, addDirEvents(t, c.rt, c.sessionID), 0)

		// The read-only answer works in the same session.
		asker.answers = []string{"r"}
		_, err = c.read(ctx, filepath.Join(c.outside, "b.txt"))
		assert.NoErr(t, err)
		assert.Eq(t, []string{c.outside + ":ro"}, c.rt.AddDirs.List())
	})

	t.Run("the question is asked even with tools.approval auto", func(t *testing.T) {
		asker := &scriptedAsker{answers: []string{"r"}}
		c := newOutsideCase(t, asker, outsideCaseOptions{auto: true})
		_, err := c.read(ctx, filepath.Join(c.outside, "b.txt"))
		assert.NoErr(t, err)
		assert.Len(t, asker.questions, 1)
	})
}

// TestChatApproverOutsideRoots: a path under one of the configured read roots is
// already reachable, so the question is not asked for it.
func TestChatApproverOutsideRoots(t *testing.T) {
	asker := &scriptedAsker{}
	ws, root := t.TempDir(), t.TempDir()
	assert.NoErr(t, os.WriteFile(filepath.Join(root, "b.txt"), []byte("root\n"), 0o644))
	addDirsSessionConfig(t, ws)
	assert.NoErr(t, gkconfig.Set("tools.read_roots", []string{root}))
	rt, _ := openAddDirsChatSession(t, Options{AllowWrite: true})
	roots, _ := readRoots()
	approver := newChatApprover(chatApproverOptions{asker: asker, workspace: ws, dirs: rt.AddDirs,
		readRoots: native.RootPaths(roots), askOutside: true})
	approver.SetAddDirs(rt.SessionAddDirs())
	approval := tools.NewApproval(rt.Registry, approver)

	res, err := approval.ExecuteTool(context.Background(), api.ToolCall{CallID: "read", Name: "file_read",
		Arguments: map[string]any{"path": filepath.Join(root, "b.txt")}})
	assert.Require(t, assert.NoErr(t, err))
	assert.StrContains(t, res.Content, "root")
	assert.Len(t, asker.questions, 0)
}
