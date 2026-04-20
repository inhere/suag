# Capability Governance Phase A Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 Host capability governance 落第一阶段骨架：收口全局能力构建入口、定义最小 `CapabilityManager / CapabilitySet / ResolvedTurnCapabilities / ToolProvider` 对象，并让 Host intrunner 开始消费 resolved tool subset 而不是直接依赖 app 全局 registry。

**Architecture:** 本阶段严格限制范围，只实现 capability governance 的最小骨架与 tools 主链接线，不实现 MCP provider、skill loader、skill resolution。实现上保持 `agentcore` 不变，新增 `internal/capabilities/runtimecaps` 作为能力装配中枢，`internal/app` 改为构建默认 capability manager，再由 Host server/intrunner 使用 turn 级 resolved tools。

**Tech Stack:** Go 1.24, `internal/platform/tools`, `internal/app`, `internal/host`, `github.com/gookit/goutil/testutil/assert`, `go test`

---

## File Structure

本阶段建议新增/修改的核心文件如下：

- Create: `internal/capabilities/runtimecaps/types.go`
  - 定义 `CapabilityManager`、`CapabilitySet`、`ResolvedTurnCapabilities`、`ToolProvider`、`ToolRef`、`CapabilityPolicy`、`CapabilityScope`
- Create: `internal/capabilities/runtimecaps/manager.go`
  - 实现最小 capability manager、默认 capability set 生成、turn 级 resolved tools 选择
- Create: `internal/capabilities/runtimecaps/manager_test.go`
  - 覆盖 provider 注册、default set、resolved tool subset
- Modify: `internal/platform/tools/registry.go`
  - 增加 clone/filter 支撑，保证 turn 级可以拿到 registry 子集
- Modify: `internal/app/app.go`
  - 把 scattered native tool 注册收口到 capability manager 构建入口
- Modify: `internal/app/server.go`
  - Host server 构建 session runner 时传入 capability manager / default capability set
- Modify: `internal/app/server_test.go`
  - 验证 app/server 走 capability manager 主线
- Modify: `internal/host/intrunner/runner.go`
  - 新增 turn 级 resolved tools 接入点，优先使用 resolved subset
- Modify: `internal/host/intrunner/runner_test.go`
  - 补“resolved tool subset 会限制本轮 tool 暴露”的测试
- Modify: `AGENTS.md`
  - 更新进行中主线

说明：

- 本阶段先不创建 `internal/capabilities/skills`
- 本阶段先不创建 `internal/platform/tools/mcp`
- 本阶段不改 `pkg/agentcore`

---

### Task 1: RuntimeCaps Skeleton

**Files:**
- Create: `internal/capabilities/runtimecaps/types.go`
- Create: `internal/capabilities/runtimecaps/manager.go`
- Create: `internal/capabilities/runtimecaps/manager_test.go`

- [x] **Step 1: 先写 provider 注册与 default capability set 的失败测试**

测试目标：

- 能注册一个 `ToolProvider`
- manager 能生成 default `CapabilitySet`
- set 中能保留 provider/source/tool refs

建议测试名：

- `TestCapabilityManager_BuildDefaultSetIncludesRegisteredToolRefs`

建议测试骨架：

```go
func TestCapabilityManager_BuildDefaultSetIncludesRegisteredToolRefs(t *testing.T) {
	t.Parallel()

	manager := runtimecaps.NewManager()
	provider := runtimecaps.NewStaticToolProvider("native", []runtimecaps.ToolRef{
		{Name: "file_read", Source: "native", ProviderID: "native"},
		{Name: "shell_exec", Source: "native", ProviderID: "native"},
	})
	manager.MustRegisterToolProvider(provider)

	set := manager.BuildDefaultCapabilitySet(runtimecaps.BuildSetInput{})

	assert.Eq(t, 2, len(set.Tools))
	assert.Eq(t, "file_read", set.Tools[0].Name)
	assert.Eq(t, "native", set.Tools[0].Source)
}
```

- [x] **Step 2: 运行测试确认失败**

Run: `go test ./internal/capabilities/runtimecaps -run TestCapabilityManager_BuildDefaultSetIncludesRegisteredToolRefs -v`

Expected:

- FAIL，报 `runtimecaps` 包或相关类型/函数不存在

- [x] **Step 3: 写最小 runtimecaps 类型与 manager 实现**

实现要求：

- `ToolProvider` 先只支持静态 tool refs
- `CapabilitySet` 先只覆盖 tools/policy/scope 的最小字段
- `ResolvedTurnCapabilities` 先至少包含：
  - `ToolRefs`
  - `ToolNames`
