# Axon v0 自用主线与阶段架构设计

> 状态：Draft for review
> 日期：2026-04-10
> 范围：Axon 整体主线、v0 分阶段演进、第一阶段自用闭环架构
> 相关文档：`docs/axon-prd.draft.md`、`pkg/agentcore/README.md`、`pkg/agentcore/docs/architecture.md`、`docs/research/ag-ui-protocol-research.md`、`docs/research/a2a-protocol-research.md`、`docs/research/a2ui-protocol.md`

---

## 1. 文档目的

本文档用于把 Axon 当前的产品想法、协议判断和架构分层收拢成一份总纲。

目标不是一次性写完所有细节，而是先回答 4 个问题：

- Axon 长期要长成什么
- 当前第一阶段到底先做什么
- 核心边界如何划分，避免后续返工
- 未来从 `v0` 演进到更成熟形态的主线是什么

本文档刻意保持“由大到小”的节奏，只定义总主线和第一阶段骨架，避免过早写出一堆后续必改的大文档。

---

## 2. 核心判断

### 2.1 产品定位

Axon 的长期定位不是单纯聊天机器人，也不是一个只会套壳调用模型的工具，而是：

**一个可长期运行的 Agent 操作系统。**

它的核心能力不是“自己直接完成所有事”，而是：

- 管理会话
- 调度能力
- 指挥外部 Agent
- 沉淀记忆
- 持续形成可复用技能

### 2.2 当前版本定位

当前不应急于定义 `v1`。

更合理的策略是：

- 明确回到 `v0`
- 连续推进几个 `v0.x` 阶段
- 等核心语义和真实使用场景稳定后，再决定 `v1`

也就是说，`v1` 不是时间点，而是能力收敛门槛。

### 2.3 当前第一子项目

第一阶段选择的主线是：

**先实现一个可自用的编码协作 Agent 主机。**

它需要跑通的闭环是：

`Web 用户发起任务 -> 服务端建立会话 -> Axon 驱动 agentcore -> 通过 ACP 指挥 codex/claude-code 等外部 Agent -> 持续跟进执行 -> 通过 WS + AG-UI 实时回传 -> 会话可恢复、可继续`

这条主线优先级高于完整多 Agent、完整 IM 生态、复杂治理面板等方向。

---

## 3. 长期分层

Axon 长期建议分成 4 层：

### 3.1 Agent Core

对应当前的 `pkg/agentcore`。

定位是最小 runtime kernel，只负责：

- run state
- resume
- tool loop
- prompt/context
- transcript/snapshot
- hooks/error policy

它不负责：

- Web / WS
- ACP / A2A 协议适配
- 会话控制面
- 多 Agent 编排
- IM 渠道
- 产品级长期记忆治理

### 3.2 Agent Host / Session Runtime

这是第一阶段最重要的一层，是把 `agentcore` 包装成真实可用产品能力的宿主层。

主要负责：

- session 生命周期
- turn / run 生命周期
- 长任务执行控制
- 挂起、恢复、继续
- ACP 外部 Agent 协作
- 事件归一化与推送
- 会话结束后的 recap 与记忆沉淀挂点

### 3.3 Control Plane

这是未来逐步长出来的中台层。

主要负责：

- 多 Agent 编排
- 团队/角色模型
- review 与治理流程
- tracing 与回放
- 权限与安全策略
- 任务控制面与运营面板
- 自进化与审核流程

### 3.4 Channel & Extension Layer

这是生态接入层。

主要负责：

- Web 客户端
- IM 渠道
- openclaw 插件
- TS/JS 扩展能力
- 未来更多外部生态接入

原则上：

- Go 负责内核、运行时、状态和治理主线
- TS/JS 负责扩展生态、前端和渠道适配

---

## 4. 第一阶段范围

第一阶段只定义成一个明确产品切片：

**让 Axon 作为一个可自用的编码协作 Agent 主机稳定运行。**

### 4.1 第一阶段必须交付

#### 1. 会话闭环

系统必须支持：

- 创建 session
- 在 session 下发起 turn
- 在 turn 下驱动一个或多个 run
- 结束后继续对同一 session 追加后续轮次

#### 2. ACP 外部 Agent 协作

Axon 第一阶段不追求自己直接具备最强编码能力，而是先稳定接管：

