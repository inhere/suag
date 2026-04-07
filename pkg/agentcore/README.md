# agentcore

`agentcore` 是 Axon 中抽离出来的最小 Agent Runtime Core，目标是将来可以作为独立包发布和嵌入使用。

它的定位更接近一个 runtime kernel，而不是一个完整 agent framework：

- 提供任何 agent 都绕不开的最小执行闭环
- 通过公共接口暴露扩展点，而不是把上层产品逻辑耦合进 core
- 保持对外可嵌入、可替换、可逐步增长

当前阶段可以把它理解成：

- 一个已经可用的 `v0` kernel
- 一个强调稳定 contract 而不是大而全功能的 runtime core
- 一个允许上层系统逐步 build on top 的独立基础包

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

## Status

`agentcore` 当前更适合被理解为：

- 可嵌入的 runtime kernel
- 仍在持续收紧 contract 的 `v0`
- 已经可以承接单-agent 执行闭环，但默认实现仍然保持克制

这意味着：

- 核心运行语义已经值得围绕它扩展
- 边界会优先通过测试和文档逐步稳定
- 一些“开箱即用”的默认能力还会继续补，但不会为了省几步接入成本把 kernel 做重

## What You Get

接入 `agentcore` 后，最小可以获得这些能力：

- 稳定 `RunInput -> RunResult` 执行入口
- `RunID + ResumeMode` 驱动的恢复模型
- append-only transcript 与 run snapshot
- provider-neutral 的 tool loop
- provider、storage、memory、tools、prompt、context、hooks 的扩展面
- 稳定错误包装类型，便于 embedding host 做统一策略

如果你正在构建自己的 agent runtime，`agentcore` 更适合承担：

- 单 agent 执行内核
- transcript authority layer
- 可测试、可替换的 runtime contract

而不是承担：

- 产品工作流编排器
- planner / orchestrator / reviewer 系统
- 渠道接入与控制面
- 产品级长期记忆治理

## Quick Start

最小可运行组合通常是：

- provider: `mock/`
- storage: `files/`
- tools: 默认空 registry

下面是一个最小可运行示例：

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

## Default Building Blocks

`agentcore` 自己只带少量默认构件，用来帮助你以较低成本起步：

- `files/`
  文件系统存储，适合本地开发、测试和简单嵌入
- `memory/`
  第一方默认 memory 实现，提供 summary slot 与 recent transcript window
- `context/`
  第一方 context presets，例如 transcript-only 与 memory-first
- `prompt/`
  第一方 prompt builders，例如 `StaticBuilder` 与 `Layered(...)`
- `hooks/`
  有序 hook chain 辅助
- `mock/`
  provider / tool 的测试与示例实现

推荐扩展顺序通常是：

1. 先用 `mock + files` 跑通最小链路
2. 再加入 `ToolRegistry` 和 `Hook`
3. 如果不想自己从零实现 memory，先接入 `memory/`
4. 默认 `ContextBuilder` 优先从 `context/` 的 presets 起步
5. 需要组合 system prompt 时，优先从 `prompt/` 的 builders 起步
6. 最后接真实 `Provider` 和自定义 `Storage`

## Package Layout

- `.`  
  Public runtime contracts and default runtime implementation
- `files/`  
  Filesystem-backed storage
- `memory/`  
  Default working-memory implementation
- `context/`  
  First-party context presets and default message-window builder
- `prompt/`  
  First-party prompt builders
- `hooks/`  
  Ordered hook-chain helper
- `mock/`  
  Test provider and tool implementations

注意：

- `agentcore` 本身不应依赖仓库的 `internal/*` 包
- 与 Axon 内部 `llm`、`tools` 等实现的桥接 adapter 放在 `internal/agentcoreadapters/`

## Docs

- Docs index: [docs/README.md](./docs/README.md)
- Getting started: [docs/getting-started.md](./docs/getting-started.md)
- Architecture: [docs/architecture.md](./docs/architecture.md)
- API guide: [docs/api.md](./docs/api.md)
- Embedding patterns: [docs/embedding-patterns.md](./docs/embedding-patterns.md)
- Compatibility: [docs/compatibility.md](./docs/compatibility.md)
- Working memory design: [docs/working-memory-design.md](./docs/working-memory-design.md)
- Context presets design: [docs/context-presets-design.md](./docs/context-presets-design.md)
- Prompt layered design: [docs/prompt-layered-design.md](./docs/prompt-layered-design.md)
- Roadmap: [docs/roadmap.md](./docs/roadmap.md)
- Examples: `examples/`
- Package overview: `doc.go`

推荐阅读顺序：