- manager 先支持：
  - 注册 provider
  - 构建 default set
  - 从 set 解析出 turn 级 resolved capability

建议最小类型骨架：

```go
type ToolRef struct {
	Name       string
	Source     string
	ProviderID string
}

type ToolProvider interface {
	ID() string
	ListToolRefs(ctx context.Context) ([]ToolRef, error)
}

type CapabilitySet struct {
	Tools    []ToolRef
	Policies CapabilityPolicy
	Scope    CapabilityScope
}

type ResolvedTurnCapabilities struct {
	ToolRefs  []ToolRef
	ToolNames []string
}
```

- [x] **Step 4: 运行 package tests 确认通过**

Run: `go test ./internal/capabilities/runtimecaps -v`

Expected:

- PASS

- [x] **Step 5: Commit**

```bash
git add internal/capabilities/runtimecaps/types.go internal/capabilities/runtimecaps/manager.go internal/capabilities/runtimecaps/manager_test.go
git commit -m "feat: add runtime capability manager skeleton"
```

---

### Task 2: Registry Subset Support

**Files:**
- Modify: `internal/platform/tools/registry.go`
- Modify: `internal/platform/tools/registry_test.go`

- [x] **Step 1: 先写 registry 子集过滤的失败测试**

测试目标：

- 现有 registry 能按 tool names 产出一个子集 registry
- 原 registry 不被修改
- 子集 registry 仍能正常 `ListTools / ExecuteTool`

建议测试名：

- `TestRegistry_SubsetKeepsOnlySelectedTools`

建议测试骨架：

```go
func TestRegistry_SubsetKeepsOnlySelectedTools(t *testing.T) {
	t.Parallel()

	reg := tools.NewRegistry()
	reg.MustRegister(newStubTool("file_read"))
	reg.MustRegister(newStubTool("shell_exec"))

	subset := reg.Subset([]string{"file_read"})

	schemas, err := subset.ListTools(context.Background())
	assert.NoErr(t, err)
	assert.Eq(t, 1, len(schemas))
	assert.Eq(t, "file_read", schemas[0].Name)

	_, err = subset.Get("shell_exec")
	assert.Err(t, err)
}
```

- [x] **Step 2: 运行测试确认失败**

Run: `go test ./internal/platform/tools -run TestRegistry_SubsetKeepsOnlySelectedTools -v`

Expected:

- FAIL，报 `Subset` 不存在

- [x] **Step 3: 写最小实现**

实现要求：

- `Subset(names []string) *Registry`
- 输出新 registry，不改原对象
- 未命中的 tool 不进入子集
- 名称顺序不影响最终行为

- [x] **Step 4: 运行 tests 确认通过**

Run: `go test ./internal/platform/tools -v`

Expected:

- PASS

- [x] **Step 5: Commit**

```bash
git add internal/platform/tools/registry.go internal/platform/tools/registry_test.go
git commit -m "feat: add tool registry subset filtering"
```

---

### Task 3: App Capability Builder

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/server_test.go`
- Create: `internal/app/capabilities.go`
- Create: `internal/app/capabilities_test.go`

- [x] **Step 1: 先写 app 默认 capability manager 构建的失败测试**

测试目标：

- app 层不再直接 scattered 注册 native tools
- 能通过统一 builder 构建 default capability manager
- default capability set 至少包含当前原生 file/shell tools

建议测试名：

- `TestBuildDefaultCapabilityManager_RegistersNativeTools`

建议测试骨架：

```go
func TestBuildDefaultCapabilityManager_RegistersNativeTools(t *testing.T) {
	t.Parallel()

	manager, err := buildDefaultCapabilityManager("D:/tmp/workspace", "")
	assert.NoErr(t, err)

	set := manager.BuildDefaultCapabilitySet(runtimecaps.BuildSetInput{})
	assert.True(t, containsToolRef(set.Tools, "file_read"))
	assert.True(t, containsToolRef(set.Tools, "shell_exec"))
}
```

- [x] **Step 2: 运行测试确认失败**

Run: `go test ./internal/app -run TestBuildDefaultCapabilityManager_RegistersNativeTools -v`

Expected:

- FAIL，builder 不存在

- [x] **Step 3: 实现 app capability builder 并替换 scattered 注册**

实现要求：

- 新建 `internal/app/capabilities.go`
- `app.New()` 通过 builder 构建：
  - global capability manager
  - default tool registry（兼容现有 orchestrator/team）
- 当前先只注册 native tools
- 保持现有 app 行为不回归

- [x] **Step 4: 运行 app tests 确认通过**

Run: `go test ./internal/app -v`

Expected:

- PASS

- [x] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/capabilities.go internal/app/capabilities_test.go internal/app/server_test.go
git commit -m "refactor: build app tools through capability manager"
```