- codex
- claude-code
- 后续可扩展的 ACP Agent App

并支持持续跟进其执行状态。

#### 3. 事件流与可视化

服务端需要统一内部事件模型，并通过：

- `WebSocket` 作为主传输
- `AG-UI` 作为下行事件语义

把运行过程实时推送给前端。

#### 4. 长任务稳定运行

第一阶段就应具备最小 Harness 思路：

- timeout
- retry
- heartbeat
- failed / blocked / canceled 状态
- resume / continue 入口

#### 5. 最小记忆与会话沉淀

第一阶段先不做完整自进化，但必须给这些能力留标准出口：

- 会话历史持久化
- 每轮结束 recap
- `MEMORY.md` 写入候选
- `USER.md` 写入候选
- SQLite / FTS 后续接入点

#### 6. Go core + TS/JS extension 的稳定边界

第一阶段就要把责任边界定清楚：

- Go：runtime、session、state、ACP bridge、治理主线
- TS/JS：Web UI、插件、IM 渠道、生态扩展

### 4.2 第一阶段明确不做

第一阶段不应承诺以下内容：

- 完整多 Agent 团队自治
- 多节点分布式调度
- 完整 IM 渠道矩阵
- 完整治理控制面
- 强约束的自动自进化闭环
- 完整 A2A mesh

第一阶段成功标准不是“功能多”，而是：

**Axon 能稳定接住一个真实开发任务，把它转成一段可跟踪、可恢复、可继续协作的外部 Agent 执行会话。**

---

## 5. 核心模块边界

为保证第一阶段可落地且不堵住未来路线，建议先拆成 7 个核心模块。

### 5.1 `pkg/agentcore`

继续保持为单 Agent runtime kernel。

它解决通用运行语义，不感知产品层协议和平台对象。

### 5.2 Host Session Runtime

这是第一阶段真正的产品核心。

第一阶段建议的代码落位是：

- `internal/host/session`

它负责：

- session 生命周期
- turn 生命周期
- run 调度
- 超时、重试、失败态
- 挂起与恢复
- 会话级元数据管理

可以把它理解成：

- `agentcore` 是发动机
- `session runtime` 是整车底盘

### 5.3 ACP Bridge

这一层是第一阶段差异化核心之一。

第一阶段建议的代码落位是：

- `internal/host/acpbridge`

它不应只是“执行一个外部命令”，而应成为统一的外部 Agent 代理层，负责：

- 发现外部 Agent 客户端
- 建立 ACP 会话
- 发送任务
- 追加指令
- 查询状态
- 收集结果
- 将外部状态映射为 Axon 内部事件

关键原则：

**ACP bridge 放在 `session runtime` 之下、`agentcore` 之外。**

原因是 ACP 属于重要但仍然产品侧的外部能力编排方式，不应污染 `agentcore` 的内核边界。

### 5.4 Event Bus / AG-UI Adapter

服务端内部先统一事件模型，再适配为 AG-UI 推给前端。

第一阶段可以先不急着单独拆包；当 AG-UI 映射真正开始落代码时，建议代码落位为：

- `internal/host/agui`

不要让内部代码直接绑定在前端展示格式上。

第一阶段建议至少覆盖：

- run 生命周期事件
- step 事件
- 文本输出事件
- 工具调用事件
- activity / progress 事件
- custom 事件

### 5.5 Memory & Recap Service

第一阶段先做轻记忆，但边界要先定好。

它负责两类事：

- 会话事实沉淀
- 会话后总结沉淀

建议分三层理解：

- transcript / run snapshot：事实层
- conversation / turn summary：运行辅助层
- `MEMORY.md` / `USER.md` / SQLite：长期记忆层

### 5.6 Gateway / WS Server

这一层是外部入口层，职责应保持很薄：

第一阶段建议的代码落位是：

- `internal/host/gateway`

- 接收客户端连接
- 管理认证和 session 绑定
- 接收用户输入
- 订阅并推送事件
- 处理继续、暂停、取消、补充说明等交互

### 5.7 Web Client

第一阶段 Web 客户端最重要的是把过程看清楚，而不是堆太多页面。

前端目录建议固定为：

- `web/app`：React + Vite 源码
- `web/dist`：构建产物

至少需要：

- 会话列表
- 当前会话时间线
- turn / run 视角切换
- 事件流
- 人工介入输入框
- 最终产出与 recap 区

