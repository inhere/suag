# Axon Package Boundary Design

> 状态：Draft for review
> 日期：2026-04-23
> 范围：Axon `pkg/` 与 `internal/` 包边界、目录组织原则、渐进迁移策略
> 相关文档：`docs/v0-self-host-design.md`、`pkg/agentcore/docs/architecture.md`、`pkg/agentcore/docs/block-result-design.md`

---

## 1. 目的

这份文档用于回答一个已经开始影响 v0 主线演进的问题：

当前项目目录逐步同时承载了：

- 底层 runtime / provider / tool / memory 能力
- Host、session、gateway、AG-UI、ACP bridge 等产品级流程
- SQLite、迁移、recap、learning、recall 等业务状态与持久化逻辑

如果继续无约束地向 `internal/` 追加新能力，后续很容易变成一个按历史堆出来的大杂烩，导致：

- 包边界逐步模糊
- 业务逻辑和基础设施互相穿透
- 新增 `model/repository/dto/data` 后进一步失控
- 后续难以继续演进、抽离和维护

本文档的目标不是立刻触发一轮大规模迁移，而是先把边界定清楚，让后续新增代码有明确放置规则，并为渐进迁移提供标准。

---

## 2. 核心判断

### 2.1 `pkg/` 应保守准入

`pkg/` 不应变成“看起来像基础能力”的回收站。

只有满足以下条件的包，才适合进入 `pkg/`：

- 与 Axon 当前业务语义无关
- 不依赖 `session / turn / run / host / capability / learning` 等应用层概念
- 依赖方向纯净，不反向依赖 `internal/`
- API 已具备相对稳定性，值得被长期复用

换句话说，`pkg/` 放的是真正的可复用包，而不是暂时还没想清楚归属的包。

### 2.2 `internal/` 应按业务域组织

`internal/` 的推荐组织方式不是全局技术分层，而是按领域/业务域组织。

不推荐顶层这样铺开：

```text
internal/
  model/
  repository/
  service/
  dto/
  request/
  response/
  dao/
  data/
```

因为这样会把所有业务重新按技术角色混在一起，长期仍然会退化成另一种大杂烩。

推荐方式是：

- 顶层按业务域拆分
- 领域内部如有需要，再出现 `model / repository / dto / service`

例如：

```text
internal/session/
  model.go
  service.go
  repository.go
  dto.go
```

而不是把所有领域的 model/repository/service 拉到全局顶层。

### 2.3 `internal/platform` 是过渡区，不是长期主干

当前 `internal/platform` 已经承载了一部分“相对基础但仍未完全定型”的包。

本设计确认：

- `internal/platform` 在一段时间内继续保留
- 但它的目标状态是“渐进清空或显著缩小”
- 新增代码原则上不再默认进入 `internal/platform`

也就是说：

- `platform` 只做存量收口与迁出过渡
- 不再作为长期主干层持续扩张

### 2.4 `session/turn/run` 是最高优先级候选业务域

当前 `session / turn / run` 还挂在 `internal/host/session` 下。

本设计不要求立即将其提升到 `internal/session`，但明确判断：

- 它是最重要、最有可能首先提升为顶层业务域的模块
- 当它开始形成独立的 model/repository/query/dto/service 体系时，应优先考虑提升为 `internal/session`

短期继续留在 `internal/host/session`，可以减少当前 v0 主线上的 import churn。

---

## 3. 目标结构

推荐的长期目录骨架如下：

```text
cmd/
  axon/

pkg/
  agentcore/
    storage/
      files/
      sqlite/
  client/
  # 候选，待稳定后再抽：
  # llm/
  # tools/
  # xtrace/

internal/
  app/
  config/

  host/
    session/      # 当前保留；未来可升为 internal/session
    gateway/
    agui/
    acpbridge/
    intrunner/

  capability/
    runtimecaps/
    skills/
    # 后续可加：planner/ policy/ catalog/

  learning/
  workspace/
  ops/

  platform/
    llm/
    memory/
    tools/
    tracing/

  agent/
  orchestrator/
  team/
```

这个结构的含义是：

- `pkg/` 保持小而硬，只放真正稳定的公共能力
- `internal/host` 继续承载 Axon Host 产品域
- `internal/capability`、`internal/learning`、`internal/workspace` 是未来业务域
- `internal/platform` 明确为过渡区

---

## 4. 顶层目录规则

### 4.1 `pkg/`

适合放：

- `pkg/agentcore`
- 未来稳定后的 provider/tool/migrate/tracing 等通用包

