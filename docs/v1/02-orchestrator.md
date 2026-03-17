# Orchestrator 设计

> 所属层级：编排层 | 相关文档：[架构总纲](./00-overview.md) | [Agent Core](./01-agent-core.md) | [Team 协作](./03-team.md)

---

## 1. 职责边界

Orchestrator 是 kagent 的核心调度中枢，负责：

- **任务路由**：分析用户请求，决定由单 Agent 还是多 Agent 团队处理
- **权限与生命周期层**：基于规则引擎管控工具访问白名单、Agent 生命周期
- **事件/Hook 总线**：驱动 SSE 推送和跨模块通信的内部事件总线
- **HITL 协调**：挂起/恢复 Agent，向用户推送干预请求

Orchestrator 不负责具体的 Agent 推理执行，那是 [Agent Core](./01-agent-core.md) 的职责。

---

## 2. 任务路由

### 2.1 路由决策流程

```
用户请求 → 生成 TraceID → Task Router 分析
    ↓
    ├─ 简单单步任务 → 直接创建 AgentRunner 执行
    ├─ 复杂多步任务 → 判断是否需要 Team
    │       ↓
    │   Team Manager 创建专家组（见 03-team.md）
    └─ 定时触发任务 → Agentic Cron（见 01-agent-core.md）
```

### 2.2 核心接口

```go
type Orchestrator interface {
    Submit(ctx context.Context, req *TaskRequest) (*Task, error)
    GetTask(taskID string) (*Task, error)
    CancelTask(taskID string) error
    ListTasks(filter TaskFilter) ([]*Task, error)
}

type TaskRouter interface {
    Route(ctx context.Context, task *Task) (RouteDecision, error)
}

type RouteDecision struct {
    Mode       RouteMode     // "single" | "team" | "cron"
    AgentID    string
    TeamConfig *TeamConfig
}
```

### 2.3 Task 数据模型

```go
type TaskStatus string
const (
    TaskPending  TaskStatus = "pending"
    TaskRunning  TaskStatus = "running"
    TaskBlocked  TaskStatus = "blocked"  // HITL 等待
    TaskDone     TaskStatus = "done"
    TaskFailed   TaskStatus = "failed"
)

type Task struct {
    ID          string
    Title       string
    Description string
    AssignedTo  string        // Agent ID
    Status      TaskStatus
    Result      string
    ParentID    string        // 父任务 ID
    TraceID     string
    SpanID      string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

---

## 3. 事件/Hook 总线

### 3.1 设计意图

EventBus 是 kagent 内部所有模块解耦通信的中枢：

- Agent 状态变更 → 广播事件 → SSE Server 推送前端
- 工具调用完成 → 广播事件 → Tracing System 记录 Span
- HITL 触发 → 广播事件 → 用户界面弹出干预通知
- Hook 机制 → 在事件流上注册拦截器，实现可插拔扩展

### 3.2 核心接口

```go
type EventBus interface {
    Publish(event Event)
    Subscribe(topic string) <-chan Event
    Unsubscribe(topic string, ch <-chan Event)
    AddHook(topic string, hook HookFn)   // 注册 Hook 拦截器
}

type Event struct {
    ID        string
    Topic     string      // "agent.state", "tool.call", "hitl.request", "trace.span", ...
    TraceID   string
    AgentID   string
    Payload   any
    Timestamp time.Time
}

