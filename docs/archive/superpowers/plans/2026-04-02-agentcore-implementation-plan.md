# Agent Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a standalone `pkg/agentcore` package that provides a minimal but complete agent runtime loop with provider, storage, tool, prompt, context, and hook extension points.

**Architecture:** The implementation introduces a new public `pkg/agentcore` package with its own stable types and interfaces, plus a small set of built-in defaults (`files` storage, default builders, hook chain, mocks). The runtime owns loop control and persistence consistency; current `internal/*` modules are integrated later through explicit adapters rather than leaking into the public API.

**Tech Stack:** Go 1.24, standard library, existing module layout, `go test`

---

## File Structure

### New public core package

- Create: `pkg/agentcore/types.go`
  Responsibility: shared public types such as `RunInput`, `RunState`, `RunResult`, `RunStatus`, `Message`, `Artifact`, `Usage`, `ResumeMode`, `Yield`, `Control`
- Create: `pkg/agentcore/provider.go`
  Responsibility: `Provider`, `GenerateRequest`, `GenerateResult`, `FinishReason`, `ToolCall`, `ToolSchema`
- Create: `pkg/agentcore/storage.go`
  Responsibility: `Storage` interface and storage-level contracts
- Create: `pkg/agentcore/tools.go`
  Responsibility: `Tool`, `ToolRegistry`, default registry implementation
- Create: `pkg/agentcore/prompt.go`
  Responsibility: `PromptBuilder` interface
- Create: `pkg/agentcore/context.go`
  Responsibility: `ContextBuilder` interface
- Create: `pkg/agentcore/hooks.go`
  Responsibility: hook interfaces, events, control flow contracts
- Create: `pkg/agentcore/errors.go`
  Responsibility: runtime error categories and helper constructors/checks
- Create: `pkg/agentcore/runtime.go`
  Responsibility: `Runtime` interface, `DefaultRuntime`, config, core loop orchestration

### Built-in defaults

- Create: `pkg/agentcore/storage/files/storage.go`
  Responsibility: file-based `Storage` implementation using `state.json`, `messages.jsonl`, `meta.json`, `artifacts/`
- Create: `pkg/agentcore/prompt/static.go`
  Responsibility: static system prompt builder
- Create: `pkg/agentcore/context/default.go`
  Responsibility: default message selection and truncation strategy
- Create: `pkg/agentcore/hooks/chain.go`
  Responsibility: ordered hook execution, short-circuit and yield handling helpers
- Create: `pkg/agentcore/mock/provider.go`
  Responsibility: deterministic provider for tests, including final answer and tool-call sequences
- Create: `pkg/agentcore/mock/tool.go`
  Responsibility: deterministic test tool implementation

### Tests

- Create: `pkg/agentcore/runtime_test.go`
  Responsibility: direct answer flow, multi-step loop, guardrails, error mapping
- Create: `pkg/agentcore/hooks_test.go`
  Responsibility: hook ordering, mutation, short-circuit, yield semantics
- Create: `pkg/agentcore/storage/files/storage_test.go`
  Responsibility: persistence format, load/reload semantics, message authority rules
- Create: `pkg/agentcore/context/default_test.go`
  Responsibility: default context selection and truncation behavior
- Create: `pkg/agentcore/mock/provider_test.go`
  Responsibility: provider script behavior and validation rules

### Compatibility adapters

- Create: `pkg/agentcore/adapters/internalllm/provider.go`
  Responsibility: adapt `internal/llm.Provider` to `agentcore.Provider`
- Create: `pkg/agentcore/adapters/internaltools/registry.go`
  Responsibility: adapt `internal/tools.Registry` to `agentcore.ToolRegistry`

### Existing files to review for patterns

- Review: `internal/llm/provider.go`
- Review: `internal/tools/registry.go`
- Review: `internal/memory/store.go`
- Review: `internal/agent/runner.go`
- Review: `internal/llm/mock.go`

### Docs

- Modify: `README.md`
  Responsibility: add brief note showing `pkg/agentcore` exists and how it differs from current app internals

---

### Task 1: Define Public Core Types and Contracts

