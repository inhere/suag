# AI agent

## 需求

```txt
kagent 大脑，入口
记忆能力
创建职责团队 专家
根据职责配备相应的 规范，工具，技能
能长时间的处理任务
有默认的任务跟踪，审核专家
小团队的管理自带跟踪审核机制
可以使用外部 agent claude-code, opencode 等实现编码或其他任务
关键决策才需要人参与
```

### 2026.03.16:

> from <https://github.com/shareAI-lab/learn-claude-code>

```txt
- agent loop 叠加策略、权限与生命周期层
- 事件 / Hook 总线
- 基于规则的权限治理与信任流程
- 会话生命周期控制 (resume/fork)

基础agent 按 12 个递进式实现, 从简单循环到隔离化的自治执行。 每个步骤添加一个机制。每个机制有一句格言。
s01   "One loop & Bash is all you need" — 一个工具 + 一个循环 = 一个智能体
s02   "加一个工具, 只加一个 handler" — 循环不用动, 新工具注册进 dispatch map 就行
s03   "没有计划的 agent 走哪算哪" — 先列步骤再动手, 完成率翻倍
s04   "大任务拆小, 每个小任务干净的上下文" — 子智能体用独立 messages[], 不污染主对话
s05   "用到什么知识, 临时加载什么知识" — 通过 tool_result 注入, 不塞 system prompt
s06   "上下文总会满, 要有办法腾地方" — 三层压缩策略, 换来无限会话
s07   "大目标要拆成小任务, 排好序, 记在磁盘上" — 文件持久化的任务图, 为多 agent 协作打基础
s08   "慢操作丢后台, agent 继续想下一步" — 后台线程跑命令, 完成后注入通知
s09   "任务太大一个人干不完, 要能分给队友" — 持久化队友 + 异步邮箱
s10   "队友之间要有统一的沟通规矩" — 一个 request-response 模式驱动所有协商
s11   "队友自己看看板, 有活就认领" — 不需要领导逐个分配, 自组织
s12   "各干各的目录, 互不干扰" — 任务管目标, worktree 管目录, 按 ID 绑定
```

### 2026.03.17 补充

- 模块化，可插拔式设计，统一定义接口  eg: 支持不同的 LLM 供应商，不同的内存实现，不同的消息入口渠道(qq, 钉钉，飞书, 命令行等)，不同的工具实现等。
- 需要新增支持 AGENTS.md 定义不同的智能体角色，每个角色有自己的系统提示词，工具集，技能集等。
  - SOUL.md 定义智能体的灵魂，包括其角色、目标、行为规范等。SOUL.md + AGENTS.md 定义一个完整的智能体。

## 核心架构分层

- **接入层 (Gateway/UI)：** 网页端仪表盘，通过 **SSE (Server-Sent Events)** 实现毫秒级追踪展示，支持“上帝模式”指令注入。
- **编排层 (Orchestrator)：** 基于 Go 协程的 ReAct Loop 循环，支持多智能体（Architect/Coder/Reviewer）并发协作与状态机管理。
- **追踪层 (Tracing)：** 全链路 TraceID/SpanID 埋点，记录 Agent 的因果关系。
- **连接LLM(Provider)**:  实现一个支持 OpenAI、Claude、DeepSeek 等多家供应商的统一LLM接口。
- **执行层 (Toolbox)：**
  - **Native Tools:** 高性能本地 IO 操作。
  - **MCP Server:** 通过标准协议接入全球开发者社区的工具。
  - Skills Usage: 实现对 `SKILL.md` 的解析，并能根据 LLM 的 Function Call 调用本地 Shell 或 Python 脚本。
  - **Browser Agent:** 专用的自动化浏览器控制器。

### 要点记录

- Orchestrator 编排层参考
- 多agent智能体协作 - 各自有自己的角色核心定义 SOUL.md
- memery  Embedded SQLite + sqlite-vec
  - **逻辑流程：** 用户提问 -> 本地 SQLite 语义检索 -> 提取历史记忆片段 -> 拼接上下文发送给 LLM -> 存储新记忆。
  - TODO 后续支持 多向量数据库 如 PGVector 等
- 多skills 连续调用执行 - 避免上下文膨胀
  - 主 Agent 执行一个类似 flow 的skill，里面通过描述指导调用 subagent 完成任务汇报结果
  - subagents 在各自的会话中执行自己的任务，返回结果给 主agent

### 智能定时任务处理 (Agentic Cron)

传统的 Cron 是死板的定时，而 **智能定时任务** 具备“自主判断能力”。

- **逻辑设计：**
  1. **定时触发：** 比如每小时触发一次。
  2. **感知阶段：** Agent 先调用工具（如 `fetch_news` 或 `check_system_status`）。
  3. **决策阶段：** Agent 思考：“根据目前获取的信息，我有必要采取行动吗？”。
  4. **执行阶段：** 若有必要，则开启一个完整的 ReAct 任务；若无必要，直接进入睡眠并写下一条日志：“状态正常，无需操作”。

### 链路追踪与因果分析

- **实施：** 每一个 `User Request` 生成唯一 `TraceID`。当 Architect 产生设计稿并触发 Coder 时，Coder 的 `ParentID` 指向 Architect 的 `SpanID`。
- **价值：** 彻底解决多 Agent 协作中的“甩锅”问题。监控界面可以清晰展示：是因为架构师设计模棱两可，还是程序员理解偏差。

### HITL (人机协作) 深度集成

- **实施：** 在代码审查（Review）环节设置“干预阈值”。若 Agent 连续 3 次尝试失败，系统自动挂起（Blocking on Channel），并通过 SSE 提醒人类导师。

### 代码架构

