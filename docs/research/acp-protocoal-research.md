# ACP - Agent Client Protocol 整理调研

ACP（Agent Client Protocol）标准化了代码编辑器/IDE 与 AI 编码 Agent 之间的通信，类似于 LSP（语言服务器协议）对语言服务的标准化方式。

Codex、ClaudeCode、OpenCode 等 Agent 工具通过 ACP 暴露自身能力，底层基于 **JSON-RPC 2.0** + **stdio** 通信。

> 协议官方仓库 https://github.com/agentclientprotocol/agent-client-protocol 已 Clone 到当前项目的 `./tmp/agent-client-protocol` 目录
>
> 官方文档：https://agentclientprotocol.com/

## 目录

- [设计理念](#设计理念)
- [传输层](#传输层)
- [完整通信流程](#完整通信流程)
- [核心概念](#核心概念)
  - [初始化与能力协商](#1-初始化与能力协商)
  - [会话管理](#2-会话管理)
  - [Prompt Turn（提示轮次）](#3-prompt-turn提示轮次)
  - [Tool Calls（工具调用）](#4-tool-calls工具调用)
  - [文件系统访问](#5-文件系统访问)
  - [终端命令执行](#6-终端命令执行)
  - [会话模式与配置选项](#7-会话模式与配置选项)
  - [Agent Plan（执行计划）](#8-agent-plan执行计划)
  - [Slash Commands（斜杠命令）](#9-slash-commands斜杠命令)
  - [内容类型](#10-内容类型)
  - [扩展机制](#11-扩展机制)
- [消息格式速查](#消息格式速查)
- [官方 SDK 实现](#官方-sdk-实现)
- [GO SDK 参考](#go-sdk-参考)

---

## 设计理念

1. **MCP 友好**：协议基于 JSON-RPC，并尽可能复用 MCP 的数据类型，降低集成成本
2. **UX 优先**：协议专门解决与 AI Agent 交互的 UX 挑战，为渲染 Agent 意图（diff、plan、tool call 状态等）提供足够多的结构化信息
3. **可信环境**：ACP 以信任代码编辑器为前提，用户仍可控制 Agent 的工具调用权限，编辑器向 Agent 提供文件系统和 MCP 服务器访问权

---

## 传输层

ACP 目前支持以下传输机制：

### stdio（主要传输方式）

- Client 将 Agent 作为子进程启动
- Agent 从 `stdin` 读取 JSON-RPC 消息，通过 `stdout` 发送消息
- 消息以换行符 `\n` 分隔，消息内部**不能**包含换行符
- Agent 可将日志写入 `stderr`，Client 可捕获、转发或忽略
- Agent `stdout` 只能输出合法的 ACP 消息

```
Client ──stdin──▶ Agent Process
Client ◀──stdout── Agent Process
Client ◀──stderr── Agent Process (optional logs)
```

### Streamable HTTP（草案，开发中）

支持云端或远程 Agent，通过 HTTP/WebSocket 通信。

---

## 完整通信流程

```
Client                          Agent
  │                               │
  │──── initialize ───────────────▶│  协商协议版本 & 能力
  │◀─── initialize response ───────│
  │                               │
  │──── authenticate (可选) ───────▶│  身份认证
  │◀─── ok ────────────────────────│
  │                               │
  │──── session/new ──────────────▶│  创建会话
  │◀─── { sessionId } ─────────────│
  │                               │
  │──── session/prompt ───────────▶│  发送用户消息
  │◀─── session/update (流式) ──────│  Agent 流式返回更新
  │◀─── session/request_permission─│  请求工具权限（可选）
  │◀─── fs/read_text_file ─────────│  读取客户端文件（可选）
  │──── { content } ──────────────▶│
  │◀─── fs/write_text_file ────────│  写入客户端文件（可选）
  │◀─── session/prompt response ───│  Turn 结束，返回 stopReason
  │                               │
  │──── session/cancel (可选) ─────▶│  中断当前 Turn
```

---

## 核心概念

### 1. 初始化与能力协商

连接建立后，Client **必须**先调用 `initialize` 方法协商协议版本和能力。

**Client → Agent 请求：**
```json
{
  "jsonrpc": "2.0",
  "id": 0,
  "method": "initialize",
  "params": {
    "protocolVersion": 1,
    "clientCapabilities": {
      "fs": {
        "readTextFile": true,
        "writeTextFile": true
      },
      "terminal": true
    },
    "clientInfo": {
      "name": "my-client",
      "version": "1.0.0"
    }
  }
}
```

**Agent → Client 响应：**
```json
{
  "jsonrpc": "2.0",
  "id": 0,
  "result": {
    "protocolVersion": 1,
    "agentCapabilities": {
      "loadSession": true,
      "promptCapabilities": {
        "image": true,
        "audio": true,
        "embeddedContext": true
      },
      "mcpCapabilities": {
        "http": true,
        "sse": true
      }
    },
    "agentInfo": {
      "name": "my-agent",
      "version": "1.0.0"
    },
    "authMethods": []
  }
}
```

**协议版本协商规则：**
- `protocolVersion` 为单个整数，仅在引入破坏性变更时递增
- Agent 支持请求版本则返回相同版本，否则返回 Agent 支持的最新版本
- Client 若不支持 Agent 返回的版本，应关闭连接

**Client 能力（`clientCapabilities`）：**
| 能力 | 说明 |
|------|------|
| `fs.readTextFile` | 支持 `fs/read_text_file` 文件读取 |
| `fs.writeTextFile` | 支持 `fs/write_text_file` 文件写入 |
| `terminal` | 支持所有 `terminal/*` 终端命令 |

**Agent 能力（`agentCapabilities`）：**
| 能力 | 说明 |
|------|------|
| `loadSession` | 支持 `session/load` 恢复历史会话 |
| `promptCapabilities.image` | 支持图片内容输入 |
| `promptCapabilities.audio` | 支持音频内容输入 |
| `promptCapabilities.embeddedContext` | 支持嵌入上下文资源 |
| `mcpCapabilities.http` | 支持 HTTP MCP 服务 |
| `mcpCapabilities.sse` | 支持 SSE MCP 服务 |

---

### 2. 会话管理

会话（Session）代表 Client 与 Agent 之间的一次独立对话线程，维护独立的上下文、历史记录和状态。

#### 创建新会话

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "session/new",
  "params": {
    "cwd": "/home/user/project",
    "mcpServers": [
      {
        "name": "filesystem",
        "command": "/path/to/mcp-server",
        "args": ["--stdio"],
        "env": []
      }
    ]
  }
}
```

响应返回唯一的 `sessionId`：
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": { "sessionId": "sess_abc123def456" }
}
```

#### 加载历史会话（需 `loadSession` 能力）

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "session/load",
  "params": {
    "sessionId": "sess_789xyz",
    "cwd": "/home/user/project",
    "mcpServers": []
  }
}
```

Agent 会通过 `session/update` 通知重播整个历史对话内容。

---

### 3. Prompt Turn（提示轮次）

一个 Prompt Turn 代表从用户发送消息到 Agent 完成回复的完整交互周期，期间可能涉及多次 LLM 调用和工具执行。

#### 发送用户消息

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "session/prompt",
  "params": {
    "sessionId": "sess_abc123def456",
    "prompt": [
      {
        "type": "text",
        "text": "请分析这段代码是否存在潜在问题"
      },
      {
        "type": "resource",
        "resource": {
          "uri": "file:///home/user/project/main.go",
          "mimeType": "text/x-go",
          "text": "func process(items []string) {..."
        }
      }
    ]
  }
}
```

#### Agent 通过 `session/update` 流式推送更新

`session/update` 是核心通知机制，`sessionUpdate` 字段区分不同更新类型：

| 类型 | 说明 |
|------|------|
| `agent_message_chunk` | Agent 文本消息片段（流式） |
| `user_message_chunk` | 用户消息片段（session/load 重播时） |
| `agent_thought_chunk` | Agent 内部推理思考片段 |
| `tool_call` | 工具调用创建 |
| `tool_call_update` | 工具调用状态更新 |
| `plan` | Agent 执行计划 |
| `available_commands_update` | 可用斜杠命令更新 |

#### Turn 结束响应

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "stopReason": "end_turn"
  }
}
```

`stopReason` 可为：`end_turn`、`cancelled`、`error` 等。

#### 取消当前 Turn

```json
{
  "jsonrpc": "2.0",
  "method": "session/cancel",
  "params": { "sessionId": "sess_abc123def456" }
}
```

---

### 4. Tool Calls（工具调用）

LLM 请求执行工具时，Agent 通过 `session/update` 通知 Client 工具调用的全生命周期。

#### 工具调用类型（kind）

| kind | 说明 |
|------|------|
| `read` | 读取文件或数据 |
| `edit` | 修改文件或内容 |
| `delete` | 删除文件或数据 |
| `move` | 移动或重命名文件 |
| `search` | 搜索信息 |
| `execute` | 执行命令或代码 |
| `think` | 内部推理规划 |
| `fetch` | 获取外部数据 |
| `other` | 其他类型（默认） |

#### 工具调用状态流转

```
pending → in_progress → completed / failed
```

#### 请求用户权限

Agent 执行高危操作前可请求用户授权：

```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "method": "session/request_permission",
  "params": {
    "sessionId": "sess_abc123def456",
    "toolCall": {
      "toolCallId": "call_001",
      "title": "删除文件 config.json",
      "kind": "delete"
    }
  }
}
```

---

### 5. 文件系统访问

Agent 可通过 Client 的文件系统能力读写文件，包括访问编辑器中未保存的内容。

#### 读取文件（`fs/read_text_file`）

由 **Agent** 调用，Client 响应：
```json
// 请求
{ "method": "fs/read_text_file", "params": { "sessionId": "...", "path": "/abs/path/to/file.go", "line": 10, "limit": 50 } }
// 响应
{ "result": { "content": "func hello() {...}" } }
```

#### 写入文件（`fs/write_text_file`）

由 **Agent** 调用，Client 执行写入，文件不存在时自动创建。

---

### 6. 终端命令执行

Agent 可在 Client 环境中执行 Shell 命令，并获取实时输出流。

#### 创建终端（`terminal/create`）

```json
{
  "method": "terminal/create",
  "params": {
    "sessionId": "sess_abc123def456",
    "command": "go",
    "args": ["test", "./..."],
    "cwd": "/home/user/project",
    "env": [{ "name": "GO_ENV", "value": "test" }],
    "outputByteLimit": 1048576
  }
}
```

Client 立即返回 `terminalId`，不等待命令完成。

---

### 7. 会话模式与配置选项

#### Session Config Options（推荐方式）

Agent 在 `session/new` 响应中返回可配置选项列表，Client 展示给用户选择：

```json
{
  "result": {
    "sessionId": "sess_abc123def456",
    "configOptions": [
      {
        "id": "mode",
        "name": "会话模式",
        "category": "mode",
        "type": "select",
        "currentValue": "ask",
        "options": [
          { "value": "ask", "name": "询问", "description": "修改前请求权限" },
          { "value": "code", "name": "代码", "description": "完整工具访问权" }
        ]
      },
      {
        "id": "model",
        "name": "模型",
        "category": "model",
        "type": "select",
        "currentValue": "model-1",
        "options": [
          { "value": "model-1", "name": "快速模型" },
          { "value": "model-2", "name": "强力模型" }
        ]
      }
    ]
  }
}
```

#### Session Modes（旧方式，将废弃）

通过 `session/set_mode` 方法切换，常见模式：`ask`、`architect`、`code`。

---

### 8. Agent Plan（执行计划）

Agent 可通过 `session/update` 推送结构化执行计划，让 Client 实时渲染任务进度：

```json
{
  "update": {
    "sessionUpdate": "plan",
    "entries": [
      { "content": "分析现有代码结构", "priority": "high", "status": "completed" },
      { "content": "识别需要重构的组件", "priority": "high", "status": "in_progress" },
      { "content": "编写单元测试", "priority": "medium", "status": "pending" }
    ]
  }
}
```

每次更新必须包含**全量** plan entries，Client 直接替换整个计划。

---

### 9. Slash Commands（斜杠命令）

Agent 可通过 `available_commands_update` 通知客户端其支持的斜杠命令：

```json
{
  "update": {
    "sessionUpdate": "available_commands_update",
    "availableCommands": [
      { "name": "web", "description": "搜索网络信息", "input": { "hint": "搜索关键词" } },
      { "name": "test", "description": "运行项目测试" },
      { "name": "plan", "description": "生成详细实施计划", "input": { "hint": "任务描述" } }
    ]
  }
}
```

斜杠命令通过普通 `session/prompt` 请求发送，命令文本包含在 prompt 中。

---

### 10. 内容类型

ACP 复用 MCP 的 `ContentBlock` 结构，支持以下类型：

| 类型 | 说明 | 能力要求 |
|------|------|---------|
| `text` | 纯文本 | 所有 Agent 必须支持 |
| `image` | Base64 图片（png/jpeg 等） | `promptCapabilities.image` |
| `audio` | Base64 音频（wav/mp3 等） | `promptCapabilities.audio` |
| `resource` | 嵌入资源（文件内容、URI 引用） | `promptCapabilities.embeddedContext` |

---

### 11. 扩展机制

#### `_meta` 字段

所有协议类型均支持 `_meta: { [key: string]: unknown }` 字段携带自定义元数据，支持 W3C 分布式追踪：

```json
{
  "method": "session/prompt",
  "params": {
    "_meta": {
      "traceparent": "00-80e1afed08e019fc1110464cfa66635c-7a085853722dc6d2-01"
    }
  }
}
```

#### 扩展方法

以下划线 `_` 开头的方法名保留给自定义扩展，不与协议版本冲突：

```json
{ "method": "_zed.dev/workspace/buffers", "params": { "language": "go" } }
```

---

## 消息格式速查

| 方法 | 方向 | 类型 | 说明 |
|------|------|------|------|
| `initialize` | C→A | Request | 初始化，协商版本与能力 |
| `authenticate` | C→A | Request | 身份认证（可选） |
| `session/new` | C→A | Request | 创建新会话 |
| `session/load` | C→A | Request | 恢复历史会话 |
| `session/prompt` | C→A | Request | 发送用户消息 |
| `session/cancel` | C→A | Notification | 取消当前 Turn |
| `session/set_mode` | C→A | Request | 切换会话模式（旧） |
| `session/update` | A→C | Notification | Agent 流式推送更新 |
| `session/request_permission` | A→C | Request | 请求工具执行权限 |
| `fs/read_text_file` | A→C | Request | 读取客户端文件 |
| `fs/write_text_file` | A→C | Request | 写入客户端文件 |
| `terminal/create` | A→C | Request | 在客户端创建终端进程 |

---

## 官方 SDK 实现

| 语言 | 仓库 | 说明 |
|------|------|------|
| TypeScript | [`@agentclientprotocol/sdk`](https://www.npmjs.com/package/@agentclientprotocol/sdk) | 官方 |
| Python | [`python-sdk`](https://github.com/agentclientprotocol/python-sdk) | 官方 |
| Rust | [`agent-client-protocol`](https://crates.io/crates/agent-client-protocol) | 官方 |
| Kotlin/JVM | [`acp-kotlin`](https://github.com/agentclientprotocol/kotlin-sdk) | 官方 |
| Java | [`java-sdk`](https://github.com/agentclientprotocol/java-sdk) | 官方 |

---

## GO SDK 参考

官方暂无 Go SDK，以下为社区实现参考：

- https://github.com/ironpark/acp-go
- https://github.com/coder/acp-go-sdk

