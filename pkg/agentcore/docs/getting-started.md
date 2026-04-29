# Agentcore Getting Started

这份文档面向第一次接入 `agentcore` 的使用者，目标是用最小概念把 runtime 跑起来，并快速理解下一步应该从哪里扩展。

如果你还不确定自己属于哪条采用路径，可以先看：

- [adoption-paths.md](./adoption-paths.md)

## 1. 什么时候适合用 Agentcore

`agentcore` 适合承担：

- 单 agent 执行内核
- 可恢复的 run loop
- 可审计的 transcript authority layer
- 可替换的 provider / storage / memory / tools runtime contract

它不适合直接承担：

- 多 agent 编排
- planner / task graph
- 渠道接入
- 产品级长期记忆治理
- review / governance 控制面

如果你要的是一个最小但可持续生长的 runtime kernel，`agentcore` 是合适的起点。

## 2. 最小可运行组合

最小组合通常是：

- `Provider`: `mock/`
- `Storage`: `files/`
- `Tools`: 默认空 registry

示例：

```go
package main

import (
	"context"
	"log"

	"github.com/inhere/axon/pkg/agentcore"
	"github.com/inhere/axon/pkg/agentcore/storage/files"
	"github.com/inhere/axon/pkg/agentcore/mock"
)

func main() {
	runtime := agentcore.New(agentcore.Config{
		Provider: mock.NewProvider(mock.Final("hello from agentcore")),
		Storage:  files.NewStorage(".agentcore-runs"),
	})

	result, err := runtime.Run(context.Background(), agentcore.RunInput{
		RunID:       "getting-started",
		AgentID:     "demo-agent",
		Instruction: "say hello",
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("status=%s output=%q steps=%d", result.Status, result.Output, result.Steps)
}
```

这个最小例子已经具备：

- 稳定 `RunID`
- run snapshot
- append-only transcript
- 后续接入 tool、hook、memory、resume 的基础

## 3. 恢复模型怎么理解

`agentcore` 用稳定 `RunID` 标识同一次 run，并通过 `ResumeMode` 控制恢复语义。

- `ResumeFromState`
  严格从已持久化 state / transcript 继续，不追加新的 user message
- `ResumeWithInput`
  先追加一条新的 user message，再继续
- `ResumeAuto`
  有输入就追加，没有输入就直接从已有状态继续

如果你的场景是审批、等待人工输入、HITL 或 pause / continue，推荐优先把这套语义跑通。

## 4. Transcript 为什么重要

`agentcore` 里的事实层不是 `RunResult`，而是 `Storage` 中的：

- run snapshot
- append-only transcript

一次 tool 交互至少会落两条关键消息：

1. `assistant(tool_call)`
2. `tool(result)`

这样做是为了保证：

- resume 时模型能看到完整上下文
- replay 时不会丢失历史工具决策
- 不同 provider / adapter 能共享统一的 `Message` 契约

## 5. 推荐扩展顺序

建议按这个顺序接入：

1. 用 `mock + files` 跑通最小链路
2. 接一个自定义 `Tool`
3. 接一个 `Hook`
4. 跑通一次 `yield -> resume`
5. 如果你不想从零实现 `Memory`，先接入 `memory/`
6. 只有需要更强消息编排时，再替换 `PromptBuilder` / `ContextBuilder`
7. 最后接真实 `Provider` 和自定义 `Storage`

如果你想直接从一个更接近真实 embedding host 的组合起步，当前更推荐继续看：

- [recommended-runtime.md](./recommended-runtime.md)
- [host-integration.md](./host-integration.md)
- [migration.md](./migration.md)

当前第一方默认 memory 实现已经提供：

- summary slot
- recent transcript window
- system/tool 消息过滤

它适合作为默认起点，再逐步演进成更强的自定义 memory。

对应的第一方 context 组合入口是：

- `context.TranscriptWindow(...)`
- `context.MemoryFirst(...)`
- `context.MemoryThenRecent(...)`

推荐理解为：

- 没有 memory 时，先用 `TranscriptWindow`
- 有默认 `WorkingMemory` 时，优先用 `MemoryFirst`
- 如果你想保留 summary/derived context 再显式补最近几条原始消息，用 `MemoryThenRecent`

如果你还需要一个更实用的默认 prompt 组合层，当前第一方 prompt 入口有两个：

- `prompt.StaticBuilder`
- `prompt.Layered(...)`

推荐理解为：

- 只需要固定 system prompt 时，用 `StaticBuilder`
- 想把 base prompt、`RunState.Metadata` 和固定段落稳定拼接时，用 `Layered(...)`

例如：

```go
runtime := agentcore.New(agentcore.Config{
	Provider: mock.NewProvider(mock.Final("hello from agentcore")),
	Storage:  files.NewStorage(".agentcore-runs"),
	PromptBuilder: prompt.Layered(prompt.Config{
		Base: "You are a focused repository assistant.",
		Metadata: prompt.MetadataConfig{
			Enabled: true,
		},
		Sections: []string{
			"Prefer grounded answers based on the current run state.",
			"Keep tool usage minimal and explicit.",
		},
	}),
})
```

它的输出顺序固定为：

1. `Base`
2. `[Run metadata]`
3. `Sections...`

各段之间使用空行分隔，metadata key 会按字典序稳定输出。

## 6. 下一步看哪里

- API guide: [docs/api.md](./api.md)
- Compatibility: [docs/compatibility.md](./compatibility.md)
- Versioning: [docs/versioning.md](./versioning.md)
- Release readiness: [docs/release-readiness.md](./release-readiness.md)
- Recommended runtime path: [docs/recommended-runtime.md](./recommended-runtime.md)
- Roadmap: [docs/roadmap.md](./roadmap.md)
- Examples: `examples/`

建议顺序：

1. `examples/minimal`
2. `examples/prompt`
3. `examples/recommended`
4. `examples/host`
5. `examples/custom`
6. `examples/resume`
7. `examples/agentcore/bridge`
