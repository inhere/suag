# Agentcore Tools And Memory Alignment Phase 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `internal/platform/tools` and `internal/platform/memory` become first-party `pkg/agentcore/api` implementations so the next Host internal runner work can attach to stable runtime contracts instead of migration adapters.

**Architecture:** Phase 2 continues the Phase 1 strangler strategy. `tools` should converge toward a direct `api.Tool` / `api.ToolRegistry` implementation with richer `ToolResult` output, while `memory` should adopt a dual-layer shape inside one module: keep `memory.Store` as the richer backend and add a runtime-facing implementation that directly satisfies `api.Memory`. `internal/acadapters/internaltools` and `internal/acadapters/internalmemory` remain compile-safe only until the main runtime path stops depending on them.

**Tech Stack:** Go 1.24, `pkg/agentcore/api`, `internal/platform/tools`, `internal/platform/memory`, `internal/agent`, `internal/acadapters`, `go test`

---

## File Structure

### Tools contract convergence

- Modify: `internal/platform/tools/registry.go`
  Responsibility: move the package contract from string-only execution toward direct `api.Tool` / `api.ToolRegistry` compatibility while preserving the current registry ergonomics
- Modify: `internal/platform/tools/registry_test.go`
  Responsibility: verify richer tool execution results and stable schema behavior on the first-party registry
- Modify: `internal/platform/tools/native/file.go`
  Responsibility: update representative built-in tools to return richer runtime results instead of plain strings
- Modify: `internal/platform/tools/native/code_exec.go`
  Responsibility: preserve shell/code execution semantics while upgrading return shape to `ToolResult`
- Modify: `internal/platform/tools/native/search.go`
  Responsibility: preserve search behavior while upgrading return shape to `ToolResult`
- Modify: `internal/platform/tools/skills/runner.go`
  Responsibility: keep skill-backed tools aligned with the richer tool result contract if they currently return plain text only
- Modify: `internal/acadapters/internaltools/registry.go`
  Responsibility: reduce to a compatibility shim once the first-party registry directly satisfies `api.ToolRegistry`
- Modify: `internal/acadapters/internaltools/registry_test.go`
  Responsibility: verify the shim still behaves correctly during the migration window

### Memory module dual-layer convergence

- Modify: `internal/platform/memory/store.go`
  Responsibility: keep the richer backend `Store` contract explicit and document its separation from `api.Memory`
- Modify: `internal/platform/memory/inmem.go`
  Responsibility: keep in-memory backend behavior while preparing it to feed a direct runtime-memory wrapper in the same module
- Modify: `internal/platform/memory/sqlite.go`
  Responsibility: preserve persistent memory backend behavior and keep it compatible with the new runtime-memory wrapper
- Create: `internal/platform/memory/runtime.go`
  Responsibility: introduce the runtime-facing `api.Memory` implementation inside the memory module
- Create or Modify: `internal/platform/memory/runtime_test.go`
  Responsibility: verify hydrate, observe, and build-context behavior directly on the first-party runtime-memory implementation
- Modify: `internal/acadapters/internalmemory/memory.go`
  Responsibility: reduce to a deprecated thin wrapper over `internal/platform/memory/runtime.go` or remove duplication if no longer needed
- Modify: `internal/acadapters/internalmemory/memory_test.go`
  Responsibility: preserve migration coverage while the adapter still exists

### Main runtime call sites

- Modify: `internal/agent/agentcore_runner.go`
  Responsibility: stop using `internaltools` / `internalmemory` adapters on the main agentcore path once first-party implementations exist
- Modify: `internal/agent/agentcore_runner_test.go`
  Responsibility: verify `BuildAgentcoreConfig()` now uses platform-native tools and memory implementations directly
- Modify: `internal/acadapters/integration_runtime_test.go`
  Responsibility: keep one end-to-end migration test green while adapters shrink down to compatibility wrappers
- Modify: `docs/usage/agentcore-internal-integration.md`
  Responsibility: update the repo-internal integration note to reflect that tools and memory have moved from bridge-first to first-party runtime implementations
- Modify: `docs/superpowers/specs/2026-04-14-agentcore-platform-contract-alignment-design.md`
  Responsibility: mark Phase 2 implementation linkage and state which adapters are still transitional after this phase

---

## Existing Reuse And Compatibility Boundary

### What already exists

- `internal/platform/tools/registry.go`
  Already owns registration, lookup, and schema listing, so Phase 2 should evolve this file rather than create a second registry abstraction.
- `internal/acadapters/internaltools/registry.go`
  Already shows the current schema and execution mapping from platform tools into `api.ToolRegistry`; Phase 2 should fold this knowledge back into the first-party registry.
- `internal/platform/memory/inmem.go`
  Already owns the short-term context window and summary behavior, so the new runtime-memory implementation should wrap or reuse it instead of re-implementing the same logic.
- `internal/platform/memory/sqlite.go`
  Already persists backend memory state and should stay a richer backend, not be flattened into `api.Memory`.
- `internal/acadapters/internalmemory/memory.go`
  Already demonstrates hydrate/observe/build-context semantics against `api.Memory`; Phase 2 should move this behavior into the memory module itself.
