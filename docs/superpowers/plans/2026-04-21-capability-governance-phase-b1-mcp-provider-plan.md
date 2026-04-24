# Capability Governance Phase B1 MCP Provider Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把现有 `internal/platform/tools/mcp` 正式并入 capability governance 主链，让 app 默认能力构建可以同时装配 native tools 与可配置 MCP tools。

**Architecture:** 本阶段只做 provider 来源统一，不提前实现 skills runtime、turn-time dynamic resolution 或复杂 policy。实现上新增一个 MCP tool provider，把 `tools/mcp.Client` 暴露成 `runtimecaps.ToolProvider + tools.Registry` 可消费的来源；`internal/app` 继续作为全局 capability builder，但开始按配置加载 MCP servers，并把其 tools 与 native tools 一起并入默认 capability set。

**Tech Stack:** Go 1.24, `internal/platform/tools/mcp`, `internal/capability/runtimecaps`, `internal/app`, `internal/config`, `github.com/gookit/config/v2`, `github.com/gookit/goutil/testutil/assert`

---

## File Structure

本阶段建议新增/修改的核心文件如下：

- Create: `internal/config/mcp.go`
  - 解析 v0 MCP server 配置，返回标准化 server settings
- Create: `internal/config/mcp_test.go`
  - 覆盖 MCP 配置读取与归一化
- Create: `internal/platform/tools/mcp/provider.go`
  - 提供 `ToolProvider` 封装，负责启动 client、列出工具、注册到 registry、关闭进程
- Create: `internal/platform/tools/mcp/provider_test.go`
  - 覆盖 provider 的 tool refs 导出、registry 注册、关闭语义
- Modify: `internal/app/capabilities.go`
  - 在默认 capability builder 中装配 native provider + configured MCP providers
- Modify: `internal/app/capabilities_test.go`
  - 覆盖默认 builder 在配置 MCP server 后能合并 provider/tool refs
- Modify: `internal/app/app.go`
  - 让 `App` 持有 capability runtime 资源关闭入口
- Modify: `cmd/axon/main.go`
  - 退出时关闭 app 持有的 MCP 子进程等资源
- Modify: `config/config.yaml`
  - 增加最小 MCP 配置示例

说明：

- 本阶段先只支持 stdio MCP server
- 本阶段先不做 session/turn 级动态 capability resolution
- 本阶段先不做 skill registry / resolver
- 本阶段不改 `pkg/agentcore`

---

### Task 1: MCP Config Reader

**Files:**
- Create: `internal/config/mcp.go`
- Create: `internal/config/mcp_test.go`
- Modify: `config/config.yaml`

- [x] **Step 1: 先写 MCP 配置读取的失败测试**

测试目标：

- 未配置时返回空 server 列表
- 配置后能读取 `id / command / args / env`
- 归一化时会裁剪空白并忽略空配置项

建议测试名：

- `TestReadMCPServerSettings_ReturnsNormalizedServers`

建议测试骨架：

```go
func TestReadMCPServerSettings_ReturnsNormalizedServers(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)

	_ = gkconfig.Set("mcp.servers", []map[string]any{
		{
			"id":      " filesystem ",
			"command": " npx ",
			"args":    []string{"-y", "@modelcontextprotocol/server-filesystem", "./workspace"},
			"env": map[string]any{
				"NODE_ENV": " development ",
			},
		},
	})

	servers := ReadMCPServerSettings()

	assert.Eq(t, 1, len(servers))
	assert.Eq(t, "filesystem", servers[0].ID)
	assert.Eq(t, "npx", servers[0].Command)
	assert.Eq(t, "development", servers[0].Env["NODE_ENV"])
}
```

- [x] **Step 2: 运行测试确认失败**

Run: `go test ./internal/config -run TestReadMCPServerSettings_ReturnsNormalizedServers -v`

Expected:

- FAIL，报 `ReadMCPServerSettings` 或相关类型不存在

- [x] **Step 3: 写最小 MCP 配置实现**