type HookFn func(event Event) (Event, bool) // 返回 (修改后的事件, 是否继续传播)
```

### 3.3 标准事件 Topic

| Topic | 触发时机 | 主要消费者 |
|-------|----------|-----------|
| `agent.state.changed` | Agent 状态变更 | SSE Server、WebUI |
| `agent.step.completed` | ReAct Loop 每步完成 | Tracing System |
| `tool.call.started` | 工具调用开始 | Tracing System |
| `tool.call.completed` | 工具调用完成 | Memory System、Tracing |
| `tool.call.failed` | 工具调用失败 | HITL Manager |
| `hitl.requested` | HITL 干预请求 | SSE Server、WebUI |
| `hitl.responded` | 人工响应干预 | Agent Runner |
| `task.completed` | 任务完成 | Gateway、Memory |
| `cron.triggered` | Cron 任务触发 | Agent Runner |

### 3.4 Hook 使用场景

```go
// 示例：在工具调用前注入权限检查 Hook
bus.AddHook("tool.call.started", func(e Event) (Event, bool) {
    call := e.Payload.(ToolCallPayload)
    if !permLayer.IsAllowed(e.AgentID, call.ToolName) {
        // 拦截事件，不继续传播
        return e, false
    }
    return e, true
})
```

---

## 4. 权限与生命周期层

### 4.1 设计意图

权限层介于 Task Router 和 Agent Runner 之间，基于规则引擎实现：

- **工具白名单**：每个 Agent（或 SOUL 角色）只能使用被授权的工具
- **工作目录沙箱**：文件操作工具严格限定在 Agent 的 workspace 目录
- **Token 预算管控**：每个 Agent 配置 token 上限，超出则挂起或失败
- **Agent 生命周期管控**：创建、运行、挂起、恢复、销毁的完整生命周期治理

### 4.2 规则引擎结构

```go
type PermissionLayer interface {
    IsAllowed(agentID string, toolName string) bool
    GetWorkspace(agentID string) string
    GetTokenBudget(agentID string) int
    EnforceLifecycle(agentID string, transition StateTransition) error
}

type PermissionRule struct {
    AgentRole   string      // SOUL 角色名，或 "*" 通配
    AllowedTools []string   // 工具白名单，空表示全部禁止
    Workspace   string      // 工作目录路径
    MaxTokens   int         // token 预算上限（0 = 不限制）
    MaxSteps    int         // 最大步骤数
}
```

### 4.3 信任流程

高风险工具（如 `shell_exec`、`file_delete`、外部 Agent 调用）在执行前须经过信任确认流程：

```
工具调用请求
    → PermissionLayer 检查白名单
    → 若工具标记为 "high_risk"
        → 触发 HITL：发送确认请求给人类
        → 等待确认（timeout 可配置）
        → 超时未确认 → 拒绝执行，记录 Span
    → 确认后执行
```

### 4.4 Agent 生命周期管控

```
Created → Initialized → Running → [Suspended | Completed | Failed]
              ↑               ↓
         (Resume)       (HITL 挂起)
              └───────────────┘
```

生命周期每次转移均通过 EventBus 广播，Tracing System 记录完整历史。

---

## 5. HITL Manager

HITL（Human-In-The-Loop）是 Orchestrator 层的关键机制，详细触发流程见 [Gateway & API 设计](./06-gateway-api.md#hitl)。

### 5.1 核心接口

```go
type HITLManager interface {
    RequestIntervention(ctx context.Context, agentID string, reason string) (*HITLTicket, error)
    WaitForResponse(ctx context.Context, ticketID string) (*HITLResponse, error)
    Respond(ticketID string, instruction string) error
    Dismiss(ticketID string, reason string) error
}

type HITLTicket struct {
    ID        string
    AgentID   string
    Reason    string
    Context   string    // 失败的工具调用上下文快照
    CreatedAt time.Time
    Status    string    // "waiting" | "responded" | "dismissed"
}
```

### 5.2 触发条件（可配置）

| 条件 | 默认阈值 | 说明 |
|------|----------|------|
| 连续工具失败 | 3 次 | failCount >= hitlThreshold |
| 高风险工具确认 | 每次 | 工具标记为 `require_approval: true` |
| 任务目标不明确 | - | LLM 判断置信度低时主动发起 |
| Cron 决策置信度低 | - | 感知阶段数据不足时 |

---

## 6. LLM Provider Pool

Provider Pool 为所有 Agent 提供统一的 LLM 访问接口，支持多供应商故障转移。

```go
type LLMProvider interface {
    Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
    Stream(ctx context.Context, req *CompletionRequest) (<-chan string, error)
    Embed(ctx context.Context, text string) ([]float32, error)
}

type CompletionRequest struct {
    Messages    []Message
    Tools       []ToolSchema
    MaxTokens   int
    Temperature float32
    Model       string
}

type CompletionResponse struct {
    Content      string
    ToolCalls    []ToolCall
    Usage        TokenUsage
    FinishReason string
}
```

**Pool 策略**：
- 主备切换：主 provider 失败自动切换备用
- 按模型路由：embedding 任务用轻量模型，推理任务用强模型
- 重试策略：指数退避，最多 3 次
