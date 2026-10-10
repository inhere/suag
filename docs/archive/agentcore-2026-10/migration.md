# Agentcore Migration Guide

这份文档面向两类迁移场景：

- 你已经有一套 agent runtime，想逐步迁到 `agentcore`
- 你已经在用 adapter 桥接，想继续收敛成更原生的 `agentcore` host

它不讨论发布策略，而是讨论：

- 迁移顺序
- 风险边界
- 哪些语义必须先对齐

---

## 1. 先迁什么，不先迁什么

推荐优先级是：

1. 对齐 `RunID + ResumeMode`
2. 对齐 authority transcript 语义
3. 桥接 `Provider`
4. 桥接 `ToolRegistry`
5. 桥接 `Storage`
6. 桥接 `Memory`
7. 最后再决定是否替换成第一方 default builders

这个顺序背后的原因是：

- resume 和 transcript 是最高风险 contract
- provider / tools 更容易桥接
- memory 通常最依赖前面语义是否已经稳定

不推荐的顺序是：

- 一开始就重写全部 memory / prompt / context
- 但 transcript、resume 语义还没对齐

这样很容易让迁移变成“行为上看起来差不多”，但恢复和审计已经悄悄分叉。

---

## 2. 第一阶段：先对齐核心运行语义

无论你当前 runtime 长什么样，第一阶段都应该先回答三件事：

### 2.1 同一次 run 的稳定标识是什么

需要明确：

- 哪个 host 实体映射到 `RunID`
- run 在 pause / resume 前后是否保持同一个 ID

### 2.2 恢复时是否追加新输入

需要明确：

- 当前系统“继续执行”时是否会自动补一条 user input
- 当前系统“继续执行”时是否真的只是从已有状态继续

然后把这些语义映射到：

- `ResumeFromState`
- `ResumeWithInput`
- `ResumeAuto`

### 2.3 transcript 是否是 authority

需要明确：

- 当前历史消息是否真的是恢复依据
- tool loop 是否保留了 `assistant(tool_call)` 和 `tool(result)` 两类消息
- 是否存在只保留最终 answer、但丢掉中间 tool decision 的情况

如果这三件事没有先对齐，后续迁移都只是表面接入。

---

## 3. 第二阶段：先用 Adapter 接进来

对于已有系统，最现实的迁移路径通常不是“一步到位重写”，而是：

- 先把已有实现桥接到 `agentcore` 公共 contract

典型桥接目标：

- 现有 LLM client -> `Provider`
- 现有 tool executor -> `ToolRegistry`
- 现有 state store -> `Storage`
- 现有 context memory -> `Memory`

这样做的好处是：

- 先统一 core loop
- 先统一 transcript / resume 语义
- 先把 host 接口稳定下来

再慢慢决定哪些 adapter 值得保留，哪些值得被更原生的实现替换。

---

## 4. 第三阶段：把 Host 包装先稳定下来

在迁移中，一个常见误区是：

- 还没把 host wrapper 稳住，就开始大规模替换内部实现

更推荐的做法是先稳定 host 层的业务入口，例如：

- `Start(sessionID, message)`
- `Continue(sessionID, message)`
- `Resume(sessionID)`

这样做的价值是：

- 上层业务先不需要知道 `agentcore` 全部细节
- 迁移可以集中发生在 host wrapper 后面
- `RunInput` 的映射逻辑只保留在一处

如果你正在做这一步，建议配合：

- [host-integration.md](./host-integration.md)

---

## 5. 第四阶段：决定哪些 adapter 保留，哪些替换

不是所有 adapter 都必须被消灭。

更推荐的判断标准是：

### 5.1 可以长期保留的 adapter

- 只是协议转换
- 只是 vendor SDK 封装
- 没有大量语义补丁

### 5.2 应该尽量收敛掉的 adapter

- 承担了大量 transcript 修补逻辑
- 承担了大量 resume 语义补丁
- 把 host 业务逻辑塞进了 adapter
- 让 `agentcore` 的 contract 看起来“对外一套、对内一套”

如果 adapter 已经开始补 runtime 语义漏洞，通常说明应该升级 core contract 或 host wrapper，而不是继续堆补丁。

---

## 6. 从 Adapter 走向更原生接入的判断信号

下面这些信号说明你可以开始减少 adapter 依赖了：

- host 已经稳定使用 `RunInput -> RunResult`
- `RunID + ResumeMode` 语义已经清晰
- transcript replay 已经可靠
- 默认 `memory/`、`context/`、`prompt/` 已足够覆盖主路径
- adapter 只剩很薄的一层 vendor 封装

这时更推荐：

- 直接围绕 `agentcore` contract 组装 runtime
- 把 adapter 留在最边缘的位置

---

## 7. 迁移时最容易踩的坑

### 7.1 把 Memory 当 Storage 用

症状：

- summary 或 compaction 结果被当成 authority transcript

风险：

- resume 失真
- replay 失真

### 7.2 只迁 final answer，不迁 tool transcript

症状：

- 最终结果看起来对
- 但历史 tool call / tool result 丢了

风险：

- 后续 resume 时上下文不完整

### 7.3 Host 没有稳定 RunID

症状：

- 每次调用都像新 run
- 恢复依赖内部存储细节或隐藏状态

风险：

- 无法稳定 pause / continue

### 7.4 把 Streaming 当最终事实层

症状：

- UI 看的是 delta
- 但 host 不再处理最终 `RunResult`

风险：

- authority answer 与 UI 状态分叉

---

## 8. 推荐的迁移检查清单

每推进一轮迁移，至少检查这些问题：

- host 是否已经稳定生成和复用 `RunID`
- `ResumeFromState` / `ResumeWithInput` 是否已映射清楚
- transcript 是否仍是 append-only
- tool loop 是否保留成对消息
- streaming 是否仍然只是观测层
- adapter 是否还在偷偷改写 runtime 语义

如果其中任何一项答案不清楚，就先不要继续大面积替换实现。

---

## 9. 迁移路线推荐

如果你要一个很实用的分步路线，当前推荐是：

1. 先读 [embedding-patterns.md](./embedding-patterns.md)
2. 再读 [recommended-runtime.md](./recommended-runtime.md)
3. 再读 [host-integration.md](./host-integration.md)
4. 然后按这份文档推进 adapter / host / storage / memory 迁移

如果你已经基本迁完，下一步通常应该看：

- [release-readiness.md](./release-readiness.md)

因为那时重点会从“怎么迁”转向“现在这套 contract 是否足够可依赖”。
