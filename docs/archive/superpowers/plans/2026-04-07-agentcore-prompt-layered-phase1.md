# Agentcore Prompt Layered Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a first-party layered prompt builder in `pkg/agentcore/prompt` so external users can compose base prompt text, optional run metadata, and ordered prompt sections without hand-building strings.

**Architecture:** Phase 1 keeps `agentcore.PromptBuilder` unchanged and preserves `prompt.StaticBuilder` as the minimal compatibility path. The new `prompt.Layered(...)` builder only assembles plain text with a fixed order and stable formatting: `Base`, optional `[Run metadata]` block, then non-empty sections joined by blank lines. This keeps the kernel small while making the default prompt story materially more useful.

**Tech Stack:** Go 1.24, standard library, existing `pkg/agentcore` runtime contracts, `go test`

---

## File Structure

- Create: `pkg/agentcore/prompt/layered.go`
  Responsibility: define `MetadataConfig`, `Config`, `Layered(...)`, and the private builder implementation
- Create: `pkg/agentcore/prompt/layered_test.go`
  Responsibility: lock output ordering, metadata formatting, empty-value behavior, and `StaticBuilder` compatibility
- Modify: `pkg/agentcore/README.md`
  Responsibility: mention first-party prompt builders and when to use `Layered(...)`
- Modify: `pkg/agentcore/docs/getting-started.md`
  Responsibility: show prompt customization starting from `StaticBuilder` and `Layered(...)`
- Modify: `pkg/agentcore/docs/api.md`
  Responsibility: document the prompt builder entry points and `Layered(...)` config
- Modify: `examples/agentcore/README.md`
  Responsibility: list the new prompt example
- Create: `examples/agentcore/prompt/main.go`
  Responsibility: demonstrate `prompt.Layered(...)` with base prompt, metadata, and sections

---

### Task 1: Write Failing Tests for Prompt Layering

**Files:**
- Create: `pkg/agentcore/prompt/layered_test.go`

- [ ] **Step 1: Add failing tests for base and sections composition**

```go
func TestLayered_BaseOnly(t *testing.T) {
    builder := prompt.Layered(prompt.Config{Base: "You are helpful."})
    got, err := builder.BuildSystemPrompt(context.Background(), &agentcore.RunState{})
    require.NoError(t, err)
    require.Equal(t, "You are helpful.", got)
}

func TestLayered_BaseAndSections(t *testing.T) {
    builder := prompt.Layered(prompt.Config{
        Base: "You are helpful.",
        Sections: []string{"Follow the repo rules.", "Prefer concise answers."},
    })
    got, err := builder.BuildSystemPrompt(context.Background(), &agentcore.RunState{})
    require.NoError(t, err)
    require.Equal(t, "You are helpful.\n\nFollow the repo rules.\n\nPrefer concise answers.", got)
}
```

- [ ] **Step 2: Add failing tests for empty sections and empty output**

```go
func TestLayered_SkipsEmptySections(t *testing.T) {
    builder := prompt.Layered(prompt.Config{
        Sections: []string{"", "  ", "Keep answers grounded."},
    })
    got, err := builder.BuildSystemPrompt(context.Background(), &agentcore.RunState{})
    require.NoError(t, err)
    require.Equal(t, "Keep answers grounded.", got)
}

func TestLayered_ReturnsEmptyWhenAllPartsEmpty(t *testing.T) {
    builder := prompt.Layered(prompt.Config{})
    got, err := builder.BuildSystemPrompt(context.Background(), &agentcore.RunState{})
    require.NoError(t, err)
    require.Equal(t, "", got)
}
```

- [ ] **Step 3: Add failing tests for metadata behavior**

