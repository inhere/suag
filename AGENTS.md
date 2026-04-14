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
- `web/app/src/app/App.tsx` 与 `web/app/src/features/session/session-store.ts` 已继续补齐 session detail bootstrap 占位，当前恢复 session 时会先生成临时 turn/run 视图，避免 WS snapshot 到来前中栏和右栏空白
- `web/app` 已完成第一阶段脚手架落地，当前 `pnpm + React + Vite + TypeScript + Vitest` 最小三栏工作台壳子、测试与构建链路已打通
- `internal/host/session` 已补齐第一轮会话闭环持久化，当前 run 完成/失败时会同步推进 run / turn / session 实体状态，避免刷新恢复时只看到事件已结束但底层状态仍停留在 queued/running
- `internal/host/gateway` 与 `web/app` 已补齐第二轮恢复闭环，当前 `GET /api/sessions/{id}` 会直接返回聚合后的 turns/runs，前端恢复 session 时优先使用真实 detail 状态，不再总是退回 HTTP bootstrap 占位
- `internal/host/gateway` 与 `web/app` 已继续补齐恢复明细，当前 `GET /api/sessions/{id}` 也会返回 external activities/messages，前端刷新后可直接恢复外部流式内容与活动态，不必完全等待 WS 首包重建
- `internal/host/gateway` 与 `web/app` 已补齐最小 run detail 查询骨架，当前可通过 `GET /api/sessions/{id}/runs/{runId}` 单独拉取 run / turn / external 明细，为后续前端按需补明细打下基础
- `web/app/src/app/App.tsx` 与 `web/app/src/features/session/session-store.ts` 已补齐 focused run 按需明细恢复，当前切换到历史/概要 run 时会调用 `GET /api/sessions/{id}/runs/{runId}` 拉取单 run 明细并合并回工作台状态
- `web/app/src/features/session/session-store.ts` 已继续收拢 run detail 加载状态，当前 store 会显式跟踪 `idle/loading/loaded/error`，避免 `App.tsx` 内散落 run 明细请求判断逻辑
- `web/app/src/features/run-inspector` 已补齐 focused run 明细展示，当前右栏在 summary 优先的同时，也会展示该 run 的 external messages / activities，并按 focused run 切换 external task 详情
- `web/app/src/features/run-inspector/focused-run-panel.tsx` 已继续结构化 focused run 视图，当前右栏按 `Summary / Run Meta / External Messages / External Activities` 分区展示，联调时更易扫读 run 明细
- `web/app/src/app/App.tsx` 与 `web/app/src/features/run-inspector/external-task-panel.tsx` 已补齐 `Add Hint -> continue_turn` 最小操作链路，当前 waiting turn 下可从右栏直接追加提示到后端 WS 会话
- `web/app/vite.config.ts` 已补齐本地联调代理，当前 `pnpm dev` 可将 `/api`、`/events`、`/ws` 转发到 Host `:8180`，前后端已具备本地真机联调基础
- `docs/usage/web-host-local-dev.md` 已补齐最小本地联调说明，当前可按统一步骤完成迁移、启动 Host/Web，并验证 create session、start turn、snapshot 增量同步与 `Add Hint`
- `internal/host/session`、`internal/host/gateway/ws.go` 与 `web/app` 已补齐第一版 `Stop` 闭环，当前前端可通过 `stop_turn` WebSocket 命令停止当前 turn，Host 会取消活动 run、落库 `run.canceled` 并把取消状态实时回推到工作台
- `web/app` 已补齐第一版 HITL 工作台面板，当前前端可调用 `/api/hitl` 查看 pending tickets，并在右栏直接执行 `respond` / `dismiss`，形成最小人工干预入口
- `internal/ops/hitl`、`internal/host/session` 与 `web/app` 已补齐第一版 HITL 上下文关联，当前 host 主链触发的 ticket 会自动携带 `sessionId/turnId/runId`，前端右栏可直接展示并高亮与当前会话关联的人工干预项
- `web/app/src/features/hitl` 与 `web/app/src/app/App.tsx` 已补齐 `Open Context` 入口，当前可从 HITL 卡片直接切回对应 session/run，上下文切换后会触发既有 HTTP bootstrap、WS 重连与 run detail 恢复链路
- `internal/host/gateway`、`internal/host/session` 与 `web/app` 已补齐第一版服务端 session list，当前 Host 可通过 `GET /api/sessions` 返回按更新时间排序的会话摘要，前端左栏首次打开时也可从服务端恢复已有会话，而不再只依赖本地 recentSessions
- `web/app/src/lib/session-ws.ts`、`web/app/src/app/App.tsx` 与 `web/app/src/features/session-list` 已补齐第一版连接状态与错误反馈，当前左栏可展示 active session 的 `connecting / connected / disconnected` 状态，以及 session list / session bootstrap / websocket 失败提示，联调时更容易判断当前链路卡在哪一层
- `web/app/src/lib/session-ws.ts`、`web/app/src/app/App.tsx` 与 `web/app/src/features/session-list` 已补齐第一版连接重试闭环，当前 active session 在 websocket 断开后会自动重连，session list / session bootstrap 失败时也可从左栏直接手动 Retry 恢复
- `web/app/src/app/App.tsx`、`web/app/src/features/session/session-store.ts` 与 `web/app/src/features/run-inspector` 已补齐 focused run detail 加载反馈闭环，当前右栏会显式展示 run 明细的 `loading / error` 状态，并可在单 run 明细加载失败后直接 `Retry Detail` 重新拉取
- `web/app/src/features/conversation`、`web/app/src/features/session/session-selectors.ts` 与 `web/app/src/app/App.tsx` 已补齐最近 turn 摘要带，当前中间会话区会展示最近几轮 turn 的紧凑卡片，并可点击某个 turn 直接聚焦其 latest run 进入回看
- `web/app/src/app/App.tsx`、`web/app/src/app/app-shell.tsx` 与 `web/app/src/styles.css` 已补齐顶部 context/status strip，当前工作台顶部可快速扫读 `session / turn / focused run / socket` 四项上下文，便于第一轮真机联调时快速定位当前视角
- `internal/host/gateway/ws.go` 与 `internal/host/gateway/ws_test.go` 已补齐本地开发 WS origin 放行，当前 `localhost/127.0.0.1/[::1]` 的前端联调地址可跨 host 连接 Host WebSocket，不再被默认 Origin 校验拦截
- `web/app/src/app/App.tsx`、`web/app/src/features/conversation`、`web/app/src/features/run-inspector` 与 `web/app/src/features/hitl` 已补齐第一轮提交中状态反馈，当前创建 session、发起 turn、继续输入、停止 turn、HITL respond/dismiss 都会展示按钮禁用与提交中文案，减少联调时的重复点击和卡住误判
- `web/app/src/app/App.tsx`、`web/app/src/features/session-list`、`web/app/src/features/conversation`、`web/app/src/features/run-inspector` 与 `web/app/src/features/hitl` 已补齐第一轮失败反馈闭环，当前创建 session、发起/继续/停止 turn 以及 HITL respond 失败时都会在原位置展示内联错误提示，不再只剩按钮恢复导致联调时难以判断失败原因
- `docs/superpowers/specs/2026-04-14-agentcore-platform-contract-alignment-design.md` 已补齐 agentcore 与 platform contract 收口设计，当前已明确在构建 Host internal `session runner` 之前，先完成 `llm / tools / memory` 围绕 `pkg/agentcore/api` 的第一方收口，并同步规划 `llm.api_type` 配置演进
- `docs/superpowers/plans/2026-04-14-agentcore-llm-contract-alignment-phase1.md` 已补齐第一阶段实施计划，当前先聚焦 `agentcore/api + platform/llm + agentcore runner + llm.api_type/provider_name` 收口，不提前进入 Host internal `session runner` 实装
- Phase 1 计划已补做一轮工程复核，当前已明确 `internal/platform/llm` 收口需要显式兼容 `ReactRunner`、`cron` 与 `Pool`，同时已开始清理文档中的旧 `internal/agentcoreadapters/*` 路径，避免后续实现判断继续被旧结构干扰
- Phase 1 实施已开始第一步，`internal/ops/cron/scheduler_test.go` 已补齐 cron 的 LLM 决策兼容回归测试，当前 `ReactRunner / cron / team` 的 legacy caller 基线已具备首轮 no-regression 护栏
- Phase 1 实施已完成 `pkg/agentcore/api` 第一轮 contract patch，当前已新增可选 `EmbeddingProvider` 扩展接口，并补齐 `provider_test.go` 覆盖基础 provider contract 行为

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
