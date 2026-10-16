# suga 功能现状分析与可加强点（2026-10-16）

范围与方法：对当前仓库做一轮分层功能盘点，并给出可以加强的点。基线 `b586777`（2026-10-15）。结论来自四路静态代码核查——会话与检索、LLM provider、内置工具与权限、前端与可观测性——文中所有"实现/缺失"判断都尽量附 `文件:行`。**没有运行时验证**（唯一例外见 §5）。本文是分析记录，不是实现合同；落地前按 AGENTS.md 另出 `docs/design/` 设计与计划。

## 1. 定位与全景

suga 是 Go 写的个人编码 agent：一个持久会话引擎 + 两个前端（终端 `chat`/`run`，ACP stdio `acp`），Gofer 在其上做作业、审批与日志。约 10.3 万行 Go（含测试），分层严格，`pkg/agentcore/layering_test.go` 守住"能力层不依赖应用层"。

| 层 | 包 | 规模 | 职责 |
|---|---|---|---|
| 应用层 | `internal/personal` | 21806 行 / 123 文件 | 回合、分段与交接、事件、运行门、SQLite 存储、trace/analyze |
| | `internal/chat` | 12819 行 / 83 文件 | 终端 chat：行编辑、渲染、审批、steering、idle、hooks |
| | `internal/wiring` | 7404 行 / 45 文件 | 组装 provider、工具、skills、提示词、审批与运行器 |
| | `internal/cli` | 5293 行 / 39 文件 | 13 个子命令（run/chat/acp/trace/stats/sessions/send/ps/gc/export/manifest…） |
| | `internal/config` / `internal/acpserver` / `internal/output` | 2969 / 2951 / 1071 行 | 配置分层、ACP 服务、输出（mdterm/stream-json/live text） |
| 能力层 | `pkg/agentcore/llm` | 38917 行（整个 agentcore）/ 244 文件 | openai、openai-responses、anthropic 三种协议，重试与 fallback 池 |
| | `pkg/agentcore/tools`（+`native`、`mcp`） | 同上 | 注册表、审批层、延迟加载、输出治理、内置工具、stdio MCP |
| | `pkg/agentcore/skills`、`memory`、`redact`、`extproc`、`schedule`、`storage` | 同上 | SKILL.md、长期记忆抽象与后端、脱敏、外部进程协议、进程内定时、run 存储 |
| 内核 | `pkg/agentcore`（根包）+ `api` | 同上 | Runtime（模型/工具循环）、run state、transcript、hooks、policies |

## 2. 现有能力盘点

### 2.1 对话与上下文

- 单库 SQLite（`$SUGA_HOME/db/sessions.db`）：`sessions`/`turns`/`entries`/`events` 四表 + `entries_fts` 虚表 + `memory_meta`，迁移靠内嵌 `migrations/*.sql` + `schema_migrations` 表（`internal/personal/store/store.go:116-173`）。
- 分段投影：稳定系统提示词 + 每段一条上下文消息（环境、项目规则、记忆索引、skills、延迟工具），段内变更以"上下文更新"追加；接近窗口或 `:compact`/空闲时开新段并生成交接（`internal/personal/segment*.go`、`handoff.go`）。
- 上下文压缩与历史折叠：`threshold = window - min(16000, window/5)`（`internal/personal/compact.go:17-75`）；`context.fold_every` 折叠旧步的 reasoning 与超长字符串参数（`internal/personal/fold.go`）。
- 中断回合重放、会话恢复（`running` 残留的接管）、跨目录续接的交互式确认（`internal/personal/replay.go`、`recovery.go`、`internal/cli/cwdcheck.go`）。
- 多轮 input steering：回合运行中 `↳` 可插入消息，Esc/Ctrl-C 只结束本回合（`internal/chat/steering.go`、`internal/personal/steer.go`）。

### 2.2 工具与权限