**Files:**
- Create: `pkg/agentcore/types.go`
- Create: `pkg/agentcore/provider.go`
- Create: `pkg/agentcore/storage.go`
- Create: `pkg/agentcore/tools.go`
- Create: `pkg/agentcore/prompt.go`
- Create: `pkg/agentcore/context.go`
- Create: `pkg/agentcore/hooks.go`
- Create: `pkg/agentcore/errors.go`
- Test: `pkg/agentcore/runtime_test.go`
- Test: `pkg/agentcore/hooks_test.go`

- [ ] **Step 1: Write the failing contract tests**

```go
func TestGenerateResultValidate_RejectsEmptyResult(t *testing.T) {
    res := &GenerateResult{}
    err := res.Validate()
    require.ErrorContains(t, err, "empty result")
}

func TestGenerateResultValidate_RejectsMixedFinalAndToolCalls(t *testing.T) {
    res := &GenerateResult{
        Content: "done",
        ToolCalls: []ToolCall{{CallID: "call-1", Name: "echo"}},
        FinishReason: FinishToolCalls,
    }
    err := res.Validate()
    require.ErrorContains(t, err, "mutually exclusive")
}

func TestGenerateResultValidate_RejectsToolCallWithoutID(t *testing.T) {
    res := &GenerateResult{
        ToolCalls: []ToolCall{{Name: "echo"}},
        FinishReason: FinishToolCalls,
    }
    err := res.Validate()
    require.ErrorContains(t, err, "call id")
}

func TestGenerateResultValidate_RejectsMissingFinishReason(t *testing.T) {
    res := &GenerateResult{Content: "done"}
    err := res.Validate()
    require.ErrorContains(t, err, "finish reason")
}

func TestControl_Yield(t *testing.T) {
    ctrl := NewControl()
    ctrl.Yield("await_approval", map[string]any{"policy": "manual"})
    require.True(t, ctrl.ShouldYield())
}

func TestHookLifecycleContracts_AreRepresentable(t *testing.T) {
    var _ Hook = (*testHook)(nil)
}
```

- [ ] **Step 2: Run tests to verify the package does not compile yet**

Run: `go test ./pkg/agentcore/...`
Expected: FAIL with missing package/files/types errors

- [ ] **Step 3: Implement the public type set**

```go
type RunInput struct {
    RunID       string
    AgentID     string
    Instruction string
    Metadata    map[string]any
    MaxSteps    int
    ResumeMode  ResumeMode
}

type RunStatus string

const (
    RunIdle      RunStatus = "idle"
    RunRunning   RunStatus = "running"
    RunYielded   RunStatus = "yielded"
    RunCompleted RunStatus = "completed"
    RunFailed    RunStatus = "failed"
)
```

- [ ] **Step 4: Implement provider/tool/storage contracts and validation helpers**

```go
type FinishReason string

const (
    FinishFinal     FinishReason = "final"
    FinishToolCalls FinishReason = "tool_calls"
)

func (r *GenerateResult) Validate() error {
    if r.Content == "" && len(r.ToolCalls) == 0 {
        return ErrEmptyGenerateResult
    }
    if r.Content != "" && len(r.ToolCalls) > 0 {
        return ErrMixedGenerateResult
    }
    if r.FinishReason == "" {
        return ErrMissingFinishReason
    }
    for _, tc := range r.ToolCalls {
        if tc.CallID == "" {
            return ErrMissingToolCallID
        }
    }
    return nil
}
```

- [ ] **Step 5: Implement hook events, `Control`, and error types**

```go
type Control struct {
    ShortCircuited bool
    Yielded        bool
    Replacement    any
    YieldReason    string
    YieldMetadata  map[string]any
}
```

- [ ] **Step 6: Make all public hook lifecycle events concrete in types**

```go
type RunEvent struct { ... }
type PromptEvent struct { ... }
type ContextEvent struct { ... }
type ModelEvent struct { ... }
type ToolEvent struct { ... }
type ErrorEvent struct { ... }
```

- [ ] **Step 7: Run focused tests**

Run: `go test ./pkg/agentcore/... -run "TestGenerateResultValidate|TestControl_Yield" -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add pkg/agentcore/types.go pkg/agentcore/provider.go pkg/agentcore/storage.go pkg/agentcore/tools.go pkg/agentcore/prompt.go pkg/agentcore/context.go pkg/agentcore/hooks.go pkg/agentcore/errors.go pkg/agentcore/runtime_test.go pkg/agentcore/hooks_test.go
git commit -m "feat: add agentcore public contracts"
```

