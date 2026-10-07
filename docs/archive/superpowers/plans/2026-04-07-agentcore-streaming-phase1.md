# Agentcore Streaming Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the smallest viable streaming contract to `agentcore` so a runtime can forward model text deltas to a host while keeping `GenerateResult` authoritative and leaving transcript persistence semantics unchanged.

**Architecture:** Phase 1 adds an optional `StreamingProvider` capability on top of the existing `Provider` contract and introduces a minimal stream event model centered on text deltas plus message stop. Runtime support is capability-based: if the configured provider supports streaming and the caller requests a stream handler, runtime uses the streaming path; otherwise it keeps the existing `Generate()` path unchanged. Transcript persistence still happens only from the final validated `GenerateResult`.

**Tech Stack:** Go 1.24, standard library, existing `pkg/agentcore` runtime contracts, `go test`

---

## File Structure

- Modify: `pkg/agentcore/provider.go`
  Responsibility: define `StreamEventKind`, `StreamEvent`, `StreamHandler` or `StreamFunc`, and `StreamingProvider`
- Modify: `pkg/agentcore/types.go`
  Responsibility: extend `RunInput` with the minimal host-facing streaming hook
- Modify: `pkg/agentcore/runtime.go`
  Responsibility: add capability-based streaming model invocation while preserving existing final/tool-call handling
- Modify: `pkg/agentcore/runtime_test.go`
  Responsibility: lock non-streaming compatibility and new streaming behaviors
- Modify: `pkg/agentcore/mock/provider.go`
  Responsibility: add a minimal streaming-capable mock provider for tests and examples
- Modify: `pkg/agentcore/mock/provider_test.go`
  Responsibility: test the mock streaming path if needed
- Modify: `pkg/agentcore/docs/api.md`
  Responsibility: document the new streaming contract
- Modify: `pkg/agentcore/docs/compatibility.md`
  Responsibility: explain Phase 1 compatibility posture for streaming
- Modify: `pkg/agentcore/docs/streaming-design.md`
  Responsibility: reflect any implementation-level narrowing made in Phase 1
- Create: `pkg/agentcore/examples/stream/main.go`
  Responsibility: show the minimal host-facing streaming path using a mock provider
- Modify: `pkg/agentcore/examples/README.md`
  Responsibility: list the new streaming example

---

### Task 1: Write Failing Tests for the New Streaming Contract

**Files:**
- Modify: `pkg/agentcore/runtime_test.go`

- [ ] **Step 1: Add a test proving non-streaming providers still use `Generate()`**

```go
func TestRuntime_Run_UsesGenerateWhenProviderDoesNotStream(t *testing.T) {
    provider := &runtimeTestProvider{result: finalResult("done")}
    runtime := New(Config{Provider: provider, Storage: newRuntimeTestStorage()})

    _, err := runtime.Run(context.Background(), RunInput{RunID: "run-1", Instruction: "hello"})

    require.NoError(t, err)
    require.Equal(t, 1, provider.generateCalls)
    require.Equal(t, 0, provider.streamCalls)
}
```

- [ ] **Step 2: Add a test proving streaming providers are only used when a handler is present**

```go
func TestRuntime_Run_UsesGenerateWithoutStreamHandler(t *testing.T) {
    provider := &runtimeTestStreamingProvider{result: finalResult("done")}
    runtime := New(Config{Provider: provider, Storage: newRuntimeTestStorage()})

    _, err := runtime.Run(context.Background(), RunInput{RunID: "run-2", Instruction: "hello"})

    require.NoError(t, err)
    require.Equal(t, 1, provider.generateCalls)
    require.Equal(t, 0, provider.streamCalls)
}
```

- [ ] **Step 3: Add a test for minimal streaming delivery**

```go
func TestRuntime_Run_StreamsTextDeltasAndReturnsFinalResult(t *testing.T) {
    provider := &runtimeTestStreamingProvider{
        streamEvents: []StreamEvent{
            {Kind: StreamEventTextDelta, Delta: "hel"},
            {Kind: StreamEventTextDelta, Delta: "lo"},
            {Kind: StreamEventMessageStop},
        },
        result: finalResult("hello"),
    }

    var seen []StreamEvent
    runtime := New(Config{Provider: provider, Storage: newRuntimeTestStorage()})

    result, err := runtime.Run(context.Background(), RunInput{
        RunID: "run-3",
        Instruction: "hello",
        Stream: func(_ context.Context, ev StreamEvent) error {
            seen = append(seen, ev)
            return nil
        },
    })

    require.NoError(t, err)
    require.Equal(t, "hello", result.Output)
    require.Len(t, seen, 3)
    require.Equal(t, 1, provider.streamCalls)
    require.Equal(t, 0, provider.generateCalls)
}
```

