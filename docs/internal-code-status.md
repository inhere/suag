# Internal Code Status

> 状态：Current
> 更新时间：2026-04-10
> 用途：说明 `internal/*` 当前哪些模块可以复用，哪些需要重审，哪些仅应作为历史参考

---

## 1. 文档目的

当前 `internal/*` 下相当一部分代码是沿着旧 `v1 foundation` 路线演进出来的。

这意味着：

- 它们并不自动等于当前 `v0` 的正确架构
- 它们的存在不应反向定义新的 `session / turn / run` 主线
- 它们更适合作为“候选资产库”，而不是“当前事实来源”

本文档的目标是降低误判成本，帮助后续开发时快速判断：

- 哪些模块可以优先复用
- 哪些模块需要在新模型下重新验证
- 哪些模块目前只适合当历史参考

---

## 2. 读取优先级

在阅读或复用 `internal/*` 代码时，建议遵循下面的优先级：

1. 当前 `v0` 设计文档
2. 当前 `v0` 实施计划
3. 新增 contract / schema / tests
4. `internal/*` 现有实现
5. 旧 `v1` 文档

也就是说：

- 旧代码不再定义新架构
- 旧模型不再约束新数据边界
- 只有和当前 `v0` 主线对齐的旧模块，才值得被吸纳

---

## 3. 模块分层结论

当前建议把 `internal/*` 分成 3 类：

1. 可复用候选
2. 需重审后复用
3. 历史参考优先

---

## 4. 可复用候选

这类模块更偏基础设施或桥接层，和当前 `v0` 主线的冲突较小，可以优先评估复用。

### `internal/agentcorestorage`

状态：`可复用候选`

原因：

- 已经围绕 `pkg/agentcore` 做了独立 SQLite 存储实现
- 语义相对聚焦，边界清楚
- 更接近 run-level authority storage，而不是旧产品层编排模型

建议：

- 可以继续借鉴其 SQLite schema 初始化、序列化和错误包装方式
- 但不要把它直接扩展成新的 `session / turn / run` host state store

### `internal/agentcoreadapters`

状态：`可复用候选`

原因：

- 主要职责是把旧 `internal/*` 能力桥接到 `pkg/agentcore`
- 与当前“让 `agentcore` 继续保持 kernel 边界”的方向一致
- 不直接承载产品层协议和控制面语义

建议：

- 后续若新主线仍需复用旧 `llm / tools / memory` 能力，可以优先从这里走 adapter 路径
- 不要反向让 adapter 成为产品层主入口

### `internal/llm`

状态：`可复用候选`

原因：

- Provider 能力相对独立
- OpenAI / Claude / DeepSeek / mock 这些实现可以继续作为基础能力层使用
- 与 `session / turn / run` 新模型耦合较弱

建议：

- 继续作为底层 provider 实现保留
- 通过 adapter 或更窄的 runtime-facing contract 对接，不要让上层 host 直接依赖旧调用习惯

### `internal/tracing`

状态：`可复用候选`

原因：

- Trace / Span 的基础模型仍有价值
- 作为内部观测基础设施，可以在新主线中继续存在
- 和业务会话模型相关，但不是强绑定

建议：

- 可保留其基本 Span/Trace 抽象
- 后续要重新对齐 `sessionId / turnId / runId / traceId` 的关系，不要默认沿用旧 trace 粒度

---

## 5. 需重审后复用

这类模块通常不是完全不能用，而是旧模型色彩比较重。是否复用，必须先用新的 `v0` 边界重新审一次。

### `internal/gateway`

状态：`需重审后复用`

原因：

- 当前以 `REST + SSE + trace` 为中心
- 但新的 `v0.self-host` 主链已经转向 `WebSocket + session-aware event flow`
- 现有 server、handler、eventbus 外壳仍然有复用价值

建议：

- 可继续借用 HTTP server、router、基础事件总线思路
- 但事件模型、订阅键、会话入口、WS 主链都需要按新主线重构
- 不应再以当前 `task/trace/SSE` 接口为未来事实标准

### `internal/memory`

状态：`需重审后复用`

原因：

- In-memory 与 SQLite 方案仍有基础价值
- 但它们围绕旧 `agent/task` 语义设计，不是新的 host-side memory layering
- 当前 `v0` 需要的是更清晰的三层边界：
  - authority transcript
  - host runtime state
  - memory recap / retrieval

建议：

- 可保留局部实现经验，尤其是 SQLite 与短期窗口机制
- 但不要直接把旧 `memory.Store` 当作新的长期记忆主设计

### `internal/tools`

状态：`需重审后复用`

原因：

- Native tools、MCP client、skills parser/runner 仍有潜在复用价值
- 但工具的产品级使用方式已经和新的 `ACP / session runtime / event flow` 主线存在差异

建议：

- 逐项按“是否仍符合当前 host/runtime 边界”来吸纳
- 可以把它们当作工具能力实现库，而不是当前 tool governance 模型本身

### `internal/hitl`

状态：`需重审后复用`

原因：

- HITL 概念仍然成立
- 但旧实现更偏“agent 连续失败后挂起”的局部机制
- 新主线中，`INPUT_REQUIRED / AUTH_REQUIRED / continue turn` 会是更高层的标准状态

建议：

- 可参考 ticket 与 manager 的基本思路
- 但应该让它服务于新的 `session / turn / run` 状态机，而不是反过来定义状态机

### `internal/cron`

状态：`需重审后复用`

原因：

- “Agentic Cron” 方向本身仍然成立
- 但当前还不是 `v0.self-host` 第一阶段核心
- 旧实现建立在更早的 orchestrator 模型之上

建议：

- 暂时保留
- 等 `session runtime` 主干稳定后，再决定如何对接系统触发型 turn

---