### Task 2: Add Default Registry, Prompt, Context, and Mock Components

**Files:**
- Modify: `pkg/agentcore/tools.go`
- Create: `pkg/agentcore/prompt/static.go`
- Create: `pkg/agentcore/context/default.go`
- Create: `pkg/agentcore/hooks/chain.go`
- Create: `pkg/agentcore/mock/provider.go`
- Create: `pkg/agentcore/mock/tool.go`
- Create: `pkg/agentcore/context/default_test.go`
- Create: `pkg/agentcore/mock/provider_test.go`
- Test: `pkg/agentcore/hooks_test.go`

- [ ] **Step 1: Write failing tests for built-in components**

```go
func TestDefaultRegistrySchemas(t *testing.T) {
    reg := NewToolRegistry()
    reg.Register(mock.NewTool("echo"))
    require.Len(t, reg.Schemas(), 1)
}

func TestDefaultContextBuilder_Truncates(t *testing.T) {
    builder := context.DefaultBuilder{MaxMessages: 3}
    msgs, err := builder.BuildMessages(ctx, stateWithMessages(5))
    require.NoError(t, err)
    require.Len(t, msgs, 3)
}
```

- [ ] **Step 2: Run the component tests to confirm failures**

Run: `go test ./pkg/agentcore/... -run "TestDefaultRegistrySchemas|TestDefaultContextBuilder_Truncates" -v`
Expected: FAIL with missing constructors and builders

- [ ] **Step 3: Implement the default tool registry**

```go
type Registry struct {
    mu    sync.RWMutex
    tools map[string]Tool
}
```

- [ ] **Step 4: Implement the static prompt builder and default context builder**

```go
type StaticBuilder struct {
    Prompt string
}

type DefaultBuilder struct {
    MaxMessages int
}
```

- [ ] **Step 5: Implement ordered hook chain execution helpers**

```go
type Chain struct {
    hooks []agentcore.Hook
}
```

- [ ] **Step 6: Implement deterministic mock provider and mock tool**

```go
provider := mock.NewProvider(
    mock.Final("hello"),
    mock.ToolCall("call-1", "echo", map[string]any{"text": "x"}),
)
```

- [ ] **Step 7: Run component package tests**

Run: `go test ./pkg/agentcore/context ./pkg/agentcore/mock ./pkg/agentcore/... -run "TestDefaultRegistrySchemas|TestDefaultContextBuilder_Truncates|TestMock" -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add pkg/agentcore/tools.go pkg/agentcore/prompt/static.go pkg/agentcore/context/default.go pkg/agentcore/hooks/chain.go pkg/agentcore/mock/provider.go pkg/agentcore/mock/tool.go pkg/agentcore/context/default_test.go pkg/agentcore/mock/provider_test.go pkg/agentcore/hooks_test.go
git commit -m "feat: add agentcore built-in components"
```

### Task 3: Implement File Storage and Reload Semantics

**Files:**
- Create: `pkg/agentcore/storage/files/storage.go`
- Create: `pkg/agentcore/storage/files/storage_test.go`
- Test: `pkg/agentcore/runtime_test.go`

- [ ] **Step 1: Write failing file storage tests**

```go
func TestFileStorage_SaveAndReload(t *testing.T) {
    store := files.NewStorage(t.TempDir())
    state := sampleRunState("run-1")
    require.NoError(t, store.SaveRun(ctx, state))
    require.NoError(t, store.AppendMessages(ctx, "run-1", state.Messages...))

    loaded, err := store.LoadRun(ctx, "run-1")
    require.NoError(t, err)
    msgs, err := store.LoadMessages(ctx, "run-1")
    require.NoError(t, err)
    require.Equal(t, "run-1", loaded.RunID)
    require.Len(t, msgs, len(state.Messages))
}
```

- [ ] **Step 2: Run the storage tests to verify they fail**

Run: `go test ./pkg/agentcore/storage/files -run TestFileStorage_SaveAndReload -v`
Expected: FAIL with missing files storage implementation

- [ ] **Step 3: Implement directory layout and JSON helpers**

