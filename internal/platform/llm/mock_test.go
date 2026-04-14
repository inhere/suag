package llm

import (
	"context"
	"errors"
	"testing"
	"time"

	agentcoreapi "github.com/inhere/axon/pkg/agentcore/api"
)

// ── scripted mode ─────────────────────────────────────────────────────────────

func TestMockProvider_ScriptedText(t *testing.T) {
	m := NewMockProvider("test").
		QueueText("hello world").
		QueueText("second reply")

	ctx := context.Background()
	req := &agentcoreapi.GenerateRequest{Messages: []agentcoreapi.Message{{Role: "user", Content: "hi"}}}

	resp, err := m.Generate(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hello world" {
		t.Errorf("content = %q, want %q", resp.Content, "hello world")
	}
	if resp.FinishReason != agentcoreapi.FinishFinal {
		t.Errorf("finish_reason = %q, want final", resp.FinishReason)
	}

	resp2, _ := m.Generate(ctx, req)
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
	req := &agentcoreapi.GenerateRequest{}

	// First call → tool call.
	resp, err := m.Generate(ctx, req)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if resp.FinishReason != agentcoreapi.FinishToolCalls {
		t.Fatalf("finish reason = %q, want tool_calls", resp.FinishReason)
	}
	if resp.ToolCalls[0].Name != "file_read" {
		t.Errorf("tool name = %q, want file_read", resp.ToolCalls[0].Name)
	}
	if resp.ToolCalls[0].Arguments["path"] != "README.md" {
		t.Errorf("unexpected args: %v", resp.ToolCalls[0].Arguments)
	}

	// Second call → completion.
	resp2, _ := m.Generate(ctx, req)
	if len(resp2.ToolCalls) != 0 {
		t.Error("expected non-tool response")
	}
	if resp2.FinishReason != agentcoreapi.FinishFinal {
		t.Errorf("finish reason = %q, want final", resp2.FinishReason)
	}
}

func TestMockProvider_ScriptedError(t *testing.T) {
	sentinel := errors.New("rate limited")
	m := NewMockProvider("test").QueueError(sentinel)

	_, err := m.Generate(context.Background(), &agentcoreapi.GenerateRequest{})
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

	type result struct {
		resp *agentcoreapi.GenerateResult
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		resp, err := m.Generate(ctx, &agentcoreapi.GenerateRequest{
			Messages: []agentcoreapi.Message{{Role: "user", Content: "what time is it?"}},
		})
		ch <- result{resp, err}
	}()

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
			t.Fatalf("Generate error: %v", r.err)
		}
		if r.resp.Content != "It's go time!" {
			t.Errorf("content = %q", r.resp.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Generate() to return")
	}
}

func TestMockProvider_ManualMode_Error(t *testing.T) {
	m := NewMockProvider("manual")
	ctx := context.Background()

	type result struct {
		resp *agentcoreapi.GenerateResult
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		resp, err := m.Generate(ctx, &agentcoreapi.GenerateRequest{})
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
		resp *agentcoreapi.GenerateResult
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		resp, err := m.Generate(ctx, &agentcoreapi.GenerateRequest{})
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
		if r.resp.FinishReason != agentcoreapi.FinishToolCalls {
			t.Errorf("finish reason = %q, want tool_calls", r.resp.FinishReason)
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
	cancel()

	_, _, err := m.WaitForCall(ctx)
	if err == nil {
		t.Error("expected error when ctx is cancelled")
	}
}

// ── scripted then manual fallback ─────────────────────────────────────────────

func TestMockProvider_ScriptedExhaustedFallsToManual(t *testing.T) {
	m := NewMockProvider("hybrid").QueueText("scripted")
	ctx := context.Background()
	req := &agentcoreapi.GenerateRequest{}

	r1, _ := m.Generate(ctx, req)
	if r1.Content != "scripted" {
		t.Errorf("got %q", r1.Content)
	}

	ch := make(chan *agentcoreapi.GenerateResult, 1)
	go func() {
		r, _ := m.Generate(ctx, req)
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
	var events []agentcoreapi.StreamEvent
	resp, err := m.Stream(context.Background(), &agentcoreapi.GenerateRequest{}, func(_ context.Context, event agentcoreapi.StreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "streamed" {
		t.Fatalf("response content = %q, want streamed", resp.Content)
	}
	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}
	if events[0].Kind != agentcoreapi.StreamEventTextDelta || events[0].Delta != "streamed" {
		t.Fatalf("first event = %#v, want text delta", events[0])
	}
	if events[1].Kind != agentcoreapi.StreamEventMessageStop {
		t.Fatalf("second event = %#v, want message stop", events[1])
	}
}
