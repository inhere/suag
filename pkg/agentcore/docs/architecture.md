# Agentcore Architecture

这份文档不重复 quickstart，也不逐条解释 API。

它回答的是另一个问题：

- `agentcore` 作为一个 runtime kernel，整体是怎么分层的
- 各层之间的边界是什么
- 为什么这些能力要拆成现在这几个 contract
- 一个 run 在 kernel 内部如何流动

---

## 1. 核心定位

`agentcore` 的定位是一个最小但可持续生长的 agent runtime kernel。

它只负责任何单 agent 运行都绕不开的那条公共执行路径：

1. 准备或恢复 `RunState`
2. 生成 system prompt
3. 生成 model-facing messages context
4. 调用模型 provider
5. 执行模型请求的 tool calls
6. 持久化 transcript 与 snapshot
7. 暴露 hooks 与稳定错误语义

它明确不负责：

- planner / task graph
- 多 agent 协作编排
- 产品级 memory governance
- 渠道接入
- review / approval / policy 控制面

这意味着 `agentcore` 应该被理解为：

- 一个执行内核
- 一组稳定 runtime contract
- 一套可嵌入、可替换的基础构件

而不是完整 agent platform。

---

## 2. 分层模型

从职责上看，`agentcore` 可以分成 5 层：

1. runtime orchestration
2. authority state
3. model-facing context
4. side effects
5. lifecycle extension

### 2.1 Runtime Orchestration

这一层由 `Runtime` / `DefaultRuntime` 负责。

它的工作是把一次 run 串起来，而不是承载业务策略。

核心职责：

- 处理 `RunInput`
- 解析 `ResumeMode`
- 组织 step loop
- 统一调用 prompt / context / provider / tools / storage / hooks
- 统一包装错误

它是 kernel 的调度层，不应该成为：

- 业务规则中心
- provider-specific adapter
- memory implementation

### 2.2 Authority State

这一层由 `Storage` 和 `RunState` / transcript 语义组成。

这里保存的是事实层：

- 当前 run snapshot
- append-only message transcript

这一层的设计原则是：

- 可恢复
- 可 replay
- 可审计

这也是为什么 `tool loop` 会明确持久化：

1. `assistant(tool_call)`
2. `tool(result)`

而不是只保存一个最终工具输出。

### 2.3 Model-Facing Context

这一层由 `Memory`、`PromptBuilder`、`ContextBuilder` 共同组成。

它们都服务于同一个目标：

- 把事实层状态变成“下一次模型调用该看到什么”

但它们的边界不同：

- `Memory`
  负责上下文派生视图，例如 summary、recent window、recall
- `PromptBuilder`
  负责 system prompt
- `ContextBuilder`
  负责 provider messages 排布

关键原则是：

- `Storage` 不是上下文工程层
- `Memory` 不是 authority transcript
- `PromptBuilder` 不应该偷接管 messages 排布
- `ContextBuilder` 不应该偷接管 transcript 持久化

### 2.4 Side Effects

这一层由 `Provider` 和 `ToolRegistry` 负责。

它们代表 runtime 与外部世界交互的两类主要副作用：

- 模型调用
- 工具执行

这两层都通过公共 contract 隔离，目的是让 kernel 不绑定某个具体 vendor 或内部实现。

### 2.5 Lifecycle Extension

这一层由 `Hook` 构成。

它负责：

- 观测
- 注入
- 拦截
- 轻量控制

但不应该形成一套“平行 runtime”。

如果大量核心逻辑只能通过 hooks 才能完成，通常说明应该升级正式 contract，而不是继续堆 hook。

---

## 3. 一个 Run 的数据流

一次 run 的简化数据流可以理解成：

```text
RunInput
  -> load / prepare RunState
  -> maybe append new user input
  -> persist snapshot + messages
  -> build system prompt
  -> build model messages
  -> list tools
  -> provider.generate
  -> execute tool calls when needed
  -> persist transcript
  -> finish with final answer / yield / failure
```

进一步展开后，大致是：

```text
RunInput
  -> Runtime.prepareState
  -> Hooks(BeforeRun)
  -> Runtime.persist input changes
  -> loop:
       PromptBuilder.BuildSystemPrompt
       ContextBuilder.BuildMessages
       ToolRegistry.ListTools
       Provider.Generate
       if tool calls:
         ToolRegistry.ExecuteTool
         Runtime.persist assistant(tool_call) + tool(result)
         continue
       if final:
         Runtime.persist assistant(final)
         return RunResult
  -> Hooks(AfterRun / OnError)
```

