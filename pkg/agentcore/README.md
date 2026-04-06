# agentcore

`pkg/agentcore` 是 Axon 中抽离出来的最小 Agent Runtime Core，目标是将来可以作为独立包发布和嵌入使用。

它的定位更接近一个 runtime kernel，而不是一个完整 agent framework：

- 提供任何 agent 都绕不开的最小执行闭环
- 通过公共接口暴露扩展点，而不是把上层产品逻辑耦合进 core
- 保持对外可嵌入、可替换、可逐步增长

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

推荐阅读顺序：

1. Quickstart
2. API guide
3. `examples/agentcore/minimal`
4. `examples/agentcore/custom`
5. `examples/agentcore/resume`
6. `examples/agentcore/bridge`

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

这个最小例子已经具备：

- 稳定 `RunID`
- 可落盘的 run snapshot
- append-only transcript
- 后续可继续接入 tool、hook、memory、resume

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

这意味着一个很重要的扩展顺序：

1. 先把 `Storage` 当作事实层跑通
2. 再把 `Memory` 当作上下文派生层接进来
3. 只有当你需要特殊 message 组装时，再自定义 `ContextBuilder`

## Resume Example

`agentcore` 的恢复模型基于稳定 `RunID` 和显式 `ResumeMode`：

- `ResumeFromState`
  明确表示“只从已有 state / transcript 继续”，即使传了 `Instruction` 也不会追加新的 user message。
- `ResumeWithInput`
  明确表示“恢复时再追加一条新的 user input”。
- `ResumeAuto`
  采用默认语义：有非空 `Instruction` 时追加新 user message；没有 `Instruction` 时直接从已有 state 继续。

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

如果你想直接看可运行示例，可以运行：

```bash
go run ./examples/agentcore/minimal
go run ./examples/agentcore/custom
go run ./examples/agentcore/resume
go run ./examples/agentcore/bridge
```

其中：

- `minimal`
  关注最小 runtime
- `custom`
  关注 tool 与 hook 扩展
- `resume`
  关注 `yield -> resume`
- `bridge`
  关注 internal adapter 桥接

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

## Release Posture

如果未来单独发布 `pkg/agentcore`，建议把它看成下面这类包：

- 一个稳定的 runtime contract 包
- 一个带少量默认实现的 kernel 包
- 一个允许不同 provider / storage / memory / tool 适配器接入的嵌入式内核

也就是说，它不应该依赖仓库的 `internal/*` 作为公共能力前提；
internal 侧应通过 adapter 使用它，而不是反过来让 `agentcore` 依赖 internal。
