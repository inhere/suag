# Agentcore Working Memory Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a first-party `pkg/agentcore/memory` package with a minimal `WorkingMemory` implementation that supports summary injection and recent-message window context building.

**Architecture:** Phase 1 keeps the implementation intentionally small. `WorkingMemory` owns only derived context state, not transcript authority. It implements `agentcore.Memory` with `Hydrate` and `BuildContext`, plus a package-local summary injector abstraction; `ObserveMessages` remains a no-op-compatible incremental entry for now but is not the focus of this phase. Tests drive the API and the exact output shape before implementation.

**Tech Stack:** Go 1.24, standard library, existing `pkg/agentcore` runtime contracts, `go test`

---

## File Structure

- Create: `pkg/agentcore/memory/working.go`
  Responsibility: `Config`, `WorkingMemory`, constructor, summary accessors, `Hydrate`, `ObserveMessages`, `BuildContext`
- Create: `pkg/agentcore/memory/summary.go`
  Responsibility: `SummaryInjector`, default injector, summary message creation
- Create: `pkg/agentcore/memory/working_test.go`
  Responsibility: public behavior tests for hydrate/build/config semantics
- Modify: `pkg/agentcore/README.md`
  Responsibility: point readers to the first-party memory package after Phase 1 exists
- Modify: `pkg/agentcore/docs/getting-started.md`
  Responsibility: mention the new default memory implementation as the next extension step
- Modify: `pkg/agentcore/docs/api.md`
  Responsibility: reference `pkg/agentcore/memory` as the first-party default implementation of `agentcore.Memory`

---

### Task 1: Add Failing WorkingMemory Tests

**Files:**
- Create: `pkg/agentcore/memory/working_test.go`

- [ ] **Step 1: Write the failing tests for the default summary injector and hydrate/build behavior**

```go
func TestWorkingMemory_BuildContext_IncludesSummaryThenRecentMessages(t *testing.T) {
    mem := memory.New(memory.Config{MaxRecentMessages: 2})
    mem.SetSummary("repo summary")
    require.NoError(t, mem.Hydrate(context.Background(), &agentcore.RunState{
        Messages: []agentcore.Message{
            {Role: "user", Content: "first"},
            {Role: "assistant", Content: "second"},
            {Role: "user", Content: "third"},
        },
    }))

    msgs, err := mem.BuildContext(context.Background(), nil)
    require.NoError(t, err)
    require.Len(t, msgs, 3)
    require.Equal(t, "system", msgs[0].Role)
    require.Contains(t, msgs[0].Content, "[Memory summary]")
    require.Equal(t, "assistant", msgs[1].Role)
    require.Equal(t, "user", msgs[2].Role)
}

func TestWorkingMemory_Hydrate_IsIdempotent(t *testing.T) {
    state := &agentcore.RunState{
        Messages: []agentcore.Message{
            {Role: "user", Content: "one"},
            {Role: "assistant", Content: "two"},
        },
    }
    mem := memory.New(memory.Config{})
    require.NoError(t, mem.Hydrate(context.Background(), state))
    first, err := mem.BuildContext(context.Background(), nil)
    require.NoError(t, err)
    require.NoError(t, mem.Hydrate(context.Background(), state))
    second, err := mem.BuildContext(context.Background(), nil)
    require.NoError(t, err)
    require.Equal(t, first, second)
}
```

- [ ] **Step 2: Add failing tests for config filtering**

```go
func TestWorkingMemory_BuildContext_FiltersSystemAndToolMessages(t *testing.T) {
    mem := memory.New(memory.Config{
        IncludeSystemMessages: false,
        IncludeToolMessages:   false,
    })
    require.NoError(t, mem.Hydrate(context.Background(), &agentcore.RunState{
        Messages: []agentcore.Message{
            {Role: "system", Content: "internal"},
            {Role: "user", Content: "question"},
            {Role: "assistant", ToolCall: &agentcore.ToolCall{CallID: "call-1", Name: "lookup"}},
            {Role: "tool", ToolResult: &agentcore.ToolResult{CallID: "call-1", Name: "lookup", Content: "result"}},
            {Role: "assistant", Content: "answer"},
        },
    }))

    msgs, err := mem.BuildContext(context.Background(), nil)
    require.NoError(t, err)
    require.Len(t, msgs, 2)
    require.Equal(t, "user", msgs[0].Role)
    require.Equal(t, "assistant", msgs[1].Role)
}
```

- [ ] **Step 3: Add failing tests for summary preservation and custom injector**