| 类别 | 工具 |
|---|---|
| 只读 | `file_read`、`file_list`、`file_grep`、`file_find`、`web_fetch`、`web_search`、`skill`、`session_search`、`recall`、`agent`、`suga_manifest` |
| 写 | `file_write`、`file_edit`、`file_remove`、`shell_exec` |
| 状态 | `memory`、`task`、`update_plan`（另有 `tool_search` 用于延迟加载的 MCP） |

- 分层与 fail-closed：observe → output → visibility → approval → registry，无 approver 时一律拒绝（`pkg/agentcore/tools/approval.go:43-91`）。
- 沙箱：文件工具走词法解析 + `os.OpenRoot`，符号链接逃逸被拒（`pkg/agentcore/tools/native/file.go:92-170`、`pkg/agentcore/tools/native/pathresolve.go:77-173`）；`web_fetch` 在 dial 后按真实 IP 拒私有网段，覆盖重定向（`pkg/agentcore/tools/native/fetch.go:38-168`）。
- 输出治理：内联阈值 8000B、原始上限 1MB，超出落 `sessions/<id>/outputs/` 并给 ref + 头尾片段（`pkg/agentcore/tools/output.go:16-28,113-157`）；`minimize_output` 用可覆盖的 YAML 规则裁剪 `go test`/cargo/jest/pytest/maven/gradle/git/rg 等输出（`pkg/agentcore/tools/native/output_rules.go:41-98`，`native/rules/`）。
- 后台任务与 watch：`background: true`（+`every`/`watch_paths`）→ 回合结束通知、`task wait`、watch 只在退出码或输出变化时打扰模型（`internal/personal/tasks.go`）。
- 子 agent：`agent` 工具把自包含任务交给只读子会话，report 回传；独立 session（`kind: agent`）、独立步数/预算/超时（`internal/personal/subagent.go`、`internal/wiring/subagent.go`）。
- MCP：stdio、`initialize`+`tools/list` 发现、按窗口占比自动延迟加载、单调用 120s、消息 8MB 上限（`pkg/agentcore/tools/mcp/client.go`、`pkg/agentcore/tools/visibility.go:117-171`）。
- 危险删除守卫：`shell_exec` 递归删除指向 `/`、`~`、盘根、工作区及其父级时，chat 每次要打 `yes`，run/ACP 直接拒绝（`internal/wiring/danger_guard.go:38-84`）。

### 2.3 工程化保障

- 冒烟：`scripts/ptysmoke/` 20 个 PTY 场景（approval/edit/idle/interrupt/danger/tasks/watch/agent/resume/steer…，`Makefile:133-155`），配合 `fakellm.py` 与 `suga trace --json` 断言事件。
- 评测：`scripts/eval/` 支持 fixture 与 repo 快照任务、多轮 prompts、隐藏验收、`--repeat`、`--probe-prefix`，可与 omp / Claude Code 横向对比（`scripts/eval/README.md`）。
- 可观测：`suga trace`（时间线、`--analyze`、`--json`、`--web`）、`suga stats`、事件级 `turn_context`/`context`/`fold`/`retry`/`task` 记录。

## 3. 可加强点

### P0 · 成本与输出长度控制

1. **从不设置 `max_tokens`。** `GenerateRequest.MaxTokens` 只有 provider 端读取（`pkg/agentcore/llm/openai.go:178`、`claude.go:219`、`responses.go:115`），上层调用方从未赋值；Claude 因此落到默认 4096，OpenAI 分支完全不发。后果是长回答被静默截断（README 的 `truncated` 标记正是在给这件事兜底）。
   *建议*：`providers.<id>.models.<m>` 增加 `max_output_tokens`，并让 wrap-up 逻辑与 `truncated` 语义联动。