- `internal/agent/agentcore_runner.go`
  Already provides the main agentcore seam, so it is the correct first call site to move off `internaltools` / `internalmemory`.

### Not in scope

- Reworking Host internal `session runner` in this phase
  Rationale: the whole point of Phase 2 is to stabilize tools and memory before Host runtime wiring depends on them.
- Removing `ReactRunner`
  Rationale: this phase is about contract convergence for the agentcore path, not legacy runner deletion.
- Designing full MCP / skills / external tool product flows
  Rationale: Phase 2 should preserve existing capability hooks but not expand product scope beyond runtime contract alignment.
- Replacing `memory.Store` with `api.Memory`
  Rationale: the richer memory backend must remain intact; only the runtime-facing layer should align directly to `api.Memory`.

---

### Task 0: Lock Tool And Memory Compatibility Fences

**Files:**
- Modify: `internal/platform/tools/registry_test.go`
- Modify: `internal/acadapters/internaltools/registry_test.go`
- Modify: `internal/platform/memory/inmem_test.go`
- Modify: `internal/acadapters/internalmemory/memory_test.go`
- Modify: `internal/agent/agentcore_runner_test.go`

- [ ] **Step 1: Add failing regression tests for the current tool and memory behavior that must survive the refactor**

Cover at least:

- first-party tool registry can still list stable schemas and execute registered tools
- adapter-backed tool registry still preserves schema metadata and call metadata
- in-memory memory store still returns stable context windows and append behavior
- runtime memory hydration still detects divergent transcript prefixes and appends missing tails
- `BuildAgentcoreConfig()` still produces working `Tools` and `Memory` wiring on the agentcore path

- [ ] **Step 2: Run the focused compatibility tests and verify the current baseline passes**

Run: `go test ./internal/platform/tools ./internal/acadapters/internaltools ./internal/platform/memory ./internal/acadapters/internalmemory ./internal/agent -run "Registry|Memory|BuildAgentcoreConfig" -v`
Expected: PASS on the pre-refactor baseline

- [ ] **Step 3: Treat these tests as the no-regression fence for the rest of Phase 2**

Do not start modifying the first-party tool or memory contracts until this fence exists.

- [ ] **Step 4: Commit**

```bash
git add internal/platform/tools/registry_test.go internal/acadapters/internaltools/registry_test.go internal/platform/memory/inmem_test.go internal/acadapters/internalmemory/memory_test.go internal/agent/agentcore_runner_test.go
git commit -m "test: lock tools and memory compatibility boundary"
```

---

### Task 1: Align `internal/platform/tools` To `api.ToolRegistry`

**Files:**
- Modify: `internal/platform/tools/registry.go`
- Modify: `internal/platform/tools/registry_test.go`
- Modify: `internal/platform/tools/native/file.go`
- Modify: `internal/platform/tools/native/code_exec.go`
- Modify: `internal/platform/tools/native/search.go`
- Modify: `internal/platform/tools/skills/runner.go`

- [ ] **Step 1: Write the failing first-party tool contract tests**

Cover at least:

- registry satisfies `api.ToolRegistry`
- registered tool execution returns `api.ToolResult` with stable `CallID` / `Name` / `Content`
- representative native tools preserve existing user-visible output while filling the richer result shape

- [ ] **Step 2: Run tests to verify they fail for the expected reason**

Run: `go test ./internal/platform/tools -run "Tool|Registry" -v`
Expected: FAIL with interface mismatch or outdated string-only execution assumptions

- [ ] **Step 3: Refactor the platform tool contract toward direct `api.Tool` compatibility**

Keep the registry surface boring and explicit:

- preserve `Kind`
- keep stable registration and lookup ergonomics
- upgrade execution from `string` to `*api.ToolResult`
- keep schema authority in one place

- [ ] **Step 4: Update representative built-in tools**

Start with native file, search, and code execution tools so the richer `ToolResult` shape is exercised by real tools, not only by test doubles.

- [ ] **Step 5: Run targeted tests and then the broader callers**

Run: `go test ./internal/platform/tools ./internal/agent ./internal/ops/cron -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/platform/tools internal/agent/runner.go internal/ops/cron
git commit -m "refactor: align platform tools with agentcore tool registry"
```

---

### Task 2: Shrink `internaltools` Into A Compatibility Shim

**Files:**
- Modify: `internal/acadapters/internaltools/registry.go`
- Modify: `internal/acadapters/internaltools/registry_test.go`
- Modify: `internal/acadapters/integration_runtime_test.go`

- [ ] **Step 1: Write or update failing migration tests that describe the intended shim behavior**

The adapter should either:

- delegate to the first-party registry without re-implementing schema/result mapping, or
- be reduced to the smallest compatibility wrapper still needed by older call sites

- [ ] **Step 2: Run the adapter-focused tests and verify they fail before the shim is updated**

Run: `go test ./internal/acadapters/internaltools ./internal/acadapters -run "InternalTools|Runtime" -v`
Expected: FAIL because the adapter still assumes old platform tool shapes

