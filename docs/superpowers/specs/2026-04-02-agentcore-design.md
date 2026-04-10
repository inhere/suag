# Agent Core Loop 设计说明

> 状态：Draft for review
> 日期：2026-04-02
> 范围：`pkg/agentcore` 最小但可扩展的运行时内核
> 相关文档：`docs/PRD.md`、`docs/SPEC.md`、`docs/archive/v1-old/01-agent-core.md`、`docs/archive/v1-old/04-tools-skills.md`

---

## 1. 文档目的

本文档定义一个独立的 `pkg/agentcore` 包设计。

这个包的定位是：

- 任何 Axon Agent 启动运行时都必须依赖的核心执行内核
- 可以嵌入 CLI、HTTP Handler、Cron Worker、上层编排器等任意宿主
- 通过小接口和生命周期 Hooks 提供扩展能力
- 在 v1 即可形成完整可运行的 Agent Core Loop，同时保持足够克制

本文档不定义完整 Axon 平台，只定义可复用的 Agent Runtime Kernel。

---

## 2. 问题背景

当前仓库已经存在 `internal/agent`、`internal/llm`、`internal/memory`、`internal/tools` 等工作中的模块。这些模块对于 Axon 应用本身是有价值的，但它们还不是一个稳定、可嵌入、可独立演进的公共 Core 包。

项目现在需要一个更小、更清晰的运行时抽象，用来满足以下目标：

- 可以被外部直接 import，而不需要拉入整个应用架构
- 可以独立于 orchestrator、team、gateway、HITL 等上层模块演进
- 对 provider、storage、prompt、context、tool 等能力暴露正确的扩展点
- 足够小，能快速实现、充分测试，并适应外部 Agent 生态的快速变化

---

## 3. 目标

`pkg/agentcore` v1 必须满足：

- 提供完整的单 Agent Core Loop
- 支持多轮执行和 Tool Call 回路
- 提供 provider、storage、tool、prompt、context、hook 等接口
- 内置最小的文件存储实现
- 仅依赖默认实现加一个 provider 就可以跑起来
- 对外 API 保持小而稳定
- 不和 Axon 应用级运行时职责强绑定

### 3.1 成功标准

如果满足以下条件，就说明设计是成功的：

- 调用方可以构造 `agentcore.Runtime` 并运行一个任务
- runtime 可以把 run state 持久化到文件，并在需要时由调用方重新加载
- runtime 可以执行一轮或多轮 tool call，再输出最终结果
- hooks 可以拦截关键生命周期节点、修改阶段输入、在受控范围内短路默认行为
- 当前 Axon 的 `internal/*` 能力后续可以通过 adapter 接入，而不是反向污染 core 包边界

---

## 4. 非目标

以下内容明确不属于 `pkg/agentcore` v1 范围：

- 多 Agent 编排
- planner 或 task DAG 执行
- provider pool 或自动 failover
- 长期记忆、向量检索、语义召回
- gateway API、SSE、Web session 管理
- HITL 管理流程
- 权限系统或策略引擎
- 分布式执行
- 平台级事件总线
- workflow DSL 或 skill engine

这些能力可以在后续叠加到 core 之上，但不是 core 包自身的职责。

---

## 5. 总体架构

本次选择的架构是：

**Runtime + Components + Hooks**

`Runtime` 是唯一执行入口。它内部协调一组可替换组件：

- `Provider`
- `Storage`
- `ToolRegistry`
- `PromptBuilder`
- `ContextBuilder`
- `HookChain`

这样做可以保证外部嵌入接口足够简单，同时保留显式扩展点。

### 5.1 为什么选择这个方案

相比纯 Pipeline-first 方案，这个方案更适合承载 loop 控制、状态持久化和一致性收口。

相比 StateMachine-first 方案，这个方案更适合 v1 阶段，因为它避免为了未来可能的复杂编排能力，在今天过早引入过重的状态机设计。

---

## 6. 包结构

初始包结构建议保持紧凑：

```text
pkg/agentcore/
  runtime.go
  types.go
  provider.go
  storage.go
  tools.go
  prompt.go
  context.go
  hooks.go
  errors.go
  files/
    storage.go
  prompt/
    static.go
  context/
    default.go
  hooks/
    chain.go
  mock/
    provider.go
    tool.go
```

这里有一个关键原则：

