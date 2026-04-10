# AG-UI 协议调研报告

> 调研日期：2026-03-31
> 数据来源：[ag-ui-protocol/ag-ui](https://github.com/ag-ui-protocol/ag-ui) 官方 monorepo，main 分支
> 协议版本：无显式版本号；以 npm/PyPI 发布版为准

---

## 1. 协议定位

AG-UI（Agent-User Interaction）是 **Agent ↔ 前端 UI** 的实时事件流通信协议，由 CopilotKit 社区发起。

核心目标：标准化 Agent 向前端 UI 推送实时事件的方式，覆盖流式文本生成、状态同步、工具调用、人机协同等场景。

一句话定义：**AG-UI 管"怎么通信"（事件流协议），A2UI 管"渲染什么 UI"（声明式 UI 描述），两者互补而非竞争。**

---

## 2. 完整事件类型列表（33 种）

### 2.1 生命周期事件（Lifecycle Events，5 种）

| 事件类型 | 关键字段 | 用途 |
|---|---|---|
| `RUN_STARTED` | `threadId`, `runId`, `parentRunId?` | 标记一次 agent run 的开始 |
| `RUN_FINISHED` | `threadId`, `runId`, `result?` | 标记 run 成功结束 |
| `RUN_ERROR` | `message`, `code?` | 标记 run 因不可恢复错误终止 |
| `STEP_STARTED` | `stepName` | run 内子步骤开始（可选） |
| `STEP_FINISHED` | `stepName` | 子步骤完成 |

### 2.2 文本消息事件（Text Message Events，4 种）

| 事件类型 | 关键字段 | 用途 |
|---|---|---|
| `TEXT_MESSAGE_START` | `messageId`, `role` | 开始一条流式文本消息 |
| `TEXT_MESSAGE_CONTENT` | `messageId`, `delta` | 文本增量块（chunk） |
| `TEXT_MESSAGE_END` | `messageId` | 消息流结束 |
| `TEXT_MESSAGE_CHUNK` | `messageId?`, `role?`, `delta?` | 便捷事件：自动展开为 Start→Content→End |

### 2.3 工具调用事件（Tool Call Events，5 种）

| 事件类型 | 关键字段 | 用途 |
|---|---|---|
| `TOOL_CALL_START` | `toolCallId`, `toolCallName`, `parentMessageId?` | 开始工具调用 |
| `TOOL_CALL_ARGS` | `toolCallId`, `delta` | 流式传输工具参数 |
| `TOOL_CALL_END` | `toolCallId` | 工具调用参数流结束 |
| `TOOL_CALL_RESULT` | `messageId`, `toolCallId`, `content` | 工具执行结果（完整） |
| `TOOL_CALL_CHUNK` | `toolCallId?`, `toolCallName?`, `delta?` | 便捷事件 |

### 2.4 状态管理事件（State Management Events，3 种）

| 事件类型 | 关键字段 | 用途 |
|---|---|---|
| `STATE_SNAPSHOT` | `snapshot: any` | 全量状态替换（初始化/重连） |
| `STATE_DELTA` | `delta: JsonPatchOperation[]` | 增量更新（RFC 6902 JSON Patch） |
| `MESSAGES_SNAPSHOT` | `messages: Message[]` | 对话历史完整快照 |

### 2.5 活动事件（Activity Events，2 种）

| 事件类型 | 关键字段 | 用途 |
|---|---|---|
| `ACTIVITY_SNAPSHOT` | `messageId`, `activityType`, `content` | 活动消息快照（进度面板等） |
| `ACTIVITY_DELTA` | `messageId`, `activityType`, `patch` | 活动消息增量更新 |

活动消息是**纯前端对象**，不转发给模型，用于渲染 checklist、进度指示器等自定义 UI。

### 2.6 特殊事件（Special Events，2 种）

| 事件类型 | 关键字段 | 用途 |
|---|---|---|
| `RAW` | `event`, `source?` | 透传外部系统事件（保留原始数据） |
| `CUSTOM` | `name`, `value` | 应用自定义扩展事件（A2UI 载荷走此路径） |

### 2.7 推理事件（Reasoning Events，7 种）

| 事件类型 | 关键字段 | 用途 |
|---|---|---|
| `REASONING_START` | `messageId` | 推理阶段开始 |
| `REASONING_MESSAGE_START` | `messageId`, `role` | 开始流式推理消息 |
| `REASONING_MESSAGE_CONTENT` | `messageId`, `delta` | 推理内容块 |
| `REASONING_MESSAGE_END` | `messageId` | 推理消息流结束 |
| `REASONING_MESSAGE_CHUNK` | `messageId`, `delta` | 便捷事件 |
| `REASONING_END` | — | 推理阶段结束 |
| `REASONING_ENCRYPTED_VALUE` | — | 加密推理值（合规） |

### 2.8 废弃事件（向后兼容，5 种）

`THINKING_START`, `THINKING_END`, `THINKING_TEXT_MESSAGE_START`, `THINKING_TEXT_MESSAGE_CONTENT`, `THINKING_TEXT_MESSAGE_END`

已标注 deprecated，应迁移到 `REASONING_*`。

---

## 3. 传输层设计

**核心结论：AG-UI 不绑定任何传输协议，是传输无关（transport-agnostic）设计。**

### 3.1 内置支持的传输

| 传输 | 说明 |
|---|---|
| **HTTP SSE** | `HttpAgent` 默认实现，兼容性最广 |
| **HTTP Binary Protocol** | 自定义二进制序列化，高性能，生产推荐 |

### 3.2 能力声明中的传输扩展

```typescript
interface TransportCapabilities {
  streaming?: boolean        // SSE 流式响应
  websocket?: boolean        // WebSocket 持久连接
  httpBinary?: boolean       // AG-UI 二进制协议
  pushNotifications?: boolean // webhook 异步推送
  resumable?: boolean        // sequence number 断线续传
}
```

**WebSocket 完全支持**，作为可选传输之一。客户端通过 `getCapabilities()` 查询后选择最优传输。

### 3.3 协议层抽象

任何传输均可接入，只需实现：

```typescript
type RunAgent = () => Observable<BaseEvent>
run(input: RunAgentInput): RunAgent
```

---

## 4. Client → Agent 方向

AG-UI 协议**不强制定义 client→agent 请求格式**，这是有意设计。Client 发起 run 时通过 `RunAgentInput` 传递：

```typescript
interface RunAgentInput {
  threadId: string         // 会话线程 ID
  runId: string            // 本次 run 的 ID
  parentRunId?: string     // 分支/时间旅行
  state: any               // 当前共享状态
  messages: Message[]      // 完整对话历史
  tools: Tool[]            // 前端定义的工具列表
  context: Context[]       // 附加上下文
  forwardedProps: any      // 透传自定义属性
}
```

**关键点**：client→agent 方向无持续推送机制。每次 run 是一次新的请求 + 流式响应。工具结果、用户输入打包在下一次 `RunAgentInput.messages` 中。

---

## 5. 认证、多端、断线恢复

### 5.1 认证

AG-UI 协议层**不定义认证机制**，完全委托传输层：
- `HttpAgent` 通过标准 HTTP headers 传递 auth token
- `RunAgentInput.forwardedProps` 可携带自定义认证上下文
- 中间件层是推荐的认证扩展点

### 5.2 多端

协议未定义多端并发订阅同一 run 的机制：
- 一个 `threadId` 可跨多个 run（时序串行）
- 多个客户端可各自发起独立 run，通过 snapshot 事件同步状态
- `parentRunId` 支持多路分支，但不是多端实时共享

### 5.3 断线恢复

提供两个层次：

**层次一：基于事件序列化重放（通用）**
- 事件流可完整序列化为 JSON 存储
- 重连后通过 `STATE_SNAPSHOT` + `MESSAGES_SNAPSHOT` 快速恢复
- `compactEvents()` 工具函数将冗余流压缩为最小快照集

**层次二：基于 sequence number 的续传（可选）**
- `TransportCapabilities.resumable = true`
- 客户端标记已接收位置，断线后从该点续传
- 实现细节由各 agent 自行决定

---

## 6. SDK 现状

### 6.1 官方 SDK

| 语言 | 包 | 状态 |
|---|---|---|
| TypeScript | `@ag-ui/core`, `@ag-ui/client` | 生产就绪 |
| Python | `ag_ui` | 生产就绪 |

### 6.2 社区 SDK

| 语言 | 状态 | 说明 |
|---|---|---|
| **Go** | 实现完整 | `sdks/community/go`，覆盖全部事件类型 |
| Kotlin | 社区维护 | |
| Java | 社区维护 | |
| Rust | 社区维护 | |
| Ruby | 社区维护 | |
| Dart | 社区维护 | 兼容 Flutter |

### 6.3 Go SDK 依赖

极简：仅 `google/uuid`, `sirupsen/logrus`, `stretchr/testify`。

---

## 7. 概念模型总结

```
Thread（线程 ≈ 会话）
  └── Run（运行 ≈ 一次 agent 执行）[runId, parentRunId]
        ├── Step（步骤，可选）[stepName]
        │     ├── TextMessage 流（START → CONTENT × N → END）
        │     ├── ToolCall 流（START → ARGS × N → END → RESULT）
        │     └── Reasoning 流（START → MESSAGE × N → END）
        ├── State（共享状态：SNAPSHOT / DELTA）
        └── Activity（前端活动：SNAPSHOT / DELTA）
```

**AG-UI 的 `threadId` 对应 gateway 的 `session_id`，`runId` 对应 gateway 的 `root_run_id`。**

---

## 8. 与 xagent-gateway 的适配评估

| AG-UI 特性 | gateway 需求 | 适配度 |
|---|---|---|
| 事件流模型（33 种事件） | 覆盖消息流、工具、推理、状态同步 | ✅ 高 |
| `STATE_SNAPSHOT` / `MESSAGES_SNAPSHOT` | 对应 `session.snapshot` | ✅ 高 |
| `CUSTOM` 扩展事件 | 可承载 gateway 特有事件（writer、question 等） | ✅ 高 |
| 传输无关 | gateway 用 WebSocket，AG-UI 支持 | ✅ 高 |
| 无认证/多端/复杂恢复 | gateway 自行实现 | ⚠️ 需扩展 |
| 无 client→agent 持续推送 | gateway 的 cmd 帧是双向的 | ⚠️ 需扩展 |

**结论：AG-UI 的事件模型（下行方向）可直接采用，但 gateway 的双向命令帧（上行方向）和多端/认证/恢复等企业级特性需在 AG-UI 基础上扩展。**
