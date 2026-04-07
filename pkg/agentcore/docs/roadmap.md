# Agentcore Roadmap

`pkg/agentcore` 的目标不是成为 Axon 全部 agent 能力的集合，而是成为一个可以长期演进、稳定嵌入、对外可发布的 agent runtime kernel。

这份 roadmap 关注的是：

- `agentcore` 未来应该继续长什么
- 什么应该进入核心包
- 什么应该建立在核心包之上
- 演进顺序应该怎么安排

它不是产品 PRD，也不是一次性 implementation checklist，而是一份面向未来几个阶段的演进路线说明。

---

## 1. Why Agentcore

`agentcore` 解决的是所有 agent 都绕不开的那条最小公共执行链路：

- 准备或恢复一次 run
- 管理 transcript 与 run state
- 构建 prompt 与 context
- 调用 provider
- 执行 tool loop
- 持久化事实层状态
- 暴露 hooks 与稳定错误语义

它不尝试在核心层解决：

- 多 agent 编排
- planner / task graph
- 产品级长期记忆策略
- tracing 控制面产品化
- 渠道接入
- policy / governance / review workflow

换句话说：

- `agentcore` 应该是 kernel
- 更高层能力应该 build on top of kernel

---

## 2. Current State

当前 `agentcore` 已经具备一个可以工作的 v0 内核：

- runtime loop
  - final answer
  - tool loop
  - max steps
  - max tool calls per step
- stable public contracts
  - `RunInput`
  - `RunState`
  - `RunResult`
  - `Message`
  - `ToolCall`
  - `ToolResult`
- extensibility points
  - `Provider`
  - `Storage`
  - `Memory`
  - `ToolRegistry`
  - `PromptBuilder`
  - `ContextBuilder`
  - `Hook`
- core lifecycle semantics
  - stable transcript persistence
  - `RunID + ResumeMode` 恢复模型
  - error category wrapping
- internal bridge status
  - `internal/llm` 已桥接
  - `internal/tools` 已桥接
  - `internal/memory` 已桥接到 `agentcore.Memory`
  - `internal.AgentcoreRunner` 已开始复用真实 resume 语义

这说明 `agentcore` 已经不只是一个概念验证，而是一个能承接内部默认单-agent runtime 的基础核心。

但它仍然只是 v0：

- 核心契约刚形成
- 默认实现仍偏薄
- 外部可用性与文档完整性还需要继续补

---

## 3. Design Principles

后续所有演进都应该尽量服从下面这些原则。

### 3.1 Kernel First

优先增强可复用的核心契约，而不是直接往核心里塞产品层功能。

判断标准：

- 任何 agent 都需要的能力 -> 倾向进 core
- 只有某一类产品或 workflow 才需要 -> 倾向放 core 之上

### 3.2 Public Contract First

如果某个能力值得长期存在，优先先定义公共 contract，再决定默认实现和 internal adapter。

也就是说：

- 不要让 `internal/*` 成为事实标准
- 也不要让默认实现意外变成唯一实现方式

### 3.3 Storage Is Authority, Memory Is Context

这是当前 `agentcore` 最重要的边界之一，后续必须坚持：

- `Storage`
  - authoritative transcript
  - run snapshot
  - resume / replay authority
- `Memory`
  - model-facing context
  - summary / recall / window
  - 派生层，而不是事实层

这两者可以共享底层实现，但不应该共享语义。

### 3.4 Resume Must Be Explicit

恢复运行必须可审计、可重放、可解释。

这意味着：

- 使用稳定 `RunID`
- 使用显式 `ResumeMode`
- 恢复时是否追加新输入必须清楚可见

任何“偷偷继续执行”的行为，长期都应该避免。

### 3.5 Hooks Are Extension Points, Not a Parallel Runtime

Hooks 应该负责：

- 观察
- 注入
- 拦截
- 轻量控制

但不应该让系统长期演进成“主要逻辑都躲在 hook 里”的 parallel runtime。

如果某种 hook 用法开始成为主路径，那通常说明核心 contract 需要升级。

### 3.6 Small Default, Strong Growth

默认实现应该简单可用，但公共接口必须给未来增长留足空间。

不追求一次性做大，但也不要把未来堵死。

---

## 4. Milestones

下面的里程碑是推荐的推进顺序，不要求一次全部做完。

### v0.2 Stable Core Contracts

目标：

- 把当前已经形成的 runtime kernel 从“能用”推进到“可依赖”

重点事项：

- 补齐 `Memory` 边界测试矩阵
  - hydrate 幂等
  - observe 的追加语义
  - context build 错误传播
  - memory 与 storage 分叉时的预期行为
- 补齐 `Resume` 边界测试矩阵
  - repeat resume
  - resume after yield again
  - terminal state resume
  - empty resume input
- 继续收紧 hook 控制语义
  - 哪些阶段支持真实控制流
  - 哪些阶段是观察型阶段
- 明确 transcript 规范
  - tool error 消息形状
  - assistant(tool_call) / tool(result) 成对语义

完成标志：

- 核心 contract 与边界测试足够稳定
- 外部实现 `Provider` / `Storage` / `Memory` 时不需要猜行为

### v0.3 Useful Default Implementations

目标：

- 让 `agentcore` 不止“可扩展”，也能“较低成本直接嵌入”

重点事项：

- 提供一个内置 `memory/` 子包
  - working memory
  - summary slot
  - transcript window
  - 简单 recall