---

### Task 4: Host Intrunner Consumes Resolved Tool Subset

**Files:**
- Modify: `internal/host/intrunner/runner.go`
- Modify: `internal/host/intrunner/runner_test.go`
- Modify: `internal/app/server.go`

- [x] **Step 1: 先写 intrunner 消费 resolved tool subset 的失败测试**

测试目标：

- 本轮 resolved capabilities 只暴露允许的 tool subset
- 即使全局 registry 里还有其他 tools，也不会暴露给本轮 runtime

建议测试名：

- `TestRunner_RunUsesResolvedToolSubset`

建议测试骨架：

```go
func TestRunner_RunUsesResolvedToolSubset(t *testing.T) {
	t.Parallel()

	registry := tools.NewRegistry()
	registry.MustRegister(&hostEchoTool{})
	registry.MustRegister(&hostAnotherTool{})

	runner, err := New(Config{
		Provider: llm.NewMockProvider("mock").QueueText("done"),
		Registry: registry,
		Tracer:   tracing.NewInMemoryTracer(nil),
		RunRoot:  t.TempDir(),
	})
	assert.NoErr(t, err)

	result, err := runner.Run(context.Background(), Request{
		SessionID:   "sess_subset",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "do something",
		Context: map[string]any{
			"resolved_tool_names": []string{"echo"},
		},
	})
	assert.NoErr(t, err)
	_ = result

	calls := runnerProviderCalls(...)
	assert.True(t, toolSchemaListed(calls[0].Tools, "echo"))
	assert.False(t, toolSchemaListed(calls[0].Tools, "another"))
}
```

- [x] **Step 2: 运行测试确认失败**

Run: `go test ./internal/host/intrunner -run TestRunner_RunUsesResolvedToolSubset -v`

Expected:

- FAIL，当前 intrunner 仍直接暴露全局 registry

- [x] **Step 3: 实现 intrunner resolved tool subset 接线**

实现要求：

- `session.RuntimeRequest.Context` 先承载最小 `resolved_tool_names`
- intrunner 从 context 读取 turn 级 resolved tools
- 如果有 resolved subset，则用 registry 子集创建本轮 agent
- 如果没有，则保持当前默认全量 registry 行为
- `internal/app/server.go` 先把 default resolved tools 以最小方式接进去

- [x] **Step 4: 运行 host tests 确认通过**

Run:

- `go test ./internal/host/intrunner -v`
- `go test ./internal/app ./internal/host/gateway`

Expected:

- PASS

- [x] **Step 5: Commit**

```bash
git add internal/host/intrunner/runner.go internal/host/intrunner/runner_test.go internal/app/server.go
git commit -m "feat: let host intrunner consume resolved tool subsets"
```

---

### Task 5: Docs And Progress Sync

**Files:**
- Modify: `AGENTS.md`
- Modify: `docs/usage/agentcore-internal-integration.md`
- Modify: `docs/usage/web-host-local-dev.md`
- Modify: `docs/superpowers/specs/2026-04-20-host-capability-governance-design.md`

- [x] **Step 1: 同步 capability governance Phase A 落地状态**

至少补充：

- app 已有统一 capability builder
- intrunner 已支持 resolved tool subset
- 本阶段仍未接 MCP provider / skills runtime

- [x] **Step 2: 跑回归测试**

Run:

- `go test ./internal/capabilities/runtimecaps -v`
- `go test ./internal/platform/tools -v`
- `go test ./internal/app -v`
- `go test ./internal/host/intrunner ./internal/host/gateway`

Expected:

- 全部 PASS

- [x] **Step 3: Commit**

```bash
git add AGENTS.md docs/usage/agentcore-internal-integration.md docs/usage/web-host-local-dev.md docs/superpowers/specs/2026-04-20-host-capability-governance-design.md
git commit -m "docs: record capability governance phase a baseline"
```

---

## Self-Review

### Spec coverage

- `CapabilityManager / CapabilitySet / ResolvedTurnCapabilities`：Task 1
- `ToolProvider` 骨架：Task 1
- app 全局注册收口：Task 3
- Host/runtime 消费 resolved tool subset：Task 4
- MCP provider / skills loader：明确 defer 到后续阶段，未纳入本计划

### Placeholder scan

本计划没有使用 `TBD` / `TODO` / “implement later” 作为任务内容；所有 deferred 内容都已明确标注“不在本阶段”。

### Type consistency

计划内统一使用：

- `CapabilityManager`
- `CapabilitySet`
- `ResolvedTurnCapabilities`
- `ToolProvider`
- `ToolRef`

未混用其他命名。
