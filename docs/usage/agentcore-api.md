# Agentcore API 与扩展指南

这份文档面向准备二次扩展 `pkg/agentcore` 的开发者。

重点不是解释内部实现细节，而是回答这些问题：

- 这个接口负责什么
- 我应该在什么场景实现它
- 它和其它接口怎么配合
- 哪些地方是稳定扩展点，哪些地方只是默认实现

---

## 1. 运行时入口

### `agentcore.New(cfg Config) *DefaultRuntime`

这是最主要的构造入口。

`Config` 当前包含：

- `Provider`
- `Storage`
- `Tools`
- `PromptBuilder`
- `ContextBuilder`
- `Hooks`
- `MaxSteps`
- `MaxToolCallsPerStep`

如果你没有传某些组件，runtime 会尽量补默认值：

- `Storage`
- `Tools`
- `PromptBuilder`
- `ContextBuilder`

但 `Provider` 是必需的。

---

## 2. Provider

### 职责

`Provider` 负责把一个已经准备好的模型请求，转换成一次稳定的生成结果。

接口：

```go
type Provider interface {
	Name() string
	Generate(ctx context.Context, req *GenerateRequest) (*GenerateResult, error)
}
```

### 你什么时候该实现它

- 想直接接 OpenAI / Anthropic / Gemini / Ollama
- 想把现有内部 LLM SDK 包一层
- 想在 provider 层做统一重试、审计或路由

### 输入输出要求

- 输入的 `Messages`、`Tools`、`System` 已经是 runtime 整理好的结果
- 输出必须返回合法的 `GenerateResult`
- `FinishReason` 必须与 payload 形状一致

也就是说：

- final 文本输出 -> `FinishFinal`
- tool calls 输出 -> `FinishToolCalls`

---

## 3. Storage

### 职责

`Storage` 负责持久化两类东西：

- 最新 run snapshot
- append-only message transcript

接口：

```go
type Storage interface {
	LoadRun(ctx context.Context, runID string) (*RunState, error)
	LoadMessages(ctx context.Context, runID string) ([]Message, error)
	SaveRun(ctx context.Context, state *RunState) error
	AppendMessages(ctx context.Context, runID string, msgs ...Message) error
}
```

### 为什么是 snapshot + append log 两层

这是当前 runtime 的重要设计点：

- snapshot 方便快速恢复状态
- append-only transcript 更适合作为 authority log

当两者出现分叉风险时，runtime 更偏向保护 transcript 的完整性。

---

## 4. ToolRegistry

### 职责

`ToolRegistry` 负责两件事：

- 暴露当前可供 provider 选择的 tool schema
- 执行 provider 请求的 tool call

接口：

```go
type ToolRegistry interface {
	ListTools(ctx context.Context) ([]ToolSchema, error)
	ExecuteTool(ctx context.Context, call ToolCall) (*ToolResult, error)
}
```

### 建议

- `ListTools` 输出保持稳定顺序
- `ExecuteTool` 返回的 `ToolResult` 尽量补齐 `CallID` / `Name`
- 工具执行失败时返回原始错误，让 runtime 包装为 `ToolError`

---

## 5. PromptBuilder

### 职责

`PromptBuilder` 从当前 `RunState` 生成 provider 的 system prompt。

接口：

```go
type PromptBuilder interface {
	BuildSystemPrompt(ctx context.Context, state *RunState) (string, error)
}
```

适合放在这里的逻辑：

- 静态系统提示词
- 基于 run metadata 的 prompt 组装
- 多 agent 角色 prompt 注入

---

## 6. ContextBuilder

### 职责

`ContextBuilder` 从 `RunState` 生成 provider 的 message 列表。

接口：

```go
type ContextBuilder interface {
	BuildMessages(ctx context.Context, state *RunState) ([]Message, error)
}
```

最适合承载：

- 滑动窗口
- 上下文裁剪
- 记忆结果注入成 messages

当前默认实现是 `pkg/agentcore/context.DefaultBuilder`。

---

## 7. Hook

### 职责

`Hook` 提供运行生命周期上的观察和扩展点。

当前节点：

- `BeforeRun`
- `BeforePromptBuild`
- `BeforeContextBuild`
- `BeforeModel`
- `AfterModel`
- `BeforeTool`
- `AfterTool`
- `AfterRun`
- `OnError`

最适合用 Hook 做的事：

- 注入 metadata
- 做审计日志
- 做轻量策略控制
- 修改 tool call / tool result

---

## 8. Resume 与 transcript

`ResumeMode` 决定恢复时是否追加新的 user input：

- `ResumeAuto`
- `ResumeFromState`
- `ResumeWithInput`

当前推荐按下面理解：

- `ResumeFromState`
  无论是否传入 `Instruction`，都只从已持久化的 state / transcript 继续。
- `ResumeWithInput`
  恢复前显式追加一条新的 user message，再继续执行。
- `ResumeAuto`
  默认走“有输入就追加、无输入就直接继续”的语义。

需要特别注意：

- tool loop 会持久化 `assistant(tool_call)` 与 `tool(result)` 两条消息
- transcript 是 resume / replay / adapter 互通的关键契约

---

## 9. 错误分类

runtime 会把错误分类成稳定的公开类型：

- `ProviderError`
- `ToolError`
- `HookError`
- `StorageError`

推荐在上层围绕这些分类做统一策略，而不是解析字符串。

---

## 10. 推荐扩展路径

推荐按这个顺序扩展：

1. 用 `mock + files` 跑通最小链路
2. 加一个自定义 `Tool`
3. 加一个 `Hook`
4. 再替换 `PromptBuilder` / `ContextBuilder`
5. 最后接真实 `Provider` 和自定义 `Storage`
