# Host State Snapshot v0

> 状态：Current
> 更新时间：2026-04-15
> Schema：`axon.host.state.v0`
> Version：`v0`

本文档用于约定当前 `internal/host/agui` 输出的 `STATE_SNAPSHOT` 消费方式。

它的目标不是把每个字段都解释成底层实现细节，而是给 Web 客户端和后续接入端一个稳定的消费边界：

- 哪些字段是根层契约
- 哪些字段适合直接渲染
- 哪些字段更适合作为补充明细
- 后续演进时哪些字段应优先保持兼容

---

## 1. 适用范围

本文档只约束当前 Host 通过 WS 首包下发的：

- `STATE_SNAPSHOT`

不覆盖：

- 后续增量事件的完整语义说明
- `CUSTOM` / `TEXT_MESSAGE_*` / `ACTIVITY_SNAPSHOT` 的逐项字段规范
- 历史归档文档中的旧 `v1` 事件模型

---

## 2. 根层契约

当前 `STATE_SNAPSHOT.snapshot` 根层应至少包含：

- `snapshotVersion`
  - 当前固定为 `v0`
- `snapshotSchema`
  - 当前固定为 `axon.host.state.v0`
- `generatedAt`
  - 当前快照生成时间，RFC3339Nano UTC 字符串
- `threadId`
- `sessionId`
- `title`
- `status`
- `turnId`
- `lastTurnId`
- `currentRunId`
- `currentAgentId`
- `latestEventType`
- `eventCount`
- `turns`
- `runs`
- `externalTasks`
- `externalActivities`
- `externalMessages`
- `events`

消费建议：

- 客户端应先检查 `snapshotSchema`
- 当 `snapshotSchema` 不识别时，应降级到最保守展示，而不是假定字段不变
- `snapshotVersion` 用于人类理解和大版本切换
- `snapshotSchema` 用于程序判断兼容性

---

## 3. 推荐消费优先级

为避免前端重新把底层事件拼回视图，建议按下面顺序消费：

1. `turns`
2. `runs`
3. `externalMessages`
4. `externalTasks`
5. `events`

这表示：

- `turns`
  - 用于渲染会话当前轮次概览、协作状态条、轮次列表
- `runs`
  - 用于渲染多 agent run 卡片、run 状态面板、摘要信息
- `externalMessages`
  - 用于恢复外部 agent 的流式消息正文
- `externalTasks`
  - 用于展示外部任务元数据、远端会话信息、桥接状态
- `events`
  - 仅用于调试、时间线面板、或客户端确实需要做底层重放时使用

因此：

- 正常 UI 不应把 `events` 作为主渲染输入
- `turns` 与 `runs` 是当前推荐的主视图输入

---

## 4. `turns` 视图

`turns` 是按 `turnId` 聚合后的展示模型，面向“这一轮整体发生了什么”。

当前字段：

- `turnId`
- `sessionId`
- `primaryRunId`
  - 当前该轮第一个出现的 run，适合作为默认展开目标
- `currentRunId`
  - 当前该轮最新事件对应的 run
- `latestRunId`
  - 与当前实现下的 `currentRunId` 含义接近，保留作兼容字段
- `latestAgentId`
- `displayState`
  - 当前推荐值：`queued` / `running` / `dispatching` / `external_running` / `input_required` / `auth_required` / `completed` / `failed` / `canceled`
- `latestEventType`
- `latestMessagePreview`
  - 当前推荐作为 turn 卡片上的一句摘要
- `eventCount`
- `runCount`
- `activeRunCount`
- `completedRunCount`
- `failedRunCount`
- `activeRunIds`
- `completedRunIds`
- `failedRunIds`
- `hasExternalWork`
- `updatedAt`

推荐用法：

- turn 列表卡片
  - 主标题可使用 `turnId` 或业务侧生成的 turn 文案
  - 状态徽标优先使用 `displayState`
  - 副标题优先使用 `latestMessagePreview`
- turn 详情页
  - 默认展开 `primaryRunId`
  - 如果要高亮当前执行项，使用 `currentRunId`
- 协作概览
  - 用 `runCount` / `activeRunCount` / `completedRunCount` / `failedRunCount`
  - 用 `hasExternalWork` 判断是否展示“外部协作”标记

兼容建议：

- 不要假设 `latestRunId == currentRunId` 永远成立
- 不要假设 `activeRunIds` 只会有一个元素
- 后续一个 `turnId` 下允许出现多个 `runId`

---

## 5. `runs` 视图

`runs` 是按 `runId` 聚合后的展示模型，面向“某个 agent/run 当前是什么状态”。

当前字段：