2. **主循环没有 token/成本硬预算。** 只有 `MaxSteps`（`pkg/agentcore/runtime.go:125-138`）；`MaxTokens` 类的软限只存在子 agent（`internal/config/agents.go:28`、`internal/personal/subagent.go:208-215`）。
   *建议*：加 `budget.tokens` / `budget.cost_usd`，超限即以 wrap-up 收尾；stream-json 已按调用输出 running usage，判定所需数据已具备。
3. **用量完全依赖 provider 返回。** provider 不报即 0，无本地 tokenizer 兜底（`internal/personal/tokens.go` 仅用于窗口估算）。
   *建议*：预算功能落地前先补一层保守估算，否则预算在小 provider/网关上失效。

### P0 · 检索质量

4. **`session_search` 对中文短词退化为全表扫描。** FTS5 用 `tokenize='trigram'`，任一查询词不足 3 rune 就整条查询改走 `LIKE '%term%'` 并按时间排序（`internal/personal/store/fts.go:69-70,114-141`）。中文 1–2 字词是常态，等于中文检索失去索引与 BM25。
   *建议*：按语言选择分词（bigram/`unicode61` 辅助列），或对短词走独立索引，而非整条退化。
5. **无分页。** `LIMIT` 硬编码、默认 20 / 上限 100、无 OFFSET（`internal/personal/store/fts.go:107-112,162`）；`workspace` scope 还要把全部 session 的 cwd 读进 Go 再拼大 `IN(...)`（`internal/personal/ledger.go:148-167`）。
   *建议*：cursor 分页 + kind/时间过滤 + 把 cwd 条件下推到 SQL。
6. **长期记忆检索是线性扫盘。** files 后端每次 search 全量读所有 `.md` 再做子串 AND（`pkg/agentcore/memory/files/files.go:251-310`），无索引无缓存；写侧预算只算索引行文本，**正文字节不计**（`pkg/agentcore/memory/governed.go:282-298`）。
   *建议*：小规模先加内存索引/缓存；正文纳入预算；条目数增长后再考虑倒排。

### P1 · 审批与安全一致性

7. **审批只有"工具名 + 单次调用"粒度，无规则文件。** 会话内"一直允许"以工具名为键（`internal/wiring/chat_approver.go:113-136`），无原生工具的 allow/deny 规则（只有 MCP 白名单 `tools.auto_allow`/`read_only_allow`，`internal/wiring/cli_mcp.go:33-114`）。
   *建议*：`命令/路径 glob → allow|ask|deny` 规则文件，并把"一直允许"细到目录或命令前缀。
8. **两条绕过通道。** 子 agent 审批全放行（`internal/wiring/subagent.go:116`）；chat 里 MCP 工具只看 glob 白名单、不经人工确认。
   *建议*：把两者收敛到同一套 Approver 策略，至少让子 agent 继承父会话的规则。
9. **shell 没有进程级隔离。** 只有超时 + 进程树杀；Windows job object 仅设 `KILL_ON_JOB_CLOSE`，无内存/CPU/进程数/网络限制（`pkg/agentcore/tools/native/code_exec.go:246-267`、`pkg/agentcore/internal/proctree/proctree_windows.go:24-67`）。危险命令守卫是 best-effort、明确不是安全边界（`pkg/agentcore/tools/native/danger.go:11-29`）。
   *建议*：job object 增加内存/进程数上限，并提供可选网络禁用；文档里继续明确"不是沙箱"。
10. **代理下 SSRF 保护变弱。** 无代理时按 dial 后的真实 IP 校验，经代理时只在请求前按 DNS 校验（`pkg/agentcore/tools/native/fetch.go:66-70`）。
    *建议*：给"经代理"路径加显式告警或禁用开关。

### P1 · chat 交互体验

11. **没有回滚能力。** 写文件与 `shell_exec` 的改动不可撤销（只有 `file_remove` 有 7 天 trash）。全仓无 undo/checkpoint/rewind 实现。
    *建议*：写前快照（`sessions/<id>/snapshots` 或依赖 git）+ `:undo`。