不适合放：

- 与 Axon Host/session/learning/capability 强绑定的实现
- 还在快速试验中的半成品包
- 主要仅被当前 Axon app 内部使用、且边界未稳定的包

### 4.2 `internal/app`

职责：

- 应用装配
- 初始化 provider / registry / capability manager / host runner / scheduler
- `cmd/axon` 的主要入口 wiring

不负责：

- 业务规则本身
- 通用基础设施抽象

### 4.3 `internal/config`

职责：

- Axon 应用配置读取、解析、默认值与环境变量映射

不负责：

- 通用 SDK config helper
- 业务流程控制

### 4.4 `internal/host`

职责：

- Axon Host 主产品域
- 对外会话入口
- 运行中状态、事件网关、AG-UI 映射、ACP 适配、内部 runner 适配

这一层是产品化宿主层，不应被误判成通用基础设施。

### 4.5 `internal/capability`

职责：

- tool policy
- capability policy
- skill matching
- planner hints
- 资源提示与能力治理

它属于 Axon 自身的能力治理域，而不是通用平台层。

当前目录为 `internal/capabilities`，建议后续收敛为单数 `internal/capability`。

### 4.6 `internal/learning`

职责：

- recap hooks
- memory candidates
- `MEMORY.md` / `USER.md` 相关沉淀
- recall governance

这部分虽然依赖底层 memory/store，但本质上是 Axon 业务能力，不应混入通用 memory 平台包。

### 4.7 `internal/workspace`

职责：

- workspace/project 语义
- 后续工程目录、项目视图、上下文范围控制

当前还未成型，但建议保留为独立业务域方向，而不是未来再塞进 `host` 或 `platform`。

### 4.8 `internal/ops`

职责：

- HITL
- cron
- maintenance/ops 类型流程

这类内容通常是系统运行操作逻辑，适合独立成域。

### 4.9 `internal/platform`

职责：

- 暂时安放归属未最终确定、但相对基础的包
- 作为迁移中转层

约束：

- 不作为长期主干
- 新增代码默认不进入这里
- 未来以持续迁出为目标

---

## 5. 当前包判断与迁移建议

### 5.1 `pkg/agentcore`

- 现状：最清晰的 runtime kernel
- 判断：保持独立，不混入 Axon Host 业务
- 处理：继续作为 `pkg/` 核心包存在

### 5.2 `internal/host/*`

- 现状：Axon Host 主干
- 判断：属于产品域，不应上提到 `pkg`
- 处理：继续保留在 `internal/host`

其中：

- `gateway`、`agui`、`acpbridge`、`intrunner` 都是 Axon Host 适配层
- `session` 当前仍放 `internal/host/session`

### 5.3 `internal/host/session`

- 现状：会话、轮次、run 状态的核心业务包
- 判断：未来优先级最高的候选顶层业务域
- 处理：短期不搬；当开始形成独立 model/repository/query/dto/service 结构时，优先考虑升为 `internal/session`

### 5.4 `internal/capability/*`

- 现状：Axon 能力治理包
- 判断：更像业务域，不像平台层
- 处理：建议后续收敛为 `internal/capability/*`

### 5.5 `internal/platform/llm`

- 现状：provider adapter 与相关 helper，依赖相对干净
- 判断：最有希望未来部分迁入 `pkg/` 的候选
- 处理：短期留在 `platform`；待 provider API 和兼容策略进一步稳定后，再评估是否抽到 `pkg/llm`

### 5.6 `internal/platform/tools`

- 现状：包含 registry、native tools、MCP 适配等
- 判断：不能整体上提 `pkg`
- 原因：
  - 部分内容是通用 tool registry/contract
  - 但 `mcp` 等子模块已引入 Axon config/capability 语义
- 处理：未来只拆真正通用的部分，其余继续留 `internal`

### 5.7 `internal/platform/memory`

- 现状：memory/store 相关实现
- 判断：不宜整体上提
- 原因：
  - memory 很容易与 Axon 的 recall/learning/MEMORY.md/USER.md 业务耦合
  - 通用 working memory contract 更应该集中在 `pkg/agentcore`
- 处理：暂留过渡区，未来仅在明确“通用 memory”与“Axon learning/recall 业务”边界后再拆

### 5.8 `internal/platform/tracing`

- 现状：应用内部 tracing 能力
- 判断：暂不需要进入 `pkg`
- 处理：继续留在 `platform`

### 5.9 `pkg/agentcore/storage/sqlite`

