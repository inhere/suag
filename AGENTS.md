# AGENTS for AI

- 请优先使用中文回复和响应，除非明确要求英文回复
- 如果当前任务有进度跟进文件，需要在完成后更新进度跟进文件
- 多阶段任务，需要在每个阶段完成后更新进度并提交 Git

## 正在进行的工作

<!-- PROCESSING WORKS:START -->
<!-- 需要实时更新: 正在进行的工作和关键md文档链接，已处理的注意去除 -->
- Axon v0 自用主线规划与阶段架构设计进行中，设计总纲见 `docs/v0-self-host-design.md`
- Axon v0 self-host 基础骨架实施计划进行中，计划文档见 `docs/superpowers/plans/2026-04-10-axon-v0-self-host-foundation-plan.md`
- internal 目录结构调整已完成首轮收敛，迁移映射见 `docs/internal-code-status.md`
- `internal/host/session` 首批基础骨架进行中，文件拆分说明见 `docs/superpowers/plans/2026-04-10-host-session-layout-note.md`
- `internal/host/session` service skeleton 已完成首轮最小主链，下一步将继续推进 gateway 与事件总线改造
- `internal/host/gateway` event bus 已完成 session-aware 第一轮改造，下一步继续推进 WS 与 session API
- `internal/host/gateway` 最小 session API 与 WS 订阅链路已打通，下一步继续推进 continue_turn 与 AG-UI 映射
- `internal/host/gateway` continue_turn 已打通最小闭环，下一步继续推进 AG-UI 映射与 ACP bridge 接线
- `internal/host/agui` 已完成第一版薄映射层，当前 WS 下行已切到 AG-UI 风格输出
- `internal/host/agui` 已补齐 run 标准生命周期映射，当前支持 RUN_STARTED / RUN_FINISHED / RUN_ERROR
- `internal/host/agui` STATE_SNAPSHOT 结构已完成第一轮收敛，前端可依赖 currentRunId/currentAgentId/latestEventType/eventCount
- `internal/host/agui` 已补齐 run_status ACTIVITY_SNAPSHOT，当前前端可直接渲染排队/运行/完成/失败状态卡
- `internal/host/agui` 已补齐最小 TEXT_MESSAGE_* 输出，当前 run.finished.summary 会映射为 assistant 文本消息
- `internal/host/acpbridge` 已完成第一版 host-facing contract 与 runner adapter，session 可通过窄接口接入外部 Agent bridge
- `internal/host/acpbridge` 已补齐最小 stub/local bridge，当前可生成 remote session/task 占位结果并回写 host runtime
- `internal/host/acpbridge` 已补齐 local command bridge 骨架与 runner factory，server 可按配置切换 stub / local_command 模式
- `internal/host/acpbridge` 已补齐 ACP stdio 最小链路，当前支持 initialize/session-new/session-prompt 与 agent_message_chunk 聚合
- `internal/host/acpbridge` 已开始收敛为直连后端路线，新增 `codex_app_server` 默认接线，`tmp/acp-adapter` 仅保留为参考实现
- `internal/host/acpbridge` 已切出独立 `codex_app_server` bridge，当前最小支持 initialize/initialized/thread-start/turn-start 与 turn 流式结果聚合
- `internal/host/acpbridge` 已补齐 codex app-server 流式事件摘要沉淀，当前 external_tasks.metadata 可携带 protocol/stopReason/eventSummary/items 等桥接元数据
- `internal/host/session` 与 `internal/host/acpbridge` 已打通 continue-turn 远端会话复用，后续 turn 可基于上一轮 external task 的 remoteSessionID 继续协作
- `internal/host/session`、`internal/host/acpbridge`、`internal/host/agui` 已打通外部 agent 流式事件透传，当前 WS 可实时收到 external activity 与 assistant message delta/end
- `internal/host/agui` 已按 Go SDK 风格收敛基础 Event interface，mapper 现返回 `[]Event`；外部流式事件也已补齐 `externalTaskId` 绑定
- `internal/host/session` 已补齐 external_tasks 存储读写，ACP bridge 返回的外部任务快照会写入 run 关联记录
- `internal/host/gateway` session 查询与 WS STATE_SNAPSHOT 已补齐 external_tasks 视图，前端首屏与重连可直接拿到外部任务状态
- `internal/host/agui` 已为 STATE_SNAPSHOT 增加 `externalActivities` / `externalMessages` 聚合视图，前端重连恢复无需自行重放外部流式事件
- `internal/host/agui` 已为 STATE_SNAPSHOT 补齐 `turns` / `runs` 聚合视图，并改为使用事件真实时间产出 `createdAt` / `updatedAt`，为后续一个 turn 下多 run 并发协作预留结构
- `internal/host/agui` 已为 `runs` 聚合视图补齐 `displayState`、`latestMessageId/latestMessageText`、`summary`、`externalTaskIds`，前端可直接基于 run 视图渲染多 agent 面板
- `internal/host/agui` 已为 `turns` 聚合视图补齐 `displayState`、`activeRunIds/completedRunIds/failedRunIds`、`hasExternalWork`，前端可直接渲染 turn 级协作概览
- `internal/host/agui` 已为 `turns` 聚合视图补齐 `primaryRunId/currentRunId`、各类 run count 与 `latestMessagePreview`，前端 turn 概览无需回查 runs 列表即可决定展示
- `internal/host/agui` 已为 STATE_SNAPSHOT 根层补齐 `snapshotVersion`、`snapshotSchema`、`generatedAt`，前端与后续多端接入可基于固定 schema 标识演进
- `internal/host/agui` 已将 STATE_SNAPSHOT 的 schema/version 标识收敛为共享常量，避免实现、测试与后续接入端各自手写字符串产生漂移
- `docs/usage/host-state-snapshot-v0.md` 已补齐当前 `STATE_SNAPSHOT v0` 的消费约定，前后端可基于同一份 schema 文档推进对接

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
