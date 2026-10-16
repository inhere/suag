package personal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/mock"
	"github.com/inhere/suga/pkg/agentcore/tools"
)

// The tests of issue suga-y0sk: a failed or cancelled turn keeps its side
// effects but not its record, so the next turn used to repeat or undo work it
// had already done. See docs/design/2026-10-15-interrupted-turn-replay-design.md.

// interruptedTurnID is the turn id the hand-built entries below belong to.
const interruptedTurnID = "t-fail"

// msgEntry builds one stored message entry of the interrupted test turn.
func msgEntry(t *testing.T, seq int64, typ string, m agentcore.Message) store.Entry {
	t.Helper()
	return store.Entry{Seq: seq, TurnID: interruptedTurnID, Type: typ, JSON: rawMessage(t, m)}
}

func rawMessage(t *testing.T, m agentcore.Message) []byte {
	t.Helper()
	raw, err := json.Marshal(m)
	assert.NoErr(t, err)
	return raw
}

func userMsg(text string) agentcore.Message {
	return agentcore.Message{Role: "user", Content: text}
}

func callMsg(id, name string, args map[string]any) agentcore.Message {
	return agentcore.Message{Role: "assistant", ToolCall: &agentcore.ToolCall{CallID: id, Name: name, Arguments: args}}
}

func resultMsg(id, name string) agentcore.Message {
	return agentcore.Message{Role: "tool", Content: name + " done",
		ToolResult: &agentcore.ToolResult{CallID: id, Name: name, Content: name + " done"}}
}

func keptSeqs(kept []store.Entry) []int64 {
	out := make([]int64, 0, len(kept))
	for _, e := range kept {
		out = append(out, e.Seq)
	}
	return out
}

// replayRunner is a runner over repo whose registry has one native "act" tool.
func replayRunner(t *testing.T, repo *store.Store, prov agentcore.Provider) *Runner {
	t.Helper()
	reg := tools.NewRegistry()
	reg.Register(fnTool{name: "act", fn: func() (*agentcore.ToolResult, error) {
		return &agentcore.ToolResult{Name: "act", Content: "acted"}, nil
	}})
	return compactRunner(t, repo, prov, reg, nil)
}

// requestText flattens a model request for content assertions. A tool message
// carries both the call and its result, so the call is printed for the assistant
// message only.
func requestText(req *agentcore.GenerateRequest) string {
	var b strings.Builder
	for _, m := range req.Messages {
		b.WriteString(m.Content)
		b.WriteString("\n")
		if m.ToolCall != nil && m.Role != "tool" {
			fmt.Fprintf(&b, "call %s %v\n", m.ToolCall.CallID, m.ToolCall.Arguments)
		}
	}
	return b.String()
}

// turnEntries returns one turn's stored message entries in seq order.
func turnEntries(t *testing.T, repo *store.Store, sessionID, turnID string) []store.Entry {
	t.Helper()
	es, err := repo.ListEntries(context.Background(), sessionID, store.EntryFilter{
		TurnID: turnID, Types: []string{store.EntryUser, store.EntryAssistant, store.EntryTool},
	})
	assert.Require(t, assert.NoErr(t, err))
	return es
}

