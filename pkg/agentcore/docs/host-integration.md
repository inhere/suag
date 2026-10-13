# Agentcore Host Integration

这份文档面向把 `agentcore` 嵌入到自己系统里的开发者。

它回答的是：

- 一个 embedding host 应该把哪些职责留给自己
- 哪些职责应该交给 `agentcore`
- 聊天接口、任务执行器、CLI、后台 worker 这类宿主，应该如何围绕 `RunInput -> RunResult` 组织代码

它不是 API 索引，也不是架构总览。

它更关注 host boundary。

---

## 1. 先明确 Host 和 Kernel 的分工

推荐分工是：

### Host 负责

- 生成或选择稳定 `RunID`
- 决定何时创建 run、何时恢复 run
- 管理用户输入、会话 ID、任务 ID、审批状态
- 决定是否把 streaming 转发到 UI / SSE / websocket
- 决定收到 `RunYielded` 后如何等待外部输入
- 决定如何记录业务日志、指标、审计事件

### Agentcore 负责

- 维护 run snapshot
- 维护 append-only transcript
- 执行 prompt / context / provider / tool loop
- 执行 `yield -> resume` runtime 语义
- 暴露稳定错误分类

一句话说：

- host 管“产品流程”
- `agentcore` 管“agent runtime semantics”

---

## 2. Host 最小封装长什么样

对大多数外部系统，一个很实用的起点不是直接在 handler 里散着 new runtime，而是做一层小的 host wrapper：

```go
type AgentHost struct {
    runtime agentcore.Runtime
}

func NewAgentHost(runtime agentcore.Runtime) *AgentHost {
    return &AgentHost{runtime: runtime}
}

func (h *AgentHost) Run(ctx context.Context, input agentcore.RunInput) (*agentcore.RunResult, error) {
    return h.runtime.Run(ctx, input)
}
```

如果你希望 host 对接入方式更稳定，可以再包一层业务输入：

```go
type StartRequest struct {
    SessionID string
    AgentID   string
    Message   string
}

func (h *AgentHost) Start(ctx context.Context, req StartRequest) (*agentcore.RunResult, error) {
    return h.runtime.Run(ctx, agentcore.RunInput{
        RunID:       req.SessionID,
        AgentID:     req.AgentID,
        Instruction: req.Message,
    })
}
```

这里的重点不是“多写一层”，而是：

- 不让上层业务直接散落 `ResumeMode` 细节
- 不让每个 handler 都重复拼接 runtime input

---

## 3. RunID 应该由谁决定

推荐答案是：

- 由 host 决定

通常不要把 `RunID` 当成 provider 或 storage 的内部细节。

更推荐把它映射到 host 已经理解的实体：

- chat session ID
- task execution ID
- job ID
- approval flow ID

这样做的好处是：

- host 更容易做 resume
- 更容易做审计和排障
- 更容易把 agent runtime 状态和产品状态关联起来

不推荐的方式是：

- 每次 `Run(...)` 都让 runtime 自己生成匿名 run ID
- 然后上层又再维护一套独立 session ID

这会让恢复和追踪变得很绕。

---

## 4. 推荐的 Host 入口形态

### 4.1 Start

用于第一次进入一条 run：

```go
result, err := runtime.Run(ctx, agentcore.RunInput{
    RunID:       sessionID,
    AgentID:     "repo-agent",
    Instruction: userMessage,
})
```

### 4.2 Continue Without New Input

用于“继续执行刚才暂停的 run，但不追加新的用户输入”：

```go
result, err := runtime.Run(ctx, agentcore.RunInput{
    RunID:      sessionID,
    ResumeMode: agentcore.ResumeFromState,
})
```

### 4.3 Continue With Human Input

用于“HITL / approval / follow-up message 后继续”：

```go
result, err := runtime.Run(ctx, agentcore.RunInput{
    RunID:       sessionID,
    Instruction: humanMessage,
    ResumeMode:  agentcore.ResumeWithInput,
})
```

如果你把这三种入口在 host 层区分清楚，整个 runtime 语义会干净很多。

如果你的 host 还需要稳定控制恢复、tool loop 或 recoverable error 语义，推荐再进一步：

- host 负责决定产品流程
- `ResumePolicy` / `ToolExecutionPolicy` / `ErrorPolicy` 负责承接 runtime 主路径决策

而不是把这些逻辑散落到每个 handler 或 hook 里。

当前仓库里的 first-party host wrapper 也已经围绕这三类 contract 做了行为级验证。

如果你想先看“哪些 host 行为已经被验证，哪些地方仍然只是 contract 等价但 lifecycle 不同”，继续看：

