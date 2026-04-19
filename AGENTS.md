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
- `internal/host/session` 与 `internal/host/gateway` 已具备 session/turn/run 基础恢复与查询链路；`DISPATCHING / EXTERNAL_RUNNING / INPUT_REQUIRED / AUTH_REQUIRED` 非终态已贯通到快照与事件流
- `docs/usage/host-state-snapshot-v0.md`、`docs/usage/llm-config-v0.md`、`docs/usage/host-recap-memory-hooks-v0.md` 已作为当前 v0 协议、配置与 recap/memory 基线文档
- 当前 recap 主线已推进到 `recaps + memory_candidates + MEMORY.draft.md / USER.draft.md`，且后端已具备 `memory candidate` 查询与 `accept/reject` REST 入口
- Web 侧已完成 `Memory Candidates` 右侧面板接入，当前支持 session 级候选加载、`draft/rendered` 过滤、Accept/Reject、错误与 pending 状态反馈，见 `docs/superpowers/specs/2026-04-17-web-memory-candidates-panel-design.md` 与 `docs/superpowers/plans/2026-04-17-web-memory-candidates-panel-implementation-plan.md`
- Host internal self-loop Phase 1-4 正持续收口：`ResumeFromRunID`、结构化 `wait_kind`、同进程 resume 边界、terminal event 去重 都已开始落地，当前重点是把 waiting/resume/stop 的状态机一致性继续压稳
- Task 1 当前已补一段 continue state guard：`continue_turn` 遇到空 session、latest waiting run 丢失或加载失败时都不再静默放行，而是返回明确错误，避免异常状态下错误续跑
- Gateway command 入口已补一段外显收口：未知 WS command 不再静默忽略，而是统一返回 `command.error`，避免前端发起误命令后无反馈卡住
- terminal lifecycle 的 session 状态已开始收口：completed / failed / canceled 后的 `session.Status` 改回 `OPEN`，不再让 `/api/sessions` 与刷新后的初始快照把已结束会话误显示成 `WAITING`
- stop/cancel 的 turn 级状态已开始收口：Host 新增 `TurnCanceled`，`stop_turn` 后 turn 不再沿用 `WAITING`，而是与 run 一样明确标记为 `CANCELED`
- canceled 的 reload 语义已补一层回归：`BuildStateSnapshot()` 与前端 `hydrateFromSnapshot()` 在纯快照恢复时都会保留 canceled 的 displayState 与取消原因，避免刷新后丢失 `stopped by operator` 文案
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