- `pkg/agentcore` 自己定义稳定接口
- `internal/*` 不直接出现在 core 包 API 中
- 当前 Axon 模块和 core 包之间的兼容，应通过 adapter 解决

---

## 7. 核心模型

runtime 围绕三个核心对象运转：

- `RunInput`：调用方发起什么任务
- `RunState`：执行过程中的唯一事实来源
- `RunResult`：返回给调用方的最终结果

### 7.1 RunInput

```go
type RunInput struct {
    RunID       string
    AgentID     string
    Instruction string
    Metadata    map[string]any
    MaxSteps    int
    ResumeMode  ResumeMode
}
```

```go
type ResumeMode string

const (
    ResumeAuto          ResumeMode = "auto"
    ResumeFromState     ResumeMode = "from_state"
    ResumeWithInput     ResumeMode = "with_input"
)
```

### 7.2 RunState

```go
type RunState struct {
    RunID      string
    AgentID    string
    Status     RunStatus
    Step       int
    Messages   []Message
    Artifacts  map[string]Artifact
    Metadata   map[string]any
    Scratch    map[string]any
    Usage      Usage
    LastError  string
}
```

### 7.3 RunResult

```go
type RunResult struct {
    RunID   string
    Status  RunStatus
    Output  string
    Steps   int
    Usage   Usage
    Yield   *Yield
}
```

```go
type Yield struct {
    Reason   string
    ResumeOK bool
    Metadata map[string]any
}
```

### 7.4 状态模型

v1 的状态建议保持克制：

```go
type RunStatus string

const (
    RunIdle      RunStatus = "idle"
    RunRunning   RunStatus = "running"
    RunYielded   RunStatus = "yielded"
    RunCompleted RunStatus = "completed"
    RunFailed    RunStatus = "failed"
)
```

这只是 runtime 运行态模型，不是平台级生命周期模型。

补充说明：

- v1 的 core loop 是同步执行模型，不引入异步 tool waiting 状态
- 如果未来需要异步 tool orchestration 或外部等待，再在更高层状态模型中补充

---

## 8. 核心接口

### 8.1 Runtime

```go
type Runtime interface {
    Run(ctx context.Context, input RunInput) (*RunResult, error)
}
```

### 8.2 Provider

```go
type Provider interface {
    Generate(ctx context.Context, req *GenerateRequest) (*GenerateResult, error)
    Name() string
}
```

`GenerateResult` 至少需要满足这些最小判定规则：

- final answer 与 tool calls 在同一轮结果中互斥
- 如果返回 tool calls，则每个 tool call 必须带稳定的 `CallID`
- 空内容且无 tool calls 的结果视为 provider 非法返回
- provider 必须明确告诉 runtime 当前轮是 `final` 还是 `tool_calls`

### 8.3 Storage

```go
type Storage interface {
    LoadRun(ctx context.Context, runID string) (*RunState, error)
    LoadMessages(ctx context.Context, runID string) ([]Message, error)
    SaveRun(ctx context.Context, state *RunState) error
    AppendMessages(ctx context.Context, runID string, msgs ...Message) error
}
```

### 8.4 Tool 与 ToolRegistry

```go
type Tool interface {
    Name() string
    Description() string
    Schema() ToolSchema
    Call(ctx context.Context, call ToolCall) (*ToolResult, error)
}

type ToolRegistry interface {
    Get(name string) (Tool, bool)
    List() []Tool
    Schemas() []ToolSchema
}
```

### 8.5 PromptBuilder 与 ContextBuilder

```go
type PromptBuilder interface {
    BuildSystemPrompt(ctx context.Context, state *RunState) (string, error)
}

type ContextBuilder interface {
    BuildMessages(ctx context.Context, state *RunState) ([]Message, error)
}
```

### 8.6 Hooks

```go
type Hook interface {
    Name() string
    BeforeRun(ctx context.Context, event *RunEvent) error
    BeforePromptBuild(ctx context.Context, event *PromptEvent) error
    BeforeContextBuild(ctx context.Context, event *ContextEvent) error
    BeforeModel(ctx context.Context, event *ModelEvent) error
    AfterModel(ctx context.Context, event *ModelEvent) error
    BeforeTool(ctx context.Context, event *ToolEvent) error
    AfterTool(ctx context.Context, event *ToolEvent) error
    AfterRun(ctx context.Context, event *RunEvent) error
    OnError(ctx context.Context, event *ErrorEvent) error
}
```

