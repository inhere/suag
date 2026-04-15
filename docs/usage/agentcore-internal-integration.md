# Internal 包接入 agentcore 的检查结论

这份文档回答两个问题：

1. 当前 `internal/*` 是否已经能直接复用 `pkg/agentcore`
2. 如果还不能，最小调整路径是什么

结论先行：

- `internal/llm`
  - 现在主路径已直接对齐 `agentcore.Provider`
- `internal/tools`
  - 现在主路径已直接对齐 `agentcore.ToolRegistry`
- `internal/memory`
  - 不应作为 `agentcore.Storage` 使用
  - 现在已在 `internal/platform/memory` 模块内提供第一方 `agentcore.Memory` 实现
- `internal/agent`
  - 已新增最小 `AgentcoreRunner` 桥接层，但默认执行路径仍保留旧 loop
- `internal/orchestrator`
  - 默认已优先走 `agentcore` runtime，同时保留显式回退旧路径的入口
  - 新路径已可承接 tracing / HITL / fail-threshold 配置透传

补充判断：

- `internaltools`、`internalmemory`
  - 原先是迁移期桥接层
  - 现在已经删除，相关能力已迁回第一方实现
也就是说，当前最适合的方式是：

- 先把 `internal/llm`、`internal/tools` 与 runtime-facing `memory` 收口到 `agentcore`
- 再继续扩大默认 `agentcore` 路径的覆盖面，并逐步压缩旧 runtime 的职责

而这一步里，`llm/tools/memory` 的主收口已经基本完成，当前剩余更多是兼容层与文档尾项。

---

## 1. 当前已满足的部分

### 1.1 `internal/llm`

当前 `internal/llm.Provider` 与 `agentcore.Provider` 的职责已经足够接近：

- 都表达一次模型调用
- 都有 message / tool schema / finish reason
- 差异主要在类型形状和 finish reason 枚举

因此现在主路径已经可以直接把 `internal/llm.Provider` 接到 `agentcore.Runtime` 上，不再需要额外的迁移期 provider bridge。

### 1.2 `internal/tools`

当前 `internal/tools.Registry` 已直接满足 `agentcore.ToolRegistry` 的关键 contract：

- 能列出 schema
- 能按工具名执行

因此现在主路径已经可以直接把 `internal/tools.Registry` 接到 `agentcore.ToolRegistry` 上，不再需要额外的迁移期 tools bridge。

---

## 2. 当前还不满足直接接入的部分

### 2.1 `internal/memory` 不是 `agentcore.Storage`，但现在可以作为 `agentcore.Memory`

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

但 `internal/memory.Store` 非常适合桥接到 `agentcore.Memory`：

- `GetContext`
  对应 `Memory.BuildContext`
- 会话消息追加
  对应 `Memory.ObserveMessages`
- 已持久化 transcript 重新加载后的对齐
  对应 `Memory.Hydrate`

当前仓库已经在 `internal/platform/memory` 模块内新增：

- `memory.RuntimeMemory`

用来把 richer `memory.Store` 暴露成第一方 `agentcore.Memory` 实现。

这也让边界更清晰了：

- `agentcore.Storage`
  保存 authoritative run state / transcript
- `agentcore.Memory`
  构造 model-facing context

### 2.2 `internal/agent` 已有 bridge，且恢复语义开始对齐 `agentcore`

当前 `internal/agent/runner.go` 里，`ReactRunner` 自己负责：

- 从 `memory.Store` 取上下文
- 直接调用 `llm.Provider`
- 直接执行 `tools.Registry`
- 连续失败计数
- HITL 挂起与恢复
- tracing span 打点

这说明旧执行器还没有被直接移除，仍然保留着一套并行运行循环。

所以现状更准确的说法是：

- `internal/agent` 已经可以通过 `AgentcoreRunner` 把 `LLM`、`Tools`、`Soul`、`Storage` 映射到 `agentcore.Config`
- 同时主路径里的 `Tools` 与 `Memory` 已直接使用 `internal/platform/tools` 和 `internal/platform/memory` 的第一方实现
- 但默认实现仍未完全收敛到单一 runtime

另外，`AgentcoreRunner` 的恢复语义也已开始收敛到 `agentcore` 的公共模型：

- 首次运行使用 `RunID + Instruction`
- yield 后的继续运行复用同一个 `RunID`
- 外部 `Resume()` 不再只是本地 memory 标记，而是会在下一次 `Run()` 时映射为：
  - `ResumeFromState`
  - 或 `ResumeWithInput`

这意味着 internal 的 `Runner` 接口已经不再只是“看起来像能恢复”，而是开始真实复用 `agentcore` 的恢复机制。

