# AGENTS for AI

- 请优先使用中文回复和响应，除非明确要求英文回复
- 如果当前任务有进度跟进文件，需要在完成后更新进度跟进文件
- 多阶段任务，需要在每个阶段完成后更新进度并提交 Git

## 正在进行的工作

> NOTE: 需要实时更新，正在进行的工作和关键md文档链接，已完成的需要移除

<!-- PROCESSING WORKS:START -->
- Axon v0 自用主线规划与阶段架构设计进行中，设计总纲见 `docs/v0-self-host-design.md`
- Axon v0 self-host 基础骨架实施计划进行中，计划文档见 `docs/superpowers/plans/2026-04-10-axon-v0-self-host-foundation-plan.md`
- internal 目录结构调整已完成首轮收敛，迁移映射见 `docs/internal-code-status.md`
- `internal/host/session` 首批基础骨架进行中，文件拆分说明见 `docs/superpowers/plans/2026-04-10-host-session-layout-note.md`
- `docs/usage/host-state-snapshot-v0.md` 已补齐当前 `STATE_SNAPSHOT v0` 的消费约定，前后端可基于同一份 schema 文档推进对接
- `docs/superpowers/specs/2026-04-12-axon-web-foundation-design.md` 已补齐前端基础工作台设计，当前以三栏单页工作台为第一阶段前端实现基线
- `docs/superpowers/plans/2026-04-12-axon-web-foundation-implementation-plan.md` 已补齐前端基础工作台实现计划，可据此开始拆分 `web/app` 脚手架、HTTP/WS 接线与三栏 UI 实施
- 前端基础工作台计划已完成一轮工程收口，已明确 `pnpm`、现有 Host API 约束、v0 的已知 session 列表策略，以及 `continue_turn`/`Stop`/`Add Hint` 的阶段边界
- `web/app/src/lib` 已补齐 Host `STATE_SNAPSHOT v0` 相关类型、session REST client 与 session WebSocket client，当前前端可按现有 Host 契约发起 session/turn 请求并发送 `continue_turn`
- `web/app/src/features/session` 已补齐最小状态层与 selectors，当前前端可基于 `STATE_SNAPSHOT v0` 选择 current turn、active runs、focused run，并在水合时同步 recent sessions
- `web/app/src/features/session-list` 已补齐左栏紧凑工作列表组件并接入 `AppShell`，当前最小前端骨架已可展示 recent sessions、搜索框与状态过滤按钮
- `web/app/src/features/conversation` 与 `web/app/src/features/run-inspector` 已补齐中栏会话区和右栏运行检查区，当前三栏工作台可展示消息流、turn 摘要、active run 列表、focused run 细节与外部任务概览
- `web/app/src/app/App.tsx`、`web/app/src/features/debug` 与 `web/app/src/features/session/session-store.ts` 已完成真实 Host 主链接线，当前前端可创建 session、启动 turn、接收 `STATE_SNAPSHOT` 与部分增量事件，并通过 debug drawer 查看原始事件
- `web/app/src/features/session/session-store.ts` 已继续补齐增量事件归并，当前前端在不等待新 snapshot 的情况下，也可消费 `RUN_STARTED`、`RUN_ERROR`、`CUSTOM(axon.run.external.activity)` 与 `ACTIVITY_SNAPSHOT` 推进 run / turn / external activity 状态
- `web/app/src/app/App.tsx` 已补齐最近会话与焦点的本地持久化，当前前端刷新后可恢复 recent sessions、current session 与 focused run，不再每次都从空白工作台开始
- `web/app/src/app/App.tsx` 与 `web/app/src/features/session/session-store.ts` 已补齐 `getSession()` 的 HTTP bootstrap 合并，当前恢复出来的 session 在 WS 首包到来前也可先刷新标题、状态和 external task 基础明细
- `web/app` 已完成第一阶段脚手架落地，当前 `pnpm + React + Vite + TypeScript + Vitest` 最小三栏工作台壳子、测试与构建链路已打通

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