```go
// <root>/<run-id>/state.json
// <root>/<run-id>/messages.jsonl
// <root>/<run-id>/meta.json
// <root>/<run-id>/artifacts/
```

- [ ] **Step 4: Implement `LoadRun`, `LoadMessages`, `SaveRun`, and `AppendMessages`**

```go
func (s *Storage) AppendMessages(ctx context.Context, runID string, msgs ...agentcore.Message) error {
    // append JSONL records to messages.jsonl
}
```

- [ ] **Step 5: Encode the authority rule in tests**

```go
func TestFileStorage_LoadMessagesIsAuthoritative(t *testing.T) {
    // state.json may contain stale Messages, but LoadMessages drives recovery
}
```

- [ ] **Step 6: Run storage tests**

Run: `go test ./pkg/agentcore/storage/files -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add pkg/agentcore/storage/files/storage.go pkg/agentcore/storage/files/storage_test.go pkg/agentcore/runtime_test.go
git commit -m "feat: add agentcore file storage"
```

### Task 4: Implement the Minimal Runtime Final-Answer Path

**Files:**
- Create: `pkg/agentcore/runtime.go`
- Modify: `pkg/agentcore/errors.go`
- Modify: `pkg/agentcore/hooks.go`
- Modify: `pkg/agentcore/runtime_test.go`

- [ ] **Step 1: Write failing runtime tests for direct final answer**

```go
func TestRuntime_Run_FinalAnswer(t *testing.T) {
    rt := newRuntimeWithMockProvider(mock.Final("done"))
    result, err := rt.Run(context.Background(), agentcore.RunInput{
        AgentID: "agent-1",
        Instruction: "say done",
    })
    require.NoError(t, err)
    require.Equal(t, agentcore.RunCompleted, result.Status)
    require.Equal(t, "done", result.Output)
}

func TestRuntime_New_WithOnlyProvider_UsesDefaults(t *testing.T) {
    rt := agentcore.New(agentcore.Config{
        Provider: mock.NewProvider(mock.Final("ok")),
    })
    result, err := rt.Run(context.Background(), agentcore.RunInput{Instruction: "hello"})
    require.NoError(t, err)
    require.Equal(t, agentcore.RunCompleted, result.Status)
}
```

- [ ] **Step 2: Run the direct-runtime tests and confirm failure**

Run: `go test ./pkg/agentcore -run TestRuntime_Run_FinalAnswer -v`
Expected: FAIL with missing runtime implementation

- [ ] **Step 3: Implement runtime config, defaults, and run initialization**

```go
type Config struct {
    Provider            Provider
    Storage             Storage
    Tools               ToolRegistry
    PromptBuilder       PromptBuilder
    ContextBuilder      ContextBuilder
    Hooks               []Hook
    MaxSteps            int
    MaxToolCallsPerStep int
}
```

- [ ] **Step 4: Auto-fill built-in defaults so a provider-only config is runnable**

```go
if cfg.Storage == nil {
    cfg.Storage = files.NewStorage(filepath.Join(os.TempDir(), "agentcore-runs"))
}
if cfg.Tools == nil { cfg.Tools = agentcore.NewToolRegistry() }
if cfg.PromptBuilder == nil { cfg.PromptBuilder = prompt.StaticBuilder{} }
if cfg.ContextBuilder == nil { cfg.ContextBuilder = context.DefaultBuilder{} }
hookChain := hooks.NewChain(cfg.Hooks)
```

补充约定：

- `Config.Hooks` 的公共配置形状固定为 `[]Hook`
- runtime 内部始终自行构造 `hooks.Chain`
- 不把 `hooks.Chain` 直接暴露为 `Config` 字段类型，避免配置面与执行器混用

- [ ] **Step 5: Implement new run and resume input handling**

```go
if input.ResumeMode == ResumeFromState {
    // do not append a new user message
}
```

- [ ] **Step 6: Implement final-answer flow and persist completion**

```go
if res.FinishReason == FinishFinal {
    state.Status = RunCompleted
    state.Messages = append(state.Messages, Message{Role: RoleAssistant, Content: res.Content})
}
```

- [ ] **Step 7: Run targeted runtime tests**

