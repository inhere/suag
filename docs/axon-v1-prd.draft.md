# AI agent

## 需求

当前项目是一个基于 AI agent 的智能体系统，用于处理复杂的任务和决策，支持团队协作和任务分配，能够自动处理任务，跟踪任务进度，审核任务结果，与外部系统集成。

```txt
axon 大脑，入口
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

模块化，可插拔式设计，统一定义接口
  - eg: 支持不同的 LLM 供应商，不同的存储实现，不同的消息入口渠道(qq, 钉钉，飞书, 命令行等)，不同的工具实现等。

Agent:
  - 需要新增支持 AGENTS.md 定义不同的智能体角色，每个角色有自己的系统提示词，工具集，技能集等。
  - SOUL.md 定义智能体的灵魂，包括其角色、目标、行为规范等。SOUL.md + AGENTS.md 定义一个完整的智能体。
  - 加上 agent 工作空间，每个 agent 有自己的目录/存储，用来存储自己的历史，缓存，记忆，任务图，工具集，技能集等。

Memory backend:
 - Hybrid: SQLite + Markdown
 - SQLite with FTS5 search (recommended)
 - Markdown files — simple, human-readable
 - In-memory LRU — no persistence, ideal for testing
 - HTTP API — delegate to external REST service

Plugin: 思考插件如何实现？
  - 支持插件机制，可插拔式扩展功能。
  - 每个插件有自己的目录，包含插件代码，配置文件等。
  - 插件可以在运行时动态加载和卸载。

### 2026.03.18 补充

OpenClaw:
1.统一控制平面Gateway网关 Gateway是OpenClaw的心脏，充当系统的单一控制平面
  - 功能职责: 负责管理会话(Sessions)、状态感知(Presence)、配置、定时任务(Cron)、网络钩子(Webhooks)以及控制界面(Control UI)和Canvas宿主
  - 通信协议: 基于WebSocket(WS) 网络构建，为所有客户端、工具和事件提供统一的连接通道。
  - 角色分离 operator（控制面）和 node（能力节点）两种角色
2.Agentic Loop
3.定时任务系统
4.工具系统
5.Channels 连接生态 将AI能力注入到用户已有的社交生态。
  - 路由规则: 具备复杂的群组路由逻辑，包括提及门控（Mention gating）、回复标签处理以及针对不同频道的自动消息分块。
6.上下文管理
7.SubAgent子智能体
8.SandBox沙箱系统
9.记忆管理
10.Skills模块
11.Session管理
12.自进化机制
13.工作区与Agent路由
14.Nodes
15.安全策略
16.配置管理

实现可控的消息路由、权限治理与工具循环执行

**智能体运行时**
是处理逻辑和生成回复的核心引擎:
RPC模型: Agent 以RPC(远程过程调用)模式运行，支持工具流(Tool Streaming)和块流(Block Streaming)，确保响应的高效与实时性。
多智能体路由: 系统能够将来自不同频道、账户或同伴的输入路由到相互隔离的智能体(拥有独立的Workspace和会话)
会话模型: 提供main模式用户直接对话，并支持群组隔离、激活模式切换和队列管理。

设备节点与伴侣应用: Nodes & Apps
通过将不同设备定义为“节点”，OpenClaw 实现了跨设备的硬件控制：
跨平台支持：包括 macOS 菜单栏应用、iOS 节点和 Android 节点。
硬件能力调用：通过协议，智能体可以远程调用各节点上的硬件功能，如摄像头拍照/录码、麦克风录音等。

**工具与自动化：Tools & Skills**
架构中集成了丰富的生产力工具：
浏览器控制：内置托管的 Chrome/Chromium 实例，支持快照、动作执行和文件上传。
Live Canvas：基于 A2UI 构建的实时交互画布，允许智能体驱动视觉化的工作空间。
技能平台 (ClawHub)：提供技能注册表，支持捆绑技能、托管技能和工作区技能的自动搜索与安装。

**部署与远程访问**
本地/远程灵活部署：Gateway 可以运行在本地或小型 Linux 实例上。
内网穿透：集成 Tailscale Serve/Funnel 或 SSH 隧道，使用户能够安全地从远程访问 Gateway 面板和 WebSocket 服务。

**工具过载**
解决“MCP 过多”的问题，核心思路是：从“全量加载”转向“按需路由”和“分层治理”
- 引入“工具检索”（Tool RAG） 不要将所有 MCP 工具的定义直接塞进 Prompt，而是将工具视为一种“知识”，使用 RAG (Retrieval-Augmented Generation) 的逻辑进行动态加载
- 采用“路由代理”模式 (Router-Worker Architecture) 不要让一个 Agent 负责所有事情，而是建立一个层级化架构。Dispatcher 负责路由请求到合适的 Worker Agent，Worker Agent 负责执行具体的任务。
- 基于权限（RBAC）的工具过滤 企业场景下，并非所有用户都有权访问所有 MCP 能力
- 动态上下文（Dynamic Context Loading） MCP 协议本身支持 Resource（资源） 的概念。我们可以利用这一点来优化
- 提示词压缩与 Schema 精简 标准的 JSON Schema 往往包含大量冗余描述（如 description, type, required 等）。
  - Schema 裁剪： 在向 LLM 传递 MCP 工具定义前，移除不必要的字段，使用更紧凑的文本格式（如 TypeScript 定义）代替繁琐的 JSON

refer:
- https://mp.weixin.qq.com/s/wVcItgqsCiwl9-PZ56z27w
- https://seanfalconer.medium.com/agentic-mesh-the-future-of-enterprise-agent-ecosystems-f26e7d53951e

### 2026.03.20 补充

- 内部多个 Agent 交互使用 A2A 协议，ACP 是 A2A 的基石之一。
- 信息拼成系统提示：AGENTS.md（行为指南）、SOUL.md（人格）、IDENTITY.md（身份）、USER.md（用户偏好）、MEMORY.md（长期记忆）、技能摘要。

#### Harness

Harness 在真实系统中的形态在真实工程系统中，这些结构其实已经广泛存在。
以代码 Agent 为例，一个典型系统通常包含：
- Environment 提供本地代码仓库和执行环境；
- Tool 系统封装 read_file、write_file、run_test 等能力；
- Control 系统限制执行步数和任务时间；
- Memory 系统保存任务状态和历史修改记录；
- Evaluation 系统自动运行单元测试。

refer:
- https://mp.weixin.qq.com/s/adQSYnS_vMjXr0DKILWIMA

### 2026.03.25 补充

- agent core, orchestrator, agents, team(leader, worker), tools, skills, api
- 支持多节点部署，每个节点运行一个 axon，一个为主节点，其他节点为从节点。通过协议进行通信协作指挥。
  - 主节点负责协调和管理所有智能体，可以调度指定从节点负责执行具体任务。
  - 一个子节点可能是一个专业的 agent team，负责执行特定类型的任务。
- A2A 用作多个agent时，允许agent是远程运行的，这时 编排或者agent 直接可以通过A2A交互通信
- ACP 是想让 agent 可以发现并操作本地已有的 claude-code, codex, opencode

### 2026.03.28 补充

- ag-ui 和 A2UI 协议
- 每个 agent 都是一个虚拟人/员工
  - 可以有 leader，也可以独立运作
  - 每个人都有自己的头像信息登
- 可以自动创建讨论群
- 可以添加真实用户到讨论群里，给一个身份 可以参与讨论或者审核等

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

> 示例代码参考 [code-refer.v0.1.draft.md](code-refer.v0.1.draft.md)

#### 基础工具

1. Web Search（SerpAPI 等）
2. 代码执行（本地，Docker 沙箱，强限资源）
3. 文件操作（限定 workspace 目录）

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

