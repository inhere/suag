# Axon Web Foundation Design

> 状态：Draft for review
> 日期：2026-04-12
> 范围：Axon `v0` Web 客户端第一阶段基础工作台
> 相关文档：`docs/v0-self-host-design.md`、`docs/usage/host-state-snapshot-v0.md`
> 实现计划：`docs/superpowers/plans/2026-04-12-axon-web-foundation-implementation-plan.md`

---

## 1. 目标

这份文档定义 Axon Web 客户端第一阶段的基础工作台设计。

这一阶段的目标不是把完整产品交互一次做完，而是先打通后端主链，并把当前已经落地的 Host 能力稳定呈现出来。

第一阶段前端需要验证的核心问题只有 4 个：

1. 能创建并切换 `session`
2. 能在 `session` 下发起或继续 `turn`
3. 能实时看到 `turn / run / external task` 的状态变化
4. 能在必要时做最小人工干预

对应的主链是：

`Web UI -> HTTP / WS -> session -> turn -> run -> external agent -> STATE_SNAPSHOT / 增量事件 -> 前端更新`

---

## 2. 设计原则

### 2.1 聊天是主入口

Axon 第一阶段的用户心智应是：

**通过聊天把事情办了。**

因此：

- 中间主区仍然是聊天/任务推进主线
- 不做纯控制台首页
- 不把前端做成“另一个后台系统”

### 2.2 追踪与干预是伴随能力

虽然聊天是主入口，但 Axon 第一阶段又必须把过程看清楚。

因此：

- `turn / run / external task` 的状态需要持续可见
- 干预入口需要随手可达
- 这些能力通过右侧运行观察栏承接，而不是把聊天区变成日志流

### 2.3 聚合视图优先于原始事件

当前 Host 已经提供：

- `turns`
- `runs`
- `externalMessages`
- `externalTasks`
- `events`

前端第一阶段应优先消费聚合视图，而不是自己重建状态机。

也就是说：

- `turns` 和 `runs` 是主展示输入
- `events` 仅作为调试和底层排查输入

### 2.4 先稳定基础工作台，再扩能力页

第一阶段不把重心放在：

- tools 独立页面
- mcp 连接管理页
- skills 市场或技能管理页

这些能力可以先作为右侧信息块或后续扩展挂点出现，但不应成为第一版主页面主题。

---

## 3. 信息架构

第一阶段采用单页三栏工作台。

```text
+-------------------+--------------------------------------+----------------------+
| Session List      | Conversation                         | Run Inspector        |
|                   |                                      |                      |
| 选择工作线程       | 聊天主线与 turn 概览                  | run / task 状态与干预 |
+-------------------+--------------------------------------+----------------------+
```

页面顶部保留一条较薄的上下文条，用于显示当前：

- workspace
- session
- turn 状态
- 连接状态

---

## 4. 页面结构

### 4.1 顶部上下文条

顶部条回答的问题是：

- 我现在在哪个 workspace
- 当前正在看的 session 是哪个
- 当前 turn 是否还在运行
- 当前连接是否正常

推荐字段：

- `workspace`
- `session title`
- `turn displayState`
- `active run count`
- `connected / reconnecting`

第一阶段保持很薄，不放复杂操作。

### 4.2 左侧 Session List

左侧的定位是：

**紧凑工作列表，而不是聊天列表。**

每个 session item 只展示：

- `title`
- `status`
- `updatedAt`
- 一句 `preview`

推荐结构：

```text
● host runtime v0
running · 2m ago
正在整理页面结构
```

顶部能力：

- `+ New Session`
- 搜索
- 过滤：`All / Running / Waiting`

排序建议：

1. `Running`
2. `Waiting`
3. `Open`
4. `Archived`

同类内部按 `updatedAt desc`

切换行为：

- 点击 session 后切换当前会话
- 如果该 session 有 active run，默认聚焦最近 active run
- 如果没有 active run，默认落到最近 turn 概览
- 恢复该 session 上次的 run 焦点和滚动位置

### 4.3 中间 Conversation 主区

中间区负责：

**任务主线，而不是执行日志。**

第一阶段只保留 4 类块：

1. `user message`
2. `assistant summary`
3. `turn summary card`
4. `input composer`

中间区不应默认出现：

- 原始 `events`
- 每一条 `external message delta`
- 工具调用参数明细
- 大段桥接元数据

这些内容进入右栏或调试抽屉。

`turn summary card` 推荐只做薄卡片，例如：

```text
Turn turn_012 · external_running · 2 active runs
latest: 正在生成页面草图
```

完成态时改为：

```text
Turn turn_012 · completed
summary: 已完成前端基础工作台结构设计
```

### 4.4 右侧 Run Inspector

右侧栏负责：

**执行过程的可见性与最小干预。**

第一阶段固定成 4 个区块：

1. `Current Turn`
2. `Active Runs`
3. `Focused Run`
4. `External Task + Actions`

推荐结构：

#### Current Turn

- `displayState`
- `runCount / activeRunCount`
- `latestMessagePreview`
- `updatedAt`

#### Active Runs

每个 run 卡片显示：

