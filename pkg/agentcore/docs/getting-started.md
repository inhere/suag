# Agentcore Getting Started

这份文档面向第一次接入 `agentcore` 的使用者，目标是用最小概念把 runtime 跑起来，并快速理解下一步应该从哪里扩展。

分层与推荐组装方式见 [README](../README.md)。

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

	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/mock"
	"github.com/inhere/suga/pkg/agentcore/storage/files"
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

1. 用 `mock` 跑通最小链路（不传 `Storage` 时 run 存在内存里；需要跨进程续跑再传 `files.NewStorage(dir)`）
2. 用 `tools.NewRegistry()` 接一个自定义 `Tool`
3. 接一个 `Hook`
4. 跑通一次 `yield -> resume`
5. 换成真实 provider（`llm.NewOpenAIProvider` / `llm.NewClaudeProvider`），加入只读内置工具与审批层，见 `examples/recommended`
6. 只有需要更强消息编排时，再实现自己的 `PromptBuilder` / `ContextBuilder` / `Memory`

`PromptBuilder` 只有一个方法，固定 system prompt 可以直接写成一个字符串类型：

```go
type staticPrompt string

func (p staticPrompt) BuildSystemPrompt(context.Context, *agentcore.RunState) (string, error) {
	return string(p), nil
}
```

宿主封装见 [host-integration.md](./host-integration.md)。

## 6. 下一步看哪里

- API guide: [api.md](./api.md)
- Runtime policies: [runtime-policies.md](./runtime-policies.md)
- Host integration: [host-integration.md](./host-integration.md)
- Examples: [examples/README.md](../examples/README.md)

建议顺序：`examples/minimal` → `examples/custom` → `examples/resume` → `examples/recommended` → `examples/host`。
