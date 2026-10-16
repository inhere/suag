package acpserver

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/acp-go-sdk"
	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/pkg/agentcore/llm"
	"github.com/inhere/suga/pkg/agentcore/mock"
)

// statusACPServer answers every model request with status and an API error body
// (the same shape the provider tests use).
func statusACPServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error":{"message":"unknown model"}}`)
	}))
	t.Cleanup(server.Close)
	return server
}

// TestACPPromptErrorCarriesErrorKind: a prompt that died on a model request
// reports the failure's class (llm.ErrorKind) as error_kind in the JSON-RPC
// error's data - alongside the error text the SDK already sends -, the same
// value the CLI writes on its stream-json error line. A client such as gofer
// can then tell a cut stream (worth continuing the session) from a request the
// provider refused. A failure that is not a model failure keeps the SDK's own
// shape, without error_kind.
func TestACPPromptErrorCarriesErrorKind(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.Run("a refused request reports provider_rejected", func(t *testing.T) {
		// Retries off: the 404 is reported as the prompt's failure.
		provider := llm.NewOpenAIProvider(llm.OpenAIConfig{APIKey: "k", Model: "m",
			BaseURL: statusACPServer(t, http.StatusNotFound).URL, Retry: llm.RetryPolicy{MaxAttempts: 1}})
		conn := connectPersonalACP(t, testACPRuntime(t, provider, nil), &testACPClient{})
		created, err := conn.NewSession(ctx, acp.NewSessionRequest{Cwd: t.TempDir(), McpServers: []acp.McpServer{}})
		assert.Require(t, assert.NoErr(t, err))

		_, err = conn.Prompt(ctx, acp.PromptRequest{SessionId: created.SessionId, Prompt: []acp.ContentBlock{acp.TextBlock("hi")}})
		assert.Require(t, assert.Err(t, err))
		data := rpcErrorData(t, err)
		assert.Eq(t, llm.ErrorKindRejected, data["error_kind"])
		assert.StrContains(t, attrString(data["error"]), "unknown model")
	})

	t.Run("a cut stream reports provider_transient", func(t *testing.T) {
		// The server closes the body in the middle of an event line, so the
		// stream is cut; retries are off, so the prompt fails with it.
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"Hel`)
		}))
		t.Cleanup(server.Close)
		provider := llm.NewOpenAIProvider(llm.OpenAIConfig{APIKey: "k", Model: "m",
			BaseURL: server.URL, Retry: llm.RetryPolicy{MaxAttempts: 1}})
		conn := connectPersonalACP(t, testACPRuntime(t, provider, nil), &testACPClient{})
		created, err := conn.NewSession(ctx, acp.NewSessionRequest{Cwd: t.TempDir(), McpServers: []acp.McpServer{}})
		assert.Require(t, assert.NoErr(t, err))

		_, err = conn.Prompt(ctx, acp.PromptRequest{SessionId: created.SessionId, Prompt: []acp.ContentBlock{acp.TextBlock("hi")}})
		assert.Require(t, assert.Err(t, err))
		data := rpcErrorData(t, err)
		assert.Eq(t, llm.ErrorKindTransient, data["error_kind"])
	})

	t.Run("a failure that is not a model failure has no error_kind", func(t *testing.T) {
		// The rule source fails: the turn dies before any model request, so the
		// error is reported as before.
		failRules := &atomic.Bool{}
		open := testACPRuntimeWith(t, mock.NewProvider(mock.Final("ok")), nil, acpTestOptions{failRules: failRules})
		conn := connectPersonalACP(t, open, &testACPClient{})
		created, err := conn.NewSession(ctx, acp.NewSessionRequest{Cwd: t.TempDir(), McpServers: []acp.McpServer{}})
		assert.Require(t, assert.NoErr(t, err))
		failRules.Store(true)

		_, err = conn.Prompt(ctx, acp.PromptRequest{SessionId: created.SessionId, Prompt: []acp.ContentBlock{acp.TextBlock("hi")}})
		assert.Require(t, assert.Err(t, err))
		data := rpcErrorData(t, err)
		if kind, ok := data["error_kind"]; ok {
			t.Fatalf("a non-provider failure must not carry error_kind, got %v", kind)
		}
		assert.StrContains(t, attrString(data["error"]), "rules unavailable")
	})
}

// rpcErrorData returns the data of a prompt's JSON-RPC error.
func rpcErrorData(t *testing.T, err error) map[string]any {
	t.Helper()
	var rpcErr *acp.RequestError
	assert.Require(t, assert.True(t, errors.As(err, &rpcErr)))
	assert.Eq(t, -32603, rpcErr.Code)
	data, ok := rpcErr.Data.(map[string]any)
	assert.Require(t, assert.True(t, ok))
	return data
}