- 提供更清晰的默认 `context/` 组合
  - transcript-only
  - memory-first
  - memory + recent window
- 提供组合式 `prompt/` 默认 builder
  - static base prompt
  - metadata injection
  - layered role prompt
- 补 quickstart / API examples
  - minimal runtime
  - runtime + memory
  - runtime + resume

完成标志：

- 外部使用者可以不自己写全部基础实现，就跑通一个有上下文能力的 agent runtime

### v0.4 Advanced Runtime Extensions

目标：

- 打开更高级的扩展面，但不破坏核心边界

重点事项：

- 设计 streaming contract
  - stream callback / event model
  - 与 transcript / resume 的兼容关系
- 强化 artifact story
  - tool artifact persistence
  - artifact replay
  - artifact-to-context strategy
- 更强的 metadata / parent-child run 关联
  - parent run ID
  - child run ID
  - trace correlation metadata

这阶段要特别小心：

- 不要为了 streaming 把现有 runtime 全部推翻
- 不要为了 artifacts 把 core 做成产品仓库

完成标志：

- `agentcore` 能承载更复杂的 runtime 形态
- 但 kernel 边界仍然清晰

### v0.5+ Platform Readiness

目标：

- 为更高层系统提供稳定地基，而不是自己演化成大而全平台

重点事项：

- 明确 compatibility / release posture
- 更稳定的 package docs
- 更清晰的 versioning / compatibility policy
- examples 与 adapter story 完整化
- 发布独立包所需的整理工作

这阶段更像“发布工程和产品化整理”，而不是 runtime 功能堆叠。

---

## 5. Capability Areas

如果按能力域来看，后续演进重点大致如下。

### 5.1 Runtime

继续保持最小但稳定：

- step loop
- resume
- yield
- error classification

runtime 本身不应该承担 planner 和 orchestration 责任。

### 5.2 Memory

这是下一阶段最值得持续投入的核心能力域之一。

短期目标：

- 让 `Memory` 成为稳定公共接口
- 给出至少一个默认实现

长期目标：

- summary
- retrieval
- structured recall
- artifact-aware context

但这些能力应通过 `Memory` 与 `ContextBuilder` 增长，而不是污染 `Storage`。

### 5.3 Hooks

Hooks 继续是关键扩展机制，但需要避免两种风险：

- 控制语义过于隐式
- 业务逻辑过多迁入 hook

如果某类 hook 模式反复出现，应考虑把它提升为正式 contract。

### 5.4 Adapters

当前 internal adapters 已经证明方向是对的：

- core 保持公共
- internal 通过桥接接入

后续建议：

- 继续把仓库内部依赖留在 adapter 层
- 不把 internal history 反向压回 core

### 5.5 Documentation

`agentcore` 如果要独立发布，文档不是附属品，而是核心质量的一部分。

下一阶段需要继续补：

- API docs
- default implementation guides
- adapter integration guides
- examples
- roadmap updates

---

## 6. Non-goals

下面这些方向目前不建议直接做进 `pkg/agentcore`：

- 多 agent DAG / team runtime
- planner / decomposition engine
- 产品级 knowledge base / governance pipeline
- channel integration
- tracing 平台产品化层
- review / policy / approval platform

这些能力都可能非常重要，但应该建立在 `agentcore` 之上，而不是并进核心包。

---

## 7. Open Questions

这些问题值得在后续阶段逐步收敛，但当前不一定要一次回答完。

### 7.1 Memory 是否需要更细分接口

当前 `Memory` 已经够用，但未来可能需要区分：

- write-side observe
- read-side recall
- summarize / compact

暂时不必拆分，先观察真实使用模式。

### 7.2 Streaming 应该是 Runtime 的一等接口还是 Hook 扩展

这是未来很关键的设计点。

当前更倾向：

- stream 是正式 contract
- 而不是只靠 hook 观察 provider token

但还不应该现在就定死。

### 7.3 Artifact 应该由 Storage 管还是单独 contract

当前 `Artifact` 已存在，但 story 还不完整。

后续需要决定：

- artifact persistence 是否属于 storage
- artifact recall 是否属于 memory
- artifact 注入 context 的权责边界在哪里

### 7.4 Versioning Policy 何时引入

如果 `agentcore` 真要独立发布，公共 contract 的变更策略会越来越重要。

当前还可以快速演进，但在默认实现和 examples 成熟后，应尽快明确兼容策略。

---

## 8. Recommended Next Steps

如果只看“接下来最值得做什么”，推荐顺序是：

1. 完成 `v0.2`
   - 核心契约稳定化
   - 测试矩阵补齐
2. 进入 `v0.3`
   - 提供一个默认 `memory/` 子包
   - 补默认 `context/` 与 `prompt/` 组合
3. 再开启 `v0.4`
   - streaming
   - artifacts
   - 高级扩展

一句话总结：

> `agentcore` 的下一阶段，不是变得更大，而是先变得更稳、更清晰、更容易被别人真正依赖。

---

## 9. How To Use This Roadmap

这份 roadmap 应该被这样使用：

- 当新增 feature 想进入 `agentcore` 时，用它来判断边界是否合理
- 当要决定优先级时，用它来对齐里程碑顺序
- 当准备补包文档或 examples 时，用它来确认当前阶段的重点

它不要求每一步都完全照做，但应该帮助团队避免两种常见偏差：

- 还没把 kernel 稳住，就开始向 core 塞产品层能力
- 因为害怕做重，而一直不把真正值得进入 core 的公共 contract 定下来