- agent-core 提供核心能力
- 围绕 core 可以提供 TUI, server, webUI, 多渠道IM
  - webUI 可以 chat, 查看任务等

代码结构设想参考：

```txt
agent/
├── core/
│   ├── agent.go
│   ├── loop.go
│   ├── memory.go
│   └── planner.go
├── tools/
│   ├── registry.go
│   ├── web_search.go
│   └── code_executor.go
├── llm/
│   ├── client.go
│   └── prompt.go
└── main.go
```

#### 基础工具

1. Web Search（SerpAPI 等）
2. 代码执行（本地，Docker 沙箱，强限资源）
3. 文件操作（限定 workspace 目录）

### 一些代码参考

```go
type Agent struct {
    llm      LLMClient
    memory   *Memory
    tools    *ToolRegistry
    maxSteps int
}

func (a *Agent) Run(ctx context.Context, task string) error {
    a.memory.AddMessage("user", task)

    for step := 0; step < a.maxSteps; step++ {
        // 1. 从 LLM 获取回复
        resp, err := a.llm.CompleteWithRetry(ctx, a.memory.GetContext())
        if err != nil {
            return err
        }

        if resp.ToolCall != nil {
            result, err := a.tools.Execute(ctx, resp.ToolCall)
            if err != nil {
                a.memory.AddMessage("system", "tool error: "+err.Error())
                continue
            }
            a.memory.AddMessage("tool", result)
            continue
        }
 
        a.memory.AddMessage("assistant", resp)
        if resp.IsComplete {
            return nil
        }
    }

    return errors.New("max steps reached")
}
```

llm client:

```go
func (c *LLMClient) CompleteWithRetry(ctx context.Context, prompt string) (*Response, error) {
    var lastErr error

    for attempt := 0; attempt < 3; attempt++ {
        resp, err := c.Complete(ctx, prompt)
        if err == nil {
            return resp, nil
        }

        lastErr = err

        backoff := time.Duration(1<<attempt) * time.Second

        select {
        case <-time.After(backoff):
        case <-ctx.Done():
            return nil, ctx.Err()
        }
    }

    return nil, fmt.Errorf("failed after retries: %w", lastErr)
}
```

context memory:

```go
type Memory struct {
    messages []Message
}

type Message struct {
    Role   string
    Content string
}

func (m *Memory) AddMessage(role, content string) {
    m.messages = append(m.messages, Message{Role: role, Content: content})
}

func (m *Memory) GetContext() string {
    if len(m.messages) <= 10 {
        return m.formatAll()
    }

    summary := m.getSummary()
    recent := m.messages[len(m.messages)-10:]
    return summary + "\n" + formatMessages(recent)
}
```

tool registry:

```go
type Tool struct {
    Name        string
    Description string
    Parameters  map[string]string
    Execute     func(context.Context, map[string]interface{}) (string, error)
}

type ToolRegistry struct {
    tools map[string]*Tool
    mu    sync.RWMutex
}

func (r *ToolRegistry) Register(tool *Tool) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.tools[tool.Name] = tool
}

func (r *ToolRegistry) GetSchema() []map[string]interface{} {
    r.mu.RLock()
    defer r.mu.RUnlock()

    schemas := make([]map[string]interface{}, 0, len(r.tools))
    for _, t := range r.tools {
        schemas = append(schemas, map[string]interface{}{
            "name":        t.Name,
            "description": t.Description,
            "parameters":  t.Parameters,
        })
    }
    return schemas
}
```

Mock LLM：

```go
type MockLLMClient struct {
    responses []Response
    callCount int
}

func (m *MockLLM) Complete(ctx context.Context, prompt string) (*Response, error) {
    if m.callCount >= len(m.responses) {
        return nil, errors.New("no more responses")
    }
    resp := m.responses[m.callCount]
    m.callCount++
    return &resp, nil
}
```

## 参考

### 学习项目

- <https://github.com/shareAI-lab/learn-claude-code>

### 参考项目

- <https://github.com/ComposioHQ/agent-orchestrator>

Golang:

- <https://github.com/smallnest/goclaw> 多智能体 AI 网关与编排引擎。专注于“团队”概念，它不仅仅是一个简单的聊天机器人，而是一个能够调度多个智能体（Agents）的中心枢纽。
- <https://github.com/sipeed/picoclaw> 极致轻量化的边缘计算 AI 助手。
- <https://github.com/mosaxiv/clawlet> 具备本地语义记忆的单机全能助手，可以参考代码逻辑

Rust:

- <https://github.com/zeroclaw-labs/zeroclaw>
- <https://github.com/moltis-org/moltis>

### 参考文章

- <https://mp.weixin.qq.com/s/IEkNPHX94707--WTYV-tIQ> 我用 Go 写了 3 个月 AI Agent：比 Python 难，但值不值？
- <https://mp.weixin.qq.com/s/Z8JYgxUdHSLo4ywgyt4ljg>  Agent/Skills/Teams 架构演进过程及技术选型之道
- <https://mp.weixin.qq.com/s/JLToUqqO-2MBL3PQojLnUQ> AI Harness 工程：Agent 能跑起来的那一层到底是什么？
- <https://mp.weixin.qq.com/s/DObso9ym9gsTXCikX3sm7A> Tool Router：Harness 如何通过三层架构驯服 AI Agent 的工具爆炸
- <https://mp.weixin.qq.com/s/adQSYnS_vMjXr0DKILWIMA> Harness Engineering：生产级 AI Agent Runtime 的架构与设计原则
- [https://geekbang.feishu.cn/wiki/TSUqwoAn5iL6WxkkxHcci3pzn7c](openclaw 知识库)

