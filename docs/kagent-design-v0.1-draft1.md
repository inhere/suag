# kagent - 智能多 Agent 编排系统架构设计

> 版本: v0.1-draft | 日期: 2025-03-15 | 状态: 待评审

***

## 目录

1. [需求关联分析](#1-需求关联分析)
2. [整体架构设计](#2-整体架构设计)
3. [核心数据模型](#3-核心数据模型)
4. [关键流程设计](#4-关键流程设计)
5. [模块详细设计](#5-模块详细设计)
6. [技术选型](#6-技术选型)
7. [代码结构](#7-代码结构)
8. [分阶段实施规划](#8-分阶段实施规划)
9. [待确认问题列表](#9-待确认问题列表)

***

## 1. 需求关联分析

### 1.1 功能模块依赖关系

```
用户请求
  │
  ▼
┌─────────────┐
│ Gateway/UI  │◄─── SSE 推送 ◄── Tracing 层
│ (接入层)     │
└──────┬──────┘
       │
       ▼
┌──────────────┐     ┌──────────────┐
│ Orchestrator │────►│ Team Manager │
│  (编排层)     │     │  (团队管理)   │
└──────┬───────┘     └──────┬───────┘
       │                     │
       ├─────────┬───────────┤
       ▼         ▼           ▼
┌──────────┐ ┌────────┐ ┌──────────┐
│ LLM      │ │ Memory │ │  HITL    │
│ Provider │ │ System │ │ Manager  │
└──────────┘ └────────┘ └──────────┘
       │
       ▼
┌──────────────────────────────────┐
│         执行层 (Toolbox)          │
├──────────┬───────┬───────┬───────┤
│ Native   │ MCP   │Skills │Browser│
│ Tools    │Server │Engine │Agent  │
└──────────┴───────┴───────┴───────┘
       │
       ▼
┌──────────────┐
│ External     │
│ Agents       │
│ (claude-code,│
│  opencode)   │
└──────────────┘
```

### 1.2 数据流分析

| 数据流     | 源                    | 目标             | 内容               | 频率          |
| ------- | -------------------- | -------------- | ---------------- | ----------- |
| 用户指令    | Gateway              | Orchestrator   | 自然语言任务           | 按需          |
| LLM 请求  | Agent                | LLM Provider   | Prompt + Context | 每个 ReAct 步骤 |
| 工具调用    | Agent                | Toolbox        | Function Call 参数 | 按需          |
| 记忆检索    | Agent                | Memory System  | 语义查询向量           | 每次对话        |
| 追踪事件    | 所有层                  | Tracing System | Span 数据          | 持续          |
| SSE 推送  | Tracing/Orchestrator | Gateway        | 状态变更事件           | 实时          |
| HITL 请求 | Agent                | HITL Manager   | 决策请求             | 条件触发        |
| 团队协作    | Agent                | Team Manager   | 任务分配/结果          | 按需          |

### 1.3 核心能力 vs 扩展能力边界

**核心能力（必须首批实现）：**

- 单 Agent ReAct Loop 引擎
- LLM Provider 统一接口（至少支持 OpenAI 兼容协议）
- 基础工具注册与执行
- 上下文记忆（短期会话内）
- 基础追踪（TraceID/SpanID）

**扩展能力（可逐步迭代）：**

- 多 Agent 团队协作（Team Manager）
- 向量语义记忆（SQLite + sqlite-vec）
- MCP 协议集成
- SKILL.md 解析引擎
- 外部 Agent 集成（claude-code, opencode）
- 智能定时任务（Agentic Cron）
- Browser Agent
- Web UI 仪表盘
- HITL 人机协作干预
- 多渠道 IM 接入

### 1.4 需求矛盾点与模糊点

| 编号  | 问题                            | 影响    | 建议                                      |
| --- | ----------------------------- | ----- | --------------------------------------- |
| C-1 | "能长时间处理任务" 与 LLM 上下文窗口限制矛盾    | 架构设计  | 需要 Sub-agent 隔离会话 + 摘要回传机制              |
| C-2 | "小团队自带跟踪审核" 与 "默认任务跟踪专家" 是否重叠 | 团队设计  | 建议: 小团队内置 Reviewer 角色，大任务额外有全局审核        |
| C-3 | SOUL.md 与 SKILL.md 的关系未明确     | 角色定义  | SOUL.md 定义角色身份/目标/约束，SKILL.md 定义可执行技能脚本 |
| C-4 | "上帝模式"指令注入的权限边界               | 安全设计  | 需要定义注入指令的优先级和覆盖范围                       |
| C-5 | 外部 Agent 的错误处理和超时策略未定义        | 可靠性   | claude-code 等外部进程可能长时间运行，需要超时和重试策略      |
| C-6 | Browser Agent 的技术方案未指定        | 实现复杂度 | 建议基于 chromedp 或 rod 实现                  |

***

## 2. 整体架构设计

### 2.1 分层架构

```
┌─────────────────────────────────────────────────────────────┐
│                     接入层 (Gateway)                         │
│  ┌─────────┐  ┌──────────┐  ┌─────────┐  ┌──────────────┐  │
│  │ Web UI  │  │  REST    │  │  TUI    │  │  IM Adapter  │  │
│  │Dashboard│  │  API     │  │ Client  │  │ (Slack/微信)  │  │
│  └────┬────┘  └────┬─────┘  └────┬────┘  └──────┬───────┘  │
│       └─────────┬──┴────────────┬┘               │          │
│                 ▼               ▼                 │          │
│  ┌──────────────────────────────────────────────┐│          │
│  │         SSE Event Bus (实时推送)              ││          │
│  └──────────────────────────────────────────────┘│          │
└──────────────────────┬───────────────────────────┘          │
                       │                                       │
┌──────────────────────▼──────────────────────────────────────┐
│                    编排层 (Orchestrator)                      │
│  ┌──────────────┐  ┌──────────────┐  ┌───────────────────┐  │
│  │ Task Router  │  │ Team Manager │  │ Agentic Cron      │  │
│  │ (任务路由)    │  │ (团队管理)    │  │ (智能定时任务)     │  │
│  └──────┬───────┘  └──────┬───────┘  └───────────────────┘  │
│         │                  │                                  │
│  ┌──────▼──────────────────▼───────────────────────────────┐ │
│  │              Agent Runtime (Agent 运行时)                │ │
│  │  ┌────────────┐  ┌───────────┐  ┌────────────────────┐ │ │
│  │  │ ReAct Loop │  │ Planner   │  │ HITL Manager       │ │ │
│  │  │ (推理循环)  │  │ (规划器)   │  │ (人机协作管理)     │ │ │
│  │  └────────────┘  └───────────┘  └────────────────────┘ │ │
│  └─────────────────────────────────────────────────────────┘ │
└──────────────────────┬───────────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────────┐
│                    能力层 (Capabilities)                      │
│  ┌────────────┐  ┌──────────────┐  ┌──────────────────────┐ │
│  │ LLM        │  │ Memory       │  │ Tracing              │ │
│  │ Provider   │  │ System       │  │ System               │ │
│  │ (多供应商)  │  │ (记忆系统)    │  │ (链路追踪)           │ │
│  └────────────┘  └──────────────┘  └──────────────────────┘ │
└──────────────────────┬───────────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────────┐
│                    执行层 (Toolbox)                           │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌───────────────┐  │
│  │ Native   │ │ MCP      │ │ Skill    │ │ External      │  │
│  │ Tools    │ │ Client   │ │ Engine   │ │ Agent Bridge  │  │
│  │(文件/搜索)│ │(MCP协议) │ │(脚本执行) │ │(claude-code等)│  │
│  └──────────┘ └──────────┘ └──────────┘ └───────────────┘  │
│  ┌────────────────────────┐                                  │
│  │ Browser Agent (浏览器) │                                  │
│  └────────────────────────┘                                  │
└─────────────────────────────────────────────────────────────┘
```

### 2.2 各层职责边界

| 层   | 职责                       | 不做什么               |
| --- | ------------------------ | ------------------ |
| 接入层 | 协议适配、认证鉴权、请求路由、SSE 推送    | 不做业务逻辑、不直接调用 LLM   |
| 编排层 | 任务分解、Agent 调度、团队协作、状态机管理 | 不直接做工具执行、不管 LLM 细节 |
| 能力层 | LLM 调用、记忆管理、追踪埋点         | 不做业务决策、不管任务调度      |
| 执行层 | 工具执行、外部系统集成、沙箱隔离         | 不做推理决策             |

### 2.3 接口契约

#### 2.3.1 Agent 接口

```go
// Agent 是所有智能体的核心抽象
type Agent interface {
    // ID 返回 Agent 唯一标识
    ID() string
    // Run 执行一个任务，返回结果
    Run(ctx context.Context, task *Task) (*TaskResult, error)
    // Soul 返回 Agent 的角色定义
    Soul() *SoulConfig
    // Status 返回当前状态
    Status() AgentStatus
}
```

#### 2.3.2 LLM Provider 接口

```go
// LLMProvider 统一 LLM 调用接口
type LLMProvider interface {
    // Complete 执行一次补全（支持 function calling）
    Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
    // Stream 流式补全
    Stream(ctx context.Context, req *CompletionRequest) (<-chan StreamChunk, error)
    // ModelInfo 返回模型信息（上下文窗口等）
    ModelInfo() ModelInfo
}
```

#### 2.3.3 Tool 接口

```go
// Tool 工具统一接口
type Tool interface {
    // Name 工具名
    Name() string
    // Description 工具描述（供 LLM 理解）
    Description() string
    // Schema 参数 JSON Schema
    Schema() json.RawMessage
    // Execute 执行工具
    Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error)
}
```

#### 2.3.4 Memory 接口

```go
// MemoryStore 记忆存储接口
type MemoryStore interface {
    // Store 存储一条记忆
    Store(ctx context.Context, entry *MemoryEntry) error
    // Search 语义检索
    Search(ctx context.Context, query string, limit int) ([]*MemoryEntry, error)
    // GetConversation 获取会话记忆
    GetConversation(ctx context.Context, sessionID string) ([]*Message, error)
}
```

***

## 3. 核心数据模型

### 3.1 实体关系图

```
┌───────────┐      ┌───────────┐      ┌───────────┐
│   Team    │1───N │  Agent    │1───N │   Task    │
│           │      │           │      │           │
│ id        │      │ id        │      │ id        │
│ name      │      │ soul_id   │      │ agent_id  │
│ agents[]  │      │ team_id   │      │ trace_id  │
│ created_at│      │ status    │      │ status    │
└───────────┘      │ tools[]   │      │ parent_id │
                   └─────┬─────┘      │ result    │
                         │            └───────────┘
                         │1
                         │
                         │N
                   ┌─────▼─────┐      ┌───────────┐
                   │  Message  │      │   Trace   │
                   │           │      │           │
                   │ id        │      │ trace_id  │
                   │ session_id│      │ span_id   │
                   │ role      │      │ parent_id │
                   │ content   │      │ agent_id  │
                   │ tool_call │      │ event     │
                   └───────────┘      │ timestamp │
                                      └───────────┘
                   ┌───────────┐
                   │  Memory   │
                   │           │
                   │ id        │
                   │ content   │
                   │ embedding │ (vec)
                   │ metadata  │
                   │ timestamp │
                   └───────────┘
```

### 3.2 核心结构体定义

```go
// === 团队与角色 ===

// SoulConfig 角色灵魂定义（对应 SOUL.md）
type SoulConfig struct {
    Name        string            `json:"name"`         // 角色名称
    Role        string            `json:"role"`         // 角色类型: architect/coder/reviewer/...
    Goal        string            `json:"goal"`         // 核心目标
    Constraints []string          `json:"constraints"`  // 行为约束
    Tools       []string          `json:"tools"`        // 可使用的工具列表
    Skills      []string          `json:"skills"`       // 可使用的技能列表
    Model       string            `json:"model"`        // 使用的 LLM 模型
    SystemPrompt string           `json:"system_prompt"` // 系统提示词
}

// Team 团队定义
type Team struct {
    ID          string            `json:"id"`
    Name        string            `json:"name"`
    Agents      []*AgentInstance  `json:"agents"`
    Tracker     *AgentInstance    `json:"tracker"`     // 内置跟踪专家
    Reviewer    *AgentInstance    `json:"reviewer"`    // 内置审核专家
    CreatedAt   time.Time         `json:"created_at"`
}

// === 任务 ===

type TaskStatus string
const (
    TaskPending    TaskStatus = "pending"
    TaskRunning    TaskStatus = "running"
    TaskBlocked    TaskStatus = "blocked"     // HITL 等待人类干预
    TaskCompleted  TaskStatus = "completed"
    TaskFailed     TaskStatus = "failed"
)

type Task struct {
    ID          string            `json:"id"`
    TeamID      string            `json:"team_id"`
    AgentID     string            `json:"agent_id"`
    ParentID    string            `json:"parent_id"`   // 父任务 ID
    TraceID     string            `json:"trace_id"`
    Title       string            `json:"title"`
    Description string            `json:"description"`
    Status      TaskStatus        `json:"status"`
    Result      *TaskResult       `json:"result"`
    RetryCount  int               `json:"retry_count"`
    MaxRetries  int               `json:"max_retries"`
    CreatedAt   time.Time         `json:"created_at"`
    UpdatedAt   time.Time         `json:"updated_at"`
}

type TaskResult struct {
    Success bool                  `json:"success"`
    Output  string                `json:"output"`
    Error   string                `json:"error,omitempty"`
    Artifacts []Artifact          `json:"artifacts,omitempty"`
}

// === 消息与记忆 ===

type Message struct {
    ID        string              `json:"id"`
    SessionID string              `json:"session_id"`
    Role      string              `json:"role"`       // user/assistant/system/tool
    Content   string              `json:"content"`
    ToolCalls []ToolCall          `json:"tool_calls,omitempty"`
    Timestamp time.Time           `json:"timestamp"`
}

type ToolCall struct {
    ID        string              `json:"id"`
    Name      string              `json:"name"`
    Arguments json.RawMessage     `json:"arguments"`
    Result    string              `json:"result,omitempty"`
}

type MemoryEntry struct {
    ID        string              `json:"id"`
    Content   string              `json:"content"`
    Embedding []float32           `json:"-"`           // 向量嵌入
    Metadata  map[string]string   `json:"metadata"`
    AgentID   string              `json:"agent_id"`
    SessionID string              `json:"session_id"`
    CreatedAt time.Time           `json:"created_at"`
}

// === 追踪 ===

type Span struct {
    TraceID   string              `json:"trace_id"`
    SpanID    string              `json:"span_id"`
    ParentID  string              `json:"parent_id,omitempty"`
    AgentID   string              `json:"agent_id"`
    Operation string              `json:"operation"`   // llm_call/tool_exec/agent_spawn/...
    Status    string              `json:"status"`
    Input     string              `json:"input"`
    Output    string              `json:"output"`
    StartTime time.Time           `json:"start_time"`
    EndTime   time.Time           `json:"end_time"`
    Tags      map[string]string   `json:"tags"`
}

// === LLM ===

type CompletionRequest struct {
    Model       string            `json:"model"`
    Messages    []Message         `json:"messages"`
    Tools       []ToolSchema      `json:"tools,omitempty"`
    Temperature float64           `json:"temperature"`
    MaxTokens   int               `json:"max_tokens"`
}

type CompletionResponse struct {
    Content    string             `json:"content"`
    ToolCalls  []ToolCall         `json:"tool_calls,omitempty"`
    IsComplete bool               `json:"is_complete"`  // Agent 判断任务完成
    Usage      TokenUsage         `json:"usage"`
}
```

***

## 4. 关键流程设计

### 4.1 单 Agent ReAct Loop

```
                    ┌─────────────┐
                    │  Task Input │
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │ 加载上下文   │
                    │ (Memory +   │
                    │  SOUL.md)   │
                    └──────┬──────┘
                           │
                ┌──────────▼──────────┐
         ┌──────┤   LLM Reasoning    │◄──────────────┐
         │      │   (思考+决策)        │               │
         │      └──────────┬──────────┘               │
         │                 │                           │
         │     ┌───────────▼────────────┐              │
         │     │  输出类型判断            │              │
         │     └───┬──────┬──────┬──────┘              │
         │         │      │      │                     │
         │    Tool Call  Text  Complete                 │
         │         │      │      │                     │
         │    ┌────▼────┐ │  ┌───▼───┐                 │
         │    │执行工具  │ │  │ 返回  │                 │
         │    │写入结果  │ │  │ 结果  │                 │
         │    └────┬────┘ │  └───────┘                 │
         │         │      │                            │
         │         └──┬───┘                            │
         │            │                                │
         │     ┌──────▼──────┐                         │
         │     │ 更新Memory  │                         │
         │     │ 发送Trace   │                         │
         │     └──────┬──────┘                         │
         │            │                                │
         │     ┌──────▼──────┐   是    ┌────────────┐  │
         │     │ 达到最大步数?│───────►│ HITL 挂起  │  │
         │     └──────┬──────┘        └────────────┘  │
         │            │ 否                             │
         │            └────────────────────────────────┘
         │
    Error/Retry
         │
    ┌────▼────────────┐
    │ 重试计数器 +1    │
    │ 达到3次? ──► HITL│
    └─────────────────┘
```

**ReAct Loop 状态机：**

```
        ┌──────────┐
        │  IDLE    │
        └────┬─────┘
             │ run()
        ┌────▼─────┐
        │ THINKING │◄──────────────┐
        └────┬─────┘               │
             │                     │
        ┌────▼──────┐         ┌────┴─────┐
        │ ACTING    │────────►│ OBSERVING│
        │(tool_call)│         │(结果处理) │
        └────┬──────┘         └──────────┘
             │
        ┌────▼─────┐
        │RESPONDING│
        └────┬─────┘
             │
        ┌────▼─────┐    ┌──────────┐
        │ COMPLETE │    │ BLOCKED  │ (HITL等待)
        └──────────┘    └──────────┘
```

### 4.2 多 Agent 协作流程

```
用户请求: "实现一个用户登录功能"
    │
    ▼
┌──────────────────────────────────────────────┐
│ Orchestrator: 任务分解                        │
│                                              │
│ 1. 创建 Trace (TraceID=abc123)               │
│ 2. 选择/创建 Team                            │
│ 3. 将任务路由给 Architect                     │
└──────────────┬───────────────────────────────┘
               │
    ┌──────────▼──────────┐
    │ Architect Agent     │ SpanID=span-001, ParentID=root
    │ 产出: 设计方案       │
    └──────────┬──────────┘
               │
    ┌──────────▼──────────┐
    │ Orchestrator:       │
    │ 分发给 Coder(s)     │
    └───┬─────────────┬───┘
        │             │
  ┌─────▼─────┐ ┌────▼──────┐
  │ Coder-1   │ │ Coder-2   │ 并发执行
  │ (后端)     │ │ (前端)     │ SpanID=span-002/003
  │ ParentID= │ │ ParentID= │ ParentID=span-001
  │ span-001  │ │ span-001  │
  └─────┬─────┘ └────┬──────┘
        │             │
        └──────┬──────┘
               │
    ┌──────────▼──────────┐
    │ Reviewer Agent      │ SpanID=span-004
    │ 审查所有产出         │ ParentID=span-001
    │ 通过? ──► 完成       │
    │ 不通过? ──► 回退Coder│
    └─────────────────────┘
```

### 4.3 Sub-Agent 隔离会话（避免上下文膨胀）

```
主 Agent (长时间运行)
│
│  上下文: [系统提示 + 任务概述 + 摘要历史]
│
├─► 发起 Sub-task A ────────────────────────┐
│                                           │
│   Sub-Agent A (独立会话)                   │
│   上下文: [系统提示 + 子任务描述]           │
│   执行 ReAct Loop...                      │
│   返回: 结构化摘要结果                     │
│                                    ◄──────┘
│
│  主 Agent 收到摘要，写入自己的上下文
│  (而非整个子会话历史)
│
├─► 发起 Sub-task B ────────────────────────┐
│                                           │
│   Sub-Agent B (独立会话)                   │
│   ...                                     │
│                                    ◄──────┘
│
▼ 汇总所有子结果，形成最终输出
```

### 4.4 HITL 干预流程

```
Agent 执行中
    │
    ├─► 连续失败 3 次
    │   或
    ├─► 触发"关键决策"标记
    │   或
    ├─► Reviewer 标记需人工审查
    │
    ▼
┌──────────────────────────┐
│ HITL Manager             │
│ 1. 任务状态 → BLOCKED    │
│ 2. 生成干预请求           │
│    (包含上下文摘要、       │
│     失败原因、建议选项)    │
│ 3. 通过 SSE 推送到 UI    │
│ 4. Go channel 阻塞等待   │
└───────────┬──────────────┘
            │
            ▼ (等待人类响应)
┌──────────────────────────┐
│ 人类通过 Web UI 响应:     │
│ - 批准继续                │
│ - 提供修正指令             │
│ - 终止任务                │
└───────────┬──────────────┘
            │
            ▼
┌──────────────────────────┐
│ Agent 恢复执行            │
│ 将人类指令注入上下文       │
│ 重置失败计数器            │
└──────────────────────────┘
```

***

## 5. 模块详细设计

### 5.1 Core Agent 引擎

**职责：** 实现 ReAct Loop 推理循环，管理单个 Agent 的生命周期。

```go
// AgentEngine 核心引擎
type AgentEngine struct {
    id          string
    soul        *SoulConfig
    llm         LLMProvider
    memory      MemoryStore
    tools       *ToolRegistry
    tracer      *Tracer
    hitl        *HITLManager
    maxSteps    int
    failCount   int               // 连续失败计数
    failLimit   int               // HITL 触发阈值(默认3)
    status      AgentStatus
    mu          sync.RWMutex
}

func (e *AgentEngine) Run(ctx context.Context, task *Task) (*TaskResult, error) {
    span := e.tracer.StartSpan(ctx, "agent.run", task.TraceID)
    defer span.End()

    // 1. 加载角色上下文
    systemPrompt := e.soul.SystemPrompt
    memories, _ := e.memory.Search(ctx, task.Description, 5)

    // 2. 构建初始消息
    messages := e.buildInitialMessages(systemPrompt, memories, task)

    // 3. ReAct 循环
    for step := 0; step < e.maxSteps; step++ {
        e.setStatus(StatusThinking)

        // 3.1 LLM 推理
        resp, err := e.llm.Complete(ctx, &CompletionRequest{
            Messages: messages,
            Tools:    e.tools.GetSchemas(),
        })
        if err != nil {
            if e.handleFailure(ctx, task, err) {
                continue // HITL 解除后重试
            }
            return nil, err
        }

        // 3.2 处理工具调用
        if len(resp.ToolCalls) > 0 {
            e.setStatus(StatusActing)
            for _, tc := range resp.ToolCalls {
                result, err := e.tools.Execute(ctx, tc.Name, tc.Arguments)
                // ... 追踪、错误处理、写入消息
            }
            continue
        }

        // 3.3 检查是否完成
        if resp.IsComplete {
            e.setStatus(StatusComplete)
            // 存储新记忆
            e.memory.Store(ctx, &MemoryEntry{Content: resp.Content})
            return &TaskResult{Success: true, Output: resp.Content}, nil
        }

        messages = append(messages, Message{Role: "assistant", Content: resp.Content})
    }

    return nil, ErrMaxStepsReached
}

// handleFailure 处理失败，达到阈值时触发 HITL
func (e *AgentEngine) handleFailure(ctx context.Context, task *Task, err error) bool {
    e.failCount++
    if e.failCount >= e.failLimit {
        e.setStatus(StatusBlocked)
        instruction, hitlErr := e.hitl.RequestIntervention(ctx, &InterventionRequest{
            TaskID:  task.ID,
            AgentID: e.id,
            Reason:  fmt.Sprintf("连续失败 %d 次: %v", e.failCount, err),
        })
        if hitlErr != nil {
            return false
        }
        e.failCount = 0 // 重置
        // 注入人类指令到上下文
        return true
    }
    return false
}
```

### 5.2 Team Manager

**职责：** 管理团队创建、角色分配、任务分发和协作。

**设计原则：**

- 每个团队自动包含一个 Tracker（跟踪）和 Reviewer（审核）角色
- 支持动态创建专家角色
- 基于 Go channel 实现 Agent 间消息传递

```go
type TeamManager struct {
    teams    map[string]*Team
    agents   map[string]*AgentEngine
    souls    *SoulRegistry        // SOUL.md 注册表
    llmPool  *LLMProviderPool
    tracer   *Tracer
    mu       sync.RWMutex
}

// CreateTeam 创建团队（自动附带 Tracker + Reviewer）
func (tm *TeamManager) CreateTeam(ctx context.Context, cfg TeamConfig) (*Team, error) {
    team := &Team{
        ID:   generateID(),
        Name: cfg.Name,
    }

    // 自动创建内置角色
    tracker := tm.spawnAgent(ctx, team.ID, tm.souls.Get("tracker"))
    reviewer := tm.spawnAgent(ctx, team.ID, tm.souls.Get("reviewer"))
    team.Tracker = tracker
    team.Reviewer = reviewer

    // 创建业务角色
    for _, soulName := range cfg.Roles {
        agent := tm.spawnAgent(ctx, team.ID, tm.souls.Get(soulName))
        team.Agents = append(team.Agents, agent)
    }

    tm.teams[team.ID] = team
    return team, nil
}

// ExecuteTask 编排多 Agent 执行任务
func (tm *TeamManager) ExecuteTask(ctx context.Context, team *Team, task *Task) (*TaskResult, error) {
    // 1. Tracker 开始跟踪
    go team.Tracker.Run(ctx, &Task{Description: "跟踪任务进展: " + task.Title})

    // 2. 根据任务类型选择执行策略
    switch {
    case task.RequiresDesign:
        // 串行: Architect -> Coder(s) -> Reviewer
        return tm.sequentialPipeline(ctx, team, task)
    default:
        // 单 Agent 直接执行
        agent := tm.selectBestAgent(team, task)
        return agent.Run(ctx, task)
    }
}

// sequentialPipeline 串行流水线
func (tm *TeamManager) sequentialPipeline(ctx context.Context, team *Team, task *Task) (*TaskResult, error) {
    // Phase 1: 架构设计
    architect := tm.findByRole(team, "architect")
    design, err := architect.Run(ctx, task)
    if err != nil {
        return nil, err
    }

    // Phase 2: 并发编码（基于设计分解子任务）
    subTasks := tm.decomposeFromDesign(design)
    results := make([]*TaskResult, len(subTasks))
    var wg sync.WaitGroup
    for i, st := range subTasks {
        wg.Add(1)
        go func(idx int, t *Task) {
            defer wg.Done()
            coder := tm.findByRole(team, "coder")
            results[idx], _ = coder.Run(ctx, t)
        }(i, st)
    }
    wg.Wait()

    // Phase 3: 审核
    reviewTask := tm.buildReviewTask(results)
    return team.Reviewer.Run(ctx, reviewTask)
}
```

**SoulRegistry - SOUL.md 解析：**

```go
// SoulRegistry 管理所有角色定义
type SoulRegistry struct {
    souls map[string]*SoulConfig
    dir   string // SOUL.md 文件目录
}

// Load 从目录加载所有 SOUL.md 文件
func (sr *SoulRegistry) Load() error {
    // 扫描 souls/ 目录
    // 解析 SOUL.md (Markdown front-matter + 正文)
    // 注册到 map
}
```

SOUL.md 文件格式示例：

```markdown
---
name: architect
role: architect
goal: 分析需求，设计高质量的技术方案
model: claude-sonnet-4-20250514
tools: [web_search, file_read, file_write]
skills: []
---

## 身份
你是一位资深软件架构师...

## 约束
- 每个设计必须包含接口定义
- 必须考虑可测试性
...

## 输出格式
...
```

### 5.3 Memory System

**职责：** 管理短期会话记忆和长期语义记忆。

**分层设计：**

```
┌─────────────────────────────────┐
│        Memory Manager           │
├─────────────┬───────────────────┤
│ 短期记忆     │ 长期记忆           │
│ (会话内)     │ (跨会话)           │
│             │                   │
│ 内存 ring   │ SQLite +          │
│ buffer      │ sqlite-vec        │
│             │ (向量语义检索)      │
└─────────────┴───────────────────┘
```

```go
type MemoryManager struct {
    db        *sql.DB              // SQLite 连接
    embedder  EmbeddingProvider    // 嵌入模型
    sessions  map[string]*SessionMemory
    mu        sync.RWMutex
}

// SessionMemory 会话级短期记忆
type SessionMemory struct {
    messages  []Message
    maxSize   int                  // 消息数上限
    summary   string               // 压缩摘要
}

// Store 存储记忆并生成嵌入向量
func (mm *MemoryManager) Store(ctx context.Context, entry *MemoryEntry) error {
    // 1. 生成嵌入向量
    embedding, err := mm.embedder.Embed(ctx, entry.Content)
    if err != nil {
        return err
    }
    entry.Embedding = embedding

    // 2. 写入 SQLite（含向量）
    _, err = mm.db.ExecContext(ctx, `
        INSERT INTO memories (id, content, embedding, metadata, agent_id, session_id, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?)
    `, entry.ID, entry.Content, serializeVec(embedding),
       jsonMarshal(entry.Metadata), entry.AgentID, entry.SessionID, entry.CreatedAt)

    return err
}

// Search 语义检索
func (mm *MemoryManager) Search(ctx context.Context, query string, limit int) ([]*MemoryEntry, error) {
    // 1. 查询文本生成嵌入
    queryVec, err := mm.embedder.Embed(ctx, query)
    if err != nil {
        return nil, err
    }

    // 2. 向量相似度搜索 (sqlite-vec)
    rows, err := mm.db.QueryContext(ctx, `
        SELECT id, content, metadata, agent_id, distance
        FROM memories
        WHERE embedding MATCH ?
        ORDER BY distance
        LIMIT ?
    `, serializeVec(queryVec), limit)

    // 3. 解析结果
    // ...
}

// GetConversation 获取会话记忆（带自动摘要压缩）
func (mm *MemoryManager) GetConversation(ctx context.Context, sessionID string) ([]*Message, error) {
    mm.mu.RLock()
    session, exists := mm.sessions[sessionID]
    mm.mu.RUnlock()

    if !exists {
        return nil, ErrSessionNotFound
    }

    // 如果消息过多，返回摘要 + 最近N条
    if len(session.messages) > session.maxSize {
        return session.getCompressedContext(), nil
    }
    return session.messages, nil
}
```

**SQLite Schema：**

```sql
CREATE TABLE memories (
    id TEXT PRIMARY KEY,
    content TEXT NOT NULL,
    embedding BLOB,          -- sqlite-vec 向量
    metadata TEXT,            -- JSON
    agent_id TEXT,
    session_id TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- sqlite-vec 虚拟表 (使用 asg017/sqlite-vec 方案)
CREATE VIRTUAL TABLE memory_vec USING vec0(
    id TEXT PRIMARY KEY,
    embedding float[1536]     -- 维度取决于嵌入模型
);

CREATE TABLE messages (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    tool_calls TEXT,          -- JSON
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_messages_session ON messages(session_id, created_at);
```

### 5.4 Tracing System

**职责：** 全链路追踪，记录 Agent 间因果关系。

```go
type Tracer struct {
    db       *sql.DB
    eventBus *EventBus          // SSE 推送用
}

type Span struct {
    TraceID   string
    SpanID    string
    ParentID  string
    AgentID   string
    Operation string             // agent.run / llm.complete / tool.execute
    Status    SpanStatus         // running / success / error
    Input     string
    Output    string
    StartTime time.Time
    EndTime   time.Time
    Tags      map[string]string
    Events    []SpanEvent        // 中间事件
}

type SpanEvent struct {
    Name      string
    Timestamp time.Time
    Data      map[string]string
}

func (t *Tracer) StartSpan(ctx context.Context, operation string, traceID string) *SpanHandle {
    span := &Span{
        TraceID:   traceID,
        SpanID:    generateSpanID(),
        ParentID:  spanFromContext(ctx),
        Operation: operation,
        Status:    SpanRunning,
        StartTime: time.Now(),
    }

    // 实时推送 span 开始事件
    t.eventBus.Publish(Event{
        Type: "span_start",
        Data: span,
    })

    return &SpanHandle{span: span, tracer: t}
}

func (h *SpanHandle) End() {
    h.span.EndTime = time.Now()
    // 持久化到 SQLite
    h.tracer.persist(h.span)
    // 推送 span 结束事件
    h.tracer.eventBus.Publish(Event{
        Type: "span_end",
        Data: h.span,
    })
}
```

**追踪数据 SQLite Schema：**

```sql
CREATE TABLE spans (
    trace_id TEXT NOT NULL,
    span_id TEXT PRIMARY KEY,
    parent_id TEXT,
    agent_id TEXT,
    operation TEXT NOT NULL,
    status TEXT NOT NULL,
    input TEXT,
    output TEXT,
    start_time DATETIME NOT NULL,
    end_time DATETIME,
    tags TEXT                  -- JSON
);

CREATE INDEX idx_spans_trace ON spans(trace_id);
CREATE INDEX idx_spans_parent ON spans(parent_id);
```

### 5.5 Tool Registry

**职责：** 统一管理 Native Tools、MCP Tools 和 Skills。

```go
type ToolRegistry struct {
    native    map[string]Tool        // 内置工具
    mcp       map[string]*MCPClient  // MCP 服务器连接
    skills    map[string]*Skill      // SKILL.md 定义的技能
    mu        sync.RWMutex
}

// RegisterNative 注册内置工具
func (r *ToolRegistry) RegisterNative(tool Tool) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.native[tool.Name()] = tool
}

// ConnectMCP 连接 MCP 服务器
func (r *ToolRegistry) ConnectMCP(ctx context.Context, serverConfig MCPServerConfig) error {
    // 使用 modelcontextprotocol/go-sdk 连接
    client, err := mcp.NewClient(serverConfig.Name, serverConfig.Version)
    if err != nil {
        return err
    }
    // 通过 stdio 或 SSE 传输层连接
    transport := mcp.NewCommandTransport(serverConfig.Command, serverConfig.Args...)
    if err := client.Connect(ctx, transport); err != nil {
        return err
    }
    // 获取服务器提供的工具列表
    tools, err := client.ListTools(ctx)
    // 注册到 registry
    r.mu.Lock()
    r.mcp[serverConfig.Name] = client
    r.mu.Unlock()
    return nil
}

// LoadSkills 加载 SKILL.md 文件
func (r *ToolRegistry) LoadSkills(dir string) error {
    // 扫描 skills/ 目录
    // 解析每个 SKILL.md
    // 注册为可调用的工具
}

// GetSchemas 返回所有工具的 JSON Schema（供 LLM function calling）
func (r *ToolRegistry) GetSchemas() []ToolSchema {
    r.mu.RLock()
    defer r.mu.RUnlock()

    var schemas []ToolSchema
    for _, t := range r.native {
        schemas = append(schemas, t.Schema())
    }
    // MCP 工具和 Skills 也加入
    // ...
    return schemas
}

// Execute 统一执行入口
func (r *ToolRegistry) Execute(ctx context.Context, name string, args json.RawMessage) (*ToolResult, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()

    // 优先查找 native
    if tool, ok := r.native[name]; ok {
        return tool.Execute(ctx, args)
    }
    // 查找 MCP
    // 查找 Skills
    return nil, fmt.Errorf("tool not found: %s", name)
}
```

**SKILL.md 格式示例：**

````markdown
---
name: deploy_service
description: 部署服务到指定环境
type: shell                  # shell / python / flow
timeout: 300s
---

## 参数
- service_name: 服务名称 (必填)
- environment: 目标环境 (dev/staging/prod)

## 执行脚本
```bash
#!/bin/bash
cd /workspace/${service_name}
make build
kubectl apply -f deploy/${environment}.yaml
````

## Flow 类型示例 (多步骤编排)

```yaml
steps:
  - name: analyze
    agent: architect
    prompt: "分析 ${service_name} 的部署风险"
  - name: deploy
    agent: executor
    prompt: "执行部署: ${service_name} to ${environment}"
    depends_on: [analyze]
  - name: verify
    agent: qa-tester
    prompt: "验证 ${service_name} 在 ${environment} 的健康状态"
    depends_on: [deploy]
```

### 5.6 LLM Provider

**职责：** 提供统一的多供应商 LLM 调用接口。

```go
// LLMProviderPool 多供应商管理
type LLMProviderPool struct {
    providers map[string]LLMProvider   // key: provider name
    models    map[string]string        // model -> provider 映射
    mu        sync.RWMutex
}

// 供应商实现
type OpenAIProvider struct {
    client  *openai.Client
    baseURL string                     // 支持自定义端点(DeepSeek等)
}

type ClaudeProvider struct {
    client  *anthropic.Client
}

// 统一适配层：将各供应商的请求/响应格式标准化
func (p *OpenAIProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
    // 将 CompletionRequest 转换为 OpenAI 格式
    openaiReq := p.convertRequest(req)
    // 调用 API
    resp, err := p.client.Chat.Completions.New(ctx, openaiReq)
    // 将响应转换回标准格式
    return p.convertResponse(resp), err
}

// 路由策略
func (pool *LLMProviderPool) GetProvider(model string) (LLMProvider, error) {
    pool.mu.RLock()
    defer pool.mu.RUnlock()

    providerName, ok := pool.models[model]
    if !ok {
        return nil, fmt.Errorf("unknown model: %s", model)
    }
    provider, ok := pool.providers[providerName]
    if !ok {
        return nil, fmt.Errorf("provider not configured: %s", providerName)
    }
    return provider, nil
}
```

**配置示例 (`config.yaml`)：**

```yaml
llm:
  providers:
    openai:
      api_key: ${OPENAI_API_KEY}
      base_url: https://api.openai.com/v1
      models:
        - gpt-4o
        - gpt-4o-mini
    claude:
      api_key: ${ANTHROPIC_API_KEY}
      models:
        - claude-sonnet-4-20250514
        - claude-haiku-4-20250414
    deepseek:
      api_key: ${DEEPSEEK_API_KEY}
      base_url: https://api.deepseek.com/v1    # OpenAI 兼容
      models:
        - deepseek-chat
        - deepseek-coder

  routing:
    default: gpt-4o
    roles:
      architect: claude-sonnet-4-20250514
      coder: deepseek-coder
      reviewer: gpt-4o
```

### 5.7 External Agent Bridge

**职责：** 集成外部 Agent（claude-code, opencode 等）。

```go
// ExternalAgentBridge 外部 Agent 桥接器
type ExternalAgentBridge struct {
    agents map[string]ExternalAgent
}

type ExternalAgent interface {
    Name() string
    Run(ctx context.Context, prompt string, opts *ExternalAgentOpts) (*ExternalAgentResult, error)
}

// ClaudeCodeAgent 通过子进程调用 claude-code CLI
type ClaudeCodeAgent struct {
    binaryPath   string
    workDir      string
    allowedTools []string
    timeout      time.Duration
}

func (c *ClaudeCodeAgent) Run(ctx context.Context, prompt string, opts *ExternalAgentOpts) (*ExternalAgentResult, error) {
    ctx, cancel := context.WithTimeout(ctx, c.timeout)
    defer cancel()

    args := []string{
        "-p", prompt,
        "--output-format", "stream-json",
    }
    if len(c.allowedTools) > 0 {
        args = append(args, "--allowedTools", strings.Join(c.allowedTools, ","))
    }

    cmd := exec.CommandContext(ctx, c.binaryPath, args...)
    cmd.Dir = opts.WorkDir

    // 通过 stdin/stdout 流式读取结果
    stdout, _ := cmd.StdoutPipe()
    cmd.Start()

    // 解析 stream-json 输出
    var result ExternalAgentResult
    decoder := json.NewDecoder(stdout)
    for decoder.More() {
        var event map[string]interface{}
        decoder.Decode(&event)
        // 处理事件，提取最终结果
    }

    cmd.Wait()
    return &result, nil
}

// OpencodeAgent 类似实现
type OpencodeAgent struct {
    binaryPath string
    // ...
}
```

### 5.8 Agentic Cron

**职责：** 智能定时任务，具备自主决策能力。

```go
type AgenticCron struct {
    scheduler *cron.Cron          // robfig/cron
    jobs      map[string]*AgenticJob
    engine    *AgentEngine
    tracer    *Tracer
    mu        sync.RWMutex
}

type AgenticJob struct {
    ID          string
    Name        string
    Schedule    string             // cron 表达式
    SenseTools  []string           // 感知阶段使用的工具
    DecisionPrompt string          // 决策提示词
    ActionPrompt   string          // 执行提示词
    LastRun     time.Time
    LastResult  string
}

func (ac *AgenticCron) RegisterJob(job *AgenticJob) error {
    ac.scheduler.AddFunc(job.Schedule, func() {
        ac.executeJob(context.Background(), job)
    })
    return nil
}

func (ac *AgenticCron) executeJob(ctx context.Context, job *AgenticJob) {
    span := ac.tracer.StartSpan(ctx, "cron.job", generateTraceID())
    defer span.End()

    // Phase 1: 感知
    senseResults := make(map[string]string)
    for _, toolName := range job.SenseTools {
        result, err := ac.engine.tools.Execute(ctx, toolName, nil)
        if err == nil {
            senseResults[toolName] = result.Output
        }
    }

    // Phase 2: 决策
    decisionPrompt := fmt.Sprintf(job.DecisionPrompt, senseResults)
    decision, _ := ac.engine.llm.Complete(ctx, &CompletionRequest{
        Messages: []Message{{Role: "user", Content: decisionPrompt}},
    })

    if !decision.ShouldAct {
        // 记录日志，不执行
        log.Info("Cron job decided no action needed",
            "job", job.Name, "reason", decision.Content)
        return
    }

    // Phase 3: 执行（启动完整 ReAct 任务）
    task := &Task{
        Title:       fmt.Sprintf("Cron: %s", job.Name),
        Description: job.ActionPrompt,
    }
    ac.engine.Run(ctx, task)
}
```

### 5.9 HITL Manager

**职责：** 管理人机协作干预请求和响应。

```go
type HITLManager struct {
    pending   map[string]*InterventionRequest
    eventBus  *EventBus          // SSE 推送
    mu        sync.RWMutex
}

type InterventionRequest struct {
    ID        string
    TaskID    string
    AgentID   string
    Reason    string
    Context   string             // 上下文摘要
    Options   []string           // 建议选项
    CreatedAt time.Time
    Response  chan *InterventionResponse
}

type InterventionResponse struct {
    Action    string             // approve / instruct / abort
    Message   string             // 人类指令
}

// RequestIntervention 请求人类干预（阻塞直到响应）
func (hm *HITLManager) RequestIntervention(ctx context.Context, req *InterventionRequest) (*InterventionResponse, error) {
    req.ID = generateID()
    req.Response = make(chan *InterventionResponse, 1)
    req.CreatedAt = time.Now()

    hm.mu.Lock()
    hm.pending[req.ID] = req
    hm.mu.Unlock()

    // 通过 SSE 推送给前端
    hm.eventBus.Publish(Event{
        Type: "hitl_request",
        Data: req,
    })

    // 阻塞等待人类响应
    select {
    case resp := <-req.Response:
        hm.mu.Lock()
        delete(hm.pending, req.ID)
        hm.mu.Unlock()
        return resp, nil
    case <-ctx.Done():
        return nil, ctx.Err()
    }
}

// Respond 人类通过 API 提交响应
func (hm *HITLManager) Respond(reqID string, resp *InterventionResponse) error {
    hm.mu.RLock()
    req, ok := hm.pending[reqID]
    hm.mu.RUnlock()
    if !ok {
        return ErrRequestNotFound
    }
    req.Response <- resp
    return nil
}
```

### 5.10 Gateway / SSE 推送

**职责：** HTTP API、SSE 实时推送、Web UI 服务。

```go
// Server HTTP 服务器
type Server struct {
    router    *chi.Mux
    eventBus  *EventBus
    orch      *Orchestrator
    hitl      *HITLManager
}

func NewServer(orch *Orchestrator) *Server {
    s := &Server{
        router: chi.NewRouter(),
        orch:   orch,
    }

    // 中间件
    s.router.Use(middleware.Logger)
    s.router.Use(middleware.Recoverer)
    s.router.Use(cors.Handler(cors.Options{...}))

    // API 路由
    s.router.Route("/api/v1", func(r chi.Router) {
        // 任务
        r.Post("/tasks", s.CreateTask)
        r.Get("/tasks", s.ListTasks)
        r.Get("/tasks/{id}", s.GetTask)

        // 团队
        r.Post("/teams", s.CreateTeam)
        r.Get("/teams", s.ListTeams)

        // HITL 响应
        r.Post("/hitl/{id}/respond", s.RespondHITL)

        // 上帝模式：指令注入
        r.Post("/god/inject", s.InjectCommand)

        // 追踪
        r.Get("/traces/{traceId}", s.GetTrace)
        r.Get("/traces/{traceId}/spans", s.GetSpans)
    })

    // SSE 端点
    s.router.Get("/api/v1/events", s.SSEHandler)

    // 静态文件（Web UI）
    s.router.Handle("/", http.FileServer(http.Dir("./web/dist")))

    return s
}

// SSEHandler SSE 实时事件推送
func (s *Server) SSEHandler(w http.ResponseWriter, r *http.Request) {
    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "SSE not supported", http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")

    // 订阅事件
    ch := s.eventBus.Subscribe()
    defer s.eventBus.Unsubscribe(ch)

    for {
        select {
        case event := <-ch:
            data, _ := json.Marshal(event)
            fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
            flusher.Flush()
        case <-r.Context().Done():
            return
        }
    }
}
```

**EventBus 实现：**

```go
type EventBus struct {
    subscribers map[chan Event]struct{}
    mu          sync.RWMutex
}

type Event struct {
    Type      string      `json:"type"`      // span_start/span_end/hitl_request/task_update/...
    Timestamp time.Time   `json:"timestamp"`
    Data      interface{} `json:"data"`
}

func (eb *EventBus) Publish(event Event) {
    event.Timestamp = time.Now()
    eb.mu.RLock()
    defer eb.mu.RUnlock()
    for ch := range eb.subscribers {
        select {
        case ch <- event:
        default:
            // 慢消费者跳过（避免阻塞）
        }
    }
}

func (eb *EventBus) Subscribe() chan Event {
    ch := make(chan Event, 256)
    eb.mu.Lock()
    eb.subscribers[ch] = struct{}{}
    eb.mu.Unlock()
    return ch
}
```

***

## 6. 技术选型

### 6.1 核心依赖

| 领域             | 选型                                                        | 理由                                             |
| -------------- | --------------------------------------------------------- | ---------------------------------------------- |
| HTTP 框架        | `go-chi/chi/v5`                                           | 轻量，兼容 net/http，中间件生态好                          |
| SQLite 驱动      | `modernc.org/sqlite`                                      | 纯 Go 实现，无需 CGO，跨平台                             |
| 向量搜索           | `asg017/sqlite-vec` (CGO) 或 `viant/sqlite-vec` (纯 Go)     | PRD 明确要求 SQLite + sqlite-vec                   |
| MCP 集成         | `modelcontextprotocol/go-sdk`                             | 官方 SDK，Google 协维护，规范全覆盖                        |
| OpenAI 客户端     | `openai/openai-go`                                        | 官方 SDK，支持 function calling + structured output |
| Anthropic 客户端  | `anthropics/anthropic-sdk-go`                             | 官方 SDK                                         |
| Claude-Code 集成 | `bpowers/go-claudecode` 或 `severity1/claude-agent-sdk-go` | 社区 SDK，支持子进程通信和沙箱                              |
| Cron 调度        | `robfig/cron/v3`                                          | Go 标准 cron 库，支持秒级调度                            |
| SSE            | 内置实现 (基于 net/http Flusher)                                | 简单场景无需额外依赖；复杂场景可用 `r3labs/sse`                 |
| 日志             | `log/slog` (标准库)                                          | Go 1.21+ 内置结构化日志                               |
| 配置             | `koanf` 或 `viper`                                         | 支持 YAML/ENV/CLI，热重载                            |
| 测试             | `testify` + `gomock`                                      | 断言 + mock 标准组合                                 |
| Browser 自动化    | `go-rod/rod`                                              | 高性能 CDP 库，API 简洁                               |
| Embedding      | OpenAI `text-embedding-3-small` 或本地模型                     | 起步用云端 API，后续可切本地                               |
| UUID           | `google/uuid`                                             | 标准 UUID 生成                                     |

### 6.2 sqlite-vec 集成方案

**推荐方案：双轨策略**

- 开发/部署优先用 `viant/sqlite-vec`（纯 Go，零 CGO 依赖，跨平台简单）
- 性能敏感场景可切换到 `asg017/sqlite-vec`（C 扩展，更成熟）
- 通过接口抽象，两者可互换

```go
// VectorStore 向量存储抽象（后续可替换为 PGVector）
type VectorStore interface {
    Insert(ctx context.Context, id string, embedding []float32, metadata map[string]string) error
    Search(ctx context.Context, query []float32, limit int) ([]VectorResult, error)
    Delete(ctx context.Context, id string) error
}

// SQLiteVecStore 基于 sqlite-vec 的实现
type SQLiteVecStore struct { ... }

// PGVectorStore 未来的 PGVector 实现
type PGVectorStore struct { ... }
```

### 6.3 外部 Agent 集成方案

```
kagent
  │
  ├─► claude-code (子进程)
  │   方式: exec.Command("claude", "-p", prompt, "--output-format", "stream-json")
  │   SDK: go-claudecode 或 claude-agent-sdk-go
  │   特点: 带沙箱、权限回调、流式输出
  │
  ├─► opencode (子进程)
  │   方式: exec.Command("opencode", ...)
  │   特点: 需要根据 opencode CLI 接口适配
  │
  └─► 自定义外部 Agent (通过 MCP 或 HTTP)
      方式: MCP Client 或 REST API
```

***

## 7. 代码结构

```
kagent/
├── cmd/
│   ├── kagent/              # 主入口（包含 server + TUI 模式）
│   │   └── main.go
│   └── kagent-cli/          # 轻量 CLI 客户端
│       └── main.go
│
├── internal/
│   ├── core/                # 核心引擎
│   │   ├── agent.go         # Agent 接口与基础实现
│   │   ├── engine.go        # AgentEngine (ReAct Loop)
│   │   ├── orchestrator.go  # 编排器
│   │   ├── planner.go       # 任务规划器
│   │   └── state.go         # 状态机定义
│   │
│   ├── team/                # 团队管理
│   │   ├── manager.go       # TeamManager
│   │   ├── soul.go          # SOUL.md 解析与注册
│   │   └── pipeline.go      # 多 Agent 流水线
│   │
│   ├── memory/              # 记忆系统
│   │   ├── manager.go       # MemoryManager
│   │   ├── session.go       # 会话级记忆
│   │   ├── vector.go        # 向量存储抽象
│   │   └── sqlite_vec.go    # sqlite-vec 实现
│   │
│   ├── llm/                 # LLM 供应商
│   │   ├── provider.go      # LLMProvider 接口
│   │   ├── pool.go          # 多供应商池
│   │   ├── openai.go        # OpenAI/DeepSeek 适配
│   │   ├── claude.go        # Anthropic Claude 适配
│   │   └── embedding.go     # 嵌入模型
│   │
│   ├── tools/               # 工具系统
│   │   ├── registry.go      # ToolRegistry
│   │   ├── native/          # 内置工具
│   │   │   ├── file.go      # 文件操作
│   │   │   ├── search.go    # Web 搜索
│   │   │   ├── exec.go      # 代码执行（沙箱）
│   │   │   └── browser.go   # 浏览器控制
│   │   ├── mcp/             # MCP 集成
│   │   │   └── client.go
│   │   ├── skill/           # Skill 引擎
│   │   │   ├── parser.go    # SKILL.md 解析
│   │   │   └── executor.go  # 脚本执行
│   │   └── bridge/          # 外部 Agent 桥接
│   │       ├── bridge.go    # ExternalAgent 接口
│   │       ├── claude_code.go
│   │       └── opencode.go
│   │
│   ├── tracing/             # 追踪系统
│   │   ├── tracer.go        # Tracer
│   │   ├── span.go          # Span 定义
│   │   └── store.go         # 持久化
│   │
│   ├── hitl/                # 人机协作
│   │   ├── manager.go       # HITLManager
│   │   └── types.go         # 请求/响应类型
│   │
│   ├── cron/                # 智能定时任务
│   │   ├── scheduler.go     # AgenticCron
│   │   └── job.go           # AgenticJob
│   │
│   ├── event/               # 事件总线
│   │   └── bus.go           # EventBus (SSE 推送)
│   │
│   └── config/              # 配置管理
│       ├── config.go        # 配置结构体
│       └── loader.go        # 加载器
│
├── api/                     # HTTP API 层
│   ├── server.go            # Server
│   ├── handler_task.go      # 任务 API
│   ├── handler_team.go      # 团队 API
│   ├── handler_hitl.go      # HITL API
│   ├── handler_trace.go     # 追踪 API
│   ├── handler_sse.go       # SSE 端点
│   └── middleware.go        # 中间件
│
├── web/                     # Web UI (前端)
│   ├── src/
│   └── dist/
│
├── souls/                   # SOUL.md 角色定义文件
│   ├── architect.soul.md
│   ├── coder.soul.md
│   ├── reviewer.soul.md
│   ├── tracker.soul.md
│   └── ...
│
├── skills/                  # SKILL.md 技能定义文件
│   ├── web_search.skill.md
│   ├── deploy.skill.md
│   └── ...
│
├── config/                  # 配置文件
│   ├── config.yaml
│   └── config.example.yaml
│
├── data/                    # 数据目录（运行时生成）
│   └── kagent.db            # SQLite 数据库
│
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

***

## 8. 分阶段实施规划

### Phase 1: 基础引擎 (MVP) -- 预计 3-4 周

**目标：** 单 Agent 能完成基本 ReAct 循环，可通过 CLI 交互。

**交付：**

- 单 Agent ReAct Loop 引擎
- LLM Provider（OpenAI 兼容协议，覆盖 OpenAI + DeepSeek）
- 基础内存记忆（会话内消息列表 + 摘要压缩）
- 3 个内置工具：文件读写、Shell 执行、Web 搜索
- ToolRegistry 基础注册/执行
- CLI 入口（`kagent -p "your prompt"`）
- 基础配置加载（YAML）
- 单元测试覆盖核心引擎

**技术风险：**

- LLM function calling 的格式差异（OpenAI vs Claude）需要适配层
- 上下文窗口管理（摘要时机）

**依赖：**

- 无外部依赖，可独立启动

***

### Phase 2: 记忆与追踪 -- 预计 2-3 周

**目标：** Agent 具备跨会话记忆能力和全链路追踪。

**交付：**

- SQLite 持久化（消息、任务、追踪）
- sqlite-vec 向量语义记忆
- Embedding 集成（OpenAI text-embedding-3-small）
- 记忆检索流程：用户提问 -> 语义检索 -> 拼接上下文
- Tracing 系统（TraceID/SpanID）
- 基础日志（slog 结构化输出）

**技术风险：**

- sqlite-vec 纯 Go 版本性能需验证
- 嵌入模型延迟对 ReAct Loop 速度的影响

**依赖：**

- Phase 1 完成

***

### Phase 3: 团队协作 -- 预计 3-4 周

**目标：** 支持多 Agent 团队协作，包含内置跟踪和审核。

**交付：**

- Team Manager
- SOUL.md 角色定义与解析
- 多 Agent 并发执行（Go 协程）
- Sub-Agent 隔离会话（避免上下文膨胀）
- 内置 Tracker + Reviewer 角色
- 任务分解与分发
- 串行/并行 Pipeline

**技术风险：**

- Agent 间通信的消息格式需标准化
- 并发执行的错误传播和取消机制
- Sub-Agent 结果摘要的质量

**依赖：**

- Phase 2 完成（需要追踪和记忆支持团队因果分析）

***

### Phase 4: 外部集成 -- 预计 2-3 周

**目标：** 集成 MCP 协议和外部 Agent。

**交付：**

- MCP Client 集成（modelcontextprotocol/go-sdk）
- claude-code 外部 Agent 桥接
- opencode 外部 Agent 桥接
- SKILL.md 解析与执行引擎（Shell/Python）
- 工具沙箱（Docker 可选，资源限制）

**技术风险：**

- 外部 Agent 子进程的稳定性和超时处理
- MCP Server 兼容性（不同 MCP 实现的差异）
- 沙箱安全隔离的完整性

**依赖：**

- Phase 1 的 ToolRegistry

***

### Phase 5: Web UI 与 HITL -- 预计 3-4 周

**目标：** 完整的 Web 仪表盘，支持实时追踪和人机协作。

**交付：**

- HTTP API 服务器（chi）
- SSE 实时事件推送
- HITL Manager（人机协作干预）
- Web UI 仪表盘：
  - 任务列表与详情
  - 实时追踪 Timeline（Span 可视化）
  - HITL 干预界面
  - Chat 对话界面
  - "上帝模式"指令注入
- API 认证（简单 Token 或 OAuth）

**技术风险：**

- SSE 长连接的稳定性（代理/负载均衡器兼容性）
- Web UI 技术选型（建议 React/Vue + Vite）
- HITL 阻塞状态的超时和恢复机制

**依赖：**

- Phase 2（Tracing）
- Phase 3（Team 协作数据展示）

***

### Phase 6: 高级特性 -- 预计 2-3 周

**目标：** 智能定时任务、Browser Agent、多渠道接入。

**交付：**

- Agentic Cron（智能定时任务）
- Browser Agent（基于 rod）
- TUI 客户端（可选，基于 bubbletea）
- 多渠道 IM 适配器（Slack/微信，接口预留）

**技术风险：**

- Browser Agent 的稳定性和资源消耗
- IM 渠道的 API 限制

**依赖：**

- Phase 1-5 基本完成

***

### 里程碑总结

```
Week 1-4:   [Phase 1] 基础引擎 MVP ────────── 可用的单 Agent CLI
Week 5-7:   [Phase 2] 记忆与追踪 ──────────── Agent 有记忆，操作可追溯
Week 8-11:  [Phase 3] 团队协作 ────────────── 多 Agent 并发协作
Week 12-14: [Phase 4] 外部集成 ────────────── MCP + 外部 Agent
Week 15-18: [Phase 5] Web UI + HITL ────────── 完整可视化 + 人机协作
Week 19-21: [Phase 6] 高级特性 ────────────── 定时任务 + Browser
```

***

## 9. 待确认问题列表

### 高优先级（影响架构设计）

| #  | 问题                                                        | 影响范围                        | 建议默认值                            |
| -- | --------------------------------------------------------- | --------------------------- | -------------------------------- |
| P1 | SOUL.md 的具体格式规范是什么？是否使用 YAML front-matter + Markdown 正文？  | Team Manager, Soul Registry | 建议用 YAML front-matter + Markdown |
| P2 | SKILL.md 中 "flow" 类型的技能编排语法如何定义？是否支持条件分支和循环？              | Skill Engine                | 建议 MVP 阶段仅支持线性 steps             |
| P3 | "上帝模式"指令注入的优先级如何？是否能覆盖 Agent 的 SOUL 约束？                   | 安全模型, Gateway               | 建议注入指令优先级高于任务上下文但不覆盖安全约束         |
| P4 | 外部 Agent（claude-code）的 workspace 是否与 kagent 共享？安全隔离到什么程度？ | External Bridge, 安全         | 建议共享 workspace 但限制在项目目录内         |
| P5 | 向量嵌入使用云端 API（OpenAI）还是本地模型？对延迟和成本的要求？                     | Memory System               | 建议起步用云端 API，后续支持本地               |

### 中优先级（影响功能实现）

| #   | 问题                              | 影响范围          | 建议默认值                       |
| --- | ------------------------------- | ------------- | --------------------------- |
| P6  | 团队的 Agent 数量上限？是否需要动态扩缩？        | Team Manager  | 建议默认上限 10，可配置               |
| P7  | HITL 干预请求的超时时间？超时后自动终止还是继续等待？   | HITL Manager  | 建议默认 30 分钟，超时后自动终止任务        |
| P8  | 记忆的保留策略？是否需要自动清理过期记忆？           | Memory System | 建议按时间 + 重要度自动清理             |
| P9  | Web UI 的技术栈偏好？React/Vue/Svelte？ | Web UI        | 建议 React + shadcn/ui + Vite |
| P10 | 是否需要多用户/多租户支持？                  | 全局架构          | 建议 MVP 单用户，后续扩展             |

### 低优先级（可后续迭代）

| #   | 问题                                      | 影响范围           | 建议默认值          |
| --- | --------------------------------------- | -------------- | -------------- |
| P11 | Agentic Cron 的任务持久化？重启后是否恢复？            | Cron 系统        | 建议配置文件定义，启动时注册 |
| P12 | Browser Agent 是否需要支持无头模式和有头模式切换？        | Browser Agent  | 建议默认无头，调试时有头   |
| P13 | 追踪数据的保留时间和存储上限？                         | Tracing System | 建议默认保留 30 天    |
| P14 | IM 渠道的优先级排序？首先接入哪个平台？                   | 接入层            | 根据实际需求确定       |
| P15 | 是否需要 Agent 的热更新能力（运行时修改 SOUL.md 后无需重启）？ | Team Manager   | 建议支持，通过文件监控实现  |

***

## 附录 A: 关键技术参考

- **MCP Go 官方 SDK**: [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk)
- **sqlite-vec (C 扩展)**: [asg017/sqlite-vec](https://github.com/asg017/sqlite-vec) -- [Go 使用文档](https://alexgarcia.xyz/sqlite-vec/go.html)
- **sqlite-vec (纯 Go)**: [viant/sqlite-vec](https://github.com/viant/sqlite-vec)
- **Claude Code 程序化调用**: [code.claude.com/docs/en/headless](https://code.claude.com/docs/en/headless)
- **Claude Agent SDK Go**: [severity1/claude-agent-sdk-go](https://github.com/severity1/claude-agent-sdk-go)
- **go-claudecode (带沙箱)**: [bpowers/go-claudecode](https://github.com/bpowers/go-claudecode)
- **OpenAI Go SDK**: [openai/openai-go](https://github.com/openai/openai-go)
- **SSE (r3labs)**: [r3labs/sse](https://github.com/r3labs/sse)
- **go-sse (spec-compliant)**: [tmaxmax/go-sse](https://github.com/tmaxmax/go-sse)
- **Chi Router**: [go-chi/chi](https://github.com/go-chi/chi)
- **Rod (Browser)**: [go-rod/rod](https://github.com/go-rod/rod)
- **Robfig Cron**: [robfig/cron](https://github.com/robfig/cron)

## 附录 B: 参考项目

- [ComposioHQ/agent-orchestrator](https://github.com/ComposioHQ/agent-orchestrator) -- 多 Agent 编排参考
- [smallnest/goclaw](https://github.com/smallnest/goclaw) -- Go 多智能体 AI 网关
- [mosaxiv/clawlet](https://github.com/mosaxiv/clawlet) -- 本地语义记忆单机助手