12. **没有 diff 预览。** diff 只在 ACP 侧构造（`internal/acpserver/policy.go:269-343`），终端里工具结果截断到 100 rune（`internal/chat/render.go:27,230-238`），只能事后 `suga trace` 回看。
    *建议*：把 ACP 的 toolDiff 复用到 chat 渲染，并支持展开。
13. **输入编辑基础。** 历史是 100 条内存环、不落盘（`internal/chat/lineedit/history.go:20-32`）；无 `Ctrl+R` 反查；`AutoCompleteCallback` 只在 lineedit 内部引用，**全仓没有任何赋值**，即补全实际未接线（`internal/chat/lineedit/terminal.go:54`、`keys.go:307`）。
    *建议*：历史落盘到 `$SUGA_HOME`、`Ctrl+R` 搜索、把补全接到 `:` 命令与路径。
14. **渲染能力弱于同类。** 代码块不高亮、表格原样输出、无折叠/分页（`internal/output/mdterm.go:139,148`）；布局硬编码且自述待配置化（`internal/chat/layout.go:25-27`）。
15. **命令面薄。** 仅 `:quit/:exit`、`:session`、`:compact`、`:add-dir`、`:dirs`、`:rm-dir`（`internal/chat/chat.go:371-399`、`chat_dirs.go:38-40`）。缺 `:help`、`:model`、`:memory`、`:tasks`、`:usage`。
16. **没有系统级通知。** 无 bell / OSC 9 / 桌面通知（实现代码零命中）；长任务结束只能盯终端。跨终端仅限本机同一 `SUGA_HOME`（`internal/cli/ps.go:42-47`）。

### P2 · 协议与生态

17. **ACP 覆盖不全。** `session/list|resume|close`、`authenticate`、`set_config_option` 一律 `MethodNotFound`（`internal/acpserver/server.go:389-406`）；权限选项只有 allow-once / reject-once（`policy.go:148-161`）；prompt 只接受 text 与 resource_link，无图片/音频内容块（`server.go:374-387`）；不使用客户端 `fs/*` 能力。
18. **MCP 仅 stdio，无认证。** transport 只有 stdio（`tools/mcp/client.go:1-3`，ACP 明确拒绝非 stdio），无 HTTP/SSE；启动发现 30s 总超时；同名工具冲突静默丢弃。
19. **无对外观测导出。** trace 事件只落 SQLite，调试靠 `SUGA_DEBUG`（日志截断 4000 字符），无 OTel/Prometheus 出口。
20. **trace web 偏静态。** 单页 + 两个 JSON API、每次请求全量重建、客户端过滤（`internal/personal/traceweb.go:281-312`、`traceweb.html:205`）；无服务端搜索/导出/时间线缩放/diff 视图。仅绑定 loopback 是有意设计，建议保留。

### P2 · 模型能力

21. **无多模态。** `api.Message` 的 `Content` 只有 string，全仓无 `image_url`/`input_image`（`pkg/agentcore/api/types.go:42-52`）——"贴截图排障"这条高频路径缺失。
22. **无结构化输出与工具参数校验。** 无 `response_format`/`json_schema`；工具参数只做必填存在性与轻量类型强制（`pkg/agentcore/tool_validation.go:15-104`）。
    *建议*：至少为写工具加 schema 校验，减少一轮试错。
23. **能力靠启发式判断。** Claude thinking 预算模式按模型名子串猜（`pkg/agentcore/llm/claude_thinking.go:37-48`）；chat/completions 无显式 prompt cache 控制（仅 Responses 发 `prompt_cache_key`，Claude 的 `cache_control` 默认关闭，`pkg/agentcore/llm/claude.go:71`）。
    *建议*：按模型声明能力（窗口、thinking、缓存能力）而不是名称推断。