```go
func TestLayered_DisablesMetadataByDefault(t *testing.T) {
    builder := prompt.Layered(prompt.Config{
        Base: "You are helpful.",
        Metadata: prompt.MetadataConfig{Enabled: false},
    })
    got, err := builder.BuildSystemPrompt(context.Background(), &agentcore.RunState{
        Metadata: map[string]any{"mode": "resume"},
    })
    require.NoError(t, err)
    require.Equal(t, "You are helpful.", got)
}

func TestLayered_RendersSortedMetadata(t *testing.T) {
    builder := prompt.Layered(prompt.Config{
        Metadata: prompt.MetadataConfig{Enabled: true},
    })
    got, err := builder.BuildSystemPrompt(context.Background(), &agentcore.RunState{
        Metadata: map[string]any{"project": "agentcore", "attempt": 2},
    })
    require.NoError(t, err)
    require.Equal(t, "[Run metadata]\nattempt: 2\nproject: agentcore", got)
}
```

- [ ] **Step 4: Add failing test for `StaticBuilder` compatibility**

```go
func TestStaticBuilder_RemainsUnchanged(t *testing.T) {
    got, err := prompt.StaticBuilder{Prompt: "static"}.BuildSystemPrompt(context.Background(), &agentcore.RunState{
        Metadata: map[string]any{"mode": "resume"},
    })
    require.NoError(t, err)
    require.Equal(t, "static", got)
}
```

- [ ] **Step 5: Run targeted tests to verify they fail**

Run: `go test ./pkg/agentcore/prompt -run "TestLayered|TestStaticBuilder" -v`
Expected: FAIL with missing `Layered`, `Config`, or `MetadataConfig`

---

### Task 2: Implement `prompt.Layered(...)`

**Files:**
- Create: `pkg/agentcore/prompt/layered.go`

- [ ] **Step 1: Add config types and constructor**

```go
type MetadataConfig struct {
    Enabled bool
}

type Config struct {
    Base     string
    Metadata MetadataConfig
    Sections []string
}

func Layered(cfg Config) agentcore.PromptBuilder { ... }
```

- [ ] **Step 2: Implement the private layered builder**

```go
type layeredBuilder struct {
    config Config
}
```

Behavior:
- keep the builder immutable after construction
- satisfy `agentcore.PromptBuilder`

- [ ] **Step 3: Implement fixed-order prompt assembly**

Rules:
- include `Base` first when non-empty after trim check
- include metadata block second when enabled and metadata is non-empty
- include non-empty sections in original order
- join rendered blocks with `"\n\n"`
- return `""` when nothing renders

- [ ] **Step 4: Implement metadata formatting**

Rules:
- title is exactly `[Run metadata]`
- sort metadata keys lexicographically
- render each line as `key: value`
- stringify values with `fmt.Sprint(...)`
- ignore metadata entirely when `state == nil` or `state.Metadata` is empty

- [ ] **Step 5: Run targeted tests until green**

Run: `go test ./pkg/agentcore/prompt -run "TestLayered|TestStaticBuilder" -v`
Expected: PASS

---

### Task 3: Update Public Docs and Example, Then Run Regression Tests

**Files:**
- Modify: `pkg/agentcore/README.md`
- Modify: `pkg/agentcore/docs/getting-started.md`
- Modify: `pkg/agentcore/docs/api.md`
- Modify: `examples/agentcore/README.md`
- Create: `examples/agentcore/prompt/main.go`

- [ ] **Step 1: Update package README**

Add:
- `prompt.StaticBuilder`
- `prompt.Layered(...)`
- when each builder is the right choice

- [ ] **Step 2: Update getting started and API docs**

Add:
- a short prompt customization example
- `prompt.Config`
- `prompt.MetadataConfig`
- stable output rules for metadata and sections

- [ ] **Step 3: Add a minimal runnable example**

Show:
- base prompt
- enabled run metadata
- extra sections

- [ ] **Step 4: Run regression tests**

Run: `go test ./pkg/agentcore ./pkg/agentcore/prompt ./pkg/agentcore/context ./pkg/agentcore/memory ./examples/agentcore/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add docs/superpowers/plans/2026-04-07-agentcore-prompt-layered-phase1.md pkg/agentcore/prompt/layered.go pkg/agentcore/prompt/layered_test.go pkg/agentcore/README.md pkg/agentcore/docs/getting-started.md pkg/agentcore/docs/api.md examples/agentcore/README.md examples/agentcore/prompt/main.go
git commit -m "feat(agentcore): add layered prompt builder"
```
