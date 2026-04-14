# Agentcore LLM Contract Alignment Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `pkg/agentcore/api` the primary LLM runtime contract by patching missing provider extensions, aligning `internal/platform/llm` to implement it directly, and evolving the top-level LLM config from vendor-first to `api_type`-first.

**Architecture:** Keep `pkg/agentcore` as the kernel contract owner, add only the minimal contract additions needed for first-party provider support, and refactor `internal/platform/llm` so OpenAI-compatible and Anthropic providers implement the public agentcore provider interfaces directly. Preserve migration safety by keeping `internal/acadapters/internalllm` temporarily compilable until all first-party call sites move, then leave adapter cleanup for a later phase.

**Tech Stack:** Go 1.24, existing `pkg/agentcore/api`, `internal/platform/llm`, `cmd/axon`, `gookit/config`, `go test`

---

## File Structure

### Contract updates in `pkg/agentcore/api`

- Modify: `pkg/agentcore/api/provider.go`
  Responsibility: add optional embedding extension contract and clarify first-party provider expectations without bloating the base provider interface
- Modify: `pkg/agentcore/api/errors.go`
  Responsibility: add any provider-related validation or error helpers needed by the new extension surface
- Create: `pkg/agentcore/api/provider_test.go`
  Responsibility: contract tests for new embedding/provider extension behavior because this package currently has no dedicated provider contract test file

### First-party LLM implementation alignment

- Modify: `internal/platform/llm/provider.go`
  Responsibility: redefine the package-level contract around `pkg/agentcore/api` instead of maintaining a parallel request/response model
- Modify: `internal/platform/llm/openai.go`
  Responsibility: implement `agentcore/api.Provider`, `StreamingProvider`, and embedding extension for OpenAI-compatible backends
- Modify: `internal/platform/llm/claude.go`
  Responsibility: implement `agentcore/api.Provider` and optionally `StreamingProvider`, while explicitly not implementing embedding
- Modify: `internal/platform/llm/deepseek.go`
  Responsibility: stay as an OpenAI-compatible specialization while exposing the aligned contract
- Modify: `internal/platform/llm/mock.go`
  Responsibility: keep the test/mock provider compatible with the aligned contract so internal tests still have a first-party fake
- Modify: `internal/platform/llm/mock_test.go`
  Responsibility: update expectations around the new contract shape

### Migration bridge and call sites

- Modify: `internal/acadapters/internalllm/provider.go`
  Responsibility: reduce to a compatibility shim or deprecation wrapper while first-party call sites transition
- Modify: `internal/acadapters/integration_runtime_test.go`
  Responsibility: keep the end-to-end adapter regression test passing while the migration bridge is thinned down
- Modify: `internal/agent/agentcore_runner.go`
  Responsibility: stop wrapping internal providers through `internalllm` once `internal/platform/llm` implements the public contract directly
- Modify: `internal/team/manager.go`
  Responsibility: verify that team-created agents still receive the aligned first-party provider cleanly and only change this file if constructor signatures actually shift

### Config evolution

- Modify: `cmd/axon/main.go`
  Responsibility: replace `llm.provider`-first assembly with `llm.api_type + llm.provider_name`, keep backward-compatible fallback during migration, and centralize defaulting logic
- Modify: `README.md`
  Responsibility: correct the outdated adapter path references and mention the new LLM config shape only if the root README is already the right place for this runtime wiring note
- Modify or Create: `docs/usage/llm-config-v0.md`
  Responsibility: document the new `llm.api_type` / `llm.provider_name` shape without depending on local checked-in config changes
- Modify: `docs/superpowers/specs/2026-04-14-agentcore-platform-contract-alignment-design.md`
  Responsibility: mark Phase 1 implementation linkage once the plan lands

---

### Task 1: Patch `agentcore/api` Provider Contract

**Files:**
- Modify: `pkg/agentcore/api/provider.go`
- Modify: `pkg/agentcore/api/errors.go`
- Create: `pkg/agentcore/api/provider_test.go`

- [ ] **Step 1: Write the failing contract tests**

