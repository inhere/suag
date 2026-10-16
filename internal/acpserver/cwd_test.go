package acpserver

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/acp-go-sdk"
	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore/mock"
	"github.com/inhere/suga/pkg/agentcore/tools/mcp"
)

// cwdRuntime opens one runner per request whose workspace is the cwd the client
// asked for (like the CLI's ACP factory does), so a session records the
// directory it was created in.
func cwdRuntime(t *testing.T, dbPath string) OpenRuntime {
	t.Helper()
	return func(ctx context.Context, cwd string, _ bool, _ []mcp.ServerSettings, _ TurnGate) (Runtime, error) {
		repo, err := store.Open(dbPath)
		if err != nil {
			return Runtime{}, err
		}
		runner, err := personal.New(personal.Config{
			Repository: repo, Provider: mock.NewProvider(mock.Final("ok")), Workspace: cwd,
		})
		if err != nil {
			_ = repo.Close()
			return Runtime{}, err
		}
		return Runtime{Runner: runner, Close: repo.Close}, nil
	}
}

// TestACPLoadSessionRefusesAnotherCwd: session/new records the client's cwd on
// the session, and session/load with a different one is refused as invalid
// params (the message names both directories) instead of running its tools in
// the new directory. The same directory and a session without a recorded one
// still load.
func TestACPLoadSessionRefusesAnotherCwd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dirA, dirB := t.TempDir(), t.TempDir()
	conn := connectPersonalACP(t, cwdRuntime(t, filepath.Join(t.TempDir(), "db", "sessions.db")), &testACPClient{})

	created, err := conn.NewSession(ctx, acp.NewSessionRequest{Cwd: dirA, McpServers: []acp.McpServer{}})
	assert.Require(t, assert.NoErr(t, err))

	t.Run("the directory the session was created in loads", func(t *testing.T) {
		_, err := conn.LoadSession(ctx, acp.LoadSessionRequest{SessionId: created.SessionId, Cwd: dirA, McpServers: []acp.McpServer{}})
		assert.NoErr(t, err)
	})

	t.Run("another directory is invalid params naming both", func(t *testing.T) {
		_, err := conn.LoadSession(ctx, acp.LoadSessionRequest{SessionId: created.SessionId, Cwd: dirB, McpServers: []acp.McpServer{}})
		assert.Require(t, assert.Err(t, err))
		var rpcErr *acp.RequestError
		assert.Require(t, assert.True(t, errors.As(err, &rpcErr)))
		assert.Eq(t, -32602, rpcErr.Code)
		assert.Eq(t, true, strings.Contains(rpcErr.Message, dirA))
		assert.Eq(t, true, strings.Contains(rpcErr.Message, dirB))
		data, ok := rpcErr.Data.(map[string]any)
		assert.Require(t, assert.True(t, ok))
		assert.Eq(t, true, personal.SameDir(dirA, attrString(data["sessionCwd"])))
		assert.Eq(t, true, personal.SameDir(dirB, attrString(data["cwd"])))
	})

	t.Run("a session without a recorded directory loads", func(t *testing.T) {
		// A session created before cwd was recorded: the runner fills it in on
		// the next turn, so loading it anywhere is allowed.
		dbPath := filepath.Join(t.TempDir(), "db", "sessions.db")
		repo, err := store.Open(dbPath)
		assert.Require(t, assert.NoErr(t, err))
		assert.NoErr(t, repo.CreateSession(ctx, store.Session{ID: "old-session", Title: "old"}))
		assert.NoErr(t, repo.Close())

		oldConn := connectPersonalACP(t, cwdRuntime(t, dbPath), &testACPClient{})
		_, err = oldConn.LoadSession(ctx, acp.LoadSessionRequest{SessionId: "old-session", Cwd: dirB, McpServers: []acp.McpServer{}})
		assert.NoErr(t, err)
	})
}

func attrString(v any) string {
	s, _ := v.(string)
	return s
}
