# trace 观测补齐与 `suag trace --web`（2026-10-13）

关联：issue suag-nmed（trace --web），plan-20261013-095749-4dd88986。参考：openEuler agent-insight（链路树 + 时间轴 + 节点详情，System Prompt / Hook 上下文标签页，慢节点与 Top N 筛选）、Claude Code OTel 事件（tool_decision 来源、tool.blocked_on_user、hook_execution、compaction 前后 token）与 `/context` 视图、OTel GenAI 语义约定（finish_reasons、cache_write、conversation.compacted）、claude-code-log（单会话 HTML：可折叠工具、thinking 分开、过滤与搜索）、omp stats（缓存命中率、TTFT）。

## 现状

事件：model（token、缓存、耗时、TTFT、调用数、思考/正文字符、前缀对比）、tool（参数摘要、目标、退出码、字节、转存/整理、test_failed、stderr、changes）、denial、turn、compaction、fold、tool_search、turn_context（模型、模式、工具集/规则/记忆哈希、污染）、schedule、memory。会话库另存完整转录与每个分段的上下文快照（环境、规则、记忆索引、skills、追加提示、延迟工具）。`suag trace` 文本、`--json`、`--analyze`。

## 缺口与补齐（第一批）

| # | 观测 | 做法 |
|---|---|---|
| O1 | **上下文组成**：每回合发给模型的上下文由哪些部分组成、各多大 | 每回合开始记一条 `context` 事件：系统提示词、工具定义（内置 / MCP 分开计数与字符）、分段上下文各部分（环境、规则、记忆索引、skills、追加提示、延迟工具）、hook 注入、历史消息、本轮输入的字符数与哈希，以及上下文窗口；web 上画成每回合的堆叠条，可点开看各部分原文（取自会话库的分段快照） |
| O2 | 模型调用结束原因 | model 事件加 `finish`：`final` / `tool_calls` / `truncated`（输出上限截断） |
| O3 | **审批**：谁放行的、等了多久 | 附在工具 / 拒绝事件上：`approval`（rule：规则、只读、auto 放行；user：问人后同意；always：之前答过“总是”；user_denied：问人后拒绝）、`approval_wait_ms`（等人回答的时间，事件耗时包含它）；观测层在 ctx 放 `tools.ApprovalReport`，问人的审批器（chat、ACP）填写 |
| O4 | **hook 调用** | 新事件 `hook`：事件名（SessionStart / UserPromptSubmit / PostToolUse / Stop …）、耗时、结果（block 及原因、注入的 additionalContext 字符数、stderr 提示）、失败 |
| O5 | 用户输入 | turn 事件加 `prompt_chars`、`injected`（非人输入，如 hook / gofer 注入） |
| O6 | 编辑规模 | file_edit / file_write 事件加 `lines_added` / `lines_removed`（按新旧文本行数） |

后续（不在本批）：provider 内部 HTTP 重试次数与 request id、子 agent 树（suag-dmv6）、后台任务（suag-ol03）、两会话对比。

## `suag trace --web`

- `suag trace <session> --web [--addr 127.0.0.1:0]`：本机起 HTTP 服务（只绑回环地址），打印地址并尽量打开浏览器；Ctrl-C 退出。页面为嵌入二进制的单个 HTML（原生 JS，无构建步骤），数据走 `/api/session`（事件 + 转录条目 + 分段上下文 + `--analyze` 结果）。
- 第一版页面：
  1. **概览条**：模型调用、工具调用、token（输入 / 缓存 / 输出）、耗时（模型 / 工具）、通过的测试、拒绝与错误数；`--analyze` 的阶段拆分与未命中归因。
  2. **时间轴（瀑布图）**：按回合分组，模型调用与工具调用为横条（长度 = 耗时，颜色区分模型 / 读 / 写 / shell / 错误），并行调用同一行并排；折叠、分段、压缩、hook、审批画成标记。
  3. **详情面板**：点任一条看属性；模型调用显示思考与正文、请求前缀对比；工具调用显示完整参数与结果（取自转录）、审批与等待。
  4. **上下文组成**：每回合的堆叠条（系统提示 / 工具 / 规则 / 记忆 / skills / 环境 / 历史 / 本轮输入），点开看各部分原文；每次模型调用的输入 token 与缓存命中折线。
  5. **筛选与搜索**：按类型、错误、慢节点（>10s / >60s）、大 token（>50k），Top 5；全文搜索参数与输出。
- 数据量：单会话通常数百事件，一次性下发；工具结果超过 64KB 的截断并给 `out:` ref。

## 验证

单测覆盖新事件与属性、`/api/session` 的数据组装、HTML 嵌入；真实会话（评测中的 T9）用浏览器打开核对时间轴、详情、上下文组成。
