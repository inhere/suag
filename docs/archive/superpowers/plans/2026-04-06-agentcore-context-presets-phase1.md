# Agentcore Context Presets Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add first-party context presets in `pkg/agentcore/context` so external users can choose transcript-only and memory-first context assembly without writing custom builders.

**Architecture:** Phase 1 stays intentionally small. It adds named preset constructors in `pkg/agentcore/context` while keeping `agentcore.ContextBuilder` unchanged and leaving `Runtime.Config` untouched. `TranscriptWindow` becomes the clear baseline preset, and `MemoryFirst` becomes the default pairing for first-party `WorkingMemory`. Existing `DefaultBuilder` remains for compatibility.

**Tech Stack:** Go 1.24, standard library, existing `pkg/agentcore` runtime contracts, `go test`

---

## File Structure

- Modify: `pkg/agentcore/context/default.go`
  Responsibility: keep `DefaultBuilder`, add `WindowConfig`, `TranscriptWindow`, `MemoryFirst`, and private builder implementations
- Modify: `pkg/agentcore/context/default_test.go`
  Responsibility: test preset behavior, fallback rules, copy semantics, and memory error passthrough
- Modify: `pkg/agentcore/README.md`
  Responsibility: mention the new first-party context presets
- Modify: `pkg/agentcore/docs/getting-started.md`
  Responsibility: show transcript-only and memory-first usage paths
- Modify: `pkg/agentcore/docs/api.md`
  Responsibility: document the new preset constructors as the preferred builder entry points

---

### Task 1: Write Failing Tests for Context Presets

**Files:**
- Modify: `pkg/agentcore/context/default_test.go`

- [ ] **Step 1: Add failing tests for `TranscriptWindow`**

```go
func TestTranscriptWindow_TruncatesLatestMessages(t *testing.T) {
    builder := builderctx.TranscriptWindow(builderctx.WindowConfig{MaxMessages: 2})
    msgs, err := builder.BuildMessages(stdctx.Background(), stateWithMessages(4))
    require.NoError(t, err)
    require.Len(t, msgs, 2)
    require.Equal(t, "message-3", msgs[0].Content)
    require.Equal(t, "message-4", msgs[1].Content)
}

func TestTranscriptWindow_ReturnsCopies(t *testing.T) {
    state := stateWithMessages(2)
    builder := builderctx.TranscriptWindow(builderctx.WindowConfig{})
    msgs, _ := builder.BuildMessages(stdctx.Background(), state)
    msgs[0].Content = "mutated"
    require.Equal(t, "message-1", state.Messages[0].Content)
}
```

- [ ] **Step 2: Add failing tests for `MemoryFirst`**

```go
func TestMemoryFirst_UsesMemoryWhenPresent(t *testing.T) {
    mem := &contextTestMemory{
        context: []agentcore.Message{{Role: "system", Content: "from-memory"}},
    }
    builder := builderctx.MemoryFirst(mem, builderctx.MemoryFirstConfig{MaxMessages: 2})
    msgs, err := builder.BuildMessages(stdctx.Background(), stateWithMessages(4))
    require.NoError(t, err)
    require.Len(t, msgs, 1)
    require.Equal(t, "from-memory", msgs[0].Content)
}

func TestMemoryFirst_FallsBackOnlyWhenMemoryNil(t *testing.T) {
    builder := builderctx.MemoryFirst(nil, builderctx.MemoryFirstConfig{MaxMessages: 2})
    msgs, err := builder.BuildMessages(stdctx.Background(), stateWithMessages(4))
    require.NoError(t, err)
    require.Len(t, msgs, 2)
    require.Equal(t, "message-3", msgs[0].Content)
    require.Equal(t, "message-4", msgs[1].Content)
}

func TestMemoryFirst_DoesNotFallbackOnEmptyMemoryContext(t *testing.T) {
    mem := &contextTestMemory{context: nil}
    builder := builderctx.MemoryFirst(mem, builderctx.MemoryFirstConfig{MaxMessages: 2})
    msgs, err := builder.BuildMessages(stdctx.Background(), stateWithMessages(4))
    require.NoError(t, err)
    require.Nil(t, msgs)
}
```

- [ ] **Step 3: Add failing test for memory errors**

```go
func TestMemoryFirst_PropagatesMemoryErrors(t *testing.T) {
    mem := &contextTestMemory{buildErr: errors.New("memory failed")}
    builder := builderctx.MemoryFirst(mem, builderctx.MemoryFirstConfig{})
    _, err := builder.BuildMessages(stdctx.Background(), stateWithMessages(1))
    require.ErrorContains(t, err, "memory failed")
}
```

- [ ] **Step 4: Run targeted tests to verify they fail**

Run: `go test ./pkg/agentcore/context -run "TestTranscriptWindow|TestMemoryFirst" -v`
Expected: FAIL with missing constructor/types errors

---

### Task 2: Implement TranscriptWindow and MemoryFirst

**Files:**
- Modify: `pkg/agentcore/context/default.go`

- [ ] **Step 1: Add preset config types and constructors**

```go
type WindowConfig struct {
    MaxMessages int
}

func TranscriptWindow(cfg WindowConfig) agentcore.ContextBuilder { ... }

type MemoryFirstConfig struct {
    MaxMessages int
}

func MemoryFirst(mem agentcore.Memory, cfg MemoryFirstConfig) agentcore.ContextBuilder { ... }
```

- [ ] **Step 2: Keep `DefaultBuilder` as compatibility wrapper**

```go
func (b DefaultBuilder) BuildMessages(ctx context.Context, state *agentcore.RunState) ([]agentcore.Message, error) {
    return TranscriptWindow(WindowConfig{MaxMessages: b.MaxMessages}).BuildMessages(ctx, state)
}
```

- [ ] **Step 3: Implement transcript window builder**

```go
type transcriptWindowBuilder struct {
    maxMessages int
}
```

Behavior:
- `state == nil` => nil
- `maxMessages <= 0` => no truncation
- return shallow copies of messages

- [ ] **Step 4: Implement memory-first builder**

```go
type memoryFirstBuilder struct {
    mem        agentcore.Memory
    maxMessages int
}
```

Behavior:
- `mem == nil` => use transcript window fallback
- `mem != nil` => return `mem.BuildContext(...)`
- do not fallback when returned context is empty
- propagate memory errors

- [ ] **Step 5: Run targeted tests until green**

Run: `go test ./pkg/agentcore/context -run "TestTranscriptWindow|TestMemoryFirst|TestDefaultContextBuilder" -v`
Expected: PASS

---

### Task 3: Document the New Presets and Run Regression Tests

**Files:**
- Modify: `pkg/agentcore/README.md`
- Modify: `pkg/agentcore/docs/getting-started.md`
- Modify: `pkg/agentcore/docs/api.md`

- [ ] **Step 1: Update README with the new preset entry points**

Add:
- transcript-only preset
- memory-first preset
- how they fit with `pkg/agentcore/memory`

- [ ] **Step 2: Update getting started docs**

Add:
- `context.TranscriptWindow(...)`
- `context.MemoryFirst(...)`

- [ ] **Step 3: Update API docs**

Document:
- `WindowConfig`
- `TranscriptWindow`
- `MemoryFirst`

- [ ] **Step 4: Run regression tests**

Run: `go test ./pkg/agentcore ./pkg/agentcore/context ./pkg/agentcore/memory ./examples/agentcore/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/agentcore/context/default.go pkg/agentcore/context/default_test.go pkg/agentcore/README.md pkg/agentcore/docs/getting-started.md pkg/agentcore/docs/api.md
git commit -m "feat(agentcore): add context preset builders"
```
