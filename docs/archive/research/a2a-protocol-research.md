# A2A 协议调研报告

> 调研日期：2026-03-31
> 数据来源：[a2aproject/A2A](https://github.com/a2aproject/A2A) 官方 repo · [a2a-protocol.org](https://a2a-protocol.org)
> 协议版本：v1.0.0（Linux Foundation 项目，Google 贡献，Apache 2.0）

---

## 1. 协议定位

A2A（Agent-to-Agent）是 **Agent ↔ Agent** 的通信协议，定义了 Agent 之间如何发现、委托任务、交换消息和传输产出物。

核心目标：让不同框架、不同厂商构建的 Agent 能够安全、标准地互操作。

与 AG-UI 和 MCP 的关系：

```
用户界面层       ← AG-UI → Agent ↔ UI 通信
Agent 编排层     ← A2A   → Agent ↔ Agent 通信
工具/资源层      ← MCP   → Agent ↔ Tool 通信
```

---

## 2. 核心概念

### 2.1 核心对象

| 对象 | 角色 | 关键特性 |
|------|------|---------|
| **Task** | 工作的基本单元 | 有状态、有生命周期、有唯一 ID |
| **Message** | 单轮通信 | 有 `role`（user/agent），含多个 Part |
| **Part** | 最小内容容器 | text / file(raw/url) / structured data |
| **Artifact** | Agent 正式交付物 | 由多个 Part 组成，可流式传输 |
| **AgentCard** | Agent "数字名片" | JSON 文档，描述能力、端点、鉴权 |
| **Context** | 逻辑会话分组 | `contextId` 关联多个相关 Task |

### 2.2 Task 状态机

| 状态 | 含义 |
|------|------|
| `TASK_STATE_WORKING` | 处理中 |
| `TASK_STATE_INPUT_REQUIRED` | 需要用户输入（中断态） |
| `TASK_STATE_AUTH_REQUIRED` | 需要授权（中断态） |
| `TASK_STATE_COMPLETED` | 成功完成（终止态） |
| `TASK_STATE_CANCELED` | 已取消（终止态） |
| `TASK_STATE_REJECTED` | Agent 拒绝（终止态） |
| `TASK_STATE_FAILED` | 执行失败（终止态） |

**不可变原则**：Task 进入终止态后不可重启。后续交互须在同一 `contextId` 下发起新 Task。

### 2.3 Message 结构

```json
{
  "role": "ROLE_USER",
  "messageId": "msg-001",
  "contextId": "ctx-abc",
  "referenceTaskIds": ["task-123"],
  "parts": [{ "text": "Generate an image of a sailboat." }],
  "metadata": {}
}
```

### 2.4 Part 结构

Part 使用 `oneof`，只能包含其中一种内容：

| 类型 | 说明 |
|------|------|
| `text` | 纯文本 |
| `raw` | 二进制内联数据 |
| `url` | 外部文件引用 URI |
| `data` | 结构化 JSON（`google.protobuf.Value`） |

附加字段：`mediaType`（MIME 类型）、`filename`、`metadata`。

### 2.5 Artifact 结构

```json
{
  "artifactId": "artifact-uuid",
  "name": "sailboat_image.png",
  "description": "A generated image...",
  "parts": [{ "url": "https://..." }],
  "metadata": {}
}
```

流式场景通过 `TaskArtifactUpdateEvent` 分块传输，`append` 和 `lastChunk` 字段控制分块组装。

---

## 3. 三层架构

```
Layer 1: Canonical Data Model (Proto)     ← 数据模型，spec/a2a.proto
           ↓
Layer 2: Abstract Operations (11 个)       ← 操作定义
           ↓
Layer 3: Protocol Bindings                 ← 传输绑定
   ├── JSON-RPC 2.0 over HTTP(S)          ← 主要绑定
   ├── gRPC                                ← 正式支持
   └── HTTP+JSON/REST                      ← 正式支持
```

**无 WebSocket**：A2A 规范选择 SSE 作为流式机制。

---

## 4. 消息格式（JSON-RPC 绑定）

### 4.1 请求结构

```json
{
  "jsonrpc": "2.0",
  "id": "req-001",
  "method": "SendMessage",
  "params": {
    "message": {
      "role": "ROLE_USER",
      "messageId": "msg-001",
      "parts": [{ "text": "Generate an image of a sailboat." }]
    }
  }
}
```

### 4.2 HTTP 头

```http
POST /rpc HTTP/1.1
Content-Type: application/json
Authorization: Bearer <token>
A2A-Version: 1.0
```

### 4.3 完整操作映射

| 操作 | JSON-RPC Method | REST |
|------|----------------|------|
| 发送消息 | `SendMessage` | `POST /message:send` |
| 流式消息 | `SendStreamingMessage` | `POST /message:stream` |
| 获取 Task | `GetTask` | `GET /tasks/{id}` |
| 列举 Tasks | `ListTasks` | `GET /tasks` |
| 取消 Task | `CancelTask` | `POST /tasks/{id}:cancel` |
| 订阅 Task | `SubscribeToTask` | `POST /tasks/{id}:subscribe` |
| 推送配置 | `CreateTaskPushNotificationConfig` | `POST /tasks/{id}/pushNotificationConfigs` |
| 获取 AgentCard | `GetExtendedAgentCard` | `GET /extendedAgentCard` |

### 4.4 错误码

| A2A 错误 | JSON-RPC Code | HTTP Status |
|---------|--------------|-------------|
| TaskNotFoundError | `-32001` | 404 |
| TaskNotCancelableError | `-32002` | 409 |
| PushNotificationNotSupportedError | `-32003` | 400 |
| UnsupportedOperationError | `-32004` | 400 |
| ContentTypeNotSupportedError | `-32005` | 415 |
| VersionNotSupportedError | `-32009` | 400 |

---

## 5. 流式推送模型

### 5.1 SSE 流式传输（实时场景）

```
Client                          A2A Server
  │── SendStreamingMessage ────────▶│
  │◀── HTTP 200 + text/event-stream─│
  │◀── data: {Task}               ──│  初始状态
  │◀── data: {TaskStatusUpdate}   ──│  状态变更
  │◀── data: {TaskArtifactUpdate} ──│  产出物分块
  │◀── data: {TaskStatusUpdate    ──│  终止
  │          state: COMPLETED}      │
  │                          [stream closes]
```

**流终止条件**：Task 进入任意终止态。

### 5.2 推送通知（长时任务/断连场景）

```json
{
  "message": { "..." },
  "configuration": {
    "pushNotificationConfig": {
      "url": "https://client.example.com/webhook",
      "token": "validation-token",
      "authentication": { "schemes": ["Bearer"] }
    }
  }
}
```

适合分钟/小时/天级任务或 Serverless 场景。

### 5.3 轮询（简单场景）

`SendMessage → taskId → 定期 GetTask(taskId) → 直到终止态`

---

## 6. AgentCard 与能力发现

### 6.1 AgentCard 结构（核心字段）

```json
{
  "name": "Travel Booking Agent",
  "description": "Books flights, hotels, and activities.",
  "provider": { "organization": "Example Corp" },
  "version": "1.2.0",
  "supportedInterfaces": [{
    "url": "https://agent.example.com/a2a/v1",
    "protocolBinding": "HTTP+JSON",
    "protocolVersion": "1.0"
  }],
  "capabilities": {
    "streaming": true,
    "pushNotifications": true
  },
  "defaultInputModes": ["text/plain", "image/png"],
  "defaultOutputModes": ["text/plain", "application/json"],
  "skills": [{
    "id": "flight-booking",
    "name": "Flight Booking",
    "description": "Searches and books flights."
  }],
  "securitySchemes": { "bearerAuth": { "type": "http", "scheme": "bearer" } }
}
```

### 6.2 三种发现策略

| 策略 | 机制 | 适用场景 |
|------|------|---------|
| **Well-Known URI** | `GET /.well-known/agent-card.json` | 公开 Agent |
| **Curated Registry** | 中心化注册表 API | 企业环境 |
| **Direct Config** | 硬编码/配置文件 | 私有 Agent |

---

## 7. Session/Conversation 概念

### 7.1 contextId = 逻辑会话

```
contextId "ctx-abc"
    ├── Task "task-001"  (Book flight)
    ├── Task "task-002"  (Book hotel, depends on task-001)
    └── Task "task-003"  (Book activities, parallel)
```

- **服务端生成**：首次交互时由 Agent 生成
- **客户端延续**：后续请求带相同 `contextId`
- **并行支持**：同一 `contextId` 下可有多个并发 Task

### 7.2 referenceTaskIds

Message 中可引用之前的 `taskId`，向 Agent 暗示当前请求与历史任务的关系。

### 7.3 Task 历史

`GetTask` 支持 `historyLength` 参数，获取 Task 内的 Message 历史。

---

## 8. 与 AG-UI 的关系

| 维度 | A2A | AG-UI |
|------|-----|-------|
| **定位** | Agent ↔ Agent | Agent ↔ 前端 UI |
| **消息格式** | JSON-RPC 2.0 | 事件驱动（33 种标准事件） |
| **传输机制** | HTTP(S) + SSE / gRPC | 传输无关（SSE / WS / Webhook） |
| **核心抽象** | Task + Artifact（有状态工作单元） | Event Stream（实时事件流） |
| **发现机制** | AgentCard + Well-Known URI | 无标准发现 |
| **状态管理** | Task 生命周期 + contextId | 共享状态同步 |
| **典型场景** | Agent 委托任务给另一 Agent | 流式 Chat、Generative UI |
| **企业特性** | 鉴权、可观测性、安全规范 | 轻量、快速集成 |

**两者可叠加使用**：

```
User (Browser)
    ↕ AG-UI（处理实时 UI 交互）
Frontend Agent / Orchestrator
    ↕ A2A（Agent 间委托任务）
Specialized Remote Agent
    ↕ MCP（调用工具/数据库/API）
```

---

## 9. 与 xagent-gateway 的适配评估

### 9.1 南向（Gateway → Agent L1）适配度

| A2A 特性 | gateway 需求 | 适配度 |
|---|---|---|
| Task 模型（有状态、有生命周期） | session.start / send 对应 Task 创建 | ✅ 高 |
| contextId | 对应 gateway 的 session_id | ✅ 高 |
| Task 状态机 | 对应会话状态机 | ✅ 高 |
| `TASK_STATE_INPUT_REQUIRED` | 对应 `WAITING_USER_INPUT` | ✅ 高 |
| SSE 流式推送 | Gateway 消费 Agent L1 的事件流 | ✅ 高 |
| AgentCard | 可用于 Agent 路由和能力发现 | ✅ 高 |
| JSON-RPC 格式 | 需在 Gateway 做格式适配 | ⚠️ 需适配层 |
| 无 WebSocket | Gateway 南向不需要 WS | ✅ 无影响 |

### 9.2 概念映射

| A2A 概念 | Gateway 概念 | 说明 |
|---|---|---|
| `contextId` | `session_id` | 逻辑会话 ID |
| `Task` | `run` | 一次执行（但 A2A Task 更重量级） |
| `taskId` | `root_run_id` | 执行实例 ID |
| `Message` | `conversation.send` 的 payload | 用户消息 |
| `Artifact` | Projection 事件中的最终产出 | 可交付结果 |
| `TASK_STATE_INPUT_REQUIRED` | `WAITING_USER_INPUT` | 需要用户补充信息 |
| `TASK_STATE_CANCELED` | `ABORTED` | 已取消 |
| `AgentCard` | AgentRegistry 中的 Agent 元数据 | 能力发现 |

### 9.3 结论

**A2A 适合作为 Gateway 南向调用 Agent L1 的通信信封标准。** Gateway 的 A2A 信封（SPEC §5.3.5）已经复用了 A2A 的核心字段（`request_id`, `trace_id`, `session_id`, `from_agent`, `to_agent` 等），v1 可进一步对齐 A2A v1.0 的 JSON-RPC 格式和 Task 状态模型。
