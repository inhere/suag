# AGENTS for AI

- 请优先使用中文回复和响应，除非明确要求英文回复
- 如果当前任务有进度跟进文件，需要在完成后更新进度跟进文件
- 多阶段任务，需要在每个阶段完成后更新进度并提交 Git

## 正在进行的工作

> **IMPORTANT**: 需要实时更新，正在进行的工作和关键md文档链接；**已完成的需要移除**，不能一直累计，总数不能超过10条。

<!-- PROCESSING WORKS:START -->

- Axon v0 自用主线规划持续推进中，设计总纲见 `docs/v0-self-host-design.md`
- Host 会话与 Web 工作台基础闭环已打通，当前继续围绕可自用联调和状态反馈做收尾，联调说明见 `docs/usage/web-host-local-dev.md`
- `internal/host/session` 与 `internal/host/gateway` 已具备 session/turn/run 基础恢复与查询链路，且 `run_events.sequence` 已收敛为 session 级单调递增基线；当前 `DISPATCHING / EXTERNAL_RUNNING / INPUT_REQUIRED / AUTH_REQUIRED` 非终态也已开始贯通到快照与事件流
- `docs/usage/host-state-snapshot-v0.md` 与 `docs/usage/llm-config-v0.md` 已作为当前 v0 协议与配置基线文档
- Phase 1 已完成 `platform/llm -> pkg/agentcore/api` 的第一方收口，`cmd/axon` 现已补齐仅基于 `llm.api_type + llm.provider_name` 的归一化配置入口与测试覆盖
- `internal/platform/llm` 已切除迁移期 `Completion* / Complete / StreamText` 兼容面，内部调用统一转向 `agentcore Generate` contract，且继续收薄为 provider 实现层
- Phase 2 已完成 `platform/tools + platform/memory -> pkg/agentcore/api` 的第一方收口，迁移期 `internal/acadapters` 已删除，相关测试与文档已迁回第一方实现路径
- `internal/platform/tools` 已进一步改为直接返回 `agentcore/api.ToolSchema`，继续保留 `map[string]any` 作为 schema authority，不再依赖 `llm.SchemaProperty`
- 下一步继续聚焦 Host internal session runner、旧 `ReactRunner` fallback 收敛与更细的运行时能力补齐
<!-- PROCESSING WORKS:END -->

## git worktree 开发规范

- 使用 git worktree 进行开发时，在 `.worktrees` 目录下创建分支

## 项目编码规范

- 关键的方法或逻辑点需要添加注释说明
- 单个go文件最大行数不能操作1000行，超过时需要按情况进行拆分

### 单元测试编写

- 使用 `github.com/gookit/goutil/testutil/assert` 断言结果
- 同一个方法的多个用例使用 `t.Run()` 包裹

require 断言结果的写法：

```go
Require(t, assert.Eq(t, 1, res.ID))

// Standard assertion
assert.Eq(t, expected, actual)
```

## 前端项目开发

- 包管理使用 pnpm