```go
func TestWorkingMemory_Hydrate_DoesNotClearSummary(t *testing.T) {
    mem := memory.New(memory.Config{})
    mem.SetSummary("keep me")
    require.NoError(t, mem.Hydrate(context.Background(), &agentcore.RunState{
        Messages: []agentcore.Message{{Role: "user", Content: "hello"}},
    }))
    require.Equal(t, "keep me", mem.Summary())
}

func TestWorkingMemory_BuildContext_UsesCustomSummaryInjector(t *testing.T) {
    injector := memorytestInjector{}
    mem := memory.New(memory.Config{SummaryInjector: injector})
    mem.SetSummary("custom")
    msgs, err := mem.BuildContext(context.Background(), nil)
    require.NoError(t, err)
    require.Len(t, msgs, 1)
    require.Equal(t, "assistant", msgs[0].Role)
    require.Equal(t, "custom: custom", msgs[0].Content)
}
```

- [ ] **Step 4: Run the package tests to verify they fail for missing package/files**

Run: `go test ./pkg/agentcore/memory -v`
Expected: FAIL with missing package or symbol errors

---

### Task 2: Implement WorkingMemory and SummaryInjector

**Files:**
- Create: `pkg/agentcore/memory/working.go`
- Create: `pkg/agentcore/memory/summary.go`

- [ ] **Step 1: Add the public config and injector contracts**

```go
type Config struct {
    MaxRecentMessages     int
    IncludeSystemMessages bool
    IncludeToolMessages   bool
    SummaryInjector       SummaryInjector
}

type SummaryInjector interface {
    InjectSummary(summary string, state *agentcore.RunState) []agentcore.Message
}
```

- [ ] **Step 2: Implement `DefaultSummaryInjector`**

```go
type DefaultSummaryInjector struct{}

func (DefaultSummaryInjector) InjectSummary(summary string, _ *agentcore.RunState) []agentcore.Message {
    if strings.TrimSpace(summary) == "" {
        return nil
    }
    return []agentcore.Message{{
        Role:    "system",
        Content: "[Memory summary]\n" + summary,
    }}
}
```

- [ ] **Step 3: Implement `WorkingMemory` construction and summary accessors**

```go
type WorkingMemory struct {
    config  Config
    summary string
    recent  []agentcore.Message
}

func New(cfg Config) *WorkingMemory { ... }
func (m *WorkingMemory) SetSummary(summary string) { ... }
func (m *WorkingMemory) Summary() string { ... }
```

- [ ] **Step 4: Implement `Hydrate` as a full rebuild of recent window**

```go
func (m *WorkingMemory) Hydrate(_ context.Context, state *agentcore.RunState) error {
    if state == nil {
        m.recent = nil
        return nil
    }
    m.recent = m.filterAndTruncate(state.Messages)
    return nil
}
```

- [ ] **Step 5: Implement `ObserveMessages` minimally but compatibly**

```go
func (m *WorkingMemory) ObserveMessages(_ context.Context, _ *agentcore.RunState, messages ...agentcore.Message) error {
    if len(messages) == 0 {
        return nil
    }
    merged := append(cloneMessages(m.recent), messages...)
    m.recent = m.filterAndTruncate(merged)
    return nil
}
```

- [ ] **Step 6: Implement `BuildContext` as `summary + recent`**

```go
func (m *WorkingMemory) BuildContext(_ context.Context, state *agentcore.RunState) ([]agentcore.Message, error) {
    injector := m.config.SummaryInjector
    if injector == nil {
        injector = DefaultSummaryInjector{}
    }
    out := injector.InjectSummary(m.summary, state)
    out = append(out, cloneMessages(m.recent)...)
    return out, nil
}
```

- [ ] **Step 7: Run focused tests until green**

Run: `go test ./pkg/agentcore/memory -v`
Expected: PASS

---

### Task 3: Verify Integration Expectations and Document the Default Memory Package

**Files:**
- Modify: `pkg/agentcore/README.md`
- Modify: `pkg/agentcore/docs/getting-started.md`
- Modify: `pkg/agentcore/docs/api.md`

- [ ] **Step 1: Add one integration-style test proving `WorkingMemory` satisfies `agentcore.Memory`**

Add to `pkg/agentcore/memory/working_test.go`:

```go
func TestWorkingMemory_ImplementsAgentcoreMemory(t *testing.T) {
    var _ agentcore.Memory = (*memory.WorkingMemory)(nil)
}
```

- [ ] **Step 2: Update README to mention the first-party memory package**

Mention:
- `pkg/agentcore/memory` now exists
- it provides a minimal default `Memory` implementation
- it is intended as the first on-ramp before custom memory systems

- [ ] **Step 3: Update package-local docs**

In:
- `pkg/agentcore/docs/getting-started.md`
- `pkg/agentcore/docs/api.md`

Add:
- where `pkg/agentcore/memory` fits
- that Phase 1 provides summary + recent window

- [ ] **Step 4: Run relevant regression tests**

Run: `go test ./pkg/agentcore ./pkg/agentcore/memory ./examples/agentcore/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/agentcore/memory/working.go pkg/agentcore/memory/summary.go pkg/agentcore/memory/working_test.go pkg/agentcore/README.md pkg/agentcore/docs/getting-started.md pkg/agentcore/docs/api.md
git commit -m "feat(agentcore): add default working memory"
```