- [ ] **Step 3: Reduce adapter logic to the minimum compile-safe wrapper**

Do not add new behavior here. The adapter should become thinner after Phase 2, not thicker.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/acadapters/internaltools ./internal/acadapters -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/acadapters/internaltools/registry.go internal/acadapters/internaltools/registry_test.go internal/acadapters/integration_runtime_test.go
git commit -m "refactor: shrink internaltools adapter after platform convergence"
```

---

### Task 3: Introduce First-Party Runtime Memory Inside `internal/platform/memory`

**Files:**
- Modify: `internal/platform/memory/store.go`
- Modify: `internal/platform/memory/inmem.go`
- Modify: `internal/platform/memory/sqlite.go`
- Create: `internal/platform/memory/runtime.go`
- Create or Modify: `internal/platform/memory/runtime_test.go`

- [ ] **Step 1: Write the failing runtime-memory tests**

Cover at least:

- runtime memory satisfies `api.Memory`
- `Hydrate()` detects diverged transcript prefixes and appends missing transcript tail
- `ObserveMessages()` compacts tool-result messages consistently
- `BuildContext()` reuses the backend store’s summarization/window behavior

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/platform/memory -run "Runtime|Hydrate|Observe|BuildContext" -v`
Expected: FAIL because the first-party runtime-memory implementation does not exist yet

- [ ] **Step 3: Add `runtime.go` and keep the dual-layer structure explicit**

Principles:

- `Store` remains the richer backend
- runtime-facing `api.Memory` lives in the same module
- avoid turning backend-specific persistence code into runtime contract code

- [ ] **Step 4: Run tests to verify the first-party runtime-memory implementation passes**

Run: `go test ./internal/platform/memory -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/platform/memory/store.go internal/platform/memory/inmem.go internal/platform/memory/sqlite.go internal/platform/memory/runtime.go internal/platform/memory/runtime_test.go
git commit -m "feat: add first-party runtime memory in platform memory"
```

---

### Task 4: Shrink `internalmemory` Into A Thin Wrapper

**Files:**
- Modify: `internal/acadapters/internalmemory/memory.go`
- Modify: `internal/acadapters/internalmemory/memory_test.go`

- [ ] **Step 1: Write or update failing adapter tests that describe the new thin-wrapper behavior**

The adapter should now prove only that the old import path still delegates correctly to the first-party runtime-memory implementation.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/acadapters/internalmemory -v`
Expected: FAIL because the adapter still owns duplicated hydrate/observe/build-context logic

- [ ] **Step 3: Replace duplication with a thin delegation wrapper**

Do not keep two independent implementations of the runtime-memory semantics.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/acadapters/internalmemory -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/acadapters/internalmemory/memory.go internal/acadapters/internalmemory/memory_test.go
git commit -m "refactor: shrink internalmemory adapter after platform convergence"
```

---

### Task 5: Move Agentcore Main Path Off `internaltools` And `internalmemory`

**Files:**
- Modify: `internal/agent/agentcore_runner.go`
- Modify: `internal/agent/agentcore_runner_test.go`
- Modify: `docs/usage/agentcore-internal-integration.md`
- Modify: `docs/superpowers/specs/2026-04-14-agentcore-platform-contract-alignment-design.md`

- [ ] **Step 1: Write the failing regression tests for direct first-party tools and memory use**

Cover at least:

- `BuildAgentcoreConfig()` uses platform-native tool registry and runtime memory directly
- agentcore runner still preserves tool transcript persistence and memory synchronization semantics

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/agent -run "BuildAgentcoreConfig|AgentcoreRunner" -v`
Expected: FAIL because the main path still goes through `internaltools` or `internalmemory`

- [ ] **Step 3: Switch the main agentcore path to first-party platform implementations**

Keep adapters only for non-main-path compatibility and older imports.

- [ ] **Step 4: Update docs to reflect the new architecture state**

Document that:

- `platform/tools` is now the first-party `api.ToolRegistry` implementation
- `platform/memory` now contains the runtime-facing `api.Memory` implementation
- `internaltools` / `internalmemory` remain transitional

- [ ] **Step 5: Run broader verification**

Run: `go test ./internal/platform/tools ./internal/platform/memory ./internal/agent ./internal/ops/cron ./internal/team ./internal/acadapters/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/agent/agentcore_runner.go internal/agent/agentcore_runner_test.go docs/usage/agentcore-internal-integration.md docs/superpowers/specs/2026-04-14-agentcore-platform-contract-alignment-design.md
git commit -m "refactor: move agentcore tools and memory off adapters"
```

---

## Notes

- Do not start Host internal `session runner` implementation in this phase.
- Do not delete `internal/acadapters/internaltools` or `internal/acadapters/internalmemory` unless all compile paths have moved and a separate cleanup step is explicitly added.
- Keep `tools` and `memory` aligned to the same rule used in Phase 1: first-party platform package owns the runtime contract; adapters are migration-only.
- For `memory`, prefer one module with two clear layers over flattening the richer backend into `api.Memory`.
- For `tools`, make `api.ToolResult` the authority and stop growing new string-only result paths.
