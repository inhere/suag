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
- `docs/usage/host-state-snapshot-v0.md`、`docs/usage/llm-config-v0.md`、`docs/usage/host-recap-memory-hooks-v0.md` 已作为当前 v0 协议、配置与 recap/memory 基线文档
- 当前 recap 主线已推进到 `recaps + memory_candidates + MEMORY.draft.md / USER.draft.md`，且后端已具备 `memory candidate` 查询与 `accept/reject` REST 入口
- Web 侧已完成 `Memory Candidates` 右侧面板接入，当前支持 session 级候选加载、`draft/rendered` 过滤、Accept/Reject、错误与 pending 状态反馈，见 `docs/superpowers/specs/2026-04-17-web-memory-candidates-panel-design.md` 与 `docs/superpowers/plans/2026-04-17-web-memory-candidates-panel-implementation-plan.md`
- Host internal self-loop Phase 1-4 正持续收口：`ResumeFromRunID`、结构化 `wait_kind`、同进程 resume 边界、terminal event 去重 已落地，当前重点是继续压稳 waiting/resume/stop 的状态机一致性
- Host runtime 状态机主链已继续收口：continue 校验、continue runtime context、stop/cancel、session reopen 与 canceled reload 语义已基本对齐；`run.finished / run.failed / run.canceled` 现统一优先于 Host 自补 terminal lifecycle
- `internal/host/session` 与 `internal/host/gateway` 已具备 session/turn/run 基础恢复、查询与 WS 命令编排链路；`DISPATCHING / EXTERNAL_RUNNING / INPUT_REQUIRED / AUTH_REQUIRED` 非终态已贯通到快照与事件流，未知 WS command 会明确返回 `command.error`，且成功 `continue_turn` 的 `command.accepted` 已保证先于该连接上的后续 run 事件流返回
- internal runner / ACP runner 的 Host contract 第一轮已基本对齐：`externalTaskId`、失败态 external task 持久化、internal suspend `waitMetadata`、ACP `EXTERNAL_RUNNING` waiting metadata 与 AG-UI run detail / snapshot 已完成第一轮收口
- 本地联调与恢复验证基线已补齐：`docs/usage/web-host-local-dev.md` 已整理为逐项手测清单，`AUTH_REQUIRED -> HITL respond` 后端链路与 `RunServer + /health` 默认 wiring 已有最小回归测试
- Task 3 正在收口 legacy React runtime 边界：team/orchestrator 的空 runtime 默认稳定走 `agentcore`，`UseAgentcore` legacy shim 不再影响默认单 agent 路径，parity tests 已明确标注为遗留对照而非 Host v0 主链
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