- `agentId`
- `displayState`
- 一行摘要

摘要优先级：

1. `summary`
2. `latestMessageText`
3. 状态占位文案

#### Focused Run

默认聚焦当前 active run。

显示字段：

- `runId`
- `agentId`
- `displayState`
- `latestEventType`
- `externalTaskIds`
- `summary / latestMessageText`

显示优先级固定为：

1. `summary`
2. `latestMessageText`
3. fallback 占位文案

也就是说：

- run 完成后，默认显示 `summary`
- run 运行中，显示 `latestMessageText`

#### External Task + Actions

第一阶段只展示最小桥接信息：

- `adapter`
- `remoteAgentName`
- `state`
- `lastHeartbeatAt`
- `resultSummary`

动作先控制在：

- `Continue`
- `Stop`
- `Add Hint`
- `Debug`

---

## 5. 数据消费约定

前端第一阶段严格遵循 `docs/usage/host-state-snapshot-v0.md`。

消费优先级：

1. `turns`
2. `runs`
3. `externalMessages`
4. `externalTasks`
5. `events`

具体用法：

### `turns`

用于：

- turn 概览
- turn 状态条
- turn summary card

### `runs`

用于：

- Active Runs 列表
- Focused Run 面板
- run 状态与摘要

### `externalMessages`

用于：

- 恢复外部 agent 的流式正文
- 在没有最终 summary 时提供 run 最新消息文本

### `externalTasks`

用于：

- 右侧 External Task 区块
- 展示桥接状态与远端任务信息

### `events`

只用于：

- 调试抽屉
- 时间线排查

不用于主视图渲染。

---

## 6. 基础交互

第一阶段只做以下交互：

### 6.1 Session 交互

- 创建 session
- 选择 session
- 搜索 session
- 按状态过滤 session

### 6.2 Turn 交互

- 在当前 session 发送消息启动 turn
- 在当前 session 继续 turn
- 页面刷新后恢复当前 turn 视图

### 6.3 Run 交互

- 默认聚焦当前 active run
- 点击某个 run 卡片切换焦点
- 在 Focused Run 中展开更多明细

### 6.4 干预交互

- Continue
- Stop
- Add Hint
- 打开 Debug Drawer

第一阶段不做：

- 复杂布局拖拽
- 多标签页 session
- project 管理
- MCP 连接管理
- Tools / Skills 独立页面

---

## 7. 状态更新策略

### 7.1 首屏恢复

进入 session 页面时：

- 先通过 HTTP 获取 session 基础信息
- 再通过 WS 收到 `STATE_SNAPSHOT`
- 基于 snapshot 恢复三栏视图

恢复原则：

- session 视图由 snapshot 驱动
- 不依赖前端本地重算

### 7.2 增量更新

收到增量事件后：

- 优先更新右侧运行观察栏
- 中间主区只在需要时追加轻量 turn summary 或 assistant 可读消息
- 不把每条底层事件直接灌入聊天流

### 7.3 快速完成的 run

如果某个 run 很快完成：

- 短时间保留完成态卡片
- `summary` 替代中间过程消息
- 不让 run “闪一下就消失”

---

## 8. 响应式策略

虽然第一阶段重点是桌面端，但结构上应为窄屏留口。

建议：

- 桌面端：三栏常驻
- 窄屏：左栏和右栏改为抽屉或底部标签面板

窄屏优先级：

1. 中间聊天主区
2. 当前 run 面板
3. session 列表

也就是说，窄屏下仍保持“聊天主入口”，右侧 inspector 退化为可切换面板。

---

## 9. 能力入口的阶段策略

### 9.1 Tools

第一阶段不做独立页面。

如果后端已经提供相关事件，可以先：

- 在右栏显示工具调用摘要
- 或在 Debug Drawer 中显示调用记录

### 9.2 Skills

第一阶段不做技能管理页。

后续可先作为：

- 建议卡
- run 关联能力提示

挂在右栏能力区。

### 9.3 MCP

第一阶段不做 MCP 连接管理 UI。

它可先被视为：

- tools 的来源
- external task 的一部分背景信息

后续宿主主链稳定后，再考虑单独的 MCP 连接与治理页面。

---

## 10. 第一阶段验收标准

当前设计的完成标准是：

1. 能创建并切换 session
2. 能在 session 中发起 turn
3. 能继续同一 session 的后续 turn
4. WS 连上后能收到首包 `STATE_SNAPSHOT`
5. 页面刷新后能基于 `STATE_SNAPSHOT v0` 恢复视图
6. 右栏能显示 active run 与 external task
7. run 完成后状态与摘要能正确收敛
8. 可以做最小人工干预

如果上述 8 点成立，就说明第一阶段前后端基础工作台已经打通。

---

## 11. 后续阶段

在基础工作台打通后，再继续考虑：

- 更细的产品交互
- Tools / Skills 的独立能力呈现
- MCP 连接管理
- 更丰富的时间线和调试体验
- 更完整的视觉系统与动效

也就是说：

**第一阶段的重点是打通后端功能与基础工作流，不是提前做完所有产品交互。**