### 2.3 `internal/orchestrator` 已支持选择性切换

当前 `internal/orchestrator` 默认已经优先创建：

- `agent.Agent`
- `agent.AgentcoreRunner`

同时保留显式回退到：

- `agent.ReactRunner`

也就是说，它已经具备渐进切换能力，并且关键运行时能力已能沿新路径透传，但还没有完成默认路径替换。

### 2.4 当前对齐进度

目前已经有一组新旧 runner 的并行对照测试，验证 `ReactRunner` 与 `AgentcoreRunner` 在这些场景下能够保持一致或等价：

- direct answer
- one tool call
- multi-step tool chain
- HITL continue
- HITL dismiss
- max steps exceeded
- context canceled
- manual step-through

这些用例说明：

- `internal/agent` 基于 `agentcore` 的最小执行链路已经不只是“能跑”
- 它已经开始具备替换默认单 agent loop 的验证基础

但这还不等于“可以无条件默认切换”。当前仍应把它视为：

- 已具备较强信心的渐进迁移入口
- 还需要继续观察与补齐更细粒度行为差异的候选默认路径

### 2.5 当前默认策略

当前推荐并已实现的单 agent 策略是：

- `orchestrator` 的默认单 agent runtime 使用 `agentcore`
- 同时保留显式回退到旧 `ReactRunner` 的入口
- `team.Manager` 也开始沿同一 runtime 选择策略收敛，默认走 `agentcore`，显式可回退 `react`

也就是说：

- 默认路径已经开始向 `agentcore` 收敛
- 旧实现还没有被移除
- 如需回退，可以通过配置把 runtime 明确切回 `react`

## 2.6 仍待收尾的项

当前还没有完全完成的部分主要是：

- `ReactRunner` 仍保留为 fallback / parity baseline
- Host internal `session runner` 尚未开始接线
- 当前 `ToolSchema` authority 已明确继续使用 `map[string]any`，`internal/platform/tools` 也已直接返回 `agentcore/api.ToolSchema`；后续主要剩下文档表述精炼

---

## 3. 最小迁移路径

如果目标是“让 internal 下的包开始使用新的 agentcore”，推荐按下面顺序做，而不是一次性重写：

### 阶段 1：桥接现有能力，先建立最小迁移入口

- 保持 `internal/llm` 不动
- 保持 `internal/tools` 不动
- 先通过迁移期桥接层验证 `agentcore`
- 先在新入口或实验入口中验证 runtime 行为

这是已经完成的历史阶段记录；相关迁移期 `internal/acadapters` 已在后续收口中删除。

### 阶段 2：抽出 internal 的 run-state persistence，并分离 memory/context 层

补一层适配，满足 `agentcore.Storage`：

- run snapshot
- authoritative transcript
- resume 所需状态

这一层不应该直接等同于 `internal/memory.Store`。

同时保留一条独立的 memory 接入层，满足 `agentcore.Memory`：

- message observe
- resume hydrate
- context recall / summary

### 阶段 3：继续让 `internal/agent` 退化为“角色/配置模型”

把 `internal/agent` 从“自己执行 loop”改成更薄的一层：

- Agent metadata / soul / policy
- 构造 `agentcore.Config`
- 注入 hooks / prompt / context / storage

也就是：

- 保留 agent 身份和角色概念
- 把真正执行交给 `agentcore.Runtime`

### 阶段 4：扩大 `internal/orchestrator` 对 `agentcore` 的默认使用面

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
- `internal/agent`
  - 逐步退化为更高层 agent 角色与配置模型
- `internal/orchestrator`
  - 作为更高层调度面，建立在 `agentcore` 之上

---

## 5. 当前状态判断

如果用一句话总结当前状态：

> `internal/llm` 和 `internal/tools` 已经可以接入新的 `agentcore`；`internal/agent`、`internal/orchestrator` 与 `internal/team` 也已经开始以 `agentcore` 为默认或首选路径收敛，而旧 `ReactRunner` 更适合作为 legacy fallback 与 parity baseline 保留。

这意味着下一阶段如果要继续推进，重点不该是继续往 `pkg/agentcore` 里塞内部桥接逻辑，而是开始规划：

- 扩大 `AgentcoreRunner` 的能力面（如 hooks / context / tracing / HITL）
- 继续清理剩余直接依赖 `ReactRunner` 的业务入口
- 把旧 `ReactRunner` 明确收敛为 fallback / test baseline
- 继续补齐与旧路径的行为对齐用例，降低未来删除成本

如果只问“现在能不能开始准备默认切换”，当前答案已经可以更新为：

> 已经适合把 `agentcore` 作为内部默认执行核心继续推进，但还不适合立即删除旧 `ReactRunner`。
