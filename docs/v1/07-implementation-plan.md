# 开发实施方案

> 相关文档：[架构总纲](./00-overview.md) | 用途：开发任务跟踪，适合映射为 GitHub Issues

---

## 总览

| 阶段 | 目标 | 预估周期 | 状态 |
|------|------|----------|------|
| Phase 1 | 单 Agent MVP - 核心 ReAct Loop 验证 | 2-3 周 | 进行中 |
| Phase 2 | 持久化 + 工具扩展 | 2-3 周 | 待开始 |
| Phase 3 | 多 Agent 团队协作 | 3-4 周 | 待开始 |
| Phase 4 | Gateway + WebUI | 3-4 周 | 待开始 |
| Phase 5 | 外部 Agent + Agentic Cron | 2-3 周 | 待开始 |
| Phase 6 | 生产增强（持续迭代） | 持续 | 待开始 |

---

## Phase 1：单 Agent MVP

**目标**：可运行的单 Agent CLI，核心 ReAct Loop 验证。

### 基础架构

- [ ] P1-001 初始化 Go 模块，建立项目目录结构（`internal/agent`、`internal/llm`、`internal/tools`、`internal/memory`）
- [ ] P1-002 定义核心接口：`LLMProvider`、`MemoryStore`、`Tool`、`AgentRunner`
- [ ] P1-003 实现 MockLLMProvider，用于单元测试（已有实现可复用）

### Agent Core（s01-s03）

- [ ] P1-010 实现最小 ReAct Loop（s01）：单工具 + for 循环
- [ ] P1-011 实现 Tool dispatch map（s02）：ToolRegistry 注册与执行
- [ ] P1-012 实现 Planner 步骤列表（s03）：执行前输出步骤计划
- [ ] P1-013 实现 AgentState 状态机：Pending/Running/Completed/Failed
- [ ] P1-014 实现 maxSteps 硬限制与 failCount 计数

### LLM Provider

- [ ] P1-020 实现 `LLMProvider` 接口
- [ ] P1-021 实现 OpenAI provider（或 Claude，任选一）
- [ ] P1-022 实现指数退避重试（最多 3 次）

### InMemory

- [ ] P1-030 实现 `InMemoryStore`：消息列表 + `GetContext(maxTokens)`
- [ ] P1-031 实现 L1 滑动窗口压缩（超过 N 条消息时）
- [ ] P1-032 实现 L2 LLM 摘要压缩

### Native Tools（基础 3 个）

- [ ] P1-040 实现 `file_read`（限 workspace 目录）
- [ ] P1-041 实现 `file_write`（限 workspace 目录）
- [ ] P1-042 实现 `shell_exec`（基础版，无沙箱）

### CLI

- [ ] P1-050 实现 `cmd/kagent` 命令行入口
- [ ] P1-051 实现基础 TUI 交互（输入任务，查看输出）

### 测试

- [ ] P1-060 ReAct Loop 单元测试（MockLLM）
- [ ] P1-061 ToolRegistry 注册/执行测试
- [ ] P1-062 InMemoryStore 测试（含压缩逻辑）

**技术验证点**：评估 sqlite-vec 跨平台可行性（有问题则 Phase 2 改用 BM25 兜底）

---

## Phase 2：持久化 + 工具扩展

**目标**：记忆持久化，工具生态初步搭建。

### SQLite 持久化

- [ ] P2-001 引入 `modernc.org/sqlite`（纯 Go，无 cgo）
- [ ] P2-002 实现 SQLite schema：`sessions`、`messages`、`fragments`、`spans` 表
- [ ] P2-003 实现 `SQLiteMemory`（替换 InMemoryStore 的持久化层）
- [ ] P2-004 实现单 writer goroutine + WAL 模式（并发安全）
- [ ] P2-005 实现 L3 重要性过滤压缩（token 预算 90% 时触发）

### 向量搜索

- [ ] P2-010 评估 sqlite-vec 集成方案（cgo 或预编译 .so）
- [ ] P2-011 实现 `VectorStore` 接口 + sqlite-vec 实现（或 BM25 fallback）
- [ ] P2-012 实现 Embedding API 调用（OpenAI 或 Ollama nomic-embed）
- [ ] P2-013 实现长期记忆检索流程（查询向量化 → ANN 搜索 → 注入上下文）

### 会话生命周期

- [ ] P2-020 实现 Session 持久化（Agent 状态快照到 SQLite）
- [ ] P2-021 实现 Agent Resume：从挂起状态恢复执行
- [ ] P2-022 实现 Agent Fork：从 Memory 快照克隆分支会话

### MCP Client

- [ ] P2-030 实现 MCP 协议客户端（JSON-RPC 2.0 over stdio）
- [ ] P2-031 实现 MCP Server 配置解析（`configs/mcp.yaml`）
- [ ] P2-032 实现 MCP 工具自动注册到 ToolRegistry

### 工具扩展

- [ ] P2-040 实现 `web_search` 工具（SerpAPI 接入）
- [ ] P2-041 实现 `code_exec` 工具（Docker 沙箱，强限资源）
- [ ] P2-042 实现 `http_get` 工具