### 5.8 开工前技术与目录冻结

为避免真正开始开发后频繁改目录或重复争论选型，当前先冻结以下决策。

#### 已确认的技术选型

- Go 继续使用当前 `1.24`
- CLI 后续准备迁移到 `gookit/gcli`，但当前阶段先不改 `cmd/axon`
- WebSocket 实现使用 `github.com/coder/websocket`
- ID 方案使用 `前缀 + ULID`
- 数据库迁移使用 `gookit/miglite`
  - 本地使用已安装的 `miglite` CLI
  - 迁移文件采用原始 SQL 文件
- 前端使用 `React + Vite`

建议的 ID 前缀包括：

- `sess_`
- `turn_`
- `run_`
- `evt_`
- `ext_`

#### 目录结构冻结原则

当前不建议再新增大量并列顶层目录，而应按“稳定业务边界”收拢。

建议的目标结构如下：

```text
cmd/
  axon/

docs/
  README.md
  v0-self-host-design.md
  internal-code-status.md
  archive/
  research/
  superpowers/

db/
  migrations/

pkg/
  agentcore/
    storage/
      files/
      sqlite/

internal/
  host/
    session/
    gateway/
    acpbridge/
    agui/          # 可后置，等真正落 AG-UI 映射时再创建
  platform/
    llm/
    tools/
    memory/
    tracing/
  ops/
    hitl/
    cron/

web/
  app/
  dist/
```

#### 命名与分层说明

- `agentcore` 的第一方 storage 实现统一放在 `pkg/agentcore/storage/*`
- `pkg/agentcore/storage/files` 是文件系统实现
- `pkg/agentcore/storage/sqlite` 是 SQLite 实现

原因是：

- storage implementation 是 `agentcore.Storage` 的具体实现层，不属于 Host adapter
- 迁移期桥接层已经完成历史使命，不再作为当前推荐目录保留

因此当前结构里直接保留 `pkg/agentcore/storage/*` 作为第一方实现层，不再继续扩展独立 adapter 目录。

#### `pkg/` 与 `internal/` 的边界

当前只有 `pkg/agentcore` 明确满足“独立、可复用、contract 已成形”的条件。

对于 `llm`、`cron`、`memory`、`tools` 等模块，虽然带有基础能力属性，但当前仍然和产品语义绑定较深，因此暂不因为“看起来更通用”而提前迁入 `pkg/`。

采用标准应是：

1. 不依赖当前产品业务模型
2. 对外 API 已相对稳定
3. 仓库外复用是合理且真实的

在这三个条件未同时满足之前，优先继续放在 `internal/`。

#### 数据库迁移约束

当前 SQLite schema 不再建议通过应用代码中的大段 `initSchema()` 字符串来长期维护，而应统一走：

- `gookit/miglite`
- `db/migrations/` 下的 SQL 文件

这样做的好处是：

- schema 变更可审计
- 与 `session / turn / run / event` 结构演进更匹配
- 后续本地开发、测试环境和部署环境的升级路径更清晰

应用代码中的存储实现可以保留必要的迁移状态检查或最小防御性初始化，但不应再把完整 schema 定义散落在 Go 代码中。

同时，为了减少 `internal/` 顶层目录数量，建议做一层内部收敛：

- `internal/platform/*`
  - 放相对基础的能力模块：`llm`、`tools`、`memory`、`tracing`
- `internal/ops/*`
  - 放更偏运行治理和系统操作的模块：`hitl`、`cron`

不建议使用 `internal/pkg/*` 这类命名，因为它会让“内部基础设施”和“对外可复用包”的语义混在一起，后续容易误导目录边界。

#### 目录增长约束

后续开发中，除非新目录承担明确且长期稳定的边界，否则：

- 优先新增文件
- 不优先新增目录

同时不建议建立全局性的：

- `internal/model`
- `internal/service`
- `internal/dto`

更推荐每个业务边界自己携带：

- `types.go`
- `service.go`
- `dto.go`
- `sqlite.go`
- `events.go`

避免后期出现“什么都往公共目录里塞”的杂物间结构。

---

## 6. 状态流转与运行主链

### 6.1 协议落位

结合已有调研，建议第一阶段协议落位如下：