## 6. 历史参考优先

这类模块最容易让人误以为“平台雏形已经存在”，但它们与当前新主线的偏差也最大。

### `internal/agent`

状态：`历史参考优先`

原因：

- 当前核心模型仍然是 `Agent / Task / Runner`
- 与新的 `Session / Turn / Run` 主线不一致
- 虽然里面已有 `agentcore` bridge，但外层语义仍然偏旧

建议：

- 可以参考其中已有的 runner 经验与 adapter 过渡方式
- 但不要让 `agent.Task` 成为新的产品层主对象

### `internal/orchestrator`

状态：`历史参考优先`

原因：

- 当前职责是旧总协调器
- 入口还是 `RunTask(description string)` 这种旧单任务模型
- 容易把新系统重新拉回“task/trace 优先”的旧路径

建议：

- 最多作为过渡适配层参考
- 新的主干应从 `session runtime` 开始，而不是从扩展旧 orchestrator 开始

### `internal/team`

状态：`历史参考优先`

原因：

- 明显建立在旧多 Agent 团队路线之上
- 当前 `v0.self-host` 还没有进入完整 team / leader / worker 编排阶段
- 若过早继续沿用，会把新主线又拉回旧 v1 目标

建议：

- 保留为未来多 Agent 阶段的历史样本
- 当前阶段不应作为主开发基座

---

## 7. 当前建议的使用策略

如果要把一句话落成执行策略，建议是：

**新的 `v0` 开发，先在新命名空间里建立主干，再按需吸纳旧资产。**

具体来说：

- 新主干优先进入新的包边界，例如：
  - `internal/host/session`
  - `internal/host/acpbridge`
  - `internal/host/agui`
  - `internal/host/gateway`
- 不要一开始就在旧 `orchestrator / team / agent` 上直接加层
- 旧模块如果要继续使用，应先通过窄接口或 adapter 被吸纳

---

## 8. 目标结构迁移映射

本节用于回答一个更具体的问题：

**如果后续要把现有 `internal/*` 朝目标结构收敛，每个包优先迁到哪里。**

建议按下面 4 种迁移类型理解：

1. 直接迁移/重命名优先
2. 目录收敛，语义大体保留
3. 保留代码资产，按新主线重构迁入
4. 暂不迁移，先留作历史参考

### 8.1 直接迁移/重命名优先

#### `internal/agentcorestorage -> internal/acstore`

类型：`直接迁移/重命名优先`

原因：

- 语义已经比较聚焦
- 新名字更短，也更符合当前冻结结构
- 仍然是 `agentcore` 周边的具体实现层

#### `internal/agentcoreadapters -> internal/acadapters`

类型：`直接迁移/重命名优先`

原因：

- 当前定位本就是 adapter 层
- 新名字更短，且与 `acstore` 保持并列关系
- 不需要重新定义职责，只需要更清晰地落位

### 8.2 目录收敛，语义大体保留

#### `internal/llm -> internal/platform/llm`

类型：`目录收敛，语义大体保留`

原因：

- 基础 provider 能力仍成立
- 更适合作为 `platform` 层基础能力收拢

#### `internal/tools -> internal/platform/tools`

类型：`目录收敛，语义大体保留`

原因：

- 工具能力仍可保留
- 但后续应按当前 host/runtime 边界逐项重审具体工具

#### `internal/memory -> internal/platform/memory`

类型：`目录收敛，语义大体保留`

原因：

- 模块仍有基础价值
- 但设计必须重新对齐当前 `authority / host state / recap retrieval` 三层边界

#### `internal/tracing -> internal/platform/tracing`

类型：`目录收敛，语义大体保留`

原因：

- Trace / Span 的基础抽象仍有价值
- 但 trace 与 `session / turn / run` 的关系需要重新对齐

#### `internal/hitl -> internal/ops/hitl`

类型：`目录收敛，语义大体保留`

原因：

- 更适合作为运行治理模块放进 `ops`
- 但要避免让旧 HITL 语义继续主导新的 host 状态机

#### `internal/cron -> internal/ops/cron`

类型：`目录收敛，语义大体保留`

原因：

- 更适合作为运行治理/系统操作相关能力放进 `ops`
- 当前可先迁目录，不急着立即深入重构

### 8.3 保留代码资产，按新主线重构迁入

#### `internal/gateway -> internal/host/gateway`

类型：`保留代码资产，按新主线重构迁入`

原因：

- 现有 server、router、eventbus 外壳仍有复用价值
- 但核心语义要从 `REST + SSE + trace` 迁到 `WS + session-aware event flow`

### 8.4 暂不迁移，先留作历史参考

#### `internal/agent`

类型：`暂不迁移`

原因：

- 当前仍围绕 `Agent / Task / Runner`
- 与新的 `Session / Turn / Run` 主线不一致

#### `internal/orchestrator`

类型：`暂不迁移`

原因：

- 旧总协调器语义太重
- 不适合作为新 host 主干的直接延续

#### `internal/team`

类型：`暂不迁移`

原因：

- 属于后续多 Agent 阶段能力
- 当前若过早迁入，会把 `v0.self-host` 重新拉回旧 v1 路线

### 8.5 一句话迁移策略

如果把迁移策略压缩成一句话，建议是：

**先迁 agentcore 周边与基础能力层，再迁 host 接入层，最后再决定是否吸纳旧 `agent / orchestrator / team`。**

---

## 9. 推荐后续动作

为了减少后续误判，建议后面继续做两件事：

1. 当 `internal/host/session` 真正建立后，在本文档中补一节“新主干模块”说明
2. 当某个旧模块已经被明确替代后，在本文档中标注“已被新模块取代”

这样 `internal/*` 的可信度会随着项目演进逐步更新，而不是一直停留在模糊状态。
