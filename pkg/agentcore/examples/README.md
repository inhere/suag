# Agentcore Examples

这组示例面向 `agentcore` 的外部使用者，覆盖几条最常见接法：

- `minimal`
  - 用 `mock + files` 跑通最小 runtime
- `custom`
  - 自定义 tool 和 hook，展示常见扩展路径
- `memory`
  - 演示第一方 `WorkingMemory` 如何构建 `summary + recent window` 上下文
- `context`
  - 演示第一方 context presets 如何组合 transcript 与 memory
- `prompt`
  - 演示第一方 prompt builders 如何组合 base prompt、run metadata 与附加段落
- `recommended`
  - 演示当前更推荐的 starter path：`files + memory + context + prompt + explicit resume + optional streaming`
- `stream`
  - 演示最小 streaming contract 如何向 host 转发 text delta
- `resume`
  - 演示 `yield -> resume` 的最小恢复链路

运行方式：

```bash
go run ./examples/minimal
go run ./examples/custom
go run ./examples/memory
go run ./examples/context
go run ./examples/prompt
go run ./examples/recommended
go run ./examples/stream
go run ./examples/resume
```

其中 `memory` 示例重点展示两件事：

- 从已持久化 transcript hydrate 第一方 `WorkingMemory`
- 用 summary + recent window 构建模型上下文

其中 `context` 示例重点展示三种默认组合的差异：

- `TranscriptWindow`
- `MemoryFirst`
- `MemoryThenRecent`

其中 `prompt` 示例重点展示两件事：

- `prompt.Layered(...)` 的固定输出顺序
- `RunState.Metadata` 如何进入第一方 prompt 组合

其中 `recommended` 示例重点展示四件事：

- 第一方 starter path 如何组合 `files + memory + context + prompt`
- host 如何通过显式 `RunID + ResumeMode` 恢复同一次 run
- yield / resume 与 tool loop 如何放在同一个主路径里
- optional streaming 如何与 authority final answer 共存

如果你希望按“先单点、再组合”的方式阅读，推荐顺序是：

1. `minimal`
2. `memory`
3. `context`
4. `prompt`
5. `recommended`
6. `stream`
7. `resume`

其中 `stream` 示例重点展示两件事：

- `RunInput.Stream` 如何接收模型输出增量
- final `RunResult` 仍然是 authority completion

其中 `resume` 示例重点展示两件事：

- 第一次 run 用稳定 `RunID` 执行并进入 `RunYielded`
- 第二次 run 使用相同 `RunID` 和显式 `ResumeMode` 继续，而不是重新开始

依赖 `internal/agentcoreadapters/*` 的桥接示例不属于 package-local examples，仍保留在仓库根：

```bash
go run ./examples/agentcore/bridge
```
