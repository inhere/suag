# Axon Web Chat-First Layout Design

> 状态：Draft for review
> 日期：2026-04-21
> 范围：Axon `v0` Web 客户端默认 `Chat` 视图的信息架构与布局收敛
> 相关文档：`docs/v0-self-host-design.md`、`docs/superpowers/specs/2026-04-12-axon-web-foundation-design.md`、`docs/usage/web-host-local-dev.md`
> 实现计划：待本文确认后编写

---

## 1. 目标

这份文档定义 Axon Web 当前默认界面的收敛方向：从“偏工作台的三栏观察视图”回到“聊天主窗口优先”的默认体验。

这次调整的目标不是重做完整产品，而是先解决一个更基础的问题：

**用户进入 Web 后，应该先顺畅地通过聊天把事情办了，而不是先学习 run inspector。**

因此这一轮设计需要同时满足 4 个条件：

1. 默认视图看起来像聊天产品，而不是调试后台
2. Host `session / turn / run` 主链仍然可见、可恢复、可干预
3. `HITL / waiting / failed` 这类关键状态不会因为“聊天化”而被隐藏
4. 后续仍能自然演进到更专业的 `Workbench` 视图，而不是推翻重做

---

## 2. 当前问题

现有 Web 主壳虽然已经打通了后端链路，但默认视觉重心仍然偏向运行态观察：

- 中间区不够“聊天主窗口”
- 右侧 `Run Inspector` 过强，容易抢掉主任务心智
- 用户在发送一条消息后，需要同时理解 `turn / run / external task / focused run`
- 对于很快完成的运行，这套心智负担是不必要的

这会带来两个直接问题：

1. 首次使用时，界面复杂度高于用户任务本身
2. 后续如果继续在默认页叠更多运行态信息，页面会越来越像“控制台”，而不是“通过聊天完成工作”的产品

所以这次收敛的核心不是删能力，而是重排能力的默认显隐层级。

---

## 3. 设计结论

### 3.1 默认进入 `Chat` 模式

Axon Web 默认视图采用 `Chat` 模式。

它的核心目标是：

**通过聊天把事情办了。**

在这个模式下：

- 中间主区始终是最大、最重要的区域
- 会话消息流是主角
- 运行态只以“必要状态反馈”的方式进入主视图
- 更重的追踪和调试能力保留，但退到次级区域

### 3.2 布局仍然保持左中右三栏

默认桌面布局继续保留三栏结构：

```text
+-------------------+--------------------------------------+----------------------+
| Sessions          | Chat                                 | Context              |
|                   |                                      |                      |
| 会话切换与新建      | Header / Summary / Messages / Input  | HITL / Memory / Run  |
+-------------------+--------------------------------------+----------------------+
```

但三栏的职责重新定义为：

- 左栏：切换会话
- 中栏：推进聊天主线
- 右栏：补充上下文与人工干预

也就是说，结构不变，但默认焦点改变。

### 3.3 选择 `B + 2` 方案

经过收敛，默认视图采用下面这套组合：

- `B`：聊天主窗口 + 少量内联状态
- `2`：消息流上方保留一条很薄的当前 `turn / run` summary strip

这代表中间区的设计取向是：

- 保持聊天体验
- 允许关键状态直接被用户看见
- 不把持续运行日志和细碎事件灌进消息流

---

## 4. 设计原则

### 4.1 聊天优先，不等于状态消失

默认页回归聊天，不意味着把运行态藏起来。

相反，设计要求是：

- `running`
- `waiting for input`
- `needs approval`
- `failed`

这些用户必须感知的状态，仍要在默认视图中被清晰看到。

区别只在于：

- 不再以“大量 inspector 面板”作为默认入口
- 而是通过 summary strip、内联状态卡、右侧轻量上下文来承接

### 4.2 状态可见，但日志不可见

默认 `Chat` 视图里，需要刻意压制原始过程噪音。

推荐边界是：

- 状态可见
- 干预可达
- 细节可查
- 日志不抢戏

也就是：

- 用户能知道当前卡在哪
- 用户能继续、审批、处理中断
- 用户需要细节时有地方看
- 但默认不需要理解整条事件流

### 4.3 中栏回答主线，右栏回答干预

中栏负责回答：

**现在和我对话的主线是什么？**

右栏负责回答：

**如果我要追踪、介入、审阅，还有哪些上下文？**

这个职责边界一旦混掉，页面就会重新回到“半个聊天窗 + 半个工作台”的状态。

### 4.4 默认产品入口与深度视图分离

默认 `Chat` 视图的任务是承接主流程。

更重的追踪、审计、调试视图属于未来 `Workbench` 模式。

这意味着现在的设计需要给未来留空间，但不能提前把默认页做成 `Workbench-lite`。

---

## 5. 信息架构

默认 `Chat` 视图的信息架构定义如下：

### 5.1 左栏：`Sessions`

职责：

- 新建会话
- 切换会话
- 快速判断哪个会话值得关注

不负责：

- 展开 run 明细
- 呈现 external task 细节
- 承担 turn 时间线