- 现状：已作为 agentcore SQLite storage adapter 迁入 `pkg/agentcore/storage/sqlite`
- 判断：属于 `agentcore.Storage` 的第一方实现层，不属于 Axon Host 业务仓储
- 处理：后续 MySQL/PostgreSQL 等 runtime storage backend 也应放在 `pkg/agentcore/storage/*`

### 5.10 `internal/orchestrator`

- 现状：早期编排层
- 判断：是否继续扩大取决于 Host 主线与 team/multi-agent 主线的关系
- 处理：短期保留，不作为新功能默认落点

### 5.11 `internal/team`

- 现状：偏未来多 agent / team 方向
- 判断：当前不是 v0 主干
- 处理：保留，但暂不围绕它展开目录骨架

---

## 6. 依赖方向规则

推荐依赖方向：

```text
cmd -> internal/app -> internal/domain -> pkg
```

更具体地说：

- `cmd/axon` 只依赖 `internal/app`
- `internal/app` 负责装配，依赖各业务域与 `pkg`
- 业务域可以依赖 `pkg`
- `pkg` 不能依赖 `internal`

禁止方向：

- `pkg/*` import `internal/*`
- `internal/platform/*` 反向依赖高层业务域并逐步演化成“假平台真业务”
- 所有业务域都横向依赖一个全局 `model/repository/service/dto` 顶层目录

---

## 7. 迁移策略

### 7.0 第一批迁移清单

第一批迁移的目标不是“把目录整理得好看”，而是：

- 建立后续新增代码的放置规则
- 先处理边界最清楚、风险最低、收益最高的目录
- 避免在 v0 主线进行一次性大搬家

本轮将现有包分成 3 类：

- `Batch 1A`：可以直接迁或直接重命名收敛
- `Batch 1B`：需要先拆职责，再决定迁移目标
- `Batch 1C`：暂不迁，只冻结边界并持续观察

#### Batch 1A：低风险、直接收敛

##### 1. `internal/capability/* -> internal/capability/*`

- 当前状态：能力治理已经形成稳定业务域
- 迁移动作：
  - `internal/capability/runtimecaps -> internal/capability/runtimecaps`
  - `internal/capability/skills -> internal/capability/skills`
- 迁移原因：
  - 从复数“很多零散能力”收敛为单数“能力治理域”
  - 与后续 `planner / policy / catalog` 扩展方向一致
- 风险等级：低
- 建议时机：近期优先处理

##### 2. 冻结 `internal/platform` 新增入口

- 当前状态：`internal/platform` 仍在作为过渡区使用
- 迁移动作：
  - 不改目录
  - 但从本轮开始新增代码默认不进入 `internal/platform`
  - 如确需暂放，PR/提交说明里要明确写出“为什么是过渡放置”
- 迁移原因：
  - 防止过渡区继续膨胀
- 风险等级：低
- 建议时机：立即生效

##### 3. 为 `internal/host/session` 标记未来提升目标

- 当前状态：session/turn/run 仍挂在 `internal/host/session`
- 迁移动作：
  - 当前不改 import path
  - 但后续新增 session 相关代码优先继续收拢到这个目录，不分散到 `host` 其他包
  - 当出现独立 repository/query/dto/service 结构时，再整体提升为 `internal/session`
- 迁移原因：
  - 先保持主线稳定
  - 为后续顶层业务域提升创造条件
- 风险等级：低
- 建议时机：立即生效

#### Batch 1B：先拆职责，再迁

##### 4. `pkg/agentcore/storage/sqlite`

- 当前状态：已从 `internal/acstore` 迁入 `pkg/agentcore/storage/sqlite`
- 当前判断：
  - 它只表达 `agentcore` runtime storage
  - 没有混入 Axon Host/session 业务语义
- 第一阶段动作：
  - 已迁入 `pkg/agentcore/storage/sqlite`
  - 构造函数收敛为 `sqlite.NewStorage(...)`
  - 调用方已改为依赖 `pkg/agentcore/storage/sqlite`
- 第二阶段目标：
  - 后续如支持 PostgreSQL/MySQL，放入 `pkg/agentcore/storage/postgres` 与 `pkg/agentcore/storage/mysql`
  - 等第二个 SQL backend 出现后，再评估是否抽 shared `sqlstore`
- 风险等级：已完成，后续观察
- 建议时机：已完成

##### 5. `internal/platform/llm`

- 当前状态：provider adapter 已相对独立，但仍与当前应用 wiring、debug 策略一起演进
- 当前判断：
  - 是最有希望上提为 `pkg/llm` 的候选
  - 但现在还处在 provider 兼容收口阶段，不适合立刻迁出