- [host-conformance.md](../../../docs/archive/agentcore-2026-10/host-conformance.md)

---

## 5. Streaming 应该怎么接到 Host

最常见的接法是：

- host 提供 `RunInput.Stream`
- runtime 把 provider 的 stream event 回调给 host
- host 再决定是否转发到 UI

典型形态：

```go
result, err := runtime.Run(ctx, agentcore.RunInput{
    RunID:       sessionID,
    AgentID:     "chat-agent",
    Instruction: userMessage,
    Stream: func(_ context.Context, event agentcore.StreamEvent) error {
        switch event.Kind {
        case agentcore.StreamEventTextDelta:
            return ui.SendDelta(event.Delta)
        case agentcore.StreamEventMessageStop:
            return ui.Flush()
        default:
            return nil
        }
    },
})
```

当前推荐理解：

- streaming 是 host-facing observation channel
- final `RunResult.Output` 才是 authority completion

所以 host 不应该只收 delta 而完全忽略最终 `RunResult`。

---

## 6. Yield / Resume 在 Host 侧怎么建模

`RunYielded` 最适合被 host 当成“等待外部动作”的状态，而不是异常。

推荐模式是：

1. host 调用 `Run(...)`
2. 收到 `RunYielded`
3. host 读取 `result.Yield`
4. host 决定等待哪类外部事件
5. 事件到达后，再用相同 `RunID` 调用 `Run(...)`

例如：

```go
result, err := runtime.Run(ctx, input)
if err != nil {
    return err
}
if result.Status == agentcore.RunYielded {
    return waitForApproval(result.RunID, result.Yield)
}
return nil
```

这里最重要的是：

- `yield` 是 runtime outcome
- 不是 provider error
- 也不是 host 失败

如果你想直接看一个更贴近宿主包装方式、并且显式接入三类 policy 的示例，继续看：

- [../examples/policyhost/main.go](../examples/policyhost/main.go)

---

## 7. 错误应该在哪层处理

推荐策略：

### Runtime 层

只负责稳定分类：

- `ProviderError`
- `ToolError`
- `HookError`
- `StorageError`
- `MemoryError`

### Host 层

负责把这些分类映射成业务动作：

- 返回 HTTP 状态码
- 重试
- 告警
- 标记会话失败
- 转人工处理

例如：

```go
result, err := runtime.Run(ctx, input)
if err != nil {
    switch {
    case agentcore.IsProviderError(err):
        return writeTemporaryFailure(err)
    case agentcore.IsStorageError(err):
        return writeCriticalFailure(err)
    default:
        return writeUnknownFailure(err)
    }
}
return writeResult(result)
```

重点是：

- `agentcore` 提供可判断的 runtime failure shape
- host 决定业务后果

---

## 8. 一个更接近真实系统的 Host 工厂

如果你准备在自己的系统里长期使用 `agentcore`，推荐把 runtime 组装集中到一处：

```go
func NewRuntime(provider agentcore.Provider, storage agentcore.Storage, workspace string, approver tools.Approver) agentcore.Runtime {
    reg := tools.NewRegistry()
    for _, tool := range native.BuildTools(native.Env{Workspace: workspace}, false) {
        reg.Register(tool)
    }
    return agentcore.New(agentcore.Config{
        Provider:      provider,
        Storage:       storage, // 持久化存储，跨进程 resume 需要
        Tools:         tools.NewApproval(reg, approver),
        PromptBuilder: hostPrompt{},
    })
}
```

suga 自己的组装（provider 池、MCP、技能、延迟加载与审批层）在应用层 `internal/wiring`，是这个模式的完整版本。

这样做的价值是：

- 默认接法集中
- host 不会到处散落 builder 细节
- 将来替换 provider / storage 更容易

---

## 9. 哪些事情不要塞进 Agentcore

下面这些更适合留在 host：

- HTTP request / response 映射
- websocket / SSE 管理
- 用户鉴权
- 会话路由
- 审批流状态机
- 任务调度与重试队列

这些能力都可能和 `agentcore` 紧密配合，但不应该被塞回 kernel。

---

## 10. 推荐阅读顺序

如果你是在做一个外部 embedding host，推荐按这个顺序看：

1. [getting-started.md](./getting-started.md)
2. [host-integration.md](./host-integration.md)
3. [api.md](./api.md)
4. [runtime-policies.md](./runtime-policies.md)

如果你要看对应示例，再继续看：

- [../examples/recommended/main.go](../examples/recommended/main.go)
- [../examples/host/main.go](../examples/host/main.go)
- [../examples/resume/main.go](../examples/resume/main.go)
- [../examples/stream/main.go](../examples/stream/main.go)