---

## 9. Loop 行为

core loop 应遵循如下执行顺序：

1. 初始化或加载 `RunState`
2. 根据恢复模式决定是否追加当前用户输入消息
3. 持久化最新状态
4. 通过 `PromptBuilder` 构造 system prompt
5. 通过 `ContextBuilder` 构造本轮模型消息
6. 触发 `BeforeModel`
7. 调用 `Provider.Generate`
8. 触发 `AfterModel`
9. 如果模型返回 final answer：
   追加 assistant 消息、标记完成、持久化并返回结果
10. 如果模型返回 tool calls：
    逐个执行 `BeforeTool -> Tool.Call -> AfterTool`
11. 把 tool result 写回 messages 和 artifacts
12. step +1，进入下一轮，直到完成或失败

### 9.1 Loop 不变量

runtime 需要保证以下不变量成立：

- `RunState` 是 runtime 在内存中的唯一协调对象
- 持久化后的消息历史以 storage 层消息日志为准
- loop 内所有公共交互语言都收敛成 `Message`
- 状态持久化只能由 runtime 控制，hooks 不直接篡改底层存储
- 每一步结束后都能形成可持久化状态
- 每次 run 最终都要落到 `completed`、`yielded` 或 `failed`

### 9.2 新建与恢复语义

v1 必须把 `RunInput.RunID` 的语义定义清楚：

- 当 `RunID` 为空时，runtime 生成一个新的 run id，并创建新 run
- 当 `RunID` 非空且 storage 中不存在对应 run 时，按新 run 处理
- 当 `RunID` 非空且 storage 中已存在对应 run 时，runtime 执行恢复逻辑

恢复输入矩阵如下：

| 场景 | `RunID` | `ResumeMode` | 是否追加 `Instruction` |
|------|---------|--------------|-------------------------|
| 新建 run | 不存在 | 任意 | 是 |
| 崩溃后无新输入恢复 | 已存在 | `ResumeFromState` | 否 |
| 同一 run 带新人工指令继续 | 已存在 | `ResumeWithInput` | 是 |
| 自动模式 | 已存在 | `ResumeAuto` | 若 `Instruction` 为空则否，否则是 |

恢复逻辑规则如下：

- `completed`、`failed` 状态的 run 不允许恢复，直接返回终态错误
- `idle`、`running`、`yielded` 状态的 run 允许恢复
- 进程中断、宿主重启等中断场景下，调用方可以使用同一个 `RunID` 重新进入 `Runtime.Run`

恢复时 runtime 需要：

- 先通过 `LoadRun` 取回最新状态快照
- 再通过 `LoadMessages` 取回消息日志，并重建 `RunState.Messages`
- 从已持久化的 `Step`、`Artifacts`、`Metadata`、`Usage` 继续执行

当执行恢复时：

- `ResumeFromState` 模式不追加新的 user message，只从已有状态继续
- `ResumeWithInput` 模式把新的 `Instruction` 作为一条新的 user message 追加后再继续
- `ResumeAuto` 是调用方便利模式，其行为必须退化为上表中的明确规则，不能额外引入隐式语义

### 9.3 终止语义

命中以下条件时，run 进入不可恢复终态：

- `MaxSteps` 超限
- `ProviderError`
- `HookError`
- `StorageError`

`ToolError` 不进入终态，而是默认转成消息继续下一轮。

### 9.4 受控让渡语义

v1 虽然不内置完整 HITL 或异步等待机制，但 core 需要支持“把控制权交回宿主”的最小契约。

该契约使用 `RunYielded` 状态和 `RunResult.Yield` 表达：

- 当 hook 或 runtime 认为当前 run 需要由宿主决定下一步时，可以返回 `yielded`
- `yielded` 不是失败，且允许后续恢复
- 宿主可以在稍后使用相同 `RunID` 和适当的 `ResumeMode` 再次调用 `Runtime.Run`

典型场景包括：

- 权限审批后再继续
- 宿主要求延后执行
- 规则命中后把决策权交回上层

v1 不要求内置审批 UI、队列或等待机制，只要求 core 能表达这种结果。

---

## 10. Hook 合约

Hooks 是一等扩展机制，但必须保持可预测。

### 10.1 Hook 分类

hooks 按能力可以分成三类：