- `Web Client <-> Gateway`：`WebSocket`
- `Gateway -> Client` 下行事件语义：`AG-UI`
- `AG-UI CUSTOM / ACTIVITY` 载荷：`A2UI`
- `Axon -> External Agent App`：`ACP`
- `Axon -> Remote Agent / Future Mesh`：后续阶段再引入 `A2A`

判断依据：

- `AG-UI` 适合 `gateway -> client` 的事件流语义，且支持 WebSocket 能力声明
- `A2A` 更适合作为未来 `Agent -> Agent` 的南向任务协议，不适合第一阶段直接承载 Web 会话主链
- `A2UI` 不是 transport protocol，更适合作为 AG-UI 事件载荷

### 6.2 Session 状态

第一阶段建议把 Session 状态控制在最小集合：

- `OPEN`
- `RUNNING`
- `WAITING`
- `ARCHIVED`

Session 是长期容器，不承载复杂执行细节。

### 6.3 Run 状态

第一阶段建议 Run 具备明确状态机：

- `QUEUED`
- `PREPARING`
- `THINKING`
- `DISPATCHING`
- `EXTERNAL_RUNNING`
- `INPUT_REQUIRED`
- `AUTH_REQUIRED`
- `SUMMARIZING`
- `COMPLETED`
- `FAILED`
- `CANCELED`

关键原则：

**Run 可以阻塞，但 Session 不应该死掉。**

这保证后续“持续跟进协作”是标准路径，而不是异常路径。

### 6.4 主链路

第一阶段的一次真实请求建议按这条链运行：

`Web Client -> WS Gateway -> Session Runtime -> agentcore -> ACP Bridge -> External Agent`

同时旁路有两条支线：

- `Session Runtime -> Event Bus -> AG-UI Adapter -> Web Client`
- `Session Runtime -> Memory / Recap Service`

这样可以把执行主线、可见性主线、沉淀主线解耦。

---

## 7. 最小数据模型

第一阶段建议先把对象关系固定为：

`Session -> Turn -> Run -> ExternalTask / RunEvent`

### 7.1 Session

表示长期会话容器。

建议核心字段：

- `sessionId`
- `title`
- `status`
- `createdAt`
- `updatedAt`
- `lastTurnId`
- `owner`
- `workspace`
- `metadata`

### 7.2 Turn

表示一次“对话轮次”或“一次任务推进意图”。

`turnId` 应第一阶段就进入正式模型，因为未来一个 turn 下可能会有多个 agent 的 `runId`。

建议核心字段：

- `turnId`
- `sessionId`
- `initiatorType`
- `initiatorId`
- `inputMessageId`
- `inputSummary`
- `status`
- `createdAt`
- `completedAt`
- `primaryRunId`
- `metadata`

`initiatorType` 建议至少支持：

- `user`
- `system`
- `agent`

### 7.3 Run

表示一个具体 Agent 的一次执行实例。

建议核心字段：

- `runId`
- `sessionId`
- `turnId`
- `agentId`
- `role`
- `parentRunId`
- `state`
- `resumeFromRunId`
- `startedAt`
- `endedAt`
- `failureCode`
- `summary`
- `metadata`

### 7.4 RunEvent

表示回放、观测和前端实时显示的基础对象。

建议核心字段：

- `eventId`
- `sessionId`
- `turnId`
- `runId`
- `agentId`
- `eventType`
- `sequence`
- `payload`
- `createdAt`
- `source`

建议从第一阶段起，所有事件都稳定带上：

- `sessionId`
- `turnId`
- `runId`
- `agentId`

### 7.5 ExternalTask

表示 ACP / 外部 Agent 协作映射对象。

建议核心字段：

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

关键原则：

**外部任务不是 run 本身，而是 run 关联的外部执行实体。**

### 7.6 Recap / MemoryWrite

表示每轮结束后的沉淀出口。

建议核心字段：

- `recapId`
- `sessionId`
- `turnId`
- `runId`
- `kind`
- `content`
- `target`

其中 `kind` 建议先支持：

- `turn_summary`
- `agent_memory_candidate`
- `user_memory_candidate`

`target` 对应后续写入位置：

- `MEMORY.md`
- `USER.md`
- `state.db`

---

## 8. 存储边界

第一阶段建议明确分三层，不把所有语义混进一个大表或一个大存储实现里。