24. **fallback 不分错误类型。** `Pool.Generate` 对任意错误都换下一个 provider（`pkg/agentcore/llm/pool.go:63-71`），400 类永久错误也会白跑其余模型；重试策略是全局配置，不能按 provider/model 区分（`internal/config/providers.go:78-80`）。

### P3 · 工具集与数据治理

25. **工具集缺口：** 无 git/commit 专用工具、无 patch/diff 应用、无 LSP/符号检索、`web_fetch` 只支持 GET（无 POST/headers，`pkg/agentcore/tools/native/fetch.go:195-224`）、无浏览器/截图/剪贴板、无 todo 清单工具（`update_plan` 是计划记录）。输出规则覆盖 go test/cargo/jest/pytest/maven/gradle/git/rg，**tsc/eslint/ruff/mypy、go build/vet、docker、make、gh 无规则**。
26. **存储演进能力有限。** 有 `schema_migrations` + 内嵌 SQL，但无 `user_version`、DOWN 不执行（`internal/personal/store/store.go:175-189`）；`entries(type)`、`events(kind)`、`events(turn_id)` 缺索引；`BuildReport` 每次全量拉 events 进内存再聚合（`internal/personal/store/report.go:58-65`）。
27. **无整库备份/恢复。** `suga export` 只导单会话 jsonl；`suga gc` 不覆盖 memory 文件、`memory_meta`、WAL，也不裁剪活跃会话的 events（无 TTL）；spill 输出无配额与压缩（`internal/personal/store/gc.go:26-94`、`internal/personal/spill.go:54-122`）。
28. **并发写同一 db。** WAL + `busy_timeout=15000`，无应用层重试、无 `BEGIN IMMEDIATE`（用"事务首条 UPDATE sessions 抢写锁"规避升级；`internal/personal/store/store.go:65-77`、`internal/personal/store/entries.go:26-33`）；超时即报错。
29. **CLI 输出不统一。** `gc`/`send`/`export` 无 `--json`；`trace`/`stats` 无分页与 kind 过滤；`export` 只有 jsonl 一种格式且无范围过滤（`internal/cli/export.go:28-30`）。

## 4. 落地顺序建议

若只做五件事，按"收益 / 成本"排序：

1. `max_output_tokens` + 回合级 token/成本预算（止住静默截断与失控花费）。
2. 中文可用的 `session_search`（分词 + 分页 + cwd 条件下推）。
3. 写操作快照与 `:undo`，以及 chat 内 diff 预览（自主改动变得可复核、可回退）。
4. 审批规则文件，并收敛子 agent / MCP 两处绕过通道（把安全模型补成一个整的）。
5. chat 输入三件套：历史落盘、`Ctrl+R`、补全接线（`AutoCompleteCallback` 是现成挂点）。

## 5. 未验证与限制

- **FTS5 trigram 的中文行为**依赖 `modernc.org/sqlite` 的实现：本文按代码里的短词退化分支（`minMatchRunes=3`）判断，未用真实库实测，改检索前应先验证。
- 第四路核查（前端与可观测性）在第 30 步上限处收尾，未覆盖 `internal/acpserver/policy.go:180+` 的 tool_call 状态收尾、`internal/output/usage.go`、`scripts/acpprobe` 的实测方法清单；这些不影响主结论，但涉及 ACP 细节的条目宜再核一遍。
- 所有 `文件:行` 是基线 `b586777` 的静态位置，代码变动后会漂移；引用时以符号名为准。
- 本文未衡量"改动收益"的真实数据（如预算能省多少成本），这些应在设计阶段用小规模实验确定。

## 6. 保持现状的设计

以下看似"缺口"，但属于有意选择，不建议为了对齐同类产品而改动：只读默认 + 审批 fail-closed、文件工具 `os.OpenRoot` 而非可配置沙箱、`trace web` 仅绑 loopback、进程诊断只写 `$SUGA_HOME/logs/suga.log`（stdout 留给协议）、危险命令守卫定位为"提醒"而非安全边界。
