# agentcore

`pkg/agentcore` 是 Axon 中抽离出来的最小 Agent Runtime Core，目标是将来可以作为独立包发布和嵌入使用。

它负责任何 agent 运行都绕不开的那条核心链路：

- 准备或恢复 run state
- 构建 prompt 与 context
- 调用模型 provider
- 执行 tool calls
- 持久化 snapshot 与 transcript
- 暴露 hooks 与稳定错误分类

它不负责完整产品层：

- 多 agent 编排
- 长期记忆策略
- tracing 控制面
- Web / API / 渠道接入
- governance / review / evolution workflow

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

如果你想桥接 Axon 仓库当前内部实现，请看：

- `internal/agentcoreadapters/internalllm`
- `internal/agentcoreadapters/internaltools`
