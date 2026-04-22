# AGENTS for AI

- 请优先使用中文回复和响应，除非明确要求英文回复
- 如果当前任务有进度跟进文件，需要在完成后更新进度跟进文件
- 多阶段任务，需要在每个阶段完成后更新进度并提交 Git

## 正在进行的工作

> **IMPORTANT**: 需要实时更新，正在进行的工作和关键md文档链接；**已完成的需要移除**，不能一直累计，总数不能超过10条。

<!-- PROCESSING WORKS:START -->

- Axon v0 自用主线持续推进中，设计总纲见 `docs/v0-self-host-design.md`；当前 Host + Web 最小闭环已打通，联调说明见 `docs/usage/web-host-local-dev.md`
- `docs/usage/host-state-snapshot-v0.md`、`docs/usage/llm-config-v0.md`、`docs/usage/host-recap-memory-hooks-v0.md`、`docs/usage/host-learning-retrieval-v0.md`、`docs/usage/host-internal-self-loop-v0.md` 共同构成当前 v0 协议、配置、learning/recall 与 self-loop 基线
- `internal/host/session`、`internal/host/gateway`、Web 工作台已具备 session/turn/run 恢复、WS 命令编排、Pending HITL 自动刷新与 Focused Run 恢复链路；`DISPATCHING / EXTERNAL_RUNNING / INPUT_REQUIRED / AUTH_REQUIRED` 已贯通到快照与事件流
- internal runner / ACP runner 的 Host contract 已完成一轮对齐：`externalTaskId`、失败态 external task 持久化、internal suspend `waitMetadata`、ACP `EXTERNAL_RUNNING` waiting metadata 与 AG-UI run detail / snapshot 已对齐
- Host learning-system 当前已形成 `recaps + memory_candidates + MEMORY.draft.md / USER.draft.md + MEMORY.md / USER.md` 闭环，并已验证 recall 会影响 self-loop 结果且不会污染 session history
- `tools / mcp / skills` 长线治理设计见 `docs/superpowers/specs/2026-04-20-host-capability-governance-design.md`，当前按“agentcore 只保留 runtime kernel 与 tool contract，axon 负责 capability governance” 方向持续推进
- Capability governance 当前已完成到 Phase C-D：`CapabilityPolicy`、tool subset、skill matching、resource hints、planner hint 已贯通；后续继续推进 richer skill matching、tool catalog 与 recall/policy/skills/planner 联动
- Web 默认界面的 `Chat-first` 设计见 `docs/superpowers/specs/2026-04-21-web-chat-first-layout-design.md`，当前已完成 Task 1 壳层重构与左栏收敛；后续继续推进中栏聊天主视图、右栏紧凑 context 与 App 接线
- `Chat-first` 默认界面的实现计划见 `docs/superpowers/plans/2026-04-21-web-chat-first-layout-implementation-plan.md`，当前按 `Task 1 -> Task 2 -> Task 3 -> Task 4` 顺序推进，Task 1 已通过 spec review 与代码质量 review
<!-- PROCESSING WORKS:END -->

## 项目编码规范

- 关键的方法或逻辑点需要添加注释说明
- 单个go文件最大行数不能操作1000行，超过时需要按情况进行拆分

### git worktree 开发规范

- 使用 git worktree 进行开发时，在 `.worktrees` 目录下创建分支

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