### 5.2 中栏：`Chat`

职责：

- 展示消息主线
- 展示当前 `turn / run` 的薄状态摘要
- 在必要时提供对当前运行态的直接感知
- 承接用户发送消息与继续输入

不负责：

- 展示完整运行日志
- 展示多 run 追踪列表
- 展示大段调试元数据

### 5.3 右栏：`Context`

职责：

- 放置辅助信息
- 放置待处理人工介入
- 放置轻量 run 细节

它是“可用时很重要，但默认不抢戏”的区域。

---

## 6. 页面结构

### 6.1 整体骨架

默认桌面端骨架固定为：

`Left Sessions -> Center Chat -> Right Context`

中栏内部结构固定为：

`Header -> Thin summary strip -> Message stream -> Composer`

这样可以保证：

- 顶部看到当前会话与连接状态
- 中部始终是消息主线
- 底部输入区稳定，不因右栏状态变化跳动

### 6.2 左栏 `Sessions`

左栏顶部只保留一个主动作：

- `+ New Session`

会话列表每项只显示最小必要字段：

- `title`
- `preview`
- `updatedAt`
- `轻量状态点`

状态点建议只表达粗粒度关注级别：

- `idle`
- `running`
- `waiting`
- `attention`

其中：

- `running` 表示当前仍有活跃执行
- `waiting` 表示当前轮需要继续输入
- `attention` 表示存在 `HITL / failed / needs approval`

不建议在左栏显示：

- run 数量列表
- external task 列表
- 当前 focused run 明细
- 多行状态摘要

搜索和 filter 可以保留，但视觉权重应低于“新建”和“会话切换”。

### 6.3 中栏 `Chat`

中栏顶部包含：

- 当前 session 标题
- 轻量连接状态
- 当前会话的辅助上下文，例如活跃 agent 或当前状态提示

紧接着是一条固定的 `Thin summary strip`，它只负责表达“当前这一轮的最短状态摘要”。

推荐文案形态：

- `Running · default · run_xxx`
- `Waiting for input`
- `Needs approval`
- `Completed`
- `Failed`

这条 strip 的要求是：

- 始终很薄
- 只显示当前轮次
- 不展示长文本
- 不展示多条 run 卡片
- 不替代消息流

真正的主区域仍然是 `Message stream`。

消息流内保留：

- 用户消息
- assistant 回复
- 必要系统状态卡片

底部 `Composer` 继续承接：

- 发起新 turn
- 在 waiting 状态下继续输入
- 与按钮 pending / error 状态联动

### 6.4 右栏 `Context`

右栏固定为 4 个块，按处理优先级排序：

1. `Pending HITL`
2. `Session summary`
3. `Memory Candidates`
4. `Run details (compact)`

这个顺序反映的是：

- 先处理当前必须人工介入的事
- 再看会话概况
- 再看候选记忆
- 最后看当前 focused run 的轻量细节

右栏不再默认承接一个大型 `Run Inspector`。

---

## 7. 中栏信息层级

默认 `Chat` 视图的中栏信息层级从强到弱分为 4 层。

### 7.1 `Message stream`

这是绝对主角。

如果某次运行很快完成，用户应当只看这里就能完成大部分交互理解。

### 7.2 `Thin summary strip`

它是“当前轮状态温度计”，不是时间线，也不是 inspector。

它的职责是让用户不进入细节也能快速知道：

- 当前是不是还在跑
- 是否需要用户继续输入
- 是否需要审批
- 是否已经完成或失败

### 7.3 `Inline status cards`

消息流中只允许插入“用户必须感知”的状态卡片。

建议仅允许下面几类状态进入消息流：

- `Waiting for input`
- `Needs approval / HITL`
- `Failed`
- `Stopped / Canceled`
- `Completed but no assistant text yet`

不建议进入消息流的内容：

- tool 调用细节
- external activity 连续流
- 原始事件序列
- 大段 run metadata

否则默认聊天区会重新退化成日志流。

### 7.4 `Right context handoff`

一旦用户需要更细的内容，就转到右栏。

这意味着中栏只保留“必须现在看到”的内容，其他内容全部通过右栏接住。

---

## 8. 右栏细化

### 8.1 `Pending HITL`

优先级最高。

只要有待处理 ticket，就应固定排在右栏最上面。

每张卡片只显示：

- reason
- agent
- `session / turn / run` 归属
- `Open context`
- `Respond`
- `Dismiss`

不承载完整历史和审计视图。

### 8.2 `Session summary`

显示当前 session 的紧凑概览信息。

建议字段：

- 当前状态
- 当前 `turnId`
- active run 数
- 最近更新时间

它是概览块，不放大段文本，不重做 inspector。

### 8.3 `Memory Candidates`

保持现有右侧面板定位，但继续遵守“辅助上下文”原则。

默认只展示：

- 当前 session 的 pending candidates
- `Accept / Reject`

如果条目较多，建议限制首屏数量，剩余通过 `View more` 或展开交互处理，避免右栏无限增长。