```go
func TestEmbeddingProvider_IsOptionalExtension(t *testing.T) {
    var _ Provider = (*fakeProvider)(nil)
    var _ EmbeddingProvider = (*fakeEmbeddingProvider)(nil)
}

func TestGenerateResultValidate_AcceptsEmbeddingIndependentProviders(t *testing.T) {
    result := &GenerateResult{
        Content:      "ok",
        FinishReason: FinishFinal,
    }
    require.NoError(t, result.Validate())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/agentcore/api -run "TestEmbeddingProvider_IsOptionalExtension|TestGenerateResultValidate_AcceptsEmbeddingIndependentProviders" -v`
Expected: FAIL with missing `EmbeddingProvider` type or compile errors

- [ ] **Step 3: Add the minimal provider extension contract**

```go
type EmbeddingProvider interface {
    Provider
    Embed(ctx context.Context, text string) ([]float32, error)
}
```

Also keep the base `Provider` small and do not fold embedding into it.

- [ ] **Step 4: Add any minimal provider error helpers required by the new contract**

If no new helper is actually needed, keep `errors.go` unchanged and document that decision in a short code comment or test note.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./pkg/agentcore/api -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add pkg/agentcore/api/provider.go pkg/agentcore/api/errors.go pkg/agentcore/api/provider_test.go
git commit -m "feat: extend agentcore provider contract for embeddings"
```

---

### Task 2: Align `internal/platform/llm` Around `agentcore/api`

**Files:**
- Modify: `internal/platform/llm/provider.go`
- Modify: `internal/platform/llm/openai.go`
- Modify: `internal/platform/llm/claude.go`
- Modify: `internal/platform/llm/deepseek.go`
- Modify: `internal/platform/llm/mock.go`
- Modify: `internal/platform/llm/mock_test.go`

- [ ] **Step 1: Write the failing first-party provider compatibility tests**

```go
func TestOpenAIProvider_ImplementsAgentcoreProviderContracts(t *testing.T) {
    var _ agentcoreapi.Provider = NewOpenAIProvider(OpenAIConfig{})
    var _ agentcoreapi.StreamingProvider = NewOpenAIProvider(OpenAIConfig{})
    var _ agentcoreapi.EmbeddingProvider = NewOpenAIProvider(OpenAIConfig{})
}