### 8.1 agentcore authority storage

继续只管 run 级权威事实：

- snapshot
- append-only transcript

这是内核事实层。

### 8.2 host runtime state store

存放产品层对象：

- session
- turn
- run
- event index
- external task mapping

第一阶段推荐使用 SQLite 作为主存储，原因是：

- 单机可用
- 结构化查询强
- 事务清晰
- 便于后续接 FTS

### 8.3 memory artifacts store

存放：

- `MEMORY.md`
- `USER.md`
- 会话 recap
- 后续检索索引

关键边界是：

- run transcript 是 authority
- session / turn / run index 是 host state
- memory recap 是 derived knowledge

---

## 9. 整体演进路线

建议 Axon 按以下 5 个阶段推进：

### Phase 0：`v0.kernel`

目标：把 `agentcore` 作为 runtime kernel 继续收紧边界，稳定 contract，不把产品层能力塞回 core。

### Phase 1：`v0.self-host`

目标：跑通可自用编码协作闭环。

核心交付：

- Web + WS 会话主链
- session / turn / run 模型
- ACP bridge
- AG-UI 事件下行
- recap 与记忆挂点

### Phase 2：`v0.control-plane-lite`

目标：从“能跑”升级到“能管”。

核心交付：

- turn / run 时间线
- 控制面板
- block / resume / cancel / retry
- Harness 化治理
- 基础回放与审计

### Phase 3：`v0.multi-agent-ready`

目标：让同一 host 内的多 Agent 协作成为正式能力。

核心交付：

- 一个 turn 下多个 run
- agent profile / role
- leader / worker / reviewer 基础模型
- trace / causality 基础链路

### Phase 4：`v0.mesh-and-ecosystem`

目标：走向远程协作与生态接入。

核心交付：

- A2A 远程 Agent 协作
- TS/JS extension runtime
- openclaw / IM channel 接入
- 更丰富的 A2UI 交互卡片

### Phase 5：`v0.learning-system`

目标：让记忆与自进化成为稳定提效机制，而不是硬编码主控制流。

核心交付：

- recap -> memory -> retrieval 主线
- SQLite + FTS5 检索
- skill 候选沉淀
- review / adopt / iterate 流程

---

## 10. 长期架构原则

为避免后续演进失控，建议长期坚持以下原则：

### 10.1 `agentcore` 不承载产品层协议与平台逻辑

ACP、A2A、AG-UI、IM、review、team 都不进 core。

### 10.2 内部状态模型优先于外部协议

先稳定：

- `session`
- `turn`
- `run`
- `event`
- `external task`

再做协议适配。

### 10.3 WebSocket 是主传输，AG-UI 是事件语义层

传输和事件语义保持分离。

当前主链路的 WebSocket 实现选择固定为：

- `github.com/coder/websocket`

### 10.4 Go 保持内核与治理，TS/JS 保持生态与扩展速度

避免两边职责打架。

### 10.5 自进化是 best-effort 机制，不是硬编码必达动作

Agent 可以建议沉淀、建议生成 skill、建议优化流程，但不应在每轮都强制触发。

---

## 11. 何时值得定义 `v1`

只有当以下条件大部分成立时，才考虑把 Axon 从 `v0` 提升到 `v1`：

- `session / turn / run` 模型稳定
- 单机会话闭环经过真实持续使用
- ACP 外部 Agent 协作已稳定，不是 demo 级
- Harness 化运行治理成熟
- AG-UI 事件模型稳定
- recap / memory 主线可用
- 多 Agent 数据模型已经证明能承接后续扩展
- 至少有 1 到 2 个长期真实使用场景被反复验证

因此：

**`v1` 不是“功能够多”，而是“核心语义够稳、真实使用够深”。**

---

## 12. 下一步建议

基于本文档，下一步不应立刻把所有后续阶段拆成大量文档，而应优先围绕第一阶段继续往下收敛成实现计划：

1. 按已冻结结构细化 `internal/host/session` 的文件拆分
2. `session / turn / run / event` 的 SQLite schema 草案
3. `ACP bridge` 的 host-facing contract
4. `WS + AG-UI` 的事件映射草案
5. 第一阶段最小 Web 客户端视图与交互流程

建议后续每次只展开一个子专题，保持总纲稳定、细节渐进补齐。