- 观测型：只读取状态和阶段数据
- 改写型：修改当前阶段输入
- 短路型：替换某个阶段的默认行为

### 10.2 允许修改的边界

hooks 可以修改如下阶段局部输入：

- model 前的 system prompt
- model 前的 message 列表
- model 前的 provider request 参数
- tool 执行前的 tool 参数

hooks 不应该直接做以下事情：

- 绕过 runtime 直接操作底层存储
- 私自推进或回退 step 计数
- 不经 runtime 控制流直接改写最终状态

### 10.3 短路边界

v1 只允许两个阶段短路：

- `BeforeModel`
- `BeforeTool`

这样就能支持最实际的一批扩展场景：

- provider response cache
- 规则引擎直接回复
- 权限拒绝
- mock
- 测试注入

如果短路逻辑希望把控制权交回宿主，而不是直接失败或直接完成，应返回 `yielded` 结果，而不是伪装成 `failed`。

### 10.4 Event 数据形状

事件对象应至少暴露：

- 只读 state view
- 当前阶段可修改的输入或输出对象
- 控制字段，例如 short-circuit、terminate、replacement result

例如：

```go
type ModelEvent struct {
    State   *RunStateView
    Request *GenerateRequest
    Result  *GenerateResult
    Control *Control
}
```

### 10.5 Hook 执行顺序与冲突规则

v1 应明确 hook 执行策略：

- hooks 按注册顺序执行
- 前一个 hook 的改动，对后一个 hook 可见
- 如果多个 hook 修改同一字段，后执行者覆盖前执行者
- 一旦某个 hook 在允许短路的阶段设置 short-circuit，runtime 停止该阶段的默认执行
- v1 中，命中 short-circuit 后默认不再继续执行后续同阶段 hooks，直接进入该阶段结果处理

---

## 11. v1 默认内置实现

v1 只内置让 runtime 独立可用所需的最小实现。

### 11.1 v1 内置内容

- `DefaultRuntime`
- `DefaultToolRegistry`
- `files.Storage`
- `prompt.StaticBuilder`
- `context.DefaultBuilder`
- `hooks.Chain`
- `mock.Provider`
- `mock.Tool`

### 11.2 v1 不内置的内容

- provider pool
- retrieval-augmented context builder
- long-term memory store
- planner
- HITL orchestration
- 分布式或异步事件总线

---

## 12. 文件存储设计

默认存储实现采用 files，原因是简单、透明、可嵌入。

### 12.1 目录结构

每个 run 对应一个目录：

```text
<root>/<run-id>/
  state.json
  messages.jsonl
  meta.json
  artifacts/
```

### 12.2 职责划分

- `state.json` 保存最新的 `RunState` 快照，但其中的 `Messages` 仅作为运行时缓存或调试快照
- `messages.jsonl` 保存 append-only 的消息历史，并作为消息持久化的权威来源
- `meta.json` 保存轻量元数据，如果后续需要与 state 分离
- `artifacts/` 保存中间产物或产物引用

### 12.3 为什么 v1 优先 files

因为 files 具备这些优点：

- 零外部基础设施依赖
- 调试时天然可读
- 非常适合本地嵌入式场景
- 对 v1 的 core contract 已经足够

后续如果需要 SQLite 或远程存储，直接通过 `Storage` 接口替换即可。

### 12.4 加载优先级

为了避免 `state.json` 和 `messages.jsonl` 双写漂移，v1 约定：

- `LoadRun` 负责读取状态快照
- `LoadMessages` 负责读取权威消息历史
- runtime 在恢复 run 时，总是以 `messages.jsonl` 重建 `RunState.Messages`
- `state.json` 中的 `Messages` 不作为恢复时的权威数据源

---

## 13. Prompt 与 Context 设计

Prompt Engineering 和 Context Engineering 必须分开。

### 13.1 PromptBuilder 职责

`PromptBuilder` 负责：

- 构建 system prompt
- 注入 runtime 级指导信息
- 合并静态说明和可选 metadata 衍生片段

v1 默认内置行为：

- 支持静态 prompt
- 支持通过 hook 追加 prompt 片段
- 不强依赖模板引擎

### 13.2 ContextBuilder 职责

`ContextBuilder` 负责：

- 决定哪些消息进入模型上下文
- 组织消息顺序
- 应用轻量截断策略

v1 默认内置行为：