func TestReplayablePrefix(t *testing.T) {
	steer := agentcore.Message{Role: "user", Content: "插话", Metadata: map[string]any{metaSteer: true}}
	notice := agentcore.Message{Role: "user", Content: "任务完成", Metadata: map[string]any{metaTaskNotice: true}}
	note := interruptionNoteMessage("note-text")
	text := agentcore.Message{Role: "assistant", Content: "写完了"}

	t.Run("a complete tool exchange replays", func(t *testing.T) {
		entries := []store.Entry{
			msgEntry(t, 1, store.EntryUser, userMsg("做一件事")),
			msgEntry(t, 2, store.EntryAssistant, callMsg("c1", "act", map[string]any{"path": "work.txt"})),
			msgEntry(t, 3, store.EntryTool, resultMsg("c1", "act")),
			msgEntry(t, 4, store.EntryAssistant, text),
			msgEntry(t, 5, store.EntryUser, note),
		}
		kept, calls, dropped, err := replayablePrefix(entries)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, []int64{1, 2, 3, 4, 5}, keptSeqs(kept))
		assert.Eq(t, []string{"act path=work.txt"}, calls)
		assert.Eq(t, 0, dropped)
	})

	t.Run("a call without its result cuts the tail, the note still replays", func(t *testing.T) {
		entries := []store.Entry{
			msgEntry(t, 1, store.EntryUser, userMsg("做一件事")),
			msgEntry(t, 2, store.EntryAssistant, callMsg("c1", "act", nil)),
			msgEntry(t, 3, store.EntryTool, resultMsg("c1", "act")),
			msgEntry(t, 4, store.EntryAssistant, callMsg("c2", "act", nil)), // no result: the turn died here
			msgEntry(t, 5, store.EntryAssistant, text),
			msgEntry(t, 6, store.EntryUser, note),
		}
		kept, calls, dropped, err := replayablePrefix(entries)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, []int64{1, 2, 3, 6}, keptSeqs(kept))
		assert.Eq(t, []string{"act"}, calls)
		assert.Eq(t, 2, dropped)
	})

	t.Run("a steering entry neither replays nor cuts", func(t *testing.T) {
		entries := []store.Entry{
			msgEntry(t, 1, store.EntryUser, userMsg("做一件事")),
			msgEntry(t, 2, store.EntryAssistant, callMsg("c1", "act", nil)),
			msgEntry(t, 3, store.EntryTool, resultMsg("c1", "act")),
			msgEntry(t, 4, store.EntryUser, steer),
			msgEntry(t, 5, store.EntryAssistant, text),
		}
		kept, _, dropped, err := replayablePrefix(entries)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, []int64{1, 2, 3, 5}, keptSeqs(kept))
		assert.Eq(t, 0, dropped)
	})

	t.Run("a task notice neither replays nor cuts", func(t *testing.T) {
		// The notice goes back to the task manager instead (finish,
		// returnTaskNotices): replaying it would show the task end twice (issue
		// suga-o26e).
		entries := []store.Entry{
			msgEntry(t, 1, store.EntryUser, userMsg("做一件事")),
			msgEntry(t, 2, store.EntryUser, notice),
		}
		kept, _, dropped, err := replayablePrefix(entries)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, []int64{1}, keptSeqs(kept))
		assert.Eq(t, 0, dropped)
	})

	t.Run("a turn without tool calls replays its messages", func(t *testing.T) {
		entries := []store.Entry{
			msgEntry(t, 1, store.EntryUser, userMsg("只说了话")),
			msgEntry(t, 2, store.EntryAssistant, text),
		}
		kept, calls, dropped, err := replayablePrefix(entries)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, []int64{1, 2}, keptSeqs(kept))
		assert.Eq(t, 0, len(calls))
		assert.Eq(t, 0, dropped)
	})
}

func TestInterruptionNoteText(t *testing.T) {
	t.Run("names the status, the calls and the reason", func(t *testing.T) {
		text := interruptionNoteText(store.TurnFailed, "network error", []string{"act path=work.txt"}, 2)
		assert.StrContains(t, text, "上一回合运行失败")
		assert.StrContains(t, text, "这不是新请求")
		assert.StrContains(t, text, "act path=work.txt")
		assert.StrContains(t, text, "另有 2 条记录")
		assert.StrContains(t, text, "原因：network error")
	})

	t.Run("says so when nothing ran", func(t *testing.T) {
		text := interruptionNoteText(store.TurnCancelled, "", nil, 0)
		assert.StrContains(t, text, "上一回合已取消")
		assert.StrContains(t, text, "未执行任何工具调用")
		assert.Eq(t, false, strings.Contains(text, "原因："))
		assert.Eq(t, false, strings.Contains(text, "没有进入上下文"))
	})

	t.Run("caps the listed calls", func(t *testing.T) {
		calls := make([]string, 0, interruptionNoteCalls+4)
		for i := 0; i < interruptionNoteCalls+4; i++ {
			calls = append(calls, "act")
		}
		text := interruptionNoteText(store.TurnFailed, "", calls, 0)
		assert.StrContains(t, text, fmt.Sprintf("共 %d 次", len(calls)))
		assert.Eq(t, interruptionNoteCalls, strings.Count(text, "act"))
	})
}

