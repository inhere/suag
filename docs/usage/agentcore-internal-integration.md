# Internal 包接入 agentcore 的检查结论

这份文档回答两个问题：

1. 当前 `internal/*` 是否已经能直接复用 `pkg/agentcore`
2. 如果还不能，最小调整路径是什么

结论先行：

- `internal/llm`
  - 可以通过 `internal/agentcoreadapters/internalllm` 接入
- `internal/tools`
  - 可以通过 `internal/agentcoreadapters/internaltools` 接入
- `internal/memory`
  - 还不能直接作为 `agentcore.Storage` 使用
- `internal/agent`
  - 还没有直接复用 `agentcore`，目前仍是并行的一套 loop
- `internal/orchestrator`
  - 也还没有直接建立在 `agentcore` 之上

也就是说，当前最适合的方式是：

- 先把 `internal/llm` 和 `internal/tools` 桥接进 `agentcore`
- 再决定是否逐步让 `internal/agent` / `internal/orchestrator` 改为基于 `agentcore` 运行

---

## 1. 当前已满足的部分

### 1.1 `internal/llm`

当前 `internal/llm.Provider` 与 `agentcore.Provider` 的职责已经足够接近：

- 都表达一次模型调用
- 都有 message / tool schema / finish reason
- 差异主要在类型形状和 finish reason 枚举

因此现在已经可以通过：

- `internal/agentcoreadapters/internalllm`

把 `internal/llm.Provider` 接到 `agentcore.Runtime` 上。

### 1.2 `internal/tools`

当前 `internal/tools.Registry` 也已经满足最关键的桥接条件：

- 能列出 schema
- 能按工具名执行

因此现在已经可以通过：

- `internal/agentcoreadapters/internaltools`

把 `internal/tools.Registry` 接到 `agentcore.ToolRegistry` 上。

---

## 2. 当前还不满足直接接入的部分

### 2.1 `internal/memory` 不是 `agentcore.Storage`

`agentcore.Storage` 的核心职责是：

- `LoadRun`
- `LoadMessages`
- `SaveRun`
- `AppendMessages`

它需要管理的是：

- run snapshot
- append-only transcript

而当前 `internal/memory.Store` 管理的是：

- 会话消息
- memory fragments
- context 截断 / summarization
- 相似检索

这两者不是一回事。

所以当前不能简单说“`internal/memory` 已经可以作为 `agentcore` 的 storage”。如果要接，需要额外做一层新的 run-state persistence 适配，而不是直接复用现有 `memory.Store`。

### 2.2 `internal/agent.ReactRunner` 仍然是并行实现

当前 `internal/agent/runner.go` 里，`ReactRunner` 自己负责：

- 从 `memory.Store` 取上下文
- 直接调用 `llm.Provider`
- 直接执行 `tools.Registry`
- 连续失败计数
- HITL 挂起与恢复
- tracing span 打点

这说明它还没有把 `agentcore` 当成执行内核，而是自己保留了一套运行循环。

所以现状更准确的说法是：

- `internal/agent` 和 `pkg/agentcore` 在概念上已经接近
- 但还没有完成 runtime 收敛

### 2.3 `internal/orchestrator` 还没有切到 `agentcore`

当前 `internal/orchestrator` 创建的仍然是：

- `agent.Agent`
- `agent.ReactRunner`

而不是：

- `agentcore.Runtime`

所以它也还不算“已经建立在新的 agentcore 上”。

---

## 3. 最小迁移路径

如果目标是“让 internal 下的包开始使用新的 agentcore”，推荐按下面顺序做，而不是一次性重写：

### 阶段 1：桥接现有能力，不动 orchestrator

- 保持 `internal/llm` 不动
- 保持 `internal/tools` 不动
- 通过 `internal/agentcoreadapters/*` 连接 `agentcore`
- 先在新入口或实验入口中验证 runtime 行为

这是当前已经具备的阶段。

### 阶段 2：抽出 internal 的 run-state persistence

补一层适配，满足 `agentcore.Storage`：

- run snapshot
- authoritative transcript
- resume 所需状态

这一层不应该直接等同于 `internal/memory.Store`。

### 阶段 3：让 `internal/agent` 退化为“角色/配置模型”

把 `internal/agent` 从“自己执行 loop”改成更薄的一层：

- Agent metadata / soul / policy
- 构造 `agentcore.Config`
- 注入 hooks / prompt / context / storage

也就是：

- 保留 agent 身份和角色概念
- 把真正执行交给 `agentcore.Runtime`

### 阶段 4：让 `internal/orchestrator` 调度 `agentcore.Runtime`

最终 `orchestrator` 负责：

- 组装依赖
- 选择单 agent 或 team
- 管理 tracing / hitl / task routing

而不是自己依赖旧的 `ReactRunner` 实现。

---

## 4. 建议的边界

为了让 `pkg/agentcore` 将来能独立发布，建议长期保持这些边界：

- `pkg/agentcore`
  - 不依赖任何 `internal/*`
  - 只放公共 contract、默认实现、纯公共辅助包
- `internal/agentcoreadapters`
  - 放仓库内部桥接层
  - 允许依赖 `internal/llm`、`internal/tools`
- `internal/agent`
  - 逐步退化为更高层 agent 角色与配置模型
- `internal/orchestrator`
  - 作为更高层调度面，建立在 `agentcore` 之上

---

## 5. 当前状态判断

如果用一句话总结当前状态：

> `internal/llm` 和 `internal/tools` 已经可以接入新的 `agentcore`；但 `internal/agent` 和 `internal/orchestrator` 还没有真正切换到以 `agentcore` 为执行内核。

这意味着下一阶段如果要继续推进，重点不该是继续往 `pkg/agentcore` 里塞内部桥接逻辑，而是开始规划：

- run-state persistence adapter
- `internal/agent` 到 `agentcore.Config` 的映射
- `orchestrator` 的 runtime 切换路径
