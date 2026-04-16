# AGENTS for AI

- 请优先使用中文回复和响应，除非明确要求英文回复
- 如果当前任务有进度跟进文件，需要在完成后更新进度跟进文件
- 多阶段任务，需要在每个阶段完成后更新进度并提交 Git

## 正在进行的工作

> **IMPORTANT**: 需要实时更新，正在进行的工作和关键md文档链接；**已完成的需要移除**，不能一直累计，总数不能超过10条。

<!-- PROCESSING WORKS:START -->

- Axon v0 自用主线规划持续推进中，设计总纲见 `docs/v0-self-host-design.md`
- Host 会话与 Web 工作台基础闭环已打通，当前继续围绕可自用联调与 Host runtime 主链稳定性做收尾，联调说明见 `docs/usage/web-host-local-dev.md`
- 下一阶段开发已整理为 `docs/superpowers/plans/2026-04-16-host-runtime-stabilization-phase3.md`，后续按该计划继续推进 Host Go 主线
- Phase 3 / Task 1 已完成 `wait-state / continue_turn / stop_turn / HITL` 合同收口；当前 `continue_turn` 仅允许继续 `INPUT_REQUIRED` run，`AUTH_REQUIRED` 已切到 HITL/approval 响应路径，且 WS `command.accepted / command.error` 时序已与真实执行结果对齐
- `internal/host/session` 与 `internal/host/gateway` 已具备 session/turn/run 基础恢复与查询链路，`run_events.sequence` 已收敛为 session 级单调递增基线；当前 `DISPATCHING / EXTERNAL_RUNNING / INPUT_REQUIRED / AUTH_REQUIRED` 非终态已贯通到快照与事件流
- Phase 3 / Task 2 已完成 internal runner / ACP runner 的第一轮 Host contract 对齐；ACP `StateRunning` 现已稳定映射为 Host `EXTERNAL_RUNNING` 非终态，且 `externalTaskId` 会贯穿 runtime 事件与 AG-UI 映射
- Host internal runner 的 runtime 数据目录已收口到统一配置；默认使用 `db/agentcore/host`，避免继续散落到工作目录 `.agentcore/`
- `cmd/axon` 逻辑已收拢到 `internal/app` 与 `internal/config`，CLI 入口后续保持薄封装，避免命令层继续承载业务组装细节
- `docs/usage/host-state-snapshot-v0.md` 与 `docs/usage/llm-config-v0.md` 已作为当前 v0 协议与配置基线文档
- Phase 3 / Task 3 已完成旧 `ReactRunner` 的保留边界收口；当前默认明确保持 `agentcore`，`react` 只作为显式 fallback 与 parity baseline 保留
- Phase 3 / Task 4 已补齐 post-turn recap hook 骨架，说明文档见 `docs/usage/host-recap-memory-hooks-v0.md`；当前默认已通过 `StructuredRecapHook` 写入 `recaps` 表，但仍未真正落到 `MEMORY.md` / `USER.md`
- 当前 recap 主线已继续推进到 `memory_candidates + *.draft.md`；默认 server wiring 会将 turn 终态沉淀到 `recaps + memory_candidates`，并渲染 `MEMORY.draft.md / USER.draft.md`
- 当前 `*.draft.md` 已收敛到 session 级聚合视图；默认会对 `memory_candidates` 做去重与状态过滤，便于后续继续接 review / adopt 流程
- `memory_candidates` 当前已具备最小后端状态流转能力，可在服务层执行 `applied/rejected`，为后续 Web 采纳/拒绝交互留出稳定接口
- Phase 3 / Task 5 已完成一轮联调文档与验证项收口；`docs/usage/web-host-local-dev.md`、`docs/usage/host-state-snapshot-v0.md` 已同步到当前代码现状
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
