package personal

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore"
)

func TestUndoQueuesFactUntilTheNextExplicitTurn(t *testing.T) {
	fixture := newUndoRestoreFixture(t, "before")
	fixture.recordFileWrite(t, "call", "after")

	beforeTurns, err := fixture.repo.ListTurns(fixture.ctx, "undo-restore")
	assert.Require(t, assert.NoErr(t, err))
	result, err := fixture.core.Undo(fixture.ctx, "undo-restore", 1, false)
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, 1, result.Restored)

	// Undo itself changes files and records the fact, but it must not start a
	// model turn until the caller supplies another prompt.
	afterUndoTurns, err := fixture.repo.ListTurns(fixture.ctx, "undo-restore")
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, len(beforeTurns), len(afterUndoTurns))
	events := eventsOf(t, fixture.repo, "undo-restore", store.EventUndo)
	assert.Require(t, assert.Len(t, events, 1))
	assert.Eq(t, "undo", events[0].Name)
	assert.Eq(t, float64(1), events[0].Attrs["turns"])
	assert.Eq(t, float64(1), events[0].Attrs["restored"])
	assert.Eq(t, float64(0), events[0].Attrs["deleted"])
	assert.Eq(t, float64(0), events[0].Attrs["skipped"])
	assert.Eq(t, float64(0), events[0].Attrs["unrecoverable"])
	assert.Eq(t, false, events[0].Attrs["forced"])
	assert.Eq(t, float64(0), events[0].Attrs["entry_seq"])

	provider := &reqProv{}
	runner, err := New(Config{Repository: fixture.repo, Provider: provider, Model: "m", ProviderName: "p"})
	assert.Require(t, assert.NoErr(t, err))
	next, err := runner.Run(fixture.ctx, RunRequest{SessionID: "undo-restore", Prompt: "继续检查这个文件"})
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, RunCompleted, next.State)
	assert.Require(t, assert.Len(t, provider.reqs, 1))

	var undoMessage *agentcore.Message
	for i := range provider.reqs[0].Messages {
		message := &provider.reqs[0].Messages[i]
		if isInjectedItem(*message) && message.Metadata[metaUndo] == true {
			undoMessage = message
			break
		}
	}
	assert.Require(t, assert.NotNil(t, undoMessage))
	assert.StrContains(t, undoMessage.Content, "这不是新请求")
	assert.StrContains(t, undoMessage.Content, "shell_exec")
	assert.StrContains(t, undoMessage.Content, "不要重做")
	assert.StrContains(t, undoMessage.Content, "重新读取")

	entries, err := fixture.repo.ListEntries(fixture.ctx, "undo-restore", store.EntryFilter{TurnID: next.TurnID, Types: []string{store.EntryUser}})
	assert.Require(t, assert.NoErr(t, err))
	var injected bool
	var entrySeq int64
	for _, entry := range entries {
		var message agentcore.Message
		assert.Require(t, assert.NoErr(t, json.Unmarshal(entry.JSON, &message)))
		if isInjectedItem(message) && message.Metadata[metaUndo] == true {
			injected = true
			entrySeq = entry.Seq
		}
	}
	assert.True(t, injected)
	assert.Gt(t, entrySeq, int64(0))

	events = eventsOf(t, fixture.repo, "undo-restore", store.EventUndo)
	assert.Require(t, assert.Len(t, events, 1))
	assert.Eq(t, float64(entrySeq), events[0].Attrs["entry_seq"])
	session, err := fixture.repo.GetSession(fixture.ctx, "undo-restore")
	assert.Require(t, assert.NoErr(t, err))
	assert.Nil(t, session.Meta[undoNoticeQueueKey])

	requests, err := runner.recentRequests(fixture.ctx, "undo-restore", "", recentRequestCount)
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, []string{"继续检查这个文件"}, requests)

	var trace bytes.Buffer
	traceEvents, err := fixture.repo.ListEvents(fixture.ctx, store.EventFilter{SessionID: "undo-restore"})
	assert.Require(t, assert.NoErr(t, err))
	assert.NoErr(t, WriteTrace(&trace, traceEvents, nil, true))
	found := false
	for _, line := range bytes.Split(bytes.TrimSpace(trace.Bytes()), []byte("\n")) {
		var row map[string]any
		assert.Require(t, assert.NoErr(t, json.Unmarshal(line, &row)))
		if row["kind"] == store.EventUndo {
			found = true
			break
		}
	}
	assert.True(t, found)
}

func TestUndoNoticeTextIsBoundedAndMarkedInjected(t *testing.T) {
	result := UndoResult{Restored: 1, Deleted: 1, Skipped: 1, Unrecoverable: 1,
		Files: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}}
	notice := newUndoNotice(result)
	message := undoNoticeMessage(notice.Text)
	assert.True(t, isInjectedItem(message))
	assert.Eq(t, true, message.Metadata[metaUndo])
	assert.StrContains(t, notice.Text, "已恢复 1 个文件")
	assert.StrContains(t, notice.Text, "已删除 1 个新建文件")
	assert.StrContains(t, notice.Text, "shell_exec")
	assert.StrContains(t, notice.Text, "另有 1 个文件")
}

func TestUndoNoticeQueueKeepsOrder(t *testing.T) {
	ctx := context.Background()
	repo := openObserveRepo(t)
	assert.NoErr(t, repo.CreateSession(ctx, store.Session{ID: "undo-queue", Status: store.SessionOpen}))
	for _, text := range []string{"first", "second"} {
		assert.NoErr(t, appendUndoNotice(ctx, repo, "undo-queue", pendingUndoNotice{Text: text}))
	}
	first, ok, err := takeUndoNotice(ctx, repo, "undo-queue")
	assert.Require(t, assert.NoErr(t, err))
	assert.True(t, ok)
	assert.Eq(t, "first", first.Text)
	second, ok, err := takeUndoNotice(ctx, repo, "undo-queue")
	assert.Require(t, assert.NoErr(t, err))
	assert.True(t, ok)
	assert.Eq(t, "second", second.Text)
	_, ok, err = takeUndoNotice(ctx, repo, "undo-queue")
	assert.Require(t, assert.NoErr(t, err))
	assert.False(t, ok)

}
