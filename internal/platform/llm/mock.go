package llm

import (
	"context"
	"fmt"
	"sync"

	agentcoreapi "github.com/inhere/suag/pkg/agentcore/api"
)

// MockResponse is a single canned response returned by MockProvider.
type MockResponse struct {
	// Content is the text reply (used when no ToolCalls).
	Content string
	// ToolCalls is the list of tool calls the mock LLM "requests".
	ToolCalls []agentcoreapi.ToolCall
	// FinishReason overrides the default ("stop" or "tool_calls").
	FinishReason string
	// Err makes the mock return an error instead of a response.
	Err error
}

// MockProvider is a deterministic LLM provider for testing.
//
// Two usage modes:
//
//  1. Scripted – pre-load responses with QueueResponse / QueueToolCall / QueueText.
//     Each Generate() call pops the next queued response in FIFO order.
//
//  2. Manual – call WaitForCall() to block until Generate() is invoked, then
//     call Reply() or ReplyError() to unblock it with a custom response.
//     Useful for stepping through a ReAct loop interactively in tests.
type MockProvider struct {
	mu      sync.Mutex
	queue   []MockResponse               // scripted responses (mode 1)
	pending chan pendingCall             // manual mode channel (mode 2)
	calls   []agentcoreapi.GenerateRequest // all recorded calls
	name    string
}

type pendingCall struct {
	req    agentcoreapi.GenerateRequest
	respCh chan mockResult
}

type mockResult struct {
	resp *agentcoreapi.GenerateResult
	err  error
}

// NewMockProvider creates a MockProvider.
// name is used for Provider.Name().
func NewMockProvider(name string) *MockProvider {
	return &MockProvider{
		name:    name,
		pending: make(chan pendingCall, 1),
	}
}

// Name implements Provider.
func (m *MockProvider) Name() string { return m.name }

// ── Scripted mode ─────────────────────────────────────────────────────────────

// QueueResponse enqueues a raw MockResponse.
func (m *MockProvider) QueueResponse(r MockResponse) *MockProvider {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queue = append(m.queue, r)
	return m
}

// QueueText enqueues a plain text response (finish_reason=stop).
func (m *MockProvider) QueueText(content string) *MockProvider {
	return m.QueueResponse(MockResponse{Content: content, FinishReason: "stop"})
}

// QueueToolCall enqueues a response that requests a single tool call.
func (m *MockProvider) QueueToolCall(name string, args map[string]any) *MockProvider {
	return m.QueueResponse(MockResponse{
		ToolCalls:    []agentcoreapi.ToolCall{{CallID: fmt.Sprintf("call-%s", name), Name: name, Arguments: args}},
		FinishReason: "tool_calls",
	})
}

// QueueError enqueues a response that returns an error.
func (m *MockProvider) QueueError(err error) *MockProvider {
	return m.QueueResponse(MockResponse{Err: err})
}

// ── Manual mode ───────────────────────────────────────────────────────────────

// WaitForCall blocks until the next Generate() call arrives and returns the
// request plus a reply function. Call the reply function to unblock Generate().
//
//	req, reply := mock.WaitForCall(ctx)
//	reply(llm.MockResponse{Content: "done"})
func (m *MockProvider) WaitForCall(ctx context.Context) (agentcoreapi.GenerateRequest, func(MockResponse), error) {
	select {
	case <-ctx.Done():
		return agentcoreapi.GenerateRequest{}, nil, ctx.Err()
	case call := <-m.pending:
		reply := func(r MockResponse) {
			if r.Err != nil {
				call.respCh <- mockResult{err: r.Err}
				return
			}
			resp := toGenerateResult(r)
			call.respCh <- mockResult{resp: resp}
		}
		return call.req, reply, nil
	}
}

// ── Provider interface ────────────────────────────────────────────────────────

// Generate implements the public agentcore provider contract.
// In scripted mode it pops the next queued response.
// If the queue is empty it switches to manual mode and blocks until Reply is called.
func (m *MockProvider) Generate(ctx context.Context, req *agentcoreapi.GenerateRequest) (*agentcoreapi.GenerateResult, error) {
	m.mu.Lock()
	m.calls = append(m.calls, *req)

	if len(m.queue) > 0 {
		r := m.queue[0]
		m.queue = m.queue[1:]
		m.mu.Unlock()
		if r.Err != nil {
			return nil, r.Err
		}
		return toGenerateResult(r), nil
	}
	m.mu.Unlock()

	// Manual mode: send to pending and wait.
	respCh := make(chan mockResult, 1)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case m.pending <- pendingCall{req: *req, respCh: respCh}:
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-respCh:
		return result.resp, result.err
	}
}

// Stream implements the public streaming contract.
func (m *MockProvider) Stream(ctx context.Context, req *agentcoreapi.GenerateRequest, emit agentcoreapi.StreamFunc) (*agentcoreapi.GenerateResult, error) {
	resp, err := m.Generate(ctx, req)
	if err != nil {
		return nil, err
	}
	if emit != nil && resp != nil && resp.Content != "" {
		if err := emit(ctx, agentcoreapi.StreamEvent{
			Kind:  agentcoreapi.StreamEventTextDelta,
			Delta: resp.Content,
		}); err != nil {
			return nil, err
		}
		if err := emit(ctx, agentcoreapi.StreamEvent{Kind: agentcoreapi.StreamEventMessageStop}); err != nil {
			return nil, err
		}
	}
	return resp, nil
}

// Embed implements Provider — returns a fixed-length zero vector.
func (m *MockProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return make([]float32, 8), nil
}

// ── Introspection ─────────────────────────────────────────────────────────────

// Calls returns all GenerateRequests received so far (thread-safe copy).
func (m *MockProvider) Calls() []agentcoreapi.GenerateRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]agentcoreapi.GenerateRequest, len(m.calls))
	copy(out, m.calls)
	return out
}

// CallCount returns the number of Generate() calls made.
func (m *MockProvider) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// QueueLen returns the number of remaining scripted responses.
func (m *MockProvider) QueueLen() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.queue)
}

// Reset clears the queue and recorded calls.
func (m *MockProvider) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queue = nil
	m.calls = nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func toGenerateResult(r MockResponse) *agentcoreapi.GenerateResult {
	out := &agentcoreapi.GenerateResult{
		Content: r.Content,
	}
	reason := r.FinishReason
	if reason == "" {
		if len(r.ToolCalls) > 0 {
			reason = "tool_calls"
		} else {
			reason = "stop"
		}
	}
	switch reason {
	case "tool_calls":
		out.FinishReason = agentcoreapi.FinishToolCalls
	default:
		out.FinishReason = agentcoreapi.FinishFinal
	}
	if len(r.ToolCalls) > 0 {
		out.ToolCalls = make([]agentcoreapi.ToolCall, len(r.ToolCalls))
		for i, call := range r.ToolCalls {
			out.ToolCalls[i] = agentcoreapi.ToolCall{
				CallID:    call.CallID,
				Name:      call.Name,
				Arguments: cloneMap(call.Arguments),
			}
		}
	}
	return out
}