实现要求：

- 新增 `MCPServerSettings`
- 配置形状先定为：

```yaml
mcp:
  servers:
    - id: filesystem
      command: npx
      args:
        - -y
        - "@modelcontextprotocol/server-filesystem"
        - ./workspace
      env:
        NODE_ENV: development
```

- 读取时做：
  - `id / command / args / env` trim
  - 忽略 `command` 为空的 server
  - `id` 为空时默认回退到 `command`

- [x] **Step 4: 运行 config tests 确认通过**

Run: `go test ./internal/config -v`

Expected:

- PASS

- [x] **Step 5: Commit**

```bash
git add internal/config/mcp.go internal/config/mcp_test.go config/config.yaml
git commit -m "feat: add mcp server config reader"
```

---

### Task 2: MCP Tool Provider

**Files:**
- Create: `internal/platform/tools/mcp/provider.go`
- Create: `internal/platform/tools/mcp/provider_test.go`

- [x] **Step 1: 先写 MCP provider 的失败测试**

测试目标：

- provider 能导出 `ToolRef`
- provider 能把 MCP tools 注册到目标 registry
- provider 关闭时能释放底层 client 资源

建议测试名：

- `TestProvider_RegisterToolsAndListToolRefs`

建议测试骨架：

```go
func TestProvider_RegisterToolsAndListToolRefs(t *testing.T) {
	registry := tools.NewRegistry()
	provider := &Provider{
		id: "mcp.filesystem",
		tools: []tools.Tool{
			newStubMCPTool("fs_read"),
			newStubMCPTool("fs_write"),
		},
	}

	err := provider.RegisterTools(registry)
	assert.NoErr(t, err)

	refs, err := provider.ListToolRefs(context.Background())
	assert.NoErr(t, err)
	assert.Eq(t, 2, len(refs))
	assert.Eq(t, "mcp", refs[0].Source)

	_, err = registry.Get("fs_read")
	assert.NoErr(t, err)
}
```

- [x] **Step 2: 运行测试确认失败**

Run: `go test ./internal/platform/tools/mcp -run TestProvider_RegisterToolsAndListToolRefs -v`

Expected:

- FAIL，报 `Provider` 或相关方法不存在

- [x] **Step 3: 实现最小 MCP provider**

实现要求：

- 新增 `Provider`
- provider 负责：
  - 持有 `ServerConfig`
  - 启动 `Client`
  - 拉取 tool list
  - 转成 `tools.Tool`
  - 导出 `runtimecaps.ToolRef`
  - 注册到 registry
  - 暴露 `Close()`
- `ToolRef.ProviderID` 建议格式：
  - `mcp.<serverID>`
- `ToolRef.Source` 固定使用：
  - `string(tools.KindMCP)`

- [x] **Step 4: 运行 MCP package tests 确认通过**

Run: `go test ./internal/platform/tools/mcp -v`

Expected:

- PASS

- [x] **Step 5: Commit**

```bash
git add internal/platform/tools/mcp/provider.go internal/platform/tools/mcp/provider_test.go
git commit -m "feat: add mcp tool provider"
```

---

### Task 3: App Default Capability Builder Integrates MCP

**Files:**
- Modify: `internal/app/capabilities.go`
- Modify: `internal/app/capabilities_test.go`
- Modify: `internal/app/app.go`
- Modify: `cmd/axon/main.go`

- [x] **Step 1: 先写 app builder 合并 MCP provider 的失败测试**

测试目标：

- 默认 capability builder 在配置 MCP server 后，会把 MCP tools 合并到默认 registry 与 capability set
- app 生命周期结束时，会关闭 capability runtime 资源

建议测试名：

- `TestBuildDefaultCapabilities_RegistersConfiguredMCPTools`

建议测试骨架：

