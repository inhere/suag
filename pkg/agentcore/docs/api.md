# Agentcore API Guide

这份文档面向要扩展 `agentcore` 的开发者，重点解释公共接口各自负责什么，以及这些接口之间的边界关系。

## 1. Runtime 入口

最主要的构造入口是：

```go
runtime := agentcore.New(agentcore.Config{...})
```

`Config` 当前主要包含：

- `Provider`
- `Storage`
- `Memory`
- `Tools`
- `PromptBuilder`
- `ContextBuilder`
- `Hooks`
- `MaxSteps`
- `MaxToolCallsPerStep`

其中：

- `Provider` 是必需的
- 其他组件如果没有显式提供，runtime 会尽量补默认实现

## 2. RunInput 与 RunResult

`RunInput` 是每次执行或恢复一次 run 的公共入口：

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

建议这样理解：

- `RunID`
  同一次 run 的稳定标识；resume 时应该复用
- `AgentID`
  当前 agent 的稳定标识
- `Instruction`
  本次新增输入；是否会被追加到 transcript，取决于 `ResumeMode`
- `Metadata`
  附加元数据
- `MaxSteps`
  覆盖默认 step 上限
- `ResumeMode`
  定义恢复语义

`RunResult` 是这次调用返回给 embedding host 的出口：

- `RunCompleted`
- `RunYielded`
- `RunFailed`

但真正用于恢复和 replay 的事实层仍然在 `Storage` 里。

## 3. Provider

`Provider` 负责把 runtime 已经准备好的模型请求，转换成一次稳定生成结果。

```go
type Provider interface {
	Name() string
	Generate(ctx context.Context, req *GenerateRequest) (*GenerateResult, error)
}
```

适合自己实现 `Provider` 的场景：

- 接 OpenAI / Anthropic / Gemini / Ollama
- 包一层现有内部 LLM SDK
- 在 provider 层做路由、审计、限流或重试

## 4. Storage

`Storage` 是 authority layer，负责保存：

- 最新 run snapshot
- append-only transcript

```go
type Storage interface {
	LoadRun(ctx context.Context, runID string) (*RunState, error)
	LoadMessages(ctx context.Context, runID string) ([]Message, error)
	SaveRun(ctx context.Context, state *RunState) error
	AppendMessages(ctx context.Context, runID string, msgs ...Message) error
}
```

建议：

- `LoadMessages` 保持追加顺序
- `AppendMessages` 保持 append 语义，不要做覆盖
- 即使底层用同一张表或同一份文件，也不要把 snapshot 和 transcript 混成一个公共对象

## 5. Memory

`Memory` 是上下文派生层，不是 authority transcript。

```go
type Memory interface {
	Hydrate(ctx context.Context, state *RunState) error
	ObserveMessages(ctx context.Context, state *RunState, messages ...Message) error
	BuildContext(ctx context.Context, state *RunState) ([]Message, error)
}
```

推荐职责拆分：

- `Hydrate`
  在 resume 或重新加载 state 后，把 transcript 同步到 memory 派生层
- `ObserveMessages`
  在 runtime 提交新消息后，增量更新 memory
- `BuildContext`
  输出最终给模型的上下文消息

如果你需要 summary、recent window、recall 或 context compaction，这一层最适合承载这些能力。

如果你只是想先用一个默认实现，而不是从零写自己的 `Memory`，当前可以直接看：

- `memory/`

它现在提供一个第一方 `WorkingMemory`，包含：

- summary slot
- recent transcript window
- 可配置的 system/tool 消息过滤

## 6. ToolRegistry

`ToolRegistry` 暴露可用 tool schema，并执行 provider 请求的 tool call。

```go
type ToolRegistry interface {
	ListTools(ctx context.Context) ([]ToolSchema, error)
	ExecuteTool(ctx context.Context, call ToolCall) (*ToolResult, error)
}
```

建议：

- `ListTools` 输出保持稳定顺序
- `ToolResult` 尽量补齐 `CallID` / `Name`
- 工具失败直接返回原始错误，让 runtime 包装成 `ToolError`

## 7. PromptBuilder 与 ContextBuilder

`PromptBuilder` 负责生成 system prompt：

```go
type PromptBuilder interface {
	BuildSystemPrompt(ctx context.Context, state *RunState) (string, error)
}
```

`ContextBuilder` 负责生成 provider messages：

```go
type ContextBuilder interface {
	BuildMessages(ctx context.Context, state *RunState) ([]Message, error)
}
```

可以把它们和 `Memory` 的关系理解成：

- `Memory`
  负责上下文派生内容
- `ContextBuilder`
  负责这些内容如何被组织成最终 messages

简单场景下，只有 `Memory` 就够了。
只有需要更强的消息排布控制时，才需要自定义 `ContextBuilder`。

如果你只是想直接使用第一方默认组合，而不是自己写 builder，当前可以看：

- `prompt.StaticBuilder`
- `prompt.Layered(...)`
- `context.TranscriptWindow(...)`
- `context.MemoryFirst(...)`
- `context.MemoryThenRecent(...)`

其中 prompt 侧分别覆盖：

- 完全静态 prompt
- `Base + Metadata + Sections` 的稳定文本组合

context 侧分别覆盖：

- transcript-only 路径
- default memory 路径
- memory + recent transcript 路径

`prompt.Layered(...)` 当前建议配置是：

```go
type MetadataConfig struct {
	Enabled bool
}

type Config struct {
	Base     string
	Metadata MetadataConfig
	Sections []string
}
```

