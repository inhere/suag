# Agentcore Quickstart

这份文档面向第一次接入 `pkg/agentcore` 的使用者，目标是用最少的概念把 runtime 跑起来，然后知道下一步该去哪里扩展。

如果你想先理解设计边界，读 [`docs/usage/agentcore.md`](./agentcore.md)。
如果你想直接看接口职责，读 [`docs/usage/agentcore-api.md`](./agentcore-api.md)。

---

## 1. 它是什么

`agentcore` 是一个最小但可扩展的 Agent Runtime Core。

它负责的事情很集中：

- 接收一次 `RunInput`
- 构建 prompt 和 context
- 调用 provider
- 执行 tool call
- 持久化 state 与 transcript
- 在关键生命周期节点暴露 hooks

它不负责完整产品层：

- 不做多 Agent 编排
- 不做长期记忆检索策略
- 不做 Web / API / 渠道接入
- 不做 tracing 平台或 review 控制面

所以最适合它的使用方式是：

- 作为独立包嵌入你的 Agent 系统
- 作为更高层 orchestrator 的单 Agent 执行内核

---

## 2. 5 分钟跑起来

最小可运行组合通常是：

- provider: `pkg/agentcore/mock`
- storage: `pkg/agentcore/files`
- tools: 默认空 registry，或自定义一个 tool

下面是一个最小可运行示例：

```go
package main

import (
	"context"
	"log"

	"github.com/inhere/axon/pkg/agentcore"
	"github.com/inhere/axon/pkg/agentcore/files"
	"github.com/inhere/axon/pkg/agentcore/mock"
)

func main() {
	runtime := agentcore.New(agentcore.Config{
		Provider: mock.NewProvider(mock.Final("hello from agentcore")),
		Storage:  files.NewStorage(".agentcore-runs"),
	})

	result, err := runtime.Run(context.Background(), agentcore.RunInput{
		RunID:       "quickstart-run",
		AgentID:     "demo-agent",
		Instruction: "say hello",
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("status=%s output=%q steps=%d", result.Status, result.Output, result.Steps)
}
```

运行后你会得到：

- 一个 `RunResult`
- 一份落在 storage 里的 run state snapshot
- 一份 append-only 的 message transcript

---

## 3. 最常见的三种接法

### 3.1 只想先跑通

推荐组合：

- `mock.NewProvider(...)`
- `files.NewStorage(...)`
- 不注册任何 tool

适合：

- smoke test
- 文档示例
- 把 runtime 接进现有服务前先验证 run loop

### 3.2 想接你自己的工具与 hook

推荐组合：

- 你自己的 `Tool`
- `NewToolRegistry()`
- 一个或多个 `Hook`
- `files.NewStorage(...)`

适合：

- 在运行前后注入审计、控制、观测逻辑
- 为 agent 增加本地工具能力
- 在 prompt / context / tool result 上做局部扩展

### 3.3 想桥接 Axon 内部实现

推荐组合：

- `internal/agentcoreadapters/internalllm`
- `internal/agentcoreadapters/internaltools`
- `files.NewStorage(...)`

适合：

- 把当前内部 `llm.Provider` / `tools.Registry` 接进公共 runtime
- 先稳定 runtime contract，再逐步替换更高层系统

---

## 4. RunResult 和 transcript 怎么看

`RunResult` 只表达本次运行的最终出口：

- `RunCompleted`
- `RunYielded`
- `RunFailed`

真正可恢复、可 replay 的事实来自 transcript。

一次典型 tool 调用会写入这些消息：

```text
user("use a tool")
assistant(tool_call=lookup)
tool(result="tool-output")
assistant("final answer")
```

这里有一个关键点：

- tool loop 会同时持久化 `assistant(tool_call)` 和 `tool(result)`

这保证了：

- resume 时 provider 能看到完整上下文
- replay 时不会丢失“模型当时决定调用什么工具”
- adapter / provider 之间围绕同一个 `Message` 契约工作

---

## 5. 什么时候该用 `files` 和 `mock`

### `files.NewStorage(...)`

适合：

- 本地开发
- 单机嵌入
- 需要一个简单可靠的 authority transcript

默认会写入：

- run snapshot
- append-only message log

### `mock.NewProvider(...)`

适合：

- 写示例
- 写单测
- 验证 tool loop / resume / hooks 语义

不适合：

- 真实模型调用

真实模型场景应该自己实现 `Provider`，或者桥接现有 provider。

---

## 6. 下一步应该看哪里

如果你已经跑通了最小示例，推荐按这个顺序继续：

1. 读 [`docs/usage/agentcore-api.md`](./agentcore-api.md)
2. 看 `examples/agentcore/minimal`
3. 看 `examples/agentcore/custom`
4. 如果你要桥接内部栈，再看 `examples/agentcore/bridge`
