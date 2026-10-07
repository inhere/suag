package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/acp-go-sdk"
	gkconfig "github.com/gookit/config/v2"
)

type appACPClient struct {
	acp.Client
	mu      sync.Mutex
	updates []acp.SessionNotification
}

func (c *appACPClient) SessionUpdate(_ context.Context, update acp.SessionNotification) error {
	c.mu.Lock()
	c.updates = append(c.updates, update)
	c.mu.Unlock()
	return nil
}

func connectAppACP(t *testing.T) (*acp.ClientSideConnection, *appACPClient) {
	t.Helper()
	clientToAgentR, clientToAgentW := io.Pipe()
	agentToClientR, agentToClientW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunPersonalACP(ctx, clientToAgentR, agentToClientW) }()
	client := &appACPClient{}
	conn := acp.NewClientSideConnection(client, clientToAgentW, agentToClientR)
	t.Cleanup(func() {
		cancel()
		_ = clientToAgentW.Close()
		_ = clientToAgentR.Close()
		_ = agentToClientW.Close()
		_ = agentToClientR.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("ACP stdio server did not exit")
		}
	})
	return conn, client
}

func TestAppACPUsesPersonalStorageAndLoadsInNewServer(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	t.Setenv("DATABASE_URL", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		n := calls.Add(1)
		if n == 2 {
			found := false
			for _, message := range body.Messages {
				found = found || message.Content == "reply-1"
			}
			if !found {
				t.Error("loaded ACP session omitted previous answer")
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":"reply-%d"},"finish_reason":"stop"}]}`, n)
	}))
	t.Cleanup(provider.Close)
	_ = gkconfig.Set("llm.api_type", "openai")
	_ = gkconfig.Set("llm.api_key", "local-test-key")
	_ = gkconfig.Set("llm.base_url", provider.URL)
	cwd := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	first, firstClient := connectAppACP(t)
	if _, err := first.Initialize(ctx, acp.InitializeRequest{ProtocolVersion: acp.ProtocolVersionNumber}); err != nil {
		t.Fatal(err)
	}
	created, err := first.NewSession(ctx, acp.NewSessionRequest{Cwd: cwd, McpServers: []acp.McpServer{}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := first.Prompt(ctx, acp.PromptRequest{SessionId: created.SessionId, Prompt: []acp.ContentBlock{acp.TextBlock("remember")}})
	if err != nil || result.StopReason != acp.StopReasonEndTurn {
		t.Fatalf("first prompt: %+v, %v", result, err)
	}
	if !hasACPAnswer(firstClient, "reply-1") {
		t.Fatal("first ACP process did not emit answer")
	}

	second, secondClient := connectAppACP(t)
	if _, err := second.Initialize(ctx, acp.InitializeRequest{ProtocolVersion: acp.ProtocolVersionNumber}); err != nil {
		t.Fatal(err)
	}
	if _, err := second.LoadSession(ctx, acp.LoadSessionRequest{SessionId: created.SessionId, Cwd: cwd, McpServers: []acp.McpServer{}}); err != nil {
		t.Fatal(err)
	}
	result, err = second.Prompt(ctx, acp.PromptRequest{SessionId: created.SessionId, Prompt: []acp.ContentBlock{acp.TextBlock("continue")}})
	if err != nil || result.StopReason != acp.StopReasonEndTurn || !hasACPAnswer(secondClient, "reply-2") {
		t.Fatalf("loaded prompt: %+v, %v", result, err)
	}
	if calls.Load() != 2 {
		t.Fatalf("provider calls = %d, want 2", calls.Load())
	}
	if _, err := os.Stat(filepath.Join(home, ".suag", "db", "suag.db")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "db")); !os.IsNotExist(err) {
		t.Fatalf("ACP wrote a database inside workspace: %v", err)
	}
}

func hasACPAnswer(client *appACPClient, text string) bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	for _, update := range client.updates {
		chunk := update.Update.AgentMessageChunk
		if chunk != nil && chunk.Content.Text != nil && chunk.Content.Text.Text == text {
			return true
		}
	}
	return false
}