稳定输出规则是：

- 顺序固定为 `Base -> metadata block -> Sections...`
- metadata block 标题固定为 `[Run metadata]`
- metadata key 按字典序排序
- 空 section 跳过
- 所有内容都为空时返回空字符串

## 8. Hook

`Hook` 是运行时生命周期扩展点。

当前主要节点：

- `BeforeRun`
- `BeforePromptBuild`
- `BeforeContextBuild`
- `BeforeModel`
- `AfterModel`
- `BeforeTool`
- `AfterTool`
- `AfterRun`
- `OnError`

它最适合做：

- 观测
- 审计
- 注入 metadata
- 轻量控制
- 局部修改 prompt / context / tool call / tool result

如果大量核心业务逻辑都只能藏在 hook 里，通常意味着核心 contract 需要升级。

## 9. Streaming

`agentcore` 当前已经支持一个最小 streaming contract。

基础事件模型是：

```go
type StreamEventKind string

const (
	StreamEventTextDelta   StreamEventKind = "text_delta"
	StreamEventMessageStop StreamEventKind = "message_stop"
)

type StreamEvent struct {
	Kind     StreamEventKind
	Delta    string
	Metadata map[string]any
}
```

host 侧通过 `RunInput.Stream` 接收事件：

```go
type StreamFunc func(ctx context.Context, event StreamEvent) error
```

provider 侧可以选择实现额外能力：

```go
type StreamingProvider interface {
	Provider
	Stream(ctx context.Context, req *GenerateRequest, emit StreamFunc) (*GenerateResult, error)
}
```

当前规则是：

- 只有当 provider 实现 `StreamingProvider` 且 caller 提供 `RunInput.Stream` 时，runtime 才走 streaming 路径
- 否则继续沿用现有 `Provider.Generate(...)`
- streaming delta 默认不直接进入 authority transcript
- 最终 `GenerateResult` 仍然是 authoritative completion

## 10. ResumeMode

`ResumeMode` 定义恢复时是否追加新的 user input：

- `ResumeFromState`
  只从已持久化状态继续
- `ResumeWithInput`
  先追加一条新的 user message 再继续
- `ResumeAuto`
  有输入就追加，没有输入就直接继续

推荐恢复流程理解为：

1. 用稳定 `RunID` 找回同一次 run
2. 加载 snapshot 与 transcript
3. 根据 `ResumeMode` 决定是否追加新的 user message
4. 再进入 runtime loop

## 11. 错误包装

runtime 会对错误做稳定分类：

- `ProviderError`
- `ToolError`
- `HookError`
- `StorageError`
- `MemoryError`

推荐在 embedding host 中围绕这些错误类型做统一策略，而不是依赖字符串匹配。

需要特别注意两点运行语义：

- provider 失败后，即使后续观察型 hook 继续上报错误，返回值仍应保留原始 provider root cause
- transcript append / 持久化失败属于更高优先级问题，因为它影响 authority transcript 的可信度

## 12. 最小扩展路径

推荐按这个顺序扩展：

1. `mock + files`
2. 自定义 `Tool`
3. 自定义 `Hook`
4. 跑通 `yield -> resume`
5. 接入 `Memory`
6. 最后接真实 `Provider` 和自定义 `Storage`

如果你不只是想看单个接口，而是想直接从当前更推荐的组合起步，再继续看：

- [recommended-runtime.md](./recommended-runtime.md)
- [host-integration.md](./host-integration.md)
- [migration.md](./migration.md)

## 13. 下一阶段 API 演进重点

如果从未来独立发布和长期演进角度看，当前更值得关注的不是继续堆更多默认实现，而是先把下面几类 public surface 稳住。

### 13.1 Conformance Testing

仅有接口定义还不够，外部实现还需要验证自己是否真正满足 contract。

下一阶段推荐补一层 package-local conformance/testing：

- `Storage` 的 append-only transcript contract tests
- `ToolRegistry` 的 schema / execute contract tests
- provider / streaming 的最小兼容 helper

这层能力的定位是：

- 只依赖 public API
- 帮助外部实现自证兼容
- 不把仓库内部测试工具直接暴露为正式 runtime 功能

### 13.2 Policy Promotion

当前 runtime 中已经存在一些稳定但尚未显式抽象的策略语义，例如：

- resume 时是否追加输入
- tool call 的执行上限
- 某些错误是否应该 yield 而不是 fail

如果这些策略未来频繁依赖 hook 才能表达，更合理的方向通常不是继续堆 hook，而是提升为更小、更明确的正式 contract。

### 13.3 Storage Optional Capabilities

`Storage` 当前保持最小接口是对的，但随着独立 embedding 增加，后续很可能需要 additive optional capabilities，例如：

- artifact store
- run listing
- checkpoint store
- transactional / compare-and-swap 能力

推荐做法是：

- 保持当前主接口不膨胀
- 通过 optional interface 向上增长
- 继续守住 `Storage = authority` 的边界

### 13.4 Streaming Phase 2

当前 streaming contract 已经够最小，但还不是最终形态。

下一阶段更合理的方向是：

- additive 扩展 event model
- 明确 host-facing 增量输出语义
- 不让 stream delta 进入 authority transcript
- 不让 streaming 改写 `resume` 与 `final completion` 的核心规则

如果你准备围绕这些方向扩展 `agentcore`，推荐同时参考：

- [streaming-design.md](./streaming-design.md)
- [release-readiness.md](./release-readiness.md)
- [roadmap.md](./roadmap.md)
