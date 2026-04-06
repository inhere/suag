# Agentcore 使用与设计说明

`pkg/agentcore` 是 Axon 当前已经落地的最小 Agent Core Loop。它不是完整产品层，不负责团队编排、长期记忆治理、Tracing 平台或渠道接入，而是把任何 Agent 运行都绕不开的那条核心执行链路独立成一个可嵌入的包。

这份文档描述的是“当前实现了什么”，不是长期蓝图。长期演进方向仍然参考 [`docs/v1/01-agent-core.md`](../v1/01-agent-core.md)。

---

## 1. 设计目标

`agentcore` 解决的是一个更小但更稳定的问题：

- 给任意 Agent 提供统一的运行循环
- 用公共接口隔离 provider、storage、tools、prompt、context
- 在关键节点暴露 hooks，允许拦截、观察和有限控制
- 让 run state、message transcript、resume 语义有稳定契约
- 可以单独作为包嵌入，不依赖 Axon 其它控制平面

换句话说，`agentcore` 是“任何 agent 启动运行的必须核心”，而不是完整的产品控制面。

---

## 2. 当前范围

当前 `pkg/agentcore` 已包含：

- `Runtime` / `DefaultRuntime`
  - 最小可运行的 agent loop
  - final answer 与 tool loop
  - step 上限与每步 tool call 上限
- 公共运行时契约
  - `RunInput`
  - `RunState`
  - `RunResult`
  - `Message`
  - `ToolCall`
  - `ToolResult`
- 扩展接口
  - `Provider`
  - `Storage`
  - `Memory`
  - `ToolRegistry`
  - `PromptBuilder`
  - `ContextBuilder`
  - `Hook`
- 内置实现
  - `files.NewStorage(...)`
  - 默认空 prompt builder
  - 默认上下文 builder
  - 默认空 tool registry
- 测试/集成辅助
  - `mock` provider / tool
- 仓库内部桥接
  - `internal/agentcoreadapters/internalllm`
  - `internal/agentcoreadapters/internalmemory`
  - `internal/agentcoreadapters/internaltools`

当前明确不包含：

- planner / task graph
- multi-agent orchestration
- tracing 平台与事件总线产品化层
- memory retrieval pipeline
- channel / webhook / DingTalk 接入
- evolution / policy / review workflow

这些能力以后可以建立在 `agentcore` 之上，而不是耦合进核心运行包本身。

---

## 3. 核心循环

一次 `Run()` 的核心流程如下：

1. 准备运行状态
2. 运行 `BeforeRun`
3. 根据 `ResumeMode` 决定是否追加新的 user message
4. 持久化 snapshot 与新增消息
5. 进入 step loop
6. 构建 system prompt
7. 构建 provider messages context
8. 列出可用 tools
9. 调用 provider 生成结果
10. 根据 `FinishReason` 分支
11. `FinishToolCalls` 时执行 tool loop 并持久化 transcript
12. `FinishFinal` 时持久化最终 assistant message
13. 无论成功、yield、short-circuit 还是失败，最后都会进入 `AfterRun`

可简化理解为：

```text
prepare state
  -> hooks(before_run)
  -> append/resume input
  -> persist input
  -> for each step:
       build prompt
       build context
       list tools
       provider.generate
       if tool calls:
         execute tools
         persist assistant(tool_call) + tool(result)
         continue
       if final:
         persist assistant(final)
         finish
  -> hooks(after_run)
```

其中当前有一个很重要的边界：

- `Storage`
  保存 authoritative transcript 与 run snapshot
- `Memory`
  保存 model-facing context 的派生层

这两个层次可以共享底层系统，但不应共享语义。

---

## 4. Transcript 契约

`agentcore` 当前最重要的稳定契约之一是消息 transcript。

### 4.1 消息角色

- `user`
- `assistant`
- `tool`

其中：

- 当模型请求调用工具时，会写入一条 `assistant` 消息，并填充 `Message.ToolCall`
- 当工具执行完成后，会写入一条 `tool` 消息，并填充 `Message.ToolResult`
- 当模型给出最终答案时，会写入一条普通 `assistant` 文本消息

### 4.2 Tool Loop 持久化规则

一次完整 tool 交互会持久化两条消息，而不是一条：

1. `assistant(tool_call)`
2. `tool(result)`

这样设计的原因是：

- transcript 可以完整 replay
- resume 时 provider 能看到“模型当时请求了什么工具”
- 不同 provider / adapter 之间能围绕统一 `Message` 契约工作
- storage log 能作为更可靠的 authority transcript

典型链路如下：

```text
user("use a tool")
assistant(tool_call=lookup)
tool(result="tool-output")
assistant("final answer")
```

---

## 5. Resume 语义

`RunInput.ResumeMode` 用来定义恢复时如何处理新输入。

### 5.1 `ResumeAuto`

- 默认模式
- 如果 `Instruction` 非空，则追加新的 user message
- 如果 `Instruction` 为空，则直接从已有状态继续