// A turn that failed after a tool call: its effect is in the workspace, so the
// next turn keeps the tool call and the note that explains the interruption.
func TestInterruptedTurnReplaysIntoTheNextTurn(t *testing.T) {
	ctx := context.Background()
	repo := openObserveRepo(t)
	prov := mock.NewProvider(
		mock.ToolCall("c1", "act", map[string]any{"path": "work.txt"}),
		mock.Error(errors.New("network error")),
	)
	first, err := replayRunner(t, repo, prov).Run(ctx, RunRequest{Prompt: "做一件事"})
	assert.Eq(t, true, err != nil) // the provider error ends the turn
	assert.Eq(t, RunFailed, first.State)

	entries := turnEntries(t, repo, first.SessionID, first.TurnID)
	assert.Require(t, assert.Eq(t, 4, len(entries)))
	assert.Eq(t, store.EntryUser, entries[3].Type)
	var note agentcore.Message
	assert.Require(t, assert.NoErr(t, json.Unmarshal(entries[3].JSON, &note)))
	assert.Eq(t, true, isInterruptionNote(note))
	assert.StrContains(t, note.Content, "上一回合运行失败")
	assert.StrContains(t, note.Content, "act path=work.txt")
	assert.StrContains(t, note.Content, "network error")

	// The next turn sends the whole interrupted turn to the model.
	next := &reqProv{}
	second, err := replayRunner(t, repo, next).Run(ctx, RunRequest{Prompt: "继续", SessionID: first.SessionID})
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, RunCompleted, second.State)
	assert.Require(t, assert.Eq(t, 1, len(next.reqs)))
	text := requestText(next.reqs[0])
	assert.StrContains(t, text, "做一件事")
	assert.StrContains(t, text, "work.txt")
	assert.StrContains(t, text, "acted")
	assert.StrContains(t, text, "上一回合运行失败")
}

// A hand-built interrupted turn whose last assistant message asked for a tool
// that never answered: the context keeps the completed exchange and the note,
// and drops the dangling tail.
func TestLoadHistoryCutsADanglingToolCall(t *testing.T) {
	ctx := context.Background()
	repo := openObserveRepo(t)
	runner := replayRunner(t, repo, mock.NewProvider(mock.Final("unused")))
	const sid = "sess-dangling"
	assert.Require(t, assert.NoErr(t, repo.CreateSession(ctx, store.Session{ID: sid, Status: store.SessionOpen})))
	assert.Require(t, assert.NoErr(t, repo.CreateTurn(ctx, store.Turn{ID: interruptedTurnID, SessionID: sid})))
	_, err := repo.AppendEntries(ctx, sid,
		store.NewEntry{TurnID: interruptedTurnID, Type: store.EntryUser, JSON: rawMessage(t, userMsg("做一件事"))},
		store.NewEntry{TurnID: interruptedTurnID, Type: store.EntryAssistant, JSON: rawMessage(t, callMsg("c1", "act", nil))},
		store.NewEntry{TurnID: interruptedTurnID, Type: store.EntryTool, JSON: rawMessage(t, resultMsg("c1", "act"))},
		store.NewEntry{TurnID: interruptedTurnID, Type: store.EntryAssistant, JSON: rawMessage(t, callMsg("c2", "act", nil))},
		store.NewEntry{TurnID: interruptedTurnID, Type: store.EntryAssistant, JSON: rawMessage(t, agentcore.Message{Role: "assistant", Content: "半截答话"})},
		store.NewEntry{TurnID: interruptedTurnID, Type: store.EntryUser, JSON: rawMessage(t, interruptionNoteMessage("note-text"))},
	)
	assert.Require(t, assert.NoErr(t, err))
	assert.Require(t, assert.NoErr(t, repo.UpdateTurn(ctx, store.Turn{
		ID: interruptedTurnID, Status: store.TurnFailed, StopReason: "run_failed", EndedAt: time.Now().UTC(),
	})))

	view, err := runner.loadHistory(ctx, sid)
	assert.Require(t, assert.NoErr(t, err))
	assert.Require(t, assert.Eq(t, 4, len(view.items)))
	assert.Eq(t, "做一件事", view.items[0].msg.Content)
	assert.Eq(t, "c1", view.items[1].msg.ToolCall.CallID)
	assert.Eq(t, "act done", view.items[2].msg.Content)
	assert.Eq(t, true, isInterruptionNote(view.items[3].msg))
	for _, it := range view.items {
		assert.Eq(t, false, strings.Contains(it.msg.Content, "半截答话"))
		assert.Eq(t, false, it.msg.ToolCall != nil && it.msg.ToolCall.CallID == "c2")
	}
}