```go
func TestBuildDefaultCapabilities_RegistersConfiguredMCPTools(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)

	_ = gkconfig.Set("mcp.servers", []map[string]any{
		{"id": "stub", "command": "stub"},
	})

	restore := swapMCPProviderFactoryForTest(func(_ context.Context, _ axconfig.MCPServerSettings) (mcpProviderRuntime, error) {
		return newStubMCPProviderRuntime("stub", []tools.Tool{
			newStubTool("mcp_echo"),
		}), nil
	})
	t.Cleanup(restore)

	built := buildDefaultCapabilities(t.TempDir())
	defer built.Close()

	assert.NotNil(t, mustGetTool(t, built.Registry, "mcp_echo"))

	set := built.Manager.BuildDefaultCapabilitySet(runtimecaps.BuildSetInput{})
	resolved := built.Manager.ResolveTurnCapabilities(set)
	assert.True(t, slices.Contains(resolved.ToolNames, "mcp_echo"))
}
```

- [x] **Step 2: 运行测试确认失败**

Run: `go test ./internal/app -run TestBuildDefaultCapabilities_RegistersConfiguredMCPTools -v`

Expected:

- FAIL，当前 builder 还未集成 MCP provider

- [x] **Step 3: 实现 app capability builder 的 MCP 接线**

实现要求：

- `buildDefaultCapabilities` 继续返回默认 registry + manager
- 同时开始持有 capability runtime closers，至少覆盖 MCP providers
- `App` 增加 `Close() error`
- `cmd/axon/main.go` 在 app 创建成功后 `defer runtimeApp.Close()`
- builder 对 MCP server 启动错误先采用：
  - fail fast
  - 避免“配置了 MCP 但实际能力静默缺失”

- [x] **Step 4: 运行 app tests 确认通过**

Run: `go test ./internal/app -v`

Expected:

- PASS

- [x] **Step 5: Commit**

```bash
git add internal/app/capabilities.go internal/app/capabilities_test.go internal/app/app.go cmd/axon/main.go
git commit -m "feat: wire mcp providers into app capabilities"
```

---

### Task 4: Docs And Progress Sync

**Files:**
- Modify: `AGENTS.md`
- Modify: `docs/usage/agentcore-internal-integration.md`
- Modify: `docs/usage/web-host-local-dev.md`
- Modify: `docs/superpowers/specs/2026-04-20-host-capability-governance-design.md`

- [x] **Step 1: 同步 Phase B1 落地状态**

至少补充：

- MCP provider 已并入 capability manager 主链
- app 默认 capability builder 已可装配 configured MCP tools
- 当前仍未接 skills runtime / turn-time dynamic resolution

- [x] **Step 2: 跑回归测试**

Run:

- `go test ./internal/config -v`
- `go test ./internal/platform/tools/mcp -v`
- `go test ./internal/app -v`
- `go test ./internal/capability/runtimecaps ./internal/host/intrunner ./internal/host/gateway`

Expected:

- 全部 PASS

- [x] **Step 3: Commit**

```bash
git add AGENTS.md docs/usage/agentcore-internal-integration.md docs/usage/web-host-local-dev.md docs/superpowers/specs/2026-04-20-host-capability-governance-design.md
git commit -m "docs: record capability governance phase b1 baseline"
```

---

## Self-Review

### Scope check

本计划只覆盖：

- MCP config
- MCP tool provider
- app 默认 capability builder 接线

没有把以下内容混进来：

- skills runtime
- session-stable capability set 扩展
- turn-time dynamic resolution
- planner / post-turn learning 联动

### Boundary check

本计划保持边界如下：

- `agentcore`
  - 不改
- `internal/platform/tools/mcp`
  - 负责 MCP tool source 封装
- `internal/capability/runtimecaps`
  - 继续只处理 provider -> tool refs -> resolved tool names
- `internal/app`
  - 负责默认 provider 装配与生命周期管理

### Risk notes

- MCP server 启动失败如果静默忽略，会造成“看起来配置了、实际能力缺失”的隐蔽问题；本计划先采用 fail-fast
- provider 生命周期如果不显式关闭，会在本地开发和测试里留下子进程；因此 app 级 `Close()` 在本阶段一并补齐
