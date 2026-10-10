# agentcore

`agentcore` 是 suag 的 Agent 内核与能力层：单 agent 的模型/工具循环、可恢复的 run 状态与 transcript，以及可直接组装的 provider、工具、技能与 MCP 接入。它不依赖 suag 的应用层（`internal/*`），可以单独嵌入；当前为 `v0`，接口仍可能调整。

它负责：

- 准备或恢复 run state，构建 prompt 与上下文
- 调用模型 provider，执行 tool calls
- 持久化 snapshot 与 append-only transcript
- 暴露 hooks、policies 与稳定的错误分类

它不负责：会话产品逻辑（会话、压缩、交接）、配置读取、终端与 ACP 接入、多 agent 编排——这些属于宿主（suag 的 `internal/*`）。

## 分层

```text
应用层   suag internal/*: config, wiring, personal(+store), chat, acpserver, cli, hooks, output
            │ 只向下依赖
能力层   agentcore/llm          OpenAI 兼容与 Claude provider、fallback 池
         agentcore/tools        注册表、审批层、延迟加载（visibility）、工具元数据
         agentcore/tools/native 内置工具：文件读写/搜索、shell、web、update_plan、tool_search、skill
         agentcore/tools/mcp    stdio MCP 客户端，工具以 MCP kind 注册
         agentcore/skills       SKILL.md 解析、多根目录加载与检索
         agentcore/storage/files 文件存储；mock、testing 用于测试与一致性检查
            │
内核     agentcore (根包) + agentcore/api：Runtime、契约类型、hooks、policies、内存默认存储
```

依赖只能自上而下。`layering_test.go` 守住边界：`pkg/agentcore` 下任何包（含测试）依赖了 `pkg/agentcore` 之外的 `github.com/inhere/suag/...` 即失败。根包不依赖能力层：不传 `Config.Tools` 时用私有空注册表，不传 `Config.Storage` 时用内存存储。

## Quick Start

最小可运行组合是一个 provider：

```go
runtime := agentcore.New(agentcore.Config{
    Provider: mock.NewProvider(mock.Final("hello")),
})

result, err := runtime.Run(ctx, agentcore.RunInput{
    AgentID:     "example-agent",
    Instruction: "say hello",
})
```

不传 `Storage` 时 runtime 使用进程内的 `agentcore.NewMemoryStorage()`：默认不持久化，进程退出即丢失；跨进程或跨 Runtime 实例续跑需显式传入 `files.NewStorage(dir)` 或自己的存储实现。

## 只读 Agent

[examples/recommended](./examples/recommended/main.go) 是推荐的组装方式：真实 provider（`llm`）+ 只读内置工具 + 技能 + 可选 MCP + 审批层。核心几步：

```go
reg := tools.NewRegistry()
for _, tool := range native.BuildTools(native.Env{Workspace: workspace}, false) { // false: 只读工具
    reg.Register(tool)
}
reg.Register(native.NewSkillTool(skills.LoadRoots(roots), 0))

approval := tools.NewApproval(reg, tools.ApproverFunc(func(ctx context.Context, call agentcore.ToolCall, tool agentcore.Tool) error {
    if tools.MetaOf(tool).ReadOnly {
        return nil
    }
    return tools.Denied(call.Name + " needs approval") // 真实宿主在这里询问用户
}))

runtime := agentcore.New(agentcore.Config{
    Provider: llm.NewOpenAIProvider(llm.OpenAIConfig{APIKey: key}),
    Tools:    approval,
})
```

写工具（`file_write`、`file_edit`、`shell_exec`）只在 `BuildTools(env, true)` 时注册，且必须经过 `tools.NewApproval` 的 Approver 才会执行；没有 Approver 时审批层拒绝一切调用。

## 工具

- `tools.Registry`：`Register`（同名 panic）、`TryRegister`（先到先得）、`MustRegister`（覆盖）、`Get`、`List`。
- 工具实现 `agentcore.Tool`（即 `api.Tool`）。可选接口：
  - `tools.Kinded`（`Kind()`），缺省 native；读取用 `tools.KindOf(tool)`。
  - `tools.Describer`（`ToolMeta()`），声明中性类别（read/edit/execute/search/fetch/think）与只读标记；读取用 `tools.MetaOf(tool)`。
- MCP 工具：`tools.IsMCP(tool)`、`tools.MCPServerOf(tool)`。`MetaOf` 对 MCP 工具一律返回“其他、非只读”，同名冒充内置工具也不会走只读快速路径。
- 分层顺序：observe（宿主）→ `tools.Visibility`（延迟加载 MCP schema）→ `tools.Approval`（审批）→ `tools.Registry`。
- 宿主把类别映射到自己的协议，例如 suag 的 ACP 服务把 `CategoryEdit` 映射为 `acp.ToolKindEdit`；ACP 概念不进入 agentcore。

## 核心契约

- `Provider` 连接 LLM，返回 final answer 或 tool calls；实现 `Stream` 时转发增量文本。
- `Storage` 保存 run snapshot 与 append-only transcript，是恢复和审计的事实来源；可选 `ArtifactStorage`。
- `ToolRegistry` 暴露 tool schema 并执行工具调用。
- `PromptBuilder` 生成 system prompt；`ContextBuilder` 组装发给模型的消息（未提供时走 `Memory.BuildContext`，默认 `Memory` 原样使用完整 transcript）。
- `Hook` 拦截生命周期节点；`ResumePolicy`、`ToolExecutionPolicy`、`ErrorPolicy` 定制恢复、工具错误与可恢复错误的处理。
- `ToolResult.IsError` 标记失败或被拒绝的工具调用，provider 据此告诉模型（如 Claude `tool_result.is_error`）。
- `api.Usage.Add` 累加用量。

读取某次 run 的权威视图可用 `agentcore.LoadRunAuthority(ctx, store, runID)` 与 `agentcore.LoadAuthorityBundle(ctx, store, runID)`。

## Resume

恢复基于稳定 `RunID` 与显式 `ResumeMode`：

- `ResumeFromState`：只从已有 state / transcript 继续，即使传了 `Instruction` 也不追加 user message。
- `ResumeWithInput`：恢复时追加一条新的 user input。
- `ResumeAuto`：有非空 `Instruction` 时追加，否则直接继续。

```go
first, err := runtime.Run(ctx, agentcore.RunInput{RunID: "task-123", AgentID: "agent-1", Instruction: "analyze this repo"})
if err != nil || first.Status != agentcore.RunYielded {
    return err
}
resumed, err := runtime.Run(ctx, agentcore.RunInput{RunID: "task-123", ResumeMode: agentcore.ResumeFromState})
```

终态（completed / failed）的 run 不能再恢复。跨 Runtime 实例恢复需要两边使用同一个持久化存储。

## Transcript

一次工具交互持久化为 `assistant(tool_call)` 与 `tool(result)` 两条消息，而不只是最终输出。这保证 resume 时 provider 看到完整上下文，replay 不丢失模型的工具决策，不同 provider 围绕同一个 `Message` 契约工作。

## 示例与文档

- 示例：[examples/README.md](./examples/README.md)（全部参与 `go vet`，用 mock 离线运行；`recommended` 设置 `OPENAI_API_KEY` 后使用真实模型）
- 文档：[docs/README.md](./docs/README.md)
- 一致性检查：[testing/README.md](./testing/README.md)
- 2026-10 分层重构中删除或归档的包与文档：[删除记录](../../docs/archive/removed-2026-10.md)