func TestClaudeProvider_DoesNotRequireEmbeddingSupport(t *testing.T) {
    var _ agentcoreapi.Provider = NewClaudeProvider(ClaudeConfig{})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/platform/llm -run "TestOpenAIProvider_ImplementsAgentcoreProviderContracts|TestClaudeProvider_DoesNotRequireEmbeddingSupport" -v`
Expected: FAIL with interface mismatch or missing methods

- [ ] **Step 3: Refactor `internal/platform/llm/provider.go` to use `agentcore/api` request and response types**

Keep the package focused on first-party implementation logic. Remove or deprecate duplicate request/response structs once first-party providers use public `GenerateRequest` / `GenerateResult`.

- [ ] **Step 4: Update OpenAI-compatible providers to implement the public contract directly**

OpenAI-compatible providers should:

- implement `Generate`
- implement `Stream`
- implement `Embed`
- preserve current retry/default behavior
- keep provider-specific config local to this package

- [ ] **Step 5: Update Claude provider to implement the public contract directly**

Claude should:

- implement `Generate`
- optionally implement `Stream`
- return a clear error for embedding only if `Embed` is retained through the extension interface path
- avoid pretending to support embedding when it does not

- [ ] **Step 6: Update mock provider and related tests**

Make sure internal tests still have a deterministic first-party fake aligned with the new contract.

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/platform/llm -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/platform/llm
git commit -m "refactor: align platform llm with agentcore provider api"
```

---

### Task 3: Move Agentcore Runner Off `internalllm`

**Files:**
- Modify: `internal/acadapters/internalllm/provider.go`
- Modify: `internal/acadapters/integration_runtime_test.go`
- Modify: `internal/agent/agentcore_runner.go`
- Test: `internal/agent/agentcore_runner_test.go`
- Test: `internal/agent/runner_factory_test.go`

- [ ] **Step 1: Write the failing regression test for direct first-party provider use**

```go
func TestBuildAgentcoreConfig_UsesPlatformProviderDirectly(t *testing.T) {
    a := newTestAgentWithMockLLM(t)

    cfg := BuildAgentcoreConfig(a, AgentcoreRunnerConfig{})

    _, ok := cfg.Provider.(agentcoreapi.Provider)
    require.True(t, ok)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent -run TestBuildAgentcoreConfig_UsesPlatformProviderDirectly -v`
Expected: FAIL because provider still comes through `internalllm`

- [ ] **Step 3: Switch `AgentcoreRunner` config assembly to use `internal/platform/llm` directly**

Update `BuildAgentcoreConfig()` so it no longer depends on `internalllm.NewProvider(...)`.

- [ ] **Step 4: Reduce `internalllm` to compatibility-only status**

Do not expand it. Either:

- turn it into a deprecated thin wrapper over the aligned provider implementation, or
- leave the smallest compile-safe shim needed for any remaining migration call sites

- [ ] **Step 5: Run affected tests**

Run: `go test ./internal/agent ./internal/acadapters/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/agent/agentcore_runner.go internal/agent/agentcore_runner_test.go internal/agent/runner_factory_test.go internal/acadapters/internalllm/provider.go internal/acadapters/integration_runtime_test.go
git commit -m "refactor: remove internalllm from agentcore runner path"
```

---

### Task 4: Evolve Top-Level LLM Config to `api_type`

**Files:**
- Modify: `cmd/axon/main.go`
- Modify: `README.md`
- Modify or Create: `docs/usage/llm-config-v0.md`
- Modify: `docs/superpowers/specs/2026-04-14-agentcore-platform-contract-alignment-design.md`
- Test: `cmd/axon/main_test.go` or a new focused config assembly test file if none exists yet

- [ ] **Step 1: Write the failing config assembly tests**

```go
func TestBuildLLMProvider_OpenAIAPITypeWithDeepSeekProviderName(t *testing.T) {
    config.Reset()
    config.Set("llm.api_type", "openai")
    config.Set("llm.provider_name", "deepseek")
    config.Set("llm.model", "deepseek-chat")

    provider, err := buildLLMProvider()
    require.NoError(t, err)
    require.Equal(t, "deepseek", provider.Name())
}

func TestBuildLLMProvider_AnthropicAPITypeBuildsClaudeProvider(t *testing.T) {
    config.Reset()
    config.Set("llm.api_type", "anthropic")
    config.Set("llm.provider_name", "claude")
    config.Set("llm.model", "claude-sonnet-4-6")

    provider, err := buildLLMProvider()
    require.NoError(t, err)
    require.Equal(t, "claude", provider.Name())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/axon -run "TestBuildLLMProvider_OpenAIAPITypeWithDeepSeekProviderName|TestBuildLLMProvider_AnthropicAPITypeBuildsClaudeProvider" -v`
Expected: FAIL because config still keys off `llm.provider`

- [ ] **Step 3: Refactor `buildLLMProvider()` to prefer `llm.api_type` and `llm.provider_name`**

Migration rules:

- prefer `llm.api_type`
- fallback to legacy `llm.provider` if `api_type` is absent
- keep `base_url`, `api_key`, and `model` explicit
- normalize `anthropic` spelling and avoid introducing `authropic`

- [ ] **Step 4: Update config-facing docs**

Document the new config shape in:

- `docs/usage/llm-config-v0.md` as the primary migration note
- `README.md` only if it already documents runtime/provider setup
- the design spec, adding a note that Phase 1 now has an implementation plan

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./cmd/axon -v`
Expected: PASS

- [ ] **Step 6: Run broader verification**

Run: `go test ./pkg/agentcore/api ./internal/platform/llm ./internal/agent ./cmd/axon -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add cmd/axon/main.go cmd/axon/main_test.go README.md docs/usage/llm-config-v0.md docs/superpowers/specs/2026-04-14-agentcore-platform-contract-alignment-design.md
git commit -m "feat: add api-type based llm provider configuration"
```

---

## Notes

- Do not start `tools`, `memory`, or Host internal `session runner` work inside this phase.
- Do not delete `internal/acadapters/internalllm` unless all compile paths have moved and a follow-up cleanup step is explicitly added.
- Keep `agentcore/api` contract additions minimal; avoid sliding provider-specific policy into the kernel.
- Do not modify the user's local `config/config.yaml` as part of this phase unless a separate explicit config migration task is requested.