// A turn marked cancelled by the recovery of a dead process (recovery.go sets
// the status, but no finish ran, so no interruption note exists) replays its
// prefix all the same (design D1).
func TestLoadHistoryReplaysACancelledTurnWithoutANote(t *testing.T) {
	ctx := context.Background()
	repo := openObserveRepo(t)
	runner := replayRunner(t, repo, mock.NewProvider(mock.Final("unused")))
	const sid = "sess-cancelled"
	assert.Require(t, assert.NoErr(t, repo.CreateSession(ctx, store.Session{ID: sid, Status: store.SessionOpen})))
	assert.Require(t, assert.NoErr(t, repo.CreateTurn(ctx, store.Turn{ID: interruptedTurnID, SessionID: sid})))
	_, err := repo.AppendEntries(ctx, sid,
		store.NewEntry{TurnID: interruptedTurnID, Type: store.EntryUser, JSON: rawMessage(t, userMsg("改一个文件"))},
		store.NewEntry{TurnID: interruptedTurnID, Type: store.EntryAssistant, JSON: rawMessage(t, callMsg("c1", "act", nil))},
		store.NewEntry{TurnID: interruptedTurnID, Type: store.EntryTool, JSON: rawMessage(t, resultMsg("c1", "act"))},
	)
	assert.Require(t, assert.NoErr(t, err))
	assert.Require(t, assert.NoErr(t, repo.UpdateTurn(ctx, store.Turn{
		ID: interruptedTurnID, Status: store.TurnCancelled, StopReason: "cancelled", EndedAt: time.Now().UTC(),
	})))

	view, err := runner.loadHistory(ctx, sid)
	assert.Require(t, assert.NoErr(t, err))
	assert.Require(t, assert.Eq(t, 3, len(view.items)))
	assert.Eq(t, "改一个文件", view.items[0].msg.Content)
	assert.Eq(t, "c1", view.items[1].msg.ToolCall.CallID)
	assert.Eq(t, "act done", view.items[2].msg.Content)
}

// A completed turn is untouched by the replay rules (regression guard).
func TestLoadHistoryKeepsCompletedTurns(t *testing.T) {
	ctx := context.Background()
	repo := openObserveRepo(t)
	prov := mock.NewProvider(
		mock.ToolCall("c1", "act", map[string]any{"path": "work.txt"}),
		mock.Final("做完了"),
	)
	res, err := replayRunner(t, repo, prov).Run(ctx, RunRequest{Prompt: "做一件事"})
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, RunCompleted, res.State)

	view, err := replayRunner(t, repo, mock.NewProvider(mock.Final("x"))).loadHistory(ctx, res.SessionID)
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, 4, len(view.items)) // user, assistant call, tool result, final answer
	assert.Eq(t, "做一件事", view.items[0].msg.Content)
	assert.Eq(t, "做完了", view.items[3].msg.Content)
	for _, it := range view.items {
		assert.Eq(t, false, isInterruptionNote(it.msg))
	}
}
