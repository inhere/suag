# Host Session Layout Note

> 状态：Current
> 日期：2026-04-10
> 作用：冻结 `internal/host/session` 第一批文件拆分，避免开工时边写边拆
> 相关文档：`docs/v0-self-host-design.md`、`docs/superpowers/plans/2026-04-10-axon-v0-self-host-foundation-plan.md`

---

## 1. 目的

本文档不是新的实现计划，也不是新的产品设计。

它只回答一个问题：

**`internal/host/session` 第一批文件应该怎么拆，才能既不做成大杂烩，也不因为过早拆碎而增加维护成本。**

原则是：

- 一开始先按稳定边界拆
- 文件数保持克制
- 允许后续再细分，但第一批不要过度设计

---

## 2. 第一批建议文件

第一批建议只建立下面 7 个文件：

```text
internal/host/session/
  doc.go
  ids.go
  types.go
  store.go
  sqlite.go
  service.go
  events.go
```

测试文件与实现文件一一跟随：

```text
internal/host/session/
  sqlite_test.go
  service_test.go
```

如果第一批实现仍然比较轻，可以先不单独建 `events.go`，但我建议预留它，避免 `types.go` 过快膨胀。

---

## 3. 每个文件的职责

### `doc.go`

职责：

- 包注释
- 明确这是 host-side session lifecycle 模块
- 说明它不等于 `pkg/agentcore`

不要放：

- 真实逻辑
- 大段常量定义

### `ids.go`

职责：

- `session / turn / run / event / external task / recap` 的 ID 生成函数
- `前缀 + ULID` 规则
- 如有需要，放最小的 ID 校验辅助

放在独立文件的原因：

- ID 规则已经冻结，是稳定边界
- 后面如果改生成策略，不会污染 `types.go` 或 `service.go`

### `types.go`

职责：

- `Session`
- `Turn`
- `Run`
- `RunEvent`
- `ExternalTask`
- `Recap`
- 各类状态枚举

不要放：

- SQLite 读写逻辑
- service 方法
- 事件发布逻辑

控制原则：

- 只放领域对象与状态
- 不要把 DTO 和数据库行模型再单独拆一个 `dto.go`

### `store.go`

职责：

- host session store interface
- 查询接口
- 事务边界抽象
- 存储层错误语义

不要放：

- SQLite 具体实现
- service 编排逻辑

建议：

- 这里定义接口
- `sqlite.go` 提供第一版实现

### `sqlite.go`

职责：

- SQLite schema 初始化
- `sessions / turns / runs / run_events / external_tasks / recaps` 的具体存取实现
- append-only event persistence

不要放：

- 业务编排逻辑
- 对外事件推送

控制原则：

- 面向 `store.go` 里的接口实现
- 不要直接长出高层 service 行为

### `service.go`

职责：

- `StartTurn`
- `ContinueTurn`
- run state transitions
- 调用 runner / adapter
- 调用 store 落状态
- 触发事件发布

这是最容易变大的文件，所以第一批就要控制：

- 只保留 host lifecycle 编排
- 不把 WebSocket、HTTP handler、AG-UI mapping 写进来
- 不把 ACP 具体协议实现写进来

如果后面逻辑增长明显，再拆：

- `continue.go`
- `transitions.go`
- `commands.go`

但第一批先不用拆。

### `events.go`

职责：

- session 层内部事件名常量
- event factory helpers
- 统一事件 payload 形状

建议把事件相关的轻量辅助提前独立出来，原因是：

- 后面 `gateway` 和 `agui` 都会依赖它
- 可以避免 `types.go` 被事件常量淹没

---

## 4. 第一批不建议单独创建的文件

下面这些名字现在先不要建：

- `dto.go`
- `model.go`
- `repository.go`
- `manager.go`
- `controller.go`
- `commands.go`
- `transitions.go`

原因很简单：

- 当前模块还没复杂到值得拆这些层
- 这些名字很容易让目录看起来“很完整”，但实际只是把少量逻辑打散

第一批先用：

- `types.go`
- `store.go`
- `sqlite.go`
- `service.go`
- `events.go`

就够了。

---

## 5. 与其他模块的边界

### `internal/host/session` 不负责

- WebSocket 连接管理
- HTTP handler
- AG-UI 协议映射
- ACP 协议细节
- `agentcore` 内核 loop

### `internal/host/session` 负责

- host-side lifecycle
- `session / turn / run` 事实状态推进
- host 级事件生成
- 和 runner / adapter / store 的协调

因此它和邻近模块的关系应是：

```text
host/gateway   -> 接入与推送
host/session   -> 生命周期主干
host/acpbridge -> 外部 Agent 协作
pkg/agentcore  -> 单 Agent runtime kernel
```

---

## 6. 一句话规则

如果实现过程中对“这个新函数/类型该放哪”犹豫，可以用下面这句来判断：

**属于 host-side lifecycle，就进 `service.go`；属于领域对象，就进 `types.go`；属于存储契约，就进 `store.go`；属于 SQLite 落盘，就进 `sqlite.go`；属于事件定义，就进 `events.go`；属于 ID 规则，就进 `ids.go`。**

这样第一批实现就能先稳稳落地，不需要一开始就演化成大而碎的目录结构。
