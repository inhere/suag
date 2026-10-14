package personal

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/mock"
)

// TestLastReply covers what a resumed chat shows as the session's last model
// reply and whether it adds the "上一回合未正常结束" hint.
func TestLastReply(t *testing.T) {
	ctx := context.Background()
	newRunner := func(t *testing.T, status string) (*Runner, *store.Store) {
		t.Helper()
		repo := openObserveRepo(t)
		runner, err := New(Config{Repository: repo, Provider: mock.NewProvider(), Model: "m", ProviderName: "p"})
		assert.NoErr(t, err)
		assert.NoErr(t, repo.CreateSession(ctx, store.Session{ID: "s1", Status: status}))
		return runner, repo
	}
	// assistant appends one assistant entry: text "" is a message that only asks
	// for a tool call.
	assistant := func(t *testing.T, repo *store.Store, turnID, text string) {
		t.Helper()
		msg := agentcore.Message{Role: "assistant", Content: text}
		if text == "" {
			msg.ToolCall = &agentcore.ToolCall{CallID: "c1", Name: "file_read"}
		}
		raw, err := json.Marshal(msg)
		assert.NoErr(t, err)
		_, err = repo.AppendEntries(ctx, "s1", store.NewEntry{TurnID: turnID, Type: store.EntryAssistant, JSON: raw})
		assert.NoErr(t, err)
	}
	// turn records a finished turn and points the session at it, like a turn
	// that ran and ended with that status.
	turn := func(t *testing.T, repo *store.Store, id, status string) {
		t.Helper()
		assert.NoErr(t, repo.CreateTurn(ctx, store.Turn{ID: id, SessionID: "s1"}))
		assert.NoErr(t, repo.UpdateTurn(ctx, store.Turn{ID: id, Status: status}))
		sess, err := repo.GetSession(ctx, "s1")
		assert.NoErr(t, err)
		sess.LastTurnID = id
		assert.NoErr(t, repo.UpdateSessionFields(ctx, sess))
	}

	t.Run("the last assistant message that has text", func(t *testing.T) {
		runner, repo := newRunner(t, store.SessionOpen)
		turn(t, repo, "t1", store.TurnCompleted)
		assistant(t, repo, "t1", "第一轮回答")
		turn(t, repo, "t2", store.TurnCompleted)
		assistant(t, repo, "t2", "第二轮回答")
		text, incomplete, err := runner.LastReply(ctx, "s1")
		assert.NoErr(t, err)
		assert.Eq(t, "第二轮回答", text)
		assert.False(t, incomplete)
	})
	t.Run("a last message with only tool calls: the text before it and the hint", func(t *testing.T) {
		runner, repo := newRunner(t, store.SessionOpen)
		turn(t, repo, "t1", store.TurnCompleted)
		assistant(t, repo, "t1", "先看一下")
		turn(t, repo, "t2", store.TurnCompleted)
		assistant(t, repo, "t2", "") // ended after the tool call, without an answer
		text, incomplete, err := runner.LastReply(ctx, "s1")
		assert.NoErr(t, err)
		assert.Eq(t, "先看一下", text)
		assert.True(t, incomplete)
	})
	t.Run("a failed last turn keeps the reply and adds the hint", func(t *testing.T) {
		runner, repo := newRunner(t, store.SessionOpen)
		turn(t, repo, "t1", store.TurnCompleted)
		assistant(t, repo, "t1", "能用的回答")
		turn(t, repo, "t2", store.TurnFailed)
		text, incomplete, err := runner.LastReply(ctx, "s1")
		assert.NoErr(t, err)
		assert.Eq(t, "能用的回答", text)
		assert.True(t, incomplete)
	})
	t.Run("an interrupted last turn adds the hint", func(t *testing.T) {
		runner, repo := newRunner(t, store.SessionOpen)
		turn(t, repo, "t1", store.TurnCompleted)
		assistant(t, repo, "t1", "被打断前的回答")
		turn(t, repo, "t2", store.TurnCancelled)
		text, incomplete, err := runner.LastReply(ctx, "s1")
		assert.NoErr(t, err)
		assert.Eq(t, "被打断前的回答", text)
		assert.True(t, incomplete)
	})
	t.Run("a session left running adds the hint", func(t *testing.T) {
		runner, repo := newRunner(t, store.SessionRunning)
		assistant(t, repo, "t1", "死掉前的回答")
		text, incomplete, err := runner.LastReply(ctx, "s1")
		assert.NoErr(t, err)
		assert.Eq(t, "死掉前的回答", text)
		assert.True(t, incomplete)
	})
	t.Run("no reply yet: nothing to show and no hint", func(t *testing.T) {
		runner, repo := newRunner(t, store.SessionOpen)
		assistant(t, repo, "t1", "") // a turn that only called tools
		text, incomplete, err := runner.LastReply(ctx, "s1")
		assert.NoErr(t, err)
		assert.Eq(t, "", text)
		assert.False(t, incomplete)
	})
	t.Run("an unknown session is an error", func(t *testing.T) {
		runner, _ := newRunner(t, store.SessionOpen)
		_, _, err := runner.LastReply(ctx, "nope")
		assert.ErrIs(t, err, ErrSessionNotFound)
	})
}