- `runId`
- `turnId`
- `sessionId`
- `agentId`
- `displayState`
- `latestEventType`
- `latestMessageId`
- `latestMessageText`
- `summary`
- `externalTaskIds`
- `eventCount`
- `updatedAt`

推荐用法：

- run 卡片标题
  - 使用 `agentId`
- run 状态
  - 使用 `displayState`
- run 主文案
  - 优先使用 `summary`
  - 如果 `summary` 为空，再使用 `latestMessageText`
- run 与外部任务关联
  - 使用 `externalTaskIds`

当前语义约定：

- 如果 run 已经有最终 `summary`
  - `latestMessageText` 也会被收敛到最终 summary
- 如果 run 仍处于外部协作阶段
  - `displayState` 通常会是 `external_running`
- 如果 run 正在等待继续输入或授权
  - `displayState` 可能会是 `input_required` 或 `auth_required`
- 如果 Host 当前走的是默认 internal runner
  - 运行前会先经过 `preparing -> thinking`
  - 当前尚未补齐更细粒度的内部 tool / reasoning 映射，先以这两个中间态作为最小可视反馈
  - 当 runtime 返回 waiting 态时，Host 不会再补发 `run.finished`
  - 同一 `sessionId` 的下一次继续请求会优先尝试复用 suspended runtime 的 resume 语义
- `externalTaskIds`
  - 允许一个 run 关联多个外部任务

---

## 6. `externalMessages` 视图

`externalMessages` 用于恢复外部 agent 的流式文本消息，不建议直接替代 `runs`。

当前字段：

- `externalTaskId`
- `messageId`
- `sessionId`
- `turnId`
- `runId`
- `agentId`
- `content`
- `started`
- `completed`
- `lastEventType`
- `lastSequence`
- `updatedAt`

推荐用法：

- 当用户断线重连后，恢复外部 agent 正在输出或已输出的正文
- 当 run 卡片需要展开“完整流式消息”时，按 `runId` 或 `externalTaskId` 关联查询

不推荐：

- 直接用它决定整个 run 或 turn 的状态

---

## 7. `externalTasks` 视图

`externalTasks` 面向桥接和远端任务元数据，不是主展示模型。

当前字段：

- `externalTaskId`
- `sessionId`
- `turnId`
- `runId`
- `adapter`
- `remoteAgentName`
- `remoteSessionId`
- `remoteTaskId`
- `state`
- `lastHeartbeatAt`
- `resultSummary`
- `metadata`

推荐用法：

- 调试 ACP / bridge 接线
- 展示远端任务元信息
- 提供“继续协作”或“打开远端上下文”类入口

---

## 8. `events` 视图

`events` 是当前快照内保留的原始 host 事件列表。

当前它主要用于：

- 调试
- 时间线面板
- 客户端做底层排查

当前不推荐：

- 直接把它作为主 UI 数据源
- 每次重连都重新从 `events` 重放生成页面状态

当前实现补充约定：

- `events[].sequence` 是 session 维度的单调递增序号
- `GET /api/sessions/{id}/events?after_seq=N` 与 WS 首包后的增量消费，都应把这个序号当作 session 级游标
- 不要把 `sequence` 视为 turn 内局部序号，也不要假设新 turn 会重新从 `1` 开始
- `events` 中已开始出现 `run.status`
  - 当前用于表达 `DISPATCHING / EXTERNAL_RUNNING / INPUT_REQUIRED / AUTH_REQUIRED` 这类非终态运行语义
  - 客户端如果直接消费 `events`，应优先读取 payload 里的 `state`

如果某个页面必须读 `events`，也建议把它视为：

- 补充明细
- 而不是主展示契约

---

## 9. 演进约束

当前 `v0` 阶段推荐遵循下面几条规则：

1. 新增字段优先追加，不轻易重命名已在使用的字段
2. 如果某个字段语义要发生明显变化，优先新增新字段，而不是静默改旧字段含义
3. 当出现不兼容调整时：
   - 优先升级 `snapshotSchema`
   - 再决定是否同步调整 `snapshotVersion`
4. `turns` 和 `runs` 应继续保持“展示友好”的方向，而不是退化回原始事件集合

---

## 10. 当前最小消费建议

如果客户端只做最小实现，建议：

1. 根层先校验 `snapshotSchema == axon.host.state.v0`
2. 用 `turns` 渲染轮次概览
3. 用 `runs` 渲染 run 面板
4. 用 `externalMessages` 恢复正文流
5. 仅在调试页展示 `externalTasks` 与 `events`

这样可以最大限度复用当前 Host 已经聚合好的状态，而不是在客户端重复实现一次聚合器。
