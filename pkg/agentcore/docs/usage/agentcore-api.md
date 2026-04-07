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

### `RunInput`

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
  本次新增的 user input；是否真的追加到 transcript，取决于 `ResumeMode`
- `Metadata`
  本次 run 的附加元数据
- `MaxSteps`
  覆盖默认 step 上限
- `ResumeMode`
  定义恢复语义

### `RunResult`

`RunResult` 是本次执行的公开出口：

- `RunCompleted`
  这次 run 正常完成并给出最终输出
- `RunYielded`
  这次 run 主动暂停，外部可以稍后继续
- `RunFailed`
  运行失败

注意：

- 真正用于恢复和 replay 的事实层仍然是 `Storage` 中的 snapshot + transcript
- `RunResult` 更像是“这次调用返回给 embedding host 的结果”

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

### 建议

- `LoadMessages` 保持 append 顺序，不要重排
- `AppendMessages` 尽量保证追加语义，而不是覆盖语义
- 如果你的底层存储同时保存 snapshot 和 transcript，也不要把两者在接口层混成一个对象

---

## 4. Memory

### 职责

`Memory` 负责构建“给模型看的上下文派生层”，而不是 authority transcript。

接口：

```go
type Memory interface {
    Hydrate(ctx context.Context, state *RunState) error
    ObserveMessages(ctx context.Context, state *RunState, messages ...Message) error
    BuildContext(ctx context.Context, state *RunState) ([]Message, error)
}
```

### 推荐理解

- `Hydrate`
  在 resume 或重新加载 state 后，把 authoritative transcript 同步进 memory 的派生层
- `ObserveMessages`
  在 runtime 正式提交消息后，增量更新 memory
- `BuildContext`
  输出最终给模型的上下文消息

### 什么时候该实现它

- 需要 summary / recall / recent window
- 需要对 transcript 做上下文压缩
- 想把“事实层存储”和“模型上下文层”明确分离

### 什么时候先不要实现它

- 只是想先把 runtime 跑起来
- 只是需要最简单的 transcript 直传

如果没有显式配置 `ContextBuilder`，runtime 默认会走 `Memory.BuildContext(...)` 这条路径。

---

## 5. ToolRegistry

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

## 6. PromptBuilder

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

## 7. ContextBuilder

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

当前如果你想直接用第一方 builder，而不是自己实现，优先看：

- `pkg/agentcore/context.TranscriptWindow(...)`
- `pkg/agentcore/context.MemoryFirst(...)`
- `pkg/agentcore/context.MemoryThenRecent(...)`

### 它和 `Memory` 的关系

可以把这两个接口理解成两个层级：

- `Memory`
  生成“已经过上下文工程处理”的派生内容
- `ContextBuilder`
  决定这些内容最终怎么组织成 provider messages

简单场景下，只有 `Memory` 就够了。
只有当你需要更强的消息排布控制时，才需要显式实现 `ContextBuilder`。

---

## 8. Hook

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

### Control 能力

Hook 可以通过 `Control` 触发两类控制信号：

- `Yield`
  暂停这次 run，并返回 `RunYielded`
- `ShortCircuit`
  用替代结果提前结束当前阶段

但不是所有阶段都适合把 control 当作真正的主控制流。

一个实用判断是：

- `Before*` 阶段更适合做拦截和控制
- `After*` 阶段更适合做观察、补充和审计

如果你的大量主逻辑都只能靠 hook 完成，通常意味着核心 contract 还需要升级，而不是继续往 hook 里堆行为。

---

## 9. Resume 与 transcript

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

推荐把恢复流程理解成：

1. 通过稳定 `RunID` 找回已有 run
2. 加载 snapshot 与 transcript
3. 根据 `ResumeMode` 决定是否追加新的 user message
4. 重新进入 runtime loop

如果你只想从暂停点继续，不要再重复把原始 instruction 作为新输入传进去。

---

## 10. 错误分类

runtime 会把错误分类成稳定的公开类型：

- `ProviderError`
- `ToolError`
- `HookError`
- `StorageError`

推荐在上层围绕这些分类做统一策略，而不是解析字符串。

如果你在 embedding host 里做策略分发，最常见的模式是：

- `ProviderError`
  重试、降级 provider、上报供应商故障
- `ToolError`
  记录工具失败并决定是否允许模型继续
- `HookError`
  把扩展层异常和核心 runtime 异常分开处理
- `StorageError`
  提高优先级，因为它影响 authority log 的可信度

---

## 11. 推荐扩展路径

推荐按这个顺序扩展：

1. 用 `mock + files` 跑通最小链路
2. 加一个自定义 `Tool`
3. 加一个 `Hook`
4. 跑一遍 `yield -> resume`
5. 再替换 `PromptBuilder` / `ContextBuilder` 或接入 `Memory`
6. 最后接真实 `Provider` 和自定义 `Storage`