- 第一阶段动作：
  - 保持目录不动
  - 继续收 provider contract、mock、compatibility tests
  - 确认哪些是通用 provider adapter，哪些是 Axon 应用构造逻辑
- 第二阶段目标：
  - 将通用 provider adapter 抽到 `pkg/llm`
  - 将基于 config 的 provider 构建逻辑继续留在 `internal/config` / `internal/app`
- 风险等级：中
- 建议时机：在 provider 兼容稳定 1-2 轮后处理

##### 6. `internal/platform/tools`

- 当前状态：registry/native tools/MCP 适配混在一起
- 当前判断：
  - 不能整体迁
  - 应先拆“通用 tool contract”与“Axon capability / config / MCP wiring”
- 第一阶段动作：
  - 梳理子模块边界：
    - registry / schema / base contract
    - native file/shell/search tools
    - MCP client / MCP adapter / Axon wiring
  - 明确 `internal/platform/tools/mcp` 为什么依赖 `internal/config` 与 `internal/capability`
- 第二阶段目标：
  - 若 registry/contract 足够纯净，可单独抽出
  - native tools 视是否独立复用再判断
  - `mcp` 大概率拆成通用 client 与 Axon adapter 两层
- 风险等级：中
- 建议时机：晚于 `acstore`、晚于 `llm`

#### Batch 1C：先冻结观察，不主动迁

##### 7. `internal/platform/memory`

- 当前状态：memory/store 语义与 learning/recall 边界仍在演进
- 当前判断：
  - 暂不适合整体迁入 `pkg`
  - 容易把 Axon 的 recall/learning 业务误抽成“通用 memory”
- 当前动作：
  - 不迁
  - 后续在实现 `internal/learning` 时，把业务语义逐步从这里抽出
- 风险等级：中
- 建议时机：等待 learning 业务域更成型后再判断

##### 8. `internal/platform/tracing`

- 当前状态：应用内部 tracing 能力
- 当前判断：
  - 暂无足够理由进入 `pkg`
  - 可以继续作为内部实现存在
- 当前动作：
  - 不迁
  - 保持 API 尽量窄，避免向外扩散依赖
- 风险等级：低

##### 9. `internal/orchestrator` 与 `internal/team`

- 当前状态：都更偏未来演进方向，而不是 v0 当前最核心主线
- 当前判断：
  - 现在不应围绕它们来设计目录骨架
- 当前动作：
  - 保留
  - 不新增与主线无关的大量代码
  - 等 host/session/capability/learning 主线更稳定后再重评
- 风险等级：中

### 7.1 第一阶段：先定规则，不大搬

当前阶段优先目标：

- 明确包边界规则
- 明确 `internal/platform` 是过渡区
- 明确新增业务默认按领域组织

当前不做：

- 全局大规模目录迁移
- 一次性把 `platform/*` 全抽到 `pkg`
- 一次性把 `host/session` 提升为顶层

### 7.2 第二阶段：低风险收敛

建议优先做：

- `internal/capabilities -> internal/capability`
- 给重要目录补文档说明
- 在新增业务时严格按新规则放置

### 7.3 第三阶段：迁出最干净的基础包

优先候选顺序建议：

1. `pkg/agentcore/storage/sqlite`，已完成
2. `internal/platform/llm`
3. `internal/platform/tools` 中真正通用的 registry/contract 部分
4. `internal/platform/tracing`

### 7.4 第四阶段：提升成熟业务域

当某个领域开始拥有独立数据模型、查询模型、repository/service/dto 结构时，再提升为顶层领域目录。

当前最高优先级候选：

- `internal/host/session -> internal/session`

---

## 8. 明确不做的事情

本设计明确不做：

- 不把当前所有“看起来通用”的包立刻上提到 `pkg`
- 不做一次性大迁移
- 不引入全局 `model/repository/service/dto/request/response/dao/data` 顶层目录
- 不把 `internal/platform` 当成长期稳定主干继续扩张

---

## 9. 当前结论

本轮确认的结论是：

- Axon 应采用“领域优先”的 `internal` 组织方式
- `pkg/` 保持保守准入
- `internal/platform` 作为过渡区继续存在，但以后只减不增
- `session/turn/run` 当前继续留在 `internal/host/session`，但被明确标记为未来最优先的顶层业务域候选

这样可以在不打断当前 v0 主线的前提下，给后续业务扩展、目录迁移和底层抽离建立一套明确标准，避免 `internal` 继续无序膨胀。