这条链路里有两个最关键的稳定点：

- `RunID + ResumeMode`
- append-only transcript

前者保证 run continuation 可解释，后者保证 replay 与恢复可信。

---

## 4. 为什么拆成这些 Contract

`agentcore` 不是把所有东西都塞进 `Runtime.Config` 就结束了，而是刻意拆成几类 contract。

原因是不同能力的变化速度不同、替换方式不同、生命周期也不同。

### 4.1 `Provider`

变化最快，通常跟模型厂商、路由策略、限流和观测要求强相关。

所以它必须是最容易替换的一层。

### 4.2 `Storage`

决定恢复、审计和 replay 的可信度。

这是 kernel 最接近 authority 的一层，所以必须语义稳定、职责单一。

### 4.3 `Memory`

是最容易继续演化的一层。

summary、window、retrieval、artifact-aware context 都更适合在这里增长，而不是把 `Storage` 或 `Runtime` 做重。

### 4.4 `PromptBuilder` / `ContextBuilder`

这两个看起来都像“上下文工程”，但职责不同：

- 一个负责 system prompt
- 一个负责 message assembly

把它们拆开，可以避免：

- prompt 与 messages 责任耦合
- memory 和 prompt 互相越界

### 4.5 `Hook`

hooks 的存在，是为了让 embedding host 可以在不 fork runtime 的前提下做局部控制和观测。

但它必须保持“扩展点”身份，而不是主路径容器。

---

## 5. 核心边界

如果要理解 `agentcore` 架构，最重要的是记住下面几条边界。

### 5.1 Storage Is Authority

`Storage` 保存的是：

- snapshot
- transcript

这是真实恢复与 replay 的依据。

### 5.2 Memory Is Derived

`Memory` 保存的是：

- summary
- recent window
- recall context

这是模型视角，不是事实层。

### 5.3 Prompt Stays Prompt

`PromptBuilder` 生成 system prompt 字符串。

它不承担：

- transcript 存储
- message assembly
- memory persistence

### 5.4 Context Stays Context

`ContextBuilder` 负责最终消息排布。

它不应该反向影响 authority transcript。

### 5.5 Hooks Stay Peripheral

hooks 可以观察、注入、拦截，但不应该长期承载核心业务主逻辑。

---

## 6. Package Shape

从 package 结构上看，`agentcore` 当前可以分成三类内容：

### 6.1 Root Package

`pkg/agentcore`

提供：

- runtime
- public types
- public contracts
- default orchestration

### 6.2 First-Party Companion Packages

- `pkg/agentcore/files`
- `pkg/agentcore/memory`
- `pkg/agentcore/context`
- `pkg/agentcore/prompt`
- `pkg/agentcore/hooks`
- `pkg/agentcore/mock`

它们的目标不是做成大而全 framework，而是提供：

- 可直接使用的默认构件
- 一组“推荐起步路径”
- 对公共 contract 的第一方示范实现

### 6.3 Package-Local Docs And Examples

- `pkg/agentcore/docs`
- `pkg/agentcore/examples`

它们的目标是让 `agentcore` 即使离开当前仓库，也能保持自解释性。

---

## 7. 对外发布视角

如果把 `pkg/agentcore` 当成未来可单独发布的包，架构上最重要的约束是：

- `pkg/agentcore` 不依赖仓库的 `internal/*`
- internal 集成通过 adapter 完成
- package-local docs 和 examples 尽量围绕公共 contract 自洽

这也是为什么：

- internal bridge example 保留在仓库根 `examples/agentcore/bridge`
- package-local examples 只保留纯公共 contract 路径

长期看，理想状态是：

- `agentcore` 提供 kernel 与 contract
- first-party companions 提供默认实现
- internal / product layers 构建在这些 contract 之上

---

## 8. 读这份文档之后去哪里

推荐顺序：

1. [../README.md](../README.md)
2. [getting-started.md](./getting-started.md)
3. [api.md](./api.md)
4. [embedding-patterns.md](./embedding-patterns.md)
5. [../examples/README.md](../examples/README.md)

如果你在看下一阶段增长方向，再继续看：

- [working-memory-design.md](./working-memory-design.md)
- [context-presets-design.md](./context-presets-design.md)
- [prompt-layered-design.md](./prompt-layered-design.md)
- [roadmap.md](./roadmap.md)