- [ ] **Step 4: Add tests for error and transcript guarantees**

```go
func TestRuntime_Run_StreamHandlerErrorFailsRun(t *testing.T) { ... }
func TestRuntime_Run_StreamingStillPersistsOnlyFinalAssistantMessage(t *testing.T) { ... }
```

- [ ] **Step 5: Run targeted tests to verify they fail**

Run: `go test ./pkg/agentcore -run "TestRuntime_Run_.*Stream" -v`
Expected: FAIL with missing `StreamingProvider`, `StreamEvent`, or `RunInput.Stream`

---

### Task 2: Add the Minimal Streaming Public Contract

**Files:**
- Modify: `pkg/agentcore/provider.go`
- Modify: `pkg/agentcore/types.go`

- [ ] **Step 1: Add the smallest useful stream event model**

```go
type StreamEventKind string

const (
    StreamEventTextDelta   StreamEventKind = "text_delta"
    StreamEventMessageStop StreamEventKind = "message_stop"
)

type StreamEvent struct {
    Kind     StreamEventKind
    Delta    string
    Metadata map[string]any
}
```

- [ ] **Step 2: Add a host-facing stream callback type**

```go
type StreamFunc func(ctx context.Context, event StreamEvent) error
```

- [ ] **Step 3: Extend `RunInput` minimally**

```go
type RunInput struct {
    ...
    Stream StreamFunc
}
```

- [ ] **Step 4: Add optional provider capability**

```go
type StreamingProvider interface {
    Provider
    Stream(ctx context.Context, req *GenerateRequest, emit StreamFunc) (*GenerateResult, error)
}
```

- [ ] **Step 5: Keep `Provider` unchanged**

Do not modify:

```go
type Provider interface {
    Name() string
    Generate(ctx context.Context, req *GenerateRequest) (*GenerateResult, error)
}
```

---

### Task 3: Implement Capability-Based Runtime Streaming

**Files:**
- Modify: `pkg/agentcore/runtime.go`

- [ ] **Step 1: Refactor model invocation into stream-aware selection**

Keep current flow:
- `BeforeModel`
- provider invocation
- `AfterModel`

But select between:
- `provider.Generate(...)`
- `streamingProvider.Stream(...)`

- [ ] **Step 2: Only stream when both conditions are true**

Rules:
- provider implements `StreamingProvider`
- `RunInput.Stream != nil`

Otherwise:
- preserve current `Generate()` path

- [ ] **Step 3: Emit events without changing transcript persistence**

Rules:
- forward events to `RunInput.Stream`
- if stream callback returns error, fail the run
- do not persist deltas as transcript messages
- still validate final `GenerateResult`

- [ ] **Step 4: Preserve current tool/final branching**

After streaming completes:
- still use `GenerateResult.Validate()`
- still route through final answer vs tool calls exactly as today

- [ ] **Step 5: Run targeted tests until green**

Run: `go test ./pkg/agentcore -run "TestRuntime_Run_.*Stream" -v`
Expected: PASS

---

### Task 4: Add Mock Support, Docs, and Example

**Files:**
- Modify: `pkg/agentcore/mock/provider.go`
- Modify: `pkg/agentcore/docs/api.md`
- Modify: `pkg/agentcore/docs/compatibility.md`
- Modify: `pkg/agentcore/docs/streaming-design.md`
- Modify: `pkg/agentcore/examples/README.md`
- Create: `pkg/agentcore/examples/stream/main.go`

- [ ] **Step 1: Add a small streaming-capable mock provider**

Support:
- configured stream events
- final result return

- [ ] **Step 2: Update docs**

Document:
- `StreamingProvider`
- `StreamEvent`
- `RunInput.Stream`
- final result remains authoritative
- deltas are not authority transcript

- [ ] **Step 3: Add a minimal runnable example**

Show:
- host receives text deltas
- runtime still returns final output

- [ ] **Step 4: Run regression tests**

Run: `go test ./pkg/agentcore/... ./examples/agentcore/bridge`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add docs/superpowers/plans/2026-04-07-agentcore-streaming-phase1.md pkg/agentcore/provider.go pkg/agentcore/types.go pkg/agentcore/runtime.go pkg/agentcore/runtime_test.go pkg/agentcore/mock/provider.go pkg/agentcore/mock/provider_test.go pkg/agentcore/docs/api.md pkg/agentcore/docs/compatibility.md pkg/agentcore/docs/streaming-design.md pkg/agentcore/examples/README.md pkg/agentcore/examples/stream/main.go
git commit -m "feat(agentcore): add minimal streaming contract"
```
