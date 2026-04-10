package llm

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ── scripted mode ─────────────────────────────────────────────────────────────

func TestMockProvider_ScriptedText(t *testing.T) {
	m := NewMockProvider("test").
		QueueText("hello world").
		QueueText("second reply")

	ctx := context.Background()
	req := &CompletionRequest{Messages: []Message{{Role: "user", Content: "hi"}}}

	resp, err := m.Complete(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hello world" {
		t.Errorf("content = %q, want %q", resp.Content, "hello world")
	}
	if resp.FinishReason != "stop" {
		t.Errorf("finish_reason = %q, want stop", resp.FinishReason)
	}

	resp2, _ := m.Complete(ctx, req)
	if resp2.Content != "second reply" {
		t.Errorf("content = %q, want %q", resp2.Content, "second reply")
	}

	if m.CallCount() != 2 {
		t.Errorf("CallCount = %d, want 2", m.CallCount())
	}
}

func TestMockProvider_ScriptedToolCall(t *testing.T) {
	m := NewMockProvider("test").
		QueueToolCall("file_read", map[string]any{"path": "README.md"}).
		QueueText("done")

	ctx := context.Background()
	req := &CompletionRequest{}

	// First call → tool call.
	resp, err := m.Complete(ctx, req)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !resp.IsToolCall() {
		t.Fatal("expected tool call response")
	}
	if resp.ToolCalls[0].Name != "file_read" {
		t.Errorf("tool name = %q, want file_read", resp.ToolCalls[0].Name)
	}
	if resp.ToolCalls[0].Arguments["path"] != "README.md" {
		t.Errorf("unexpected args: %v", resp.ToolCalls[0].Arguments)
	}

	// Second call → completion.
	resp2, _ := m.Complete(ctx, req)
	if resp2.IsToolCall() {
		t.Error("expected non-tool response")
	}
	if !resp2.IsComplete() {
		t.Error("expected IsComplete() == true")
	}
}

func TestMockProvider_ScriptedError(t *testing.T) {
	sentinel := errors.New("rate limited")
	m := NewMockProvider("test").QueueError(sentinel)

	_, err := m.Complete(context.Background(), &CompletionRequest{})
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want %v", err, sentinel)
	}
}

func TestMockProvider_Reset(t *testing.T) {
	m := NewMockProvider("test").QueueText("a").QueueText("b")
	if m.QueueLen() != 2 {
		t.Fatalf("queue len = %d, want 2", m.QueueLen())
	}
	m.Reset()
	if m.QueueLen() != 0 {
		t.Errorf("queue len after reset = %d, want 0", m.QueueLen())
	}
	if m.CallCount() != 0 {
		t.Errorf("call count after reset = %d, want 0", m.CallCount())
	}
}

// ── manual mode ───────────────────────────────────────────────────────────────

func TestMockProvider_ManualMode(t *testing.T) {
	m := NewMockProvider("manual")
	ctx := context.Background()

	// Fire Complete() asynchronously — it will block until we reply.
	type result struct {
		resp *CompletionResponse
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		resp, err := m.Complete(ctx, &CompletionRequest{
			Messages: []Message{{Role: "user", Content: "what time is it?"}},
		})
		ch <- result{resp, err}
	}()

	// The goroutine is now blocked in manual mode.
	// WaitForCall receives it and we inject a reply.
	req, reply, err := m.WaitForCall(ctx)
	if err != nil {
		t.Fatalf("WaitForCall error: %v", err)
	}
	if len(req.Messages) == 0 || req.Messages[0].Content != "what time is it?" {
		t.Errorf("unexpected request: %+v", req)
	}

	reply(MockResponse{Content: "It's go time!", FinishReason: "stop"})

	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("Complete error: %v", r.err)
		}
		if r.resp.Content != "It's go time!" {
			t.Errorf("content = %q", r.resp.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Complete() to return")
	}
}

func TestMockProvider_ManualMode_Error(t *testing.T) {
	m := NewMockProvider("manual")
	ctx := context.Background()

	type result struct {
		resp *CompletionResponse
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		resp, err := m.Complete(ctx, &CompletionRequest{})
		ch <- result{resp, err}
	}()

	_, reply, err := m.WaitForCall(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("injected error")
	reply(MockResponse{Err: sentinel})

	select {
	case r := <-ch:
		if !errors.Is(r.err, sentinel) {
			t.Errorf("err = %v, want sentinel", r.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestMockProvider_ManualMode_ToolCall(t *testing.T) {
	m := NewMockProvider("manual")
	ctx := context.Background()

	type result struct {
		resp *CompletionResponse
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		resp, err := m.Complete(ctx, &CompletionRequest{})
		ch <- result{resp, err}
	}()

	_, reply, _ := m.WaitForCall(ctx)
	reply(MockResponse{
		ToolCalls: []ToolCall{{
			ID:        "call-1",
			Name:      "shell_exec",
			Arguments: map[string]any{"command": "ls"},
		}},
	})

	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatal(r.err)
		}
		if !r.resp.IsToolCall() {
			t.Error("expected tool call")
		}
		if r.resp.ToolCalls[0].Name != "shell_exec" {
			t.Errorf("tool = %q", r.resp.ToolCalls[0].Name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestMockProvider_WaitForCall_CtxCancel(t *testing.T) {
	m := NewMockProvider("manual")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	_, _, err := m.WaitForCall(ctx)
	if err == nil {
		t.Error("expected error when ctx is cancelled")
	}
}

// ── scripted then manual fallback ─────────────────────────────────────────────

func TestMockProvider_ScriptedExhaustedFallsToManual(t *testing.T) {
	m := NewMockProvider("hybrid").QueueText("scripted")
	ctx := context.Background()
	req := &CompletionRequest{}

	// First call: scripted.
	r1, _ := m.Complete(ctx, req)
	if r1.Content != "scripted" {
		t.Errorf("got %q", r1.Content)
	}

	// Second call: queue empty → manual mode.
	ch := make(chan *CompletionResponse, 1)
	go func() {
		r, _ := m.Complete(ctx, req)
		ch <- r
	}()

	_, reply, err := m.WaitForCall(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reply(MockResponse{Content: "manual"})

	select {
	case r := <-ch:
		if r.Content != "manual" {
			t.Errorf("got %q, want manual", r.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

// ── embed + stream ────────────────────────────────────────────────────────────

func TestMockProvider_Embed(t *testing.T) {
	m := NewMockProvider("test")
	vec, err := m.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 8 {
		t.Errorf("embedding dim = %d, want 8", len(vec))
	}
}

func TestMockProvider_Stream(t *testing.T) {
	m := NewMockProvider("test").QueueText("streamed")
	ch, err := m.Stream(context.Background(), &CompletionRequest{})
	if err != nil {
		t.Fatal(err)
	}
	got := <-ch
	if got != "streamed" {
		t.Errorf("stream content = %q, want streamed", got)
	}
}