Run: `go test ./pkg/agentcore -run "TestRuntime_Run_FinalAnswer|TestRuntime_New_WithOnlyProvider_UsesDefaults|TestGenerateResultValidate" -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add pkg/agentcore/runtime.go pkg/agentcore/errors.go pkg/agentcore/hooks.go pkg/agentcore/runtime_test.go
git commit -m "feat: add agentcore runtime final-answer path"
```

### Task 5: Implement Tool Loop, Guardrails, and Error Mapping

**Files:**
- Modify: `pkg/agentcore/runtime.go`
- Modify: `pkg/agentcore/runtime_test.go`
- Modify: `pkg/agentcore/mock/provider.go`
- Modify: `pkg/agentcore/mock/tool.go`

- [ ] **Step 1: Write failing tests for tool loop and error cases**

```go
func TestRuntime_Run_ToolCallThenFinal(t *testing.T) {
    rt := runtimeWithScript(
        mock.ToolCall("call-1", "echo", map[string]any{"text": "hello"}),
        mock.Final("done"),
    )
    result, err := rt.Run(context.Background(), input("do work"))
    require.NoError(t, err)
    require.Equal(t, "done", result.Output)
}

func TestRuntime_Run_ToolErrorBecomesMessage(t *testing.T) {
    rt := runtimeWithFailingTool()
    _, err := rt.Run(context.Background(), input("recover from tool failure"))
    require.NoError(t, err)
}

func TestRuntime_Run_ProviderErrorFailsRun(t *testing.T) {
    rt := runtimeWithProviderError()
    _, err := rt.Run(context.Background(), input("boom"))
    require.Error(t, err)
}

func TestRuntime_Run_StorageErrorFailsRun(t *testing.T) {
    rt := runtimeWithBrokenStorage()
    _, err := rt.Run(context.Background(), input("persist"))
    require.Error(t, err)
}
```

- [ ] **Step 2: Run the tool-flow tests to verify they fail**

Run: `go test ./pkg/agentcore -run "TestRuntime_Run_ToolCallThenFinal|TestRuntime_Run_ToolErrorBecomesMessage" -v`
Expected: FAIL with missing tool-loop support

- [ ] **Step 3: Implement tool resolution, execution, and message writeback**

```go
for _, call := range res.ToolCalls {
    tool, ok := r.tools.Get(call.Name)
    if !ok { ... }
    out, err := tool.Call(ctx, call)
}
```

- [ ] **Step 4: Implement `MaxSteps` and `MaxToolCallsPerStep` guardrails**

```go
if len(res.ToolCalls) > cfg.MaxToolCallsPerStep {
    return failRun(state, ErrTooManyToolCalls)
}
```

- [ ] **Step 5: Implement runtime error mapping rules**

```go
// Provider/Hook/Storage => fail + returned error
// Tool error => tool message + continue
```

- [ ] **Step 6: Persist failed terminal state and `LastError` on hard failures**

```go
state.Status = RunFailed
state.LastError = err.Error()
_ = r.storage.SaveRun(ctx, state)
```

- [ ] **Step 7: Run runtime tests**

Run: `go test ./pkg/agentcore -run "TestRuntime_Run_|TestMax|TestRuntime_Run_ProviderErrorFailsRun|TestRuntime_Run_StorageErrorFailsRun" -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add pkg/agentcore/runtime.go pkg/agentcore/runtime_test.go pkg/agentcore/mock/provider.go pkg/agentcore/mock/tool.go
git commit -m "feat: add agentcore tool loop and guardrails"
```

### Task 6: Implement Hook Mutation, Short-Circuit, and Yield

**Files:**
- Modify: `pkg/agentcore/runtime.go`
- Modify: `pkg/agentcore/hooks.go`
- Modify: `pkg/agentcore/hooks/chain.go`
- Modify: `pkg/agentcore/hooks_test.go`
- Modify: `pkg/agentcore/runtime_test.go`

- [ ] **Step 1: Write failing tests for hook mutation and yield behavior**