1. [README.md](./README.md)
2. [docs/README.md](./docs/README.md)
3. [docs/getting-started.md](./docs/getting-started.md)
4. [docs/architecture.md](./docs/architecture.md)
5. [docs/api.md](./docs/api.md)
6. [docs/embedding-patterns.md](./docs/embedding-patterns.md)
7. [docs/compatibility.md](./docs/compatibility.md)
8. [docs/working-memory-design.md](./docs/working-memory-design.md)
9. [docs/context-presets-design.md](./docs/context-presets-design.md)
10. [docs/prompt-layered-design.md](./docs/prompt-layered-design.md)
11. `doc.go`
12. `examples/README.md`
13. `examples/minimal`
14. `examples/custom`
15. `examples/prompt`
16. `examples/resume`
17. [docs/roadmap.md](./docs/roadmap.md)

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
2. 再把 `memory/` 当作默认上下文派生层接进来
3. 先尝试 `context/` 的第一方 presets
4. 只有当你需要特殊 message 组装时，再自定义 `ContextBuilder`

如果你只需要一个最小但可用的默认 memory，可以直接这样起步：

```go
mem := memory.New(memory.Config{
    MaxRecentMessages:   12,
    IncludeToolMessages: true,
})
mem.SetSummary("User is debugging agentcore resume behavior.")
```

这个默认实现当前提供：

- summary slot
- recent transcript window
- 可配置的 system/tool 消息过滤

如果你希望直接使用第一方 context 组合，可以从这些入口开始：

```go
builder := context.TranscriptWindow(context.WindowConfig{
    MaxMessages: 20,
})
```

```go
builder := context.MemoryFirst(mem, context.MemoryFirstConfig{
    MaxMessages: 12,
})
```

```go
builder := context.MemoryThenRecent(mem, context.MemoryThenRecentConfig{
    MaxMessages: 6,
})
```

其中：

- `TranscriptWindow`
  适合 transcript-only 场景
- `MemoryFirst`
  适合默认 `WorkingMemory` 路径
- `MemoryThenRecent`
  适合希望先使用 memory，再显式追加最近原始消息的场景

如果你需要在 system prompt 中组合固定 base 文本、run metadata 与附加段落，可以直接使用第一方 prompt builder：

```go
builder := prompt.StaticBuilder{
    Prompt: "You are a focused repository assistant.",
}
```

```go
builder := prompt.Layered(prompt.Config{
    Base: "You are a focused repository assistant.",
    Metadata: prompt.MetadataConfig{
        Enabled: true,
    },
    Sections: []string{
        "Prefer grounded answers based on the current run state.",
        "Keep tool usage minimal and explicit.",
    },
})
```

其中：

- `StaticBuilder`
  适合完全静态的 system prompt
- `Layered`
  适合把 base prompt、`RunState.Metadata` 和固定补充段落稳定拼接起来

## Resume Model

`agentcore` 的恢复模型基于稳定 `RunID` 和显式 `ResumeMode`：

- `ResumeFromState`
  明确表示“只从已有 state / transcript 继续”，即使传了 `Instruction` 也不会追加新的 user message。
- `ResumeWithInput`
  明确表示“恢复时再追加一条新的 user input”。
- `ResumeAuto`
  采用默认语义：有非空 `Instruction` 时追加新 user message；没有 `Instruction` 时直接从已有 state 继续。

恢复时最重要的判断标准是：

- 想“严格从暂停点继续” -> `ResumeFromState`
- 想“继续执行前追加一条新的人工输入” -> `ResumeWithInput`
- 想“默认有输入就追加、没输入就直接继续” -> `ResumeAuto`

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

如果你在包目录下直接看包内示例，可以运行：

```bash
go run ./examples/minimal
go run ./examples/custom
go run ./examples/resume
go run ./examples/prompt
```

其中：

- `minimal`
  关注最小 runtime
- `custom`
  关注 tool 与 hook 扩展
- `resume`
  关注 `yield -> resume`
- `prompt`
  关注第一方 prompt 组合

如果你要看仓库内部桥接路径，再额外运行：

```bash
go run ./examples/agentcore/bridge
```

## Transcript Contract

`agentcore` 当前最关键的稳定语义之一是 transcript。

一次完整 tool 交互会持久化：

1. `assistant(tool_call)`
2. `tool(result)`

而不只是最终工具输出。

这保证了：

- resume 时 provider 能看到完整上下文
- replay 时不会丢失模型当时的工具决策
- 不同 provider / adapter 可以围绕统一的 `Message` 契约工作

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

如果未来单独发布 `agentcore`，建议把它看成下面这类包：

- 一个稳定的 runtime contract 包
- 一个带少量默认实现的 kernel 包
- 一个允许不同 provider / storage / memory / tool 适配器接入的嵌入式内核

也就是说，它不应该依赖仓库的 `internal/*` 作为公共能力前提；
internal 侧应通过 adapter 使用它，而不是反过来让 `agentcore` 依赖 internal。

更具体一点说，未来对外发布时更应该承诺的是：

- 稳定的 runtime contract
- 清晰的 transcript / resume 语义
- 少量但可靠的默认构件

而不是：

- 绑定某个具体 provider
- 绑定某个具体存储后端
- 绑定 Axon 内部运行时实现
