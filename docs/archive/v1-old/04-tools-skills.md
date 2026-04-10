# Tools & Skills 设计

> 状态：Legacy reference
> 说明：本文档基于旧 `v1 foundation` 假设撰写，当前仅作为历史参考。新的 `v0` 主线请先看 `docs/README.md`。

> 所属层级：执行层 | 相关文档：[架构总纲](./00-overview.md) | [Orchestrator 权限层](./02-orchestrator.md#4-权限与生命周期层)

---

## 1. Tool Registry

Tool Registry 是所有工具的统一注册与分发中心，负责：

- 工具注册、发现、Schema 导出（供 LLM Function Call）
- 按类型路由执行（Native / MCP / Skill / Browser / ExtAgent）
- 配合权限层过滤可用工具集

### 1.1 核心接口

```go
type Tool interface {
    Name() string
    Description() string
    Schema() ToolSchema
    Execute(ctx context.Context, args map[string]any) (string, error)
}

type ToolRegistry interface {
    Register(tool Tool) error
    Get(name string) (Tool, bool)
    ListSchemas(allowList []string) []ToolSchema  // 按白名单过滤 Schema
    Execute(ctx context.Context, call *ToolCall) (string, error)
}

type ToolCall struct {
    Name      string
    Arguments map[string]any
}

type ToolKind string
const (
    ToolNative   ToolKind = "native"
    ToolMCP      ToolKind = "mcp"
    ToolSkill    ToolKind = "skill"
    ToolBrowser  ToolKind = "browser"
    ToolExtAgent ToolKind = "ext_agent"
)
```

---

## 2. 工具类型一览

| 类型 | 注册方式 | 执行方式 | 适用场景 |
|------|----------|----------|----------|
| Native | 代码注册 | 直接函数调用 | 高频本地 IO、搜索 |
| MCP | 配置文件 | JSON-RPC over stdio/SSE | 接入第三方 MCP Server |
| Skill | SKILL.md 解析 | Shell/Python subprocess | 可描述的本地脚本任务 |
| Browser | 内置 | chromedp 控制 | 网页自动化、截图、抓取 |
| ExtAgent | 配置 | subprocess + 结果解析 | 调用 claude-code、opencode |

---

## 3. Native Tools

直接用 Go 实现，注册到 Registry，无进程开销：

**内置工具清单**：

| 工具名 | 功能 |
|--------|------|
| `file_read` | 读取文件内容（限 workspace 目录） |
| `file_write` | 写入文件（限 workspace 目录） |
| `file_list` | 列举目录文件 |
| `shell_exec` | 执行 shell 命令（高风险，需权限确认） |
| `web_search` | 网络搜索（SerpAPI 等） |
| `code_exec` | 代码执行（Docker 沙箱，强限资源） |
| `http_get` | HTTP GET 请求 |

所有文件操作工具强制限定在 Agent workspace 目录内，防止越权访问。

---

## 4. MCP Client

通过 MCP（Model Context Protocol）接入第三方工具生态：

```go
type MCPClient interface {
    Connect(server MCPServerConfig) error
    ListTools() ([]ToolSchema, error)
    CallTool(name string, args map[string]any) (string, error)
    Close() error
}

type MCPServerConfig struct {
    Name      string
    Transport string      // "stdio" | "sse"
    Command   string      // stdio 模式：可执行文件路径
    Args      []string
    URL       string      // SSE 模式：服务器 URL
}
```

MCP Server 在配置文件中声明，启动时自动连接并将工具注册到 Registry。

---

## 5. Skills Engine

Skills 是由 SKILL.md 文件描述的可复用脚本任务，LLM 通过 Function Call 调用。

### 5.1 SKILL.md 格式

SKILL.md 采用 YAML front matter + Markdown 正文结构（与 SOUL.md 风格一致）。

**shell 类型示例**：

```yaml
---
name: run_tests
description: "运行项目单元测试并返回结果"
type: shell
input_schema:
  workspace:
    type: string
    description: "项目根目录路径"
    required: true
  package:
    type: string
    description: "测试包路径，默认 ./..."
    required: false
command: "cd {{workspace}} && go test {{package | default './...'}} -v 2>&1"
timeout: 120
---
```

**type 支持**：
- `shell`：Shell 命令模板，支持 `{{变量}}` 插值
- `python`：Python 脚本文件路径
- `flow`：多步骤流程，用 LLM 可读的自然语言描述步骤，引擎将每步委托给 subagent 执行

### 5.1.1 flow 类型 Skill 模板

flow 类型的步骤用自然语言描述，不写代码——LLM 读懂后决定如何委托 subagent：

```yaml
---
name: review_and_fix_code
description: "对指定文件进行代码审查并修复发现的问题"
type: flow
input_schema:
  file_path:
    type: string
    description: "要审查的文件路径"
    required: true
  fix_issues:
    type: boolean
    description: "是否自动修复发现的问题，默认 true"
    required: false
steps:
  - name: read_code
    description: "读取 {{file_path}} 的完整内容，理解其功能和结构"
    agent_role: analyst   # 使用哪种角色的 subagent

  - name: review
    description: >
      对上一步读取的代码进行审查，重点检查：
      1. 是否有明显的 bug 或逻辑错误
      2. 是否有安全漏洞（SQL 注入、越界访问等）
      3. 是否有性能问题
      输出审查报告，格式为 Markdown 列表
    agent_role: reviewer
    depends_on: [read_code]

  - name: fix
    description: >
      根据 review 步骤的审查报告，修复所有发现的问题。
      如果 fix_issues 为 false，则跳过此步骤，只输出报告。
      修复后输出 diff 格式的变更说明。
    agent_role: coder
    depends_on: [review]
    condition: "{{fix_issues | default true}}"

  - name: summary
    description: "汇总审查和修复结果，生成一份简洁的执行报告返回给调用方"
    agent_role: analyst
    depends_on: [fix]
timeout: 300
---

# review_and_fix_code

对代码文件进行自动审查并修复。适合在 CI 流程或手动触发时使用。
```

**flow 引擎执行逻辑**：
1. 解析 `steps[]`，构建局部 DAG（按 `depends_on` 建立依赖）
2. 按拓扑顺序逐步执行，每步创建对应 `agent_role` 的 subagent
3. 上一步的输出通过 tool_result 注入下一步（s05 按需加载）
4. 满足 `condition` 才执行该步骤，否则跳过
5. 最后一步的输出作为整个 flow 的返回值

### 5.2 Skills 数据模型

```go
type Skill struct {
    Name        string
    Description string
    InputSchema ToolSchema
    Type        string    // "shell" | "python" | "flow"
    Command     string    // shell 命令模板
    ScriptPath  string    // python 脚本路径
    FlowSteps   []FlowStep // flow 类型步骤列表
    Timeout     int       // 执行超时（秒）
}

type FlowStep struct {
    Name        string
    Description string
    AgentRole   string    // 调用哪种角色的 subagent 执行
    Input       string    // 步骤输入模板
}
```

### 5.3 Skills 搜索路径

Skills 文件按以下顺序查找：

1. `./skills/` 项目本地目录
2. `~/.axon/skills/` 用户全局目录
3. `configs/skills/` 内置默认 Skills

---

## 6. Browser Agent

Browser Agent 封装 chromedp，提供网页自动化能力：

```go
type BrowserTool struct {
    headless bool
    timeout  time.Duration
}

// 支持的操作
// - navigate: 打开 URL
// - screenshot: 截图
// - click: 点击元素
// - type: 输入文本
// - extract: 提取页面内容（转 Markdown）
// - wait: 等待元素出现
```

Browser Agent 作为独立 Tool 注册到 Registry，LLM 通过 Function Call 使用。

---

## 7. External Agent 调用（ACP 协议）

通过 **ACP（Agent Communication Protocol）** 对接 claude-code、opencode 等外部 AI 工具。
ACP 是专为 Agent 间通信设计的标准协议，参考：https://agentclientprotocol.com/get-started/introduction

### 7.1 ACP 协议概述

ACP 定义了 Agent 之间发送任务、接收结果的标准消息格式，支持：
- 单次请求/响应（`run` 消息）
- 流式进度推送（`stream` 模式）
- 任务状态查询（`status` 查询）

### 7.2 ExtAgentTool 接口

```go
type ExtAgentTool struct {
    Name      string    // "claude-code" | "opencode"
    Endpoint  string    // ACP 服务端点（本地 socket 或 HTTP URL）
    Workspace string    // 隔离工作目录（按 Task ID 绑定，s12）
    Timeout   time.Duration
}

// ACP 任务请求
type ACPRequest struct {
    ID        string            `json:"id"`
    Task      string            `json:"task"`       // 自然语言任务描述
    Workspace string            `json:"workspace"`  // 隔离工作目录
    Context   map[string]any    `json:"context"`    // 附加上下文（文件、约束等）
}

// ACP 任务响应
type ACPResponse struct {
    ID       string     `json:"id"`
    Status   string     `json:"status"`   // "completed" | "failed" | "running"
    Result   string     `json:"result"`   // 最终输出摘要
    Artifacts []string  `json:"artifacts"` // 产出物文件路径列表
    Error    string     `json:"error"`
}

func (t *ExtAgentTool) Execute(ctx context.Context, args map[string]any) (string, error) {
    // 1. 准备隔离 workspace（基于 Task ID，对应 s12 worktree 隔离）
    // 2. 构建 ACPRequest（task 描述 + workspace 路径）
    // 3. 通过 ACP 协议发送请求到外部 Agent
    // 4. 等待 ACPResponse（支持流式进度回调）
    // 5. 将产出物路径和结果摘要封装为 SubAgentResult 返回
}
```

### 7.3 隔离机制

- 每次调用创建独立子目录 `workspace/ext-agents/{task_id}/`
- 外部 Agent 的文件操作通过 ACP workspace 字段限定在该目录
- 任务完成后产出物保留，支持调试查看
- subprocess 强制 timeout + goroutine 取消，防僵尸进程