```go
func TestHooks_BeforeModelCanMutateRequest(t *testing.T) {
    hook := hooktest.BeforeModel(func(e *agentcore.ModelEvent) error {
        e.Request.SystemPrompt = "override"
        return nil
    })
}

func TestHooks_BeforeToolCanYield(t *testing.T) {
    hook := hooktest.BeforeTool(func(e *agentcore.ToolEvent) error {
        e.Control.Yield("await_approval", map[string]any{"tool": e.Call.Name})
        return nil
    })
}

func TestHooks_BeforeToolCanMutateArgs(t *testing.T) {
    hook := hooktest.BeforeTool(func(e *agentcore.ToolEvent) error {
        e.Call.Arguments["mode"] = "safe"
        return nil
    })
}

func TestHooks_FullLifecycleEvents(t *testing.T) {
    order := []string{}
    hook := hooktest.FullLifecycle(&order)
    _ = hook
}

func TestHooks_OnErrorAndAfterRun(t *testing.T) {
    hook := hooktest.RecordLifecycle()
    _ = hook
}

func TestHooks_ShortCircuitStopsLaterSameStageHooks(t *testing.T) {
    var order []string
    hooks := []agentcore.Hook{
        hooktest.BeforeModelNamed("first", &order, func(e *agentcore.ModelEvent) error {
            e.Control.ShortCircuitWithResult(mock.FinalResult("cached"))
            return nil
        }),
        hooktest.BeforeModelNamed("second", &order, nil),
    }
    _ = hooks
}
```

- [ ] **Step 2: Run hook-focused tests to confirm failure**

Run: `go test ./pkg/agentcore -run "TestHooks_|TestRuntime_Run_Yield" -v`
Expected: FAIL with missing hook control behavior

- [ ] **Step 3: Implement stage event creation and mutation handoff**

```go
modelEvent := &ModelEvent{State: NewRunStateView(state), Request: req, Control: NewControl()}
```

- [ ] **Step 4: Trigger all lifecycle hooks, including prompt/context/error/finalization**

```go
// BeforeRun
// BeforePromptBuild
// BeforeContextBuild
// BeforeModel / AfterModel
// BeforeTool / AfterTool
// OnError
// AfterRun
```

- [ ] **Step 5: Implement short-circuit semantics**

```go
if event.Control.ShortCircuited {
    // replacement model/tool result
}
```

- [ ] **Step 5a: Ensure `BeforeTool` mutations are applied to actual tool calls**

```go
// mutated e.Call.Arguments must be what Tool.Call receives
```

- [ ] **Step 5b: Ensure short-circuit stops later hooks in the same stage**

```go
// once one BeforeModel/BeforeTool hook short-circuits,
// later hooks for that same stage are not executed
```

- [ ] **Step 6: Implement `yielded` result handling**

```go
if event.Control.ShouldYield() {
    state.Status = RunYielded
    return &RunResult{RunID: state.RunID, Status: RunYielded, Yield: ...}, nil
}
```

- [ ] **Step 7: Decide and encode after-stage behavior in tests**

```go
// short-circuit still leads to AfterRun
// yielded skips default provider/tool execution
// hard failures trigger OnError and still finish with AfterRun
```

- [ ] **Step 8: Run hook and runtime tests**