### 8.4 `Run details (compact)`

这是对当前大型 `Run Inspector` 的降级重构版本。

默认只展示当前 focused run 的少量字段：

- `runId`
- `agent`
- `status`
- `wait kind`
- `summary`
- `failed reason`

它的目标是：

- 帮用户快速确认当前 run 的具体归属和状态
- 不在默认视图中呈现完整运行轨道

不应默认展示：

- external activity 长列表
- 多个 run 的并行细节
- 原始事件和 payload 明细

---

## 9. 需要弱化或隐藏的现有内容

为了把默认页收回到 `Chat` 主线，下面这些内容要么隐藏，要么降级：

- 大型 `Run Inspector`
- `Active Runs` 详细列表
- `External activities` 连续事件流
- 调试事件抽屉的默认入口权重
- 过强的 `recent turns strip`

这些内容并不是删除，而是退出默认主视图。

后续如果需要强追踪体验，应进入 `Workbench` 模式，而不是继续往默认 `Chat` 页里叠。

---

## 10. `Chat / Workbench` 演进关系

### 10.1 `Chat` 是默认入口

`Chat` 模式适合完成下面这些动作：

- 创建 session
- 发起 turn
- 看回复
- 处理中断
- 继续输入
- 处理 HITL

它的目标不是做全量观察，而是高频完成任务。

### 10.2 `Workbench` 是后续增强视图

未来的 `Workbench` 模式适合承接：

- 多 run / 多 agent 并行追踪
- 更完整的 timeline
- external task / external activity 细节
- 审计与调试能力

它不是当前默认入口，但必须在架构上可自然生长出来。

### 10.3 两种模式共享同一套数据模型

`Chat` 和 `Workbench` 必须共享同一套：

- session
- turn
- run
- external task
- HITL
- memory candidate

数据来源与状态模型。

前端不应为了两种视图各自再造一套领域模型。

这意味着当前前端结构要尽量做到：

- 壳层负责模式和区域装配
- features 负责清晰的小职责块
- 默认聊天页与未来工作台页可以复用同一批数据输入

---

## 11. 响应式策略

### 11.1 桌面端

桌面端保持三栏：

- 左栏窄
- 中栏最大
- 右栏固定偏窄

中栏始终拥有最高优先级的可视宽度。

### 11.2 平板或窄屏

收敛为：

- 中栏主视图常驻
- 左栏和右栏通过抽屉或 tab 切换

窄屏下不建议继续硬挤三栏，否则聊天主线会被切碎。

### 11.3 手机端

手机端默认退化成单列聊天页：

- 主体只保留 `Header / Summary strip / Messages / Composer`
- `Sessions` 与 `Context` 通过顶部入口或底部入口打开

如果存在待处理 `HITL`，需要高优先显式提示，不能依赖用户自行翻侧栏。

---

## 12. 对现有前端结构的影响

这轮设计意味着前端主壳需要从“Run Inspector 驱动页面”转向“Conversation 驱动页面”。

建议的结构影响如下：

### 12.1 壳层重心调整

`AppShell` 需要明确表达三栏新职责：

- 左：`SessionList`
- 中：聊天主区装配
- 右：上下文侧栏装配

### 12.2 会话消息区升级为主视图

当前 `ConversationPane` 应成为默认主舞台，而不是工作台中的一个子块。

它需要承接：

- header 附近的轻量状态
- thin summary strip
- message stream
- composer

### 12.3 Run 相关模块降级为紧凑信息块

现有 `run-inspector` 相关模块需要拆成：

- 默认聊天页可消费的紧凑块
- 未来 `Workbench` 可消费的更重块

而不是继续维持一个默认就很重的大型组合面板。

---

## 13. 非目标

这轮明确不做：

- 完整 `Workbench` 模式实现
- 多 agent 专业任务编排界面
- 更复杂的 run timeline
- 独立 tools / mcp / skills 管理页
- 大规模视觉系统重做

本轮唯一目标是把默认页收回到聊天主线，同时不损失当前后端主链的可操作性。

---

## 14. 验收标准

如果满足下面这些条件，就说明默认 `Chat` 视图的设计目标达成：

1. 用户进入页面后，第一眼看到的是会话聊天主窗口，而不是 run inspector
2. 用户发起 turn 后，可以在中栏直接感知当前状态是否 `running / waiting / approval / failed / completed`
3. 需要继续输入或处理 `HITL` 时，默认视图内可直接完成
4. 不查看右栏，也能完成基础聊天主链
5. 查看右栏时，能快速拿到当前 session / run 的必要辅助上下文
6. 默认中栏不会被 tool / event / external activity 日志污染
7. 后续仍可基于相同数据模型继续扩展出 `Workbench` 视图

---

## 15. 下一步

这份设计文档确认后，下一步应是：

1. 写实现计划
2. 按壳层装配、聊天主区、右栏降级重组的顺序落地
3. 最后再做一轮前端联调与交互收口

在实现之前，不继续扩展更重的工作台能力，先把默认聊天体验稳定下来。