### SOUL.md

- [ ] P2-050 实现 SOUL.md 解析（YAML front matter + Markdown 正文）
- [ ] P2-051 实现基于 Soul 的工具白名单过滤
- [ ] P2-052 实现 Soul 配置的 token_budget 和 max_steps 约束

---

## Phase 3：多 Agent 团队协作

**目标**：团队编排，Subagent 隔离，完整追踪。

### Subagent 隔离（s04-s05）

- [ ] P3-001 实现 Subagent 独立 MemoryStore 创建（按 Task ID 隔离）
- [ ] P3-002 实现 Subagent 独立会话（独立 `messages[]`，不污染主对话）
- [ ] P3-003 实现 tool_result 上下文注入（s05：按需加载，不塞 system prompt）
- [ ] P3-004 实现 SubAgentResult 结构化结果上报

### 文件持久化任务图（s07）

- [ ] P3-010 实现 Task DAG 数据模型（ParentID、依赖关系）
- [ ] P3-011 实现 Task 状态持久化到 SQLite
- [ ] P3-012 实现自组织任务认领（s11）：Agent CAS 原子抢占 pending 任务

### Team Manager

- [ ] P3-020 实现 `TeamManager` 接口
- [ ] P3-021 实现团队创建（解析 soul_files，创建专家 Agent 池）
- [ ] P3-022 实现 LLM 任务 DAG 分解（复杂任务自动拆分子任务）
- [ ] P3-023 实现任务分配（专长匹配 + 自组织认领两种模式）
- [ ] P3-024 实现团队监控（进度汇总、结果收集）

### Tracing System

- [ ] P3-030 实现 `Tracer` 接口 + SQLite 存储
- [ ] P3-031 实现 TraceID/SpanID 生成与 context 传递
- [ ] P3-032 实现调用树构建（ParentSpanID 关联）
- [ ] P3-033 实现 Tracer 与 EventBus 集成

### EventBus

- [ ] P3-040 实现内部 EventBus（内存 pub/sub）
- [ ] P3-041 实现标准 Event Topic 定义（见 02-orchestrator.md）
- [ ] P3-042 实现 Hook 注册机制（拦截器）

### HITL Manager

- [ ] P3-050 实现 `HITLManager` 接口
- [ ] P3-051 实现 channel 挂起/恢复机制
- [ ] P3-052 实现 HITL ticket 持久化
- [ ] P3-053 实现高风险工具信任确认流程

### 后台异步执行（s08）

- [ ] P3-060 实现慢操作后台 goroutine 模式
- [ ] P3-061 实现 channel 注入通知机制

---

## Phase 4：Gateway + WebUI

**目标**：Web 仪表盘，实时监控，"上帝模式"。

### REST API

- [ ] P4-001 实现 HTTP Server（net/http + chi router）
- [ ] P4-002 实现任务 CRUD 端点（POST/GET/DELETE `/api/tasks`）
- [ ] P4-003 实现 Agent 查询端点（GET `/api/agents`）
- [ ] P4-004 实现 HITL 端点（GET/POST/DELETE `/api/hitl`）
- [ ] P4-005 实现上帝模式注入端点（POST `/api/agents/{id}/inject`）
- [ ] P4-006 实现健康检查端点（GET `/api/health`）

### SSE Server

- [ ] P4-010 实现 SSE Handler，订阅 EventBus 事件
- [ ] P4-011 实现按 trace_id 过滤订阅
- [ ] P4-012 实现 SSE 断线重连支持（Last-Event-ID）

### WebUI

- [ ] P4-020 搭建前端框架（Vue 或 React，go:embed 打包）
- [ ] P4-021 实现任务列表页（实时状态，SSE 更新）
- [ ] P4-022 实现 Trace 调用树可视化（树形或时序图）
- [ ] P4-023 实现 HITL 干预面板（ticket 列表 + 指令输入）
- [ ] P4-024 实现 Agent 日志流（SSE 实时滚动）
- [ ] P4-025 实现上帝模式操作界面

### 权限与生命周期层

- [ ] P4-030 实现 `PermissionLayer` 接口
- [ ] P4-031 实现工具白名单规则引擎
- [ ] P4-032 实现工作目录沙箱（workspace 路径强制前缀检查）
- [ ] P4-033 实现 token 预算管控

---

## Phase 5：外部 Agent + Agentic Cron

**目标**：接入 claude-code/opencode，部署智能定时任务。

### Skills Engine

- [ ] P5-001 实现 SKILL.md 解析（YAML front matter）
- [ ] P5-002 实现 shell 类型 Skill 执行（命令模板插值 + subprocess）
- [ ] P5-003 实现 python 类型 Skill 执行
- [ ] P5-004 实现 flow 类型 Skill（多步骤，调用 subagent）
- [ ] P5-005 实现 Skills 搜索路径（本地 → 用户全局 → 内置）

### External Agent

