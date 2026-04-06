# agentcore

`pkg/agentcore` 是 Axon 中抽离出来的最小 Agent Runtime Core，目标是将来可以作为独立包发布和嵌入使用。

它负责任何 agent 运行都绕不开的那条核心链路：

- 准备或恢复 run state
- 构建和维护 memory-driven context
- 构建 prompt 与 context
- 调用模型 provider
- 执行 tool calls
- 持久化 snapshot 与 transcript
- 暴露 hooks 与稳定错误分类

它不负责完整产品层：

- 多 agent 编排
- 产品级长期记忆策略
- tracing 控制面
- Web / API / 渠道接入
- governance / review / evolution workflow

## Core Contracts

- `Provider`
  连接任意 LLM backend，返回 final answer 或 tool calls。
- `Storage`
  保存 run snapshot 与 append-only transcript，是恢复和审计的事实来源。
- `Memory`
  保存上下文工程所需的派生视图，例如 summary、window、retrieval recall。
- `ToolRegistry`
  暴露 tool schema 并执行工具调用。
- `PromptBuilder`
  生成 system prompt。
- `ContextBuilder`
  组装最终发给模型的消息；不显式提供时，默认走 `Memory.BuildContext(...)`。
- `Hook`
  拦截和修改关键生命周期节点。

其中一个关键边界是：

- `Storage` 负责 authoritative transcript
- `Memory` 负责 model-facing context

这两个层次可以使用同一个底层系统，也可以完全分离，但语义上不应混为一谈。

## Package Layout

- `.`  
  Public runtime contracts and default runtime implementation
- `files/`  
  Filesystem-backed storage
- `context/`  
  Default message-window context builder
- `prompt/`  
  Static prompt builder
- `hooks/`  
  Ordered hook-chain helper
- `mock/`  
  Test provider and tool implementations

注意：

- `pkg/agentcore` 本身不应依赖仓库的 `internal/*` 包
- 与 Axon 内部 `llm`、`tools` 等实现的桥接 adapter 放在 `internal/agentcoreadapters/`

## Docs

- Quickstart: `docs/usage/agentcore-quickstart.md`
- API guide: `docs/usage/agentcore-api.md`
- Design note: `docs/usage/agentcore.md`
- Roadmap: `pkg/agentcore/docs/roadmap.md`
- Examples: `examples/agentcore/`

## Quick Example

```go
runtime := agentcore.New(agentcore.Config{
    Provider: mock.NewProvider(mock.Final("hello")),
    Storage:  files.NewStorage(".agentcore-runs"),
})

result, err := runtime.Run(ctx, agentcore.RunInput{
    RunID:       "example-run",
    AgentID:     "example-agent",
    Instruction: "say hello",
})
```

## Memory And Context

如果你需要 summary / recall / context compaction，不应该把这些逻辑塞进 `Storage`。

更推荐的方式是实现 `agentcore.Memory`：

```go
type Memory interface {
    Hydrate(ctx context.Context, state *agentcore.RunState) error
    ObserveMessages(ctx context.Context, state *agentcore.RunState, messages ...agentcore.Message) error
    BuildContext(ctx context.Context, state *agentcore.RunState) ([]agentcore.Message, error)
}
```

推荐职责分工：

- `Hydrate`
  在 resume 或重新加载 run state 后，把 authoritative transcript 同步到 memory 的派生层。
- `ObserveMessages`
  在 runtime 每次正式提交消息后，增量更新 summary / recall index / memory window。
- `BuildContext`
  生成最终喂给模型的上下文消息。

如果没有显式配置 `ContextBuilder`，默认上下文路径会直接使用 `Memory.BuildContext(...)`。

## Resume Example

`agentcore` 的恢复模型基于稳定 `RunID` 和显式 `ResumeMode`：

```go
first, err := runtime.Run(ctx, agentcore.RunInput{
    RunID:       "task-123",
    AgentID:     "agent-1",
    Instruction: "analyze this repo",
})
if err != nil {
    return err
}
if first.Status != agentcore.RunYielded {
    return nil
}

resumed, err := runtime.Run(ctx, agentcore.RunInput{
    RunID:      "task-123",
    ResumeMode: agentcore.ResumeFromState,
})
```

如果恢复时需要带入新的人工输入：

```go
resumed, err := runtime.Run(ctx, agentcore.RunInput{
    RunID:       "task-123",
    Instruction: "继续，但不要再调用危险工具",
    ResumeMode:  agentcore.ResumeWithInput,
})
```

## Axon Internal Adapters

如果你想桥接 Axon 仓库当前内部实现，请看：

- `internal/agentcoreadapters/internalllm`
- `internal/agentcoreadapters/internalmemory`
- `internal/agentcoreadapters/internaltools`

当前 internal 集成已经遵循同样的边界：

- `internal/agentcorestorage`
  提供 `agentcore.Storage`
- `internal/memory`
  通过 `internal/agentcoreadapters/internalmemory` 提供 `agentcore.Memory`
- `internal/agent.AgentcoreRunner`
  把 external `Suspend/Resume` 语义映射到 `RunID + ResumeMode`