- 包含最近消息历史
- 包含当前用户指令
- 包含最近 tool results
- 通过消息数或简单 token 估算进行裁剪

v1 不包含高级压缩或检索增强。

---

## 14. Provider 合约

Provider 是 runtime 和外部 LLM 之间的适配边界。

### 14.1 必须支持的能力

provider contract 至少要支持：

- 普通文本生成
- tool call 返回
- 通过 `Name()` 暴露 provider 标识

### 14.2 v1 有意简化的点

v1 不强制要求：

- streaming
- embedding
- 多 provider failover

这些能力都可以后续补充，而不需要改变 runtime 唯一入口。

---

## 15. Tool 执行模型

runtime 支持零个或多个 tools。

### 15.1 Tool 执行方式

当模型返回 tool calls 时：

- runtime 从 registry 中解析 tool
- 按模型给出的顺序逐个调用
- 每个结果都写成消息
- 如有需要，把产物引用写入 `RunState.Artifacts`

### 15.2 Tool 错误处理

Tool Error 默认不直接终止 run。

默认策略是：

- 把错误转换成一条对模型可见的消息
- 追加到 run history
- 继续下一轮 loop，让模型自行决定是修正参数、换路径还是给出 fallback answer

这也是更适合 agent core 的默认行为。

---

## 16. 错误处理

v1 应把错误分类压缩为少量 runtime 级类型。

### 16.1 ProviderError

- 来源：provider 请求失败、超时、返回结构非法
- 默认行为：立即 fail 当前 run

### 16.2 ToolError

- 来源：tool 执行失败
- 默认行为：转成 tool message 后继续

### 16.3 HookError

- 来源：hook 执行失败
- 默认行为：立即 fail 当前 run

原因是：

hook 已经参与关键阶段输入改写，如果 hook 出错却静默忽略，会让 runtime 行为变得不可相信。

### 16.4 StorageError

- 来源：load、append、save 失败
- 默认行为：立即 fail 当前 run

原因是：

一旦持久化不可信，runtime 的一致性就不再可信。

---

## 17. 基础保护机制

v1 必须包含最基础的执行保护：

- `MaxSteps`，防止无穷 loop
- `MaxToolCallsPerStep`，防止某一轮返回过量 tool calls

当命中保护阈值时，runtime 应返回明确错误并结束执行。

---

## 18. 兼容策略

`pkg/agentcore` 是一个新的、刻意与当前 `internal/*` 模块解耦的包。

兼容应该通过 adapter 完成，而不是让现有 internal 类型泄漏到新的公共 API 中。

未来可预期的 adapter 例如：

- `internal/llm.Provider` 到 `agentcore.Provider` 的 adapter
- `internal/tools.Registry` 到 `agentcore.ToolRegistry` 的 adapter
- `internal/memory` 或现有 run logs 到 `agentcore.Storage` 的 adapter

这样可以让 core 包先稳定下来，而不用一次性重构整个仓库。

---

## 19. 测试要求

后续 implementation plan 必须覆盖这些测试场景：

- final answer 直出流程
- 单个 tool call 流程
- 多轮 `tool -> model -> final` 流程
- hook 改写 model request
- hook 改写 tool args
- hook 短路 model 阶段
- hook 短路 tool 阶段
- tool error 恢复流程
- max steps 失败流程
- file storage 持久化格式
- runtime 从 storage reload 的流程

为了支撑这些测试，`mock.Provider` 和 `mock.Tool` 是必须内置的。

---

## 20. 有意延后的开放决策

以下问题刻意延后到 v1 实现完成后再决定：

- `Storage` 是否要直接接管 artifact 二进制写入
- streaming 是否应该进入 core provider contract
- async tool execution 应该属于 core，还是由上层包装
- resumable step snapshot 是否需要独立 checkpoint 接口
- 未来事件总线是补充 hooks，还是替代一部分 hooks 职责

这些问题不应该阻塞 v1 实现。

---

## 21. 下一步建议

在这份 spec 审核通过后，下一份文档应是 implementation plan，内容包括：

- 创建 `pkg/agentcore`
- 添加核心接口与 runtime
- 实现 file storage 和默认 builders
- 添加 mocks 与测试
- 说明当前 Axon internals 如何通过 adapters 逐步接入

实现时建议从最小可运行 loop 开始，逐层补上 storage、hooks、tools 和测试，不要把 scope 扩张到 orchestration 或平台能力。