- [ ] P5-010 实现 `ExtAgentTool` 基础框架（subprocess + workspace 隔离，s12）
- [ ] P5-011 实现 claude-code 适配（输入/输出格式协商）
- [ ] P5-012 实现 opencode 适配
- [ ] P5-013 实现外部 Agent 超时控制（goroutine 取消，防僵尸进程）

### Agentic Cron

- [ ] P5-020 实现 Cron 调度器（基于 cron 表达式）
- [ ] P5-021 实现 `CronJob` 数据模型及 SQLite 持久化
- [ ] P5-022 实现感知-决策-执行三阶段流程
- [ ] P5-023 实现 Cron 执行日志与 SSE 推送
- [ ] P5-024 实现 CLI 管理命令：`kagent cron add/list/enable/disable/delete`
- [ ] P5-025 实现 WebUI Cron 管理页（创建/编辑/启停），与 CLI 共用同一存储层

### LLM Provider Pool

- [ ] P5-030 实现 Provider Pool（多供应商注册）
- [ ] P5-031 实现主备切换（主 provider 失败自动 fallback）
- [ ] P5-032 实现 DeepSeek provider
- [ ] P5-033 按模型路由（embedding 用轻量模型，推理用强模型）

### Browser Agent

- [ ] P5-040 实现 BrowserTool（chromedp 封装）
- [ ] P5-041 实现 navigate、screenshot、extract 基础操作
- [ ] P5-042 实现 click、type、wait 交互操作

---

## Phase 6：生产增强（持续迭代）

**目标**：稳定性、可观测性、扩展性增强。

### 可观测性

- [ ] P6-001 实现 Agent token 用量统计与报表
- [ ] P6-002 实现 Agent 执行耗时统计
- [ ] P6-003 实现任务历史导出（JSON/CSV）
- [ ] P6-004 实现配置热重载

### 存储扩展

- [ ] P6-010 实现 PGVector 可选支持（VectorStore 接口替换）
- [ ] P6-011 实现跨数据库迁移工具

### 多用户支持

- [ ] P6-020 数据模型增加 `user_id` 字段（Task、Agent、CronJob、HITLTicket）
- [ ] P6-021 实现 API 认证（API Key 优先，可扩展 JWT）
- [ ] P6-022 实现请求中间件：认证 → 解析 user_id → 注入 context
- [ ] P6-023 实现数据隔离：所有查询按 user_id 过滤，禁止跨用户访问
- [ ] P6-024 实现用户管理 API（创建/停用账号、API Key 轮换）
- [ ] P6-025 实现审计日志（上帝模式注入、HITL 操作、跨用户操作均记录）

### 开发者体验

- [ ] P6-030 实现 Agent 市场/模板库（SOUL.md 分享）
- [ ] P6-031 完善 CLI 文档和 --help
- [ ] P6-032 补全集成测试（端到端 ReAct Loop）
- [ ] P6-033 完善 README 和使用示例

---

## 待确认问题清单

### ✅ 已确认

| # | 问题 | 结论 | 影响模块 |
|---|------|------|----------|
| P0-1 | SOUL.md 格式 | **YAML front matter + Markdown 正文**，已确定 | Team Manager、权限层 |
| P0-2 | 外部 Agent 对接协议 | **ACP 协议**（Agent Communication Protocol），参考 [agentclientprotocol.com](https://agentclientprotocol.com) | ExtAgentTool |
| P0-3 | Embedding 模型 | **统一设计 `EmbeddingProvider` 接口**，实现时同时支持云端（OpenAI/Claude）和本地（Ollama），配置切换 | Memory System |
| P1-1 | SKILL.md flow 类型语法 | **LLM 可读的自然语言描述步骤**，设计了 `steps[]` 模板（含 agent_role / depends_on / condition），见 [04-tools-skills.md](./04-tools-skills.md#511-flow-类型-skill-模板) | Skills Engine |
| P1-2 | 上帝模式安全边界 | **不可以覆盖** SOUL.md / AGENTS.md 约束，权限层强制校验，见 [06-gateway-api.md](./06-gateway-api.md#23-上帝模式注入) | Gateway、权限层 |
| P1-3 | Team 任务分配策略 | **Orchestrator LLM 自动分解任务 DAG**，不需要手工定义 | Team Manager |
| P1-4 | Subagent 结果格式 | **结构化 JSON `SubAgentResult`**，含 Summary + Artifacts，沿用当前方案 | Agent Core |
| P2-1 | Web UI 技术栈 | **Vue**（go:embed 打包为单二进制） | WebUI |
| P2-2 | 多用户支持 | **需要**，涉及用户隔离、任务按 user_id 分区、API 认证 | Gateway、存储层 |
| P2-3 | Agentic Cron 任务定义 | **CLI + WebUI 双入口**均支持配置，底层统一存储到 SQLite | Agentic Cron |
| P2-4 | 开源计划 | **先内部开发**，稳定后可能开源；第三方库优先选宽松许可证 | 全局 |

### ⏳ 待确认

> 全部问题已确认，无待确认项。