### 5.2 `ResumeFromState`

- 严格从持久化状态继续
- 不追加新的 user message

### 5.3 `ResumeWithInput`

- 一定追加一条新的 user message
- 即使 `Instruction` 是空串或纯空白，也会按输入追加

这个行为是有意的，目的是让“恢复并注入外部输入”具备可审计性，而不是悄悄吞掉一次 resume 指令。

---

## 6. Hooks 模型

`Hook` 是 `agentcore` 的主要扩展机制之一。

当前暴露的生命周期节点：

- `BeforeRun`
- `BeforePromptBuild`
- `BeforeContextBuild`
- `BeforeModel`
- `AfterModel`
- `BeforeTool`
- `AfterTool`
- `AfterRun`
- `OnError`

### 6.1 Hook 可以做什么

- 观察生命周期事件
- 修改 prompt / context / tool call / tool result
- 写入额外 metadata
- 在部分阶段触发 `Yield`
- 在部分阶段触发 `ShortCircuit`

### 6.2 Hook 控制语义

大多数阶段如果设置了 control，会停止该阶段后续 hook，并交给 runtime 解析。

但有两个例外是刻意设计的：

- `AfterTool`
  - 即使较早的 hook 设置了 control，后续 `AfterTool` hooks 仍会继续执行
  - 原因是这个阶段更偏“观察与补充结果”，不应该让前一个 hook 把后续观察器静默跳过
- 非可恢复错误上的 `OnErrorObserved`
  - 用于在 root cause 已确定后让所有观察型 hook 都能看到该错误
  - 观察器的控制信号不会覆盖原始错误

### 6.3 `AfterTool` 的限制

`AfterTool` 阶段不支持把 control 当作真正的控制流出口。

如果 `AfterTool` hook 设置了 `Yield` 或 `ShortCircuit`：

- runtime 仍会先持久化本次 tool transcript
- 然后把它视为 hook 错误返回

这样可以避免工具已经执行完但 transcript 没落盘，导致状态出现不可 replay 的分叉。

---

## 7. 错误语义

公开错误包装类型包括：

- `ProviderError`
- `ToolError`
- `HookError`
- `StorageError`

运行时遵循两条原则：

- 尽可能保留 root cause
- 不让观察型 hook 覆盖不可恢复错误

例如：

- provider 失败后，即使 `AfterRun` 再次报错，返回的仍应是原始 provider 错误
- tool transcript append 失败后，即使 `OnError` 或 `AfterRun` 想 yield，也不会把错误伪装成可恢复结果

---

## 8. 默认实现与推荐组合

最小嵌入建议：

- provider: `pkg/agentcore/mock` 或你自己的 provider 实现
- storage: `pkg/agentcore/files`
- memory: 可以先不接；需要上下文工程时再实现 `agentcore.Memory`
- tools: 默认空 registry 或自定义 registry
- prompt: 默认空 prompt，或 `pkg/agentcore/prompt.StaticBuilder`
- context: 默认 builder，或 `pkg/agentcore/context.DefaultBuilder`

如果你要桥接当前 Axon 内部实现：

- LLM: `internal/agentcoreadapters/internalllm`
- Tools: `internal/agentcoreadapters/internaltools`

---

## 9. 包布局

当前建议从这些包入口理解 `agentcore`：

- `pkg/agentcore`
  - 核心运行时与公共契约
- `pkg/agentcore/files`
  - 文件存储实现
- `pkg/agentcore/context`
  - 默认上下文构建器
- `pkg/agentcore/prompt`
  - 静态 prompt builder
- `pkg/agentcore/hooks`
  - hook chain 辅助实现
- `pkg/agentcore/mock`
  - provider / tool mock
- `internal/agentcoreadapters/internalllm`
  - internal `llm.Provider` -> `agentcore.Provider`
- `internal/agentcoreadapters/internaltools`
  - internal `tools.Registry` -> `agentcore.ToolRegistry`

---

## 10. 与长期蓝图的关系

[`docs/v1/01-agent-core.md`](../v1/01-agent-core.md) 讨论的是 Axon 最终 Agent Core 的演进路线，包括 planner、subagent、长期记忆、后台异步、多 agent 协作等更大范围能力。

`pkg/agentcore` 当前承担的是其中更小、更基础、更稳定的那一层：

- 一个单 Agent 可运行内核
- 一个统一的 transcript 与 resume contract
- 一组可替换的 runtime 扩展接口

后续更高层能力应优先复用 `agentcore` 的公共契约，而不是重新定义一套 runtime loop。

---

## 11. 对外阅读顺序

如果你是第一次接触 `agentcore`，推荐按这个顺序阅读：

1. [`docs/usage/agentcore-quickstart.md`](./agentcore-quickstart.md)
2. [`docs/usage/agentcore-api.md`](./agentcore-api.md)
3. `examples/agentcore/minimal`
4. `examples/agentcore/custom`
5. `examples/agentcore/resume`
6. `examples/agentcore/bridge`