Run: `go test ./pkg/agentcore -run "TestHooks_|TestRuntime_Run_Yield|TestHooks_OnErrorAndAfterRun|TestHooks_BeforeToolCanMutateArgs|TestHooks_ShortCircuitStopsLaterSameStageHooks" -v`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add pkg/agentcore/runtime.go pkg/agentcore/hooks.go pkg/agentcore/hooks/chain.go pkg/agentcore/hooks_test.go pkg/agentcore/runtime_test.go
git commit -m "feat: add agentcore hook control flow"
```

### Task 7: Lock Resume Semantics Matrix

**Files:**
- Modify: `pkg/agentcore/runtime_test.go`
- Modify: `pkg/agentcore/runtime.go`

- [ ] **Step 1: Write failing table-driven tests for the full resume matrix**

```go
func TestRuntime_Run_ResumeMatrix(t *testing.T) {
    tests := []struct{
        name       string
        resumeMode agentcore.ResumeMode
        instruction string
        wantAppend bool
    }{
        {"auto no input", agentcore.ResumeAuto, "", false},
        {"auto with input", agentcore.ResumeAuto, "continue", true},
        {"from state", agentcore.ResumeFromState, "", false},
        {"with input", agentcore.ResumeWithInput, "continue", true},
    }
}
```

- [ ] **Step 2: Write failing tests for terminal runs refusing resume**

```go
func TestRuntime_Run_RejectsResumeForCompletedOrFailed(t *testing.T) {
    // completed and failed runs must return terminal-state errors
}
```

- [ ] **Step 3: Run resume-focused tests and confirm failure**

Run: `go test ./pkg/agentcore -run "TestRuntime_Run_ResumeMatrix|TestRuntime_Run_RejectsResumeForCompletedOrFailed" -v`
Expected: FAIL until the full matrix is implemented

- [ ] **Step 4: Implement the full `ResumeAuto` / `ResumeFromState` / `ResumeWithInput` rules**

```go
switch input.ResumeMode {
case ResumeFromState:
    appendInput = false
case ResumeWithInput:
    appendInput = true
case ResumeAuto:
    appendInput = input.Instruction != ""
}
```

- [ ] **Step 5: Run resume tests**

Run: `go test ./pkg/agentcore -run "TestRuntime_Run_ResumeMatrix|TestRuntime_Run_RejectsResumeForCompletedOrFailed" -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add pkg/agentcore/runtime.go pkg/agentcore/runtime_test.go
git commit -m "feat: lock agentcore resume semantics"
```

### Task 8: Implement Recovery Adapters and Usage Docs

**Files:**
- Create: `pkg/agentcore/adapters/internalllm/provider.go`
- Create: `pkg/agentcore/adapters/internaltools/registry.go`
- Modify: `pkg/agentcore/runtime_test.go`
- Modify: `README.md`

- [ ] **Step 1: Write failing tests for recovery and adapter compilation**

```go
func TestRuntime_Run_ResumeFromState(t *testing.T) {
    store := files.NewStorage(t.TempDir())
    rt := newRuntime(store, mockProviderScript(...))
    first, err := rt.Run(ctx, inputWithRunID("run-1"))
    _ = first
    resumed, err := rt.Run(ctx, agentcore.RunInput{
        RunID: "run-1",
        ResumeMode: agentcore.ResumeFromState,
    })
    require.NoError(t, err)
    require.NotNil(t, resumed)
}
```

- [ ] **Step 2: Run the final focused tests and confirm failure**

Run: `go test ./pkg/agentcore/... -run "TestRuntime_Run_ResumeFromState|TestAdapter" -v`
Expected: FAIL with missing adapters or reload wiring

- [ ] **Step 3: Implement adapter packages**

```go
func NewProvider(p internalllm.Provider) agentcore.Provider { ... }
func NewRegistry(r *internaltools.Registry) agentcore.ToolRegistry { ... }
```

- [ ] **Step 3a: Add adapter mapping tests for finish reasons**

```go
func TestInternalLLMAdapter_MapsFinishReasons(t *testing.T) {
    // stop -> FinishFinal
    // tool_calls -> FinishToolCalls
    // length -> ProviderError
    // unknown -> ProviderError
}
```

- [ ] **Step 3b: Implement explicit finish-reason mapping policy**

```go
switch resp.FinishReason {
case "stop":
    out.FinishReason = agentcore.FinishFinal
case "tool_calls":
    out.FinishReason = agentcore.FinishToolCalls
case "length":
    return nil, agentcore.NewProviderError("unsupported finish reason: length")
default:
    return nil, agentcore.NewProviderError("unknown finish reason")
}
```

- [ ] **Step 4: Finalize reload semantics in runtime tests**

```go
// ResumeFromState does not append a duplicate user instruction
// ResumeWithInput appends a new user instruction
// yielded runs can resume
```

- [ ] **Step 5: Update `README.md` with a minimal embedding example**

```go
rt := agentcore.New(agentcore.Config{Provider: provider, Storage: files.NewStorage("./runs")})
result, err := rt.Run(ctx, agentcore.RunInput{Instruction: "hello"})
```

- [ ] **Step 6: Run the full package test suite**

Run: `go test ./pkg/agentcore/... ./pkg/agentcore/adapters/... -v`
Expected: PASS

- [ ] **Step 7: Run repository-level regression tests for touched areas**

Run: `go test ./internal/llm ./internal/tools ./internal/memory -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add pkg/agentcore/adapters/internalllm/provider.go pkg/agentcore/adapters/internaltools/registry.go pkg/agentcore/runtime_test.go README.md
git commit -m "feat: add agentcore adapters and docs"
```
