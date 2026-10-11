<!-- template_id: plan; template_version: 1.2.0 -->
# 状态、记忆与可扩展核心（第一期） 实施计划

> 状态：Draft 0.3 / 已批准（2026-10-11 用户：“批准计划 0.2，开始执行”；0.3 为实施中用户提出的 T10 补充：“shell_exec 如果返回内容过多，也可以加上标记”）

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-11 | Claude | 初稿 |
| 0.2 | 2026-10-11 | Claude | 按合并评审修订：只读模式记忆写入由工具自身在治理层之前拒绝，wiring 增加 `MemoryReadOnly`，ACP 放行点改为 `Decide`，补 5 种模式 × 污染的表格测试（F1）；ACP 确认路径落到 acpserver 与 `cli/acp.go` 并补测试（F2）；`Rereadable` 只给 `file_read` 且包装层不再截断它，`skill`/`file_list` 走外置（F3）；外置文件名改为哈希、ref 严格校验并用 `os.Root` 限定（F4）；评测与 T17 强制 files 后端、fixture 自成 git 根，不写 gofer memory（F5）；fixture 放 `testdata` 且各带 go.mod（F6）；成本先扣缓存 token（F7）；T10/T13 衔接：快照 hash 预留与按结果元数据置污染（F8）；FTS 不依赖 rowid，回填改在迁移 SQL 内一次完成，删除改由 sessions 触发器（F9）；采纳建议：压缩同步更新内存 metadata 与旧会话首回合补快照、包装层透传错误并实现 Lookup、ref 说明行置顶、WAL 备份、修正检查命令；更正证据行号 |
| 0.3 | 2026-10-11 | Claude | 按用户实施中的要求：T10 增加“输出被外置且工具在 `memory.taint_on_spill`（默认 `[shell_exec]`）中时置污染” |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

## 目标与完成定义

思考模式 RIGOROUS；核心目标：落地设计 0.3 的 W0–W4——评测与成本、底账检索与输出治理、记忆 v1、扩展面、文档；scope freeze：设计 0.3 的范围、D1–D18、Q1–Q6 决议；review budget：一轮合并轴评审（低暴露度，SR1409）；停止条件：评审无 CORE_BLOCKING 且用户批准。

完成定义（对应设计“用户可见结果”1–5）：

1. `go run ./scripts/eval` 在假模型与真实模型下都能跑完全部任务并输出表格与 `results.jsonl`；配置价格后 `suag stats` 成本列非 0。
2. 模型可用 `session_search` / `recall` 检索与取回历史（中文可搜）；删除会话后搜不到、ref 不悬空；`suag trace` 显示每回合 `turn_context`。
3. 任何工具（含 MCP）超过内联阈值的输出写入 `~/.suag/sessions/<sid>/outputs/`，DB 与上下文只有头尾片段与 ref；可按 ref 分段读回与搜索；gc 同步删除目录。
4. `memory` 工具可写四类条目；新会话带记忆索引快照；仓库根 `MEMORY.md`、gofer、`~/.suag` 三种项目后端按优先级生效；污染会话写入需确认（chat 询问、ACP 权限请求、`run` 拒绝）。
5. exec 后端可经配置接入；`suag manifest` 列出能力与后端。
6. 通用验收：`go test -race ./...`、`go vet ./...`、`make smoke-chat` 通过；主机 Windows `go test ./...` 与 staticcheck 通过；分层守卫通过；单文件不超 1000 行（GR104）。

## 范围、排除项与授权

运行目录：Git root `inhere-tools/suag`（容器路径 `/d/work/inhere/my-tools-dev/inhere-tools/suag`）；`AC = pkg/agentcore`。

范围：设计 0.3「范围」W0–W4。排除：设计非目标（分段与交接代替压缩、子 agent、编辑稳健性、检查点、离线整理、前瞻记忆、people、权限档位、向量检索、OTel、配置补丁提案）；与 opencode/pi 的对比评测（设计“继续的条件”）不在本计划实施，完成后作为剩余工作提出。

实施中对设计的两处命名/单位细化（Corrective，不改变行为）：

- 内联阈值沿用现有键 `tools.max_output_bytes`（其文档含义就是“存储的单个工具结果上限”），默认从 30000 改为 8000；新增 `tools.max_spill_bytes`（原文上限，默认 1 MiB）。设计中的 `tools.inline_output_bytes` 不另建。
- “头尾各 2000 字符”按字节实现（各 2000 字节、UTF-8 安全），保证片段不超过内联阈值。
- `--read-only`（及 ACP 只读模式）下 `memory` 只允许 get/search；默认 `run`（未开写工具）允许写记忆——记忆是 suag 自身状态，不是工作区写入（D7、D12）。
- 设计 D16 把 `file_list` 列为可重读，但它没有分页参数；只有 `file_read` 标 `Rereadable`（自身已按行分页并给出 offset 提示，包装层不再截断它），`file_list`、`skill` 走外置。
- 设计“数据”一节的“FTS 行 rowid 等于 entries rowid、打开 store 时分批回填”改为：FTS 使用自身 rowid、以 `session_id`/`seq` 列定位；删除由 `AFTER DELETE ON sessions` 触发器按 `session_id` 清理；存量回填在迁移 SQL 中一次完成。原因：`entries` 为隐式 rowid 表，gc 的 VACUUM 不保证 rowid 不变；迁移内回填避免每次打开（ACP 每回合）的扫描与写库。用户可见行为不变。
- 外置文件名不用 `<turn>-<call>`（call_id 来自模型、可含 `:`、`../`），改为 `sha256(turn_id+call_id)` 前 16 位十六进制；ref 为 `out:<session_id>:<hex>`。

授权：本计划批准即授权按任务本地原子提交（SR1211），以及下列验证动作：经 `gofer job run -p my-tools-dev -a exec --runner local --cwd inhere-tools/suag --sync` 在主机跑 `go test ./...`、staticcheck、`make install` 与 `go run ./scripts/eval`（真实模型，按 Q4 用 `opencode-go/deepseek-v4.1-flash`，评测使用隔离的临时 `SUAG_HOME`，只读复制主机 `~/.suag/config.yaml` 与 `.env`，临时配置强制 `memory.user.backend: files`、`memory.project.backend: files`）；`make install` 前在主机确认 `suag ps` 无运行中的进程后，把 `~/.suag/db/sessions.db` 连同 `-wal`、`-shm` 一起复制备份（新迁移会在主机库上执行）。不授权写入任何真实 gofer memory：评测与 T17 的记忆写入只落在临时目录的 files 后端，用后删除。push、发版不在授权内。

- `host_or_non_offline_action=REQUIRED`

## 输入与批准证据

- 设计：`docs/design/2026-10-10-state-memory-extensibility-design.md` Draft 0.3（1f47b5a，批准记录 ed861f5）；用户原话：“批准设计 0.3，Q5–Q6 按推荐”；此前 Q1–Q4 “待确认按推荐”，Q1 补充“项目下的 MEMORY.md 用仓库根目录”，输出治理“原始输出的大内容不进入 db……进会话目录下的文件，db 只存 meta 信息/前 N 字符等”。
- 设计评审：合并轴评审 NEEDS_REVISION（5 条 CORE_BLOCKING）→ 0.2 全部处理；0.3 的输出治理由本计划评审覆盖。
- 代码事实（2026-10-11 检索）：迁移 `internal/personal/store/store.go:20,105`（embed + `schema_migrations` 账本）；`AppendEntries` `entries.go:15`；`DeleteSession` `sessions.go:119`（外键级联）；`PatchSessionMeta` `sessions.go:129`；`NewSession` `runner.go:203`（规则 meta `:225-228`）；提示词 `rulesPromptBuilder.BuildSystemPrompt` `runner.go:711`；压缩落点 `compact.go:222`；观测 `observe.go:176`（工具）、`:97-100`（模型 cost）；`observeScope{sessionID,turnID}` 经 ctx 传递（`observe.go:41`）；工具组装 `internal/wiring/runtime.go:187-197`、`personal.New` 包 observe（`runner.go:150-151`）；`native.Env` `catalog.go:12`、`BuildTools` `:95`；`tools.Meta` `meta.go:21`；MCP 结果 `mcp/client.go:393,403`、凭据剥离 `mcp/env.go:15,34`；`RedactSecretValues` 调用方 10 处（chat、render、streamjson、runner、transcript、chat_approver）；chat 审批 `chat_approver.go:56`、ACP 审批 `policy.go:113`（`Decide`，只读拒绝 `:137`、`RequestPermission` `:141`；`BeforeTool` `:79` 只发公告）；ACP 打开 runtime 只传 `AllowWrite: !readOnly`（`internal/cli/acp.go:45`），`TurnGate` 在 `acpserver/server.go:46`；`run` 的审批器 `cliMCPGate` 从不询问；`ReportUsage` 说明 `InputTokens` 已含缓存读写（`personal/usage.go:78-84`）；`skill` 参数只有 name/query/file（`skill.go:52-58`）；gc 执行 VACUUM（`store/gc.go:71`）；`entries.json` 为 `api.Message` 默认字段名（`Content`、`ToolResult.Content`）；CLI 注册 `internal/cli/root.go:47-56`；配置 `internal/config/providers.go:20`（`ModelConfig`）、`paths.go:30`（`ResolveSuagHome`）；transcripts 路径在 `chat.go:115`、`cli/gc.go:89` 各自拼接；`api.Memory` 非测试引用 `api/memory.go`、`types.go:76`、`runtime.go:20,41,59`、`runtime_helpers.go:52,87,374`、`doc.go:26`。
- 基线：计划提交时 HEAD 为本计划的提交（0.1 为 6f96655）；Go 1.26.0。
- 计划评审：合并轴评审 NEEDS_REVISION（9 条 CORE_BLOCKING）→ 0.2 全部处理并采纳 5 条建议。

## Capability Discovery

### Capability decisions

| capability_id | required_capability | searched_candidates | direct_reuse | thin_adapter_or_owner_extension | decision | proven_gap | duplication_and_lifecycle_risk |
|---|---|---|---|---|---|---|---|
| CAP-01 | 成本计算 | provider 返回的 `cost_usd`（`observe.go:100`）、`ModelConfig` | 部分 | `ModelConfig` 增加 `Price`，观测层在返回值为 0 时按价格计算 | OWNER_EXTENSION | 无价格表 | 一处计算，provider 返回值优先 |
| CAP-02 | 评测运行器 | `make smoke-chat`（PTY 冒烟）、`scripts/ptysmoke/fakellm.py`、`suag stats --json` | 部分：fakellm 与 stats 直接复用 | 新建 `scripts/eval`（见 NEW-06） | MINIMAL_NEW_MODULE | 冒烟只验交互，不汇总任务成败与用量 | 不进发布二进制；与 ptysmoke 共用 fakellm |
| CAP-03 | 全文检索 | SQLite LIKE、FTS5（已编入） | 是 | `store` 新迁移 + `AppendEntries` 同事务写入 | OWNER_EXTENSION | none | 外置文件不入 FTS，避免两份大文本 |
| CAP-04 | 输出治理包装层 | `native.TruncateHeadTail`、各工具 `maxOut`、上下文 `shrink` 阶梯 | 部分 | `AC/tools` 增加 `Output` 包装（与 Visibility/Approval 同包同构）+ `Spiller` 接口；personal 提供会话目录实现 | OWNER_EXTENSION | MCP 无上限、截断丢原文 | 工具内部改用原文上限，删除各自的内联截断，避免双重截断 |
| CAP-05 | 检索工具 | `native.ToolSearcher`/`PlanSink` 模式 | 是：消费方窄接口 | `native` 增加 `LedgerSearcher` 与两个工具；personal 实现 | OWNER_EXTENSION | none | 不建 `AC/recall` 包（D2） |
| CAP-06 | 污染与回合快照 | `observedTools.ExecuteTool`、`PatchSessionMeta`、events | 是 | observe 写 session meta 与 `turn_context` 事件 | OWNER_EXTENSION | none | 标记只写一次 |
| CAP-07 | 记忆接口、治理与路由 | `api.Memory`（语义不符，改名 History）、`AC/tools` 包装层模式、gofer memory | 否 | 新建 `AC/memory`（NEW-01） | MINIMAL_NEW_MODULE | 无长期记忆抽象；需 ≥2 后端（D2） | 接口小、三个实现 |
| CAP-13 | 内置记忆后端（MEMORY.md 索引 + 一事一文件，含仓库模式） | 规则加载 `rules.go`、`AC/storage/files` | 否 | 新建 `AC/memory/files`（NEW-02） | MINIMAL_NEW_MODULE | 规则是只读拼接，storage/files 按 runID 存运行状态 | 默认后端，零外部依赖 |
| CAP-08 | gofer 后端 | `gofer memory` CLI | 是：CLI 直接调用 | 应用层适配器（NEW-05） | MINIMAL_NEW_MODULE | agentcore 不得认识 gofer | 只管 `suag` 标签条目 |
| CAP-09 | 脱敏下沉 | `personal.RedactSecretValues` | 否：在 internal，AC 不能依赖 | 移到 `AC/redact`（NEW-03），personal 调用方改用 | MINIMAL_NEW_MODULE | 分层守卫 | 移动不复制，旧函数删除 |
| CAP-10 | exec 协议与凭据剥离 | hooks 协议（`internal/hooks`）、`mcp/env.go` | 部分 | `AC/extproc`（NEW-04）承接 `mcp/env.go`，MCP 改用 | MINIMAL_NEW_MODULE | hooks 在 internal 且语义是事件通知 | 剥离逻辑只留一份 |
| CAP-11 | 能力清单 | `internal/wiring/capabilities.go`（工具组装）、`suag stats` | 部分 | `internal/cli/manifest.go` + native 工具，数据由 wiring 提供 | OWNER_EXTENSION | none | 命名避开 capabilities（D15） |
| CAP-12 | 确认交互 | `tools.Asker`/`BindableAsker`、ACP `RequestPermission`、chat 审批 | 是 | `memory.Policy.Confirm` 由 wiring 适配到 Asker（chat）与 ACP 权限请求 | DIRECT_REUSE | none | 不新增交互机制 |

### New module candidates

| candidate_id | capability_id | proposed_module | searched_candidates | direct_reuse_gap | thin_adapter_or_owner_extension_gap | proven_gap | unique_owner_and_lifecycle | deletion_or_merge_handling |
|---|---|---|---|---|---|---|---|---|
| NEW-01 | CAP-07 | `AC/memory`（类型、Store、可选接口、Governed、Router） | `api.Memory`、`AC/tools` | `api.Memory` 是历史组装，语义不同 | 放进 `AC/tools` 会把记忆与工具层耦合 | 无长期记忆抽象 | AC 能力层，suag 与嵌入方共用 | 若后端只剩一个，合并回 files 包 |
| NEW-02 | CAP-13 | `AC/memory/files` | 规则加载、`storage/files` | 规则是只读拼接；storage/files 按 runID 存运行状态 | 不同数据模型 | 需 MEMORY.md 索引 + 一事一文件 | 内置默认后端 | 无 |
| NEW-03 | CAP-09 | `AC/redact` | `personal/transcript.go` | 在 internal | 只能移动 | AC 治理层与工具需要 | 移动后唯一实现 | 删除 personal 中的旧函数 |
| NEW-04 | CAP-10 | `AC/extproc` | `internal/hooks`、`mcp/env.go` | hooks 在 internal；env.go 未导出且只服务 MCP | 导出 mcp 内部函数会让 memory 依赖 mcp | 外部后端需要统一协议与剥离 | AC 能力层；MCP 与 exec 后端共用 | 删除 `mcp/env.go` 中被移走的函数 |
| NEW-05 | CAP-08 | `internal/adapters/gofermem` | `internal/wiring` | 放 wiring 会混入组装代码 | 独立小包更易替换与测试 | AC 不得认识 gofer | 应用层；可整体删除而不影响内置后端 | 无 |
| NEW-06 | CAP-02 | `scripts/eval`（Go `package main` + 任务目录） | `scripts/ptysmoke`、`make smoke-chat` | ptysmoke 只驱动终端交互 | 加进 ptysmoke 会混淆冒烟与评测 | 需要任务成败与用量汇总 | 开发工具，不发布 | 无 |

### Rejected new tools

| rejected_candidate | capability_id | deletion_test_and_reason |
|---|---|---|
| `AC/recall` 包 | CAP-05 | 只有一个真实实现，删掉它只需在 native 声明窄接口（D2） |
| `suag eval` 子命令 | CAP-02 | 删掉后用户无损失；评测是开发工具，放 scripts（D10） |
| 外置输出写入 FTS | CAP-03 | 与“大内容不进 DB”冲突；单文件逐行扫描足够（D17） |

## 前置检查与 fail-closed 条件

- workspace baseline：Git root `inhere-tools/suag`，branch `main`，HEAD 为计划最终提交（实施前以 T0 实测为准）；`git status --short` 中 `.gofer/tracker/issues.jsonl`、`docs/plans/2026-10-06-personal-agent-redesign-plan.md` 为他人改动，全程不暂存、不修改（Ownership Conflict 时停止）；Go 1.26；主机 gofer 可用。
- 测试隔离：测试的 `SUAG_HOME`/`HOME` 指向临时目录，不触碰真实 `~/.suag`；评测同样使用临时 `SUAG_HOME`。
- 每个任务：改动 → `gofmt -l .` 为空 → `go build ./... && go vet ./...` → 全量 `go test -race ./... > tmp/sm-tN.log 2>&1; echo $?` → 提交；以退出码判定。终端交互改动（T7、T13）另跑 `make smoke-chat`。
- fail-closed：全量回归即停止修复；分层守卫失败或出现 import cycle 即停止并回到设计；单文件超 1000 行先拆分；实施中发现的新路径按计划合同四类分类记录，Semantic Amendment 停止并回到计划 Gate。
- 非交互 shell 中 `rg` 带显式路径并 `< /dev/null`；后台提交前等待其他后台提交完成。

## 波次与依赖

| 波次 | 任务 | 依赖 |
|---|---|---|
| 0 | T0 基线 | — |
| 1 基础 | T1 History 改名 → T2 redact 下沉 → T3 extproc 与凭据剥离 → T4 工具元数据扩展 | T0，顺序 |
| 2 W0 | T5 价格与成本 → T6 评测运行器 | T1–T4 |
| 3 W1 | T7 输出治理与会话目录 → T8 FTS 迁移与回填 → T9 检索工具 → T10 污染与回合快照 | T6 |
| 4 W2 | T11 memory 包与 files 后端 → T12 gofer 后端与来源旁表 → T13 memory 工具、快照注入与确认 | T10 |
| 5 W3 | T14 exec 后端 → T15 `suag manifest` | T13 |
| 6 W4 | T16 文档 → T17 全量与真实验证 | T15 |

单 Agent 顺序执行；进度记在新建的 gofer plan（批准执行时创建，含 T0–T17 全部 todo）。

## 任务

### T0 基线

- 文件: 无
- 动作: 记录 HEAD 与 `git status --short`；跑全量测试与冒烟。
- 验证: `go test -race ./... > tmp/sm-t0.log 2>&1; echo $?`；`make smoke-chat`。
- 完成标准: 全绿，记入 gofer plan 备注。
- 依赖: —

### T1 `api.Memory` 改名为 `api.History`（D13）

- 文件: `AC/api/memory.go`→`history.go`（`History`、`HistoryError`、`ErrHistoryStateConflict`）；`AC/types.go:76`、`AC/errors.go`、`AC/api/errors.go`；`AC/runtime.go:20,41,59`（`Config.History`）；`AC/runtime_helpers.go:52,87,374`；`AC/doc.go:26`；`AC/runtime_memory_test.go`→`runtime_history_test.go`
- 动作: 机械改名；不动 `MemoryStorage`/`NewMemoryStorage`；改完 `rg -n "Memory" pkg/agentcore --type go < /dev/null` 逐条确认剩余均为内存存储。
- 验证: 全量测试。
- 完成标准: 测试通过；`rg -n "api\.Memory\b|cfg\.Memory\b|Memory:\s|MemoryError|ErrMemoryStateConflict|defaultMemory" pkg internal --type go < /dev/null` 为空。
- 依赖: T0

### T2 脱敏下沉 `AC/redact`（D14）

- 文件: 新建 `AC/redact/redact.go`(+test)：`Secrets(string) string`（原 `RedactSecretValues` 逻辑与测试移入）、`Clean(string) string`（去隐形字符、HTML 注释、伪装标签后再 `Secrets`）；删除 `internal/personal/transcript.go:143` 的函数；10 处调用方改用 `redact.Secrets`
- 动作: 移动测试用例，新增 `Clean` 表格测试（零宽字符、`<!-- -->`、`<system-reminder>` 样式标签、密钥）。
- 验证: 全量测试；`go test ./pkg/agentcore/redact/ -run . -v`。
- 完成标准: `rg -n RedactSecretValues --type go . < /dev/null` 为空；守卫通过。
- 依赖: T1

### T3 `AC/extproc`：exec 协议 v1 与凭据剥离（D11、D14）

- 文件: 新建 `AC/extproc/{env.go,call.go}`(+tests)：`StripCredentials(parent, configured []string) []string`（自 `mcp/env.go:15,34` 移入）、`Call(ctx, Spec{Command, Args, Env, Timeout}, req any, resp any) error`（stdin 一行 JSON、stdout 一行 JSON、默认 10s、非 0 退出或非法 JSON 报错、stderr 尾部进错误信息）；`AC/tools/mcp/env.go` 删除被移函数，`client.go:90` 改用 `extproc.StripCredentials`
- 动作: 实现与测试（用 `os.Args[0]` 自身作为测试子进程，覆盖成功、超时、非法输出、凭据被剥离）。
- 验证: 全量测试。
- 完成标准: MCP 测试不变通过；`extproc` 测试覆盖四种情形。
- 依赖: T2

### T4 工具元数据扩展（D7、D16）

- 文件: `AC/tools/meta.go`：`Meta` 增加 `External`（结果来自外部，触发污染）、`Rereadable`（工具自身分页，包装层不截断不外置）、`AgentState`（只改 agent 自身状态，写入权限由工具自己按模式判定）；`MetaOf` 对 MCP 工具返回 `External=true`、`AgentState=false`（MCP 不能借此绕过审批）；`AC/tools/native/*.go` 的 `ToolMeta`：`web_fetch`、`web_search` 置 `External`，仅 `file_read` 置 `Rereadable`；`internal/wiring/chat_approver.go:56` 对 `AgentState` 工具直接放行；`internal/acpserver/policy.go:113`（`Decide`）在 `:137` 只读拒绝分支之前对 `AgentState` 放行；`internal/wiring/approval_table_test.go` 增加 `AgentState` 行（含“同名 MCP 工具声明 AgentState 仍需审批”）
- 动作: 实现与表格测试；本任务尚无 `AgentState` 工具，用测试桩验证。只读模式下的写拒绝由 T13 的 memory 工具自己实现。
- 验证: 全量测试；`go test ./internal/wiring/ ./internal/acpserver/ -run 'Approval|Policy' -v`。
- 完成标准: 表格测试中原有行结果不变，新行符合预期。
- 依赖: T3

### T5 模型价格与成本（W0、D10）

- 文件: `internal/config/providers.go`（`ModelConfig.Price{Input, Output, CacheRead, CacheWrite}`，美元/百万 token，解析于 `:134` 附近）；`internal/personal/observe.go:97-100`（`cost_usd` 为 0 且有价格时计算）；价格查找经 `Config` 注入 personal（不在 personal 读全局配置）；`config/config.yaml` 示例
- 动作: 实现与单测：有返回值时以返回值为准；否则非缓存输入 = `InputTokens - CacheReadTokens - CacheWriteTokens`（与 `ReportUsage` 同一归一化，下限 0）按 `Input` 单价，缓存读、写、输出各按单价。
- 验证: 全量测试。
- 完成标准: 测试中（含缓存命中的用例）配置价格后 `store` 汇总的 `CostUSD` 等于手算值。
- 依赖: T4

### T6 评测运行器 `scripts/eval`（W0、Q4）

- 文件: 新建 `scripts/eval/main.go`、`scripts/eval/README.md`、`scripts/eval/testdata/tasks/<name>/{task.yaml,fixture/}`（首批 6 个：定位并修复失败测试、跨文件改名、读大日志找错误、按说明改配置、只读调研回答问题、长输出命令后基于结果作答）；`testdata` 不参与 `./...`，每个 fixture 根目录另带 `go.mod`（复制出去后成为独立模块，不进入主模块的 build/vet/test）
- 动作: `task.yaml` 含 `prompt`、`mode`（read-only/allow-write）、`checks`（`file_contains`、`file_equals`、`answer_contains`，不用 shell，保证 Windows 可跑）；运行器为每个任务复制 fixture 到 `tmp/eval/<run>/<task>/` 并在其中 `git init`（自成 git 根，项目记忆后端探测不到上层仓库的 `.gofer/tracker`），以临时 `SUAG_HOME` 运行 `suag run --session-id eval-<run>-<task>`；临时 `config.yaml` 强制 `memory.user.backend: files`、`memory.project.backend: files`（T11 前该键被忽略）；`--fake` 时启动 `fakellm.py`（仅做管线验证），否则合并 `--config-from` 目录的 `config.yaml`/`.env`；判定 checks；读 `suag stats --session <id> --json` 汇总调用次数、token、缓存命中、成本，工具输出字节字段在 T7 后才有值、之前输出空值不报错；输出表格与 `results.jsonl`
- 验证: `go vet ./scripts/...`；`go build -o tmp/smoke/suag ./cmd/suag && go run ./scripts/eval --fake --bin tmp/smoke/suag` 退出 0；随后全量测试仍通过（证明 fixture 不进入主模块）。
- 完成标准: 假模型下全部任务跑完（成败不要求），结果文件字段齐全。
- 依赖: T5

### T7 输出治理与会话目录（W1、D16、D17）

- 文件: 新建 `AC/tools/output.go`(+test)：`Spiller` 接口 `Spill(ctx, call api.ToolCall, content string) (ref string, err error)`、`NewOutput(inner, OutputConfig{InlineBytes, Spiller})`——`Rereadable` 工具原样透传（自身按 `InlineBytes` 分页）；其余超阈值时调用 `Spill`，返回内容为**首行说明**（`[输出 N 字节 / M 行已保存为 ref out:<sid>:<hex>；recall(ref, offset, limit) 分段读，session_search(query, ref) 在其中搜]`）+ 头尾各 2000 字节，结果 `Metadata` 记 `{ref, bytes, lines, sha256}`；说明行置顶，保证上下文截断阶梯最小档（2000）仍保留 ref；error 与 `IsError` 结果原样透传（拒绝计数 `observe.go:186` 不受影响）；实现 `tools.Lookup`；无 `Spiller` 时退化为头尾截断；`internal/config/paths.go` 增加 `ResolveSessionsDir()`；新建 `internal/personal/spill.go`：文件名 `sha256(turn_id+call_id)` 前 16 位十六进制，写入 `sessions/<sid>/outputs/<hex>.txt`，经 `os.Root` 打开会话 outputs 目录后写入，写前 `redact.Secrets`，目录权限沿用运行目录保护；`internal/personal/runner.go:151` 改为 `observeTools(tools.NewOutput(cfg.Tools, …))`；`AC/tools/native/catalog.go` 的 `Env` 改为 `InlineBytes`（`file_read` 自用）与 `RawBytes`（其余工具的原文上限）；`code_exec.go:211`、`fetch.go:205`、`grep.go:120`、`find.go:111`、`file_list.go:97`、`skill.go:105` 改用原文上限；`AC/tools/mcp/client.go:393,403` 截到原文上限；`internal/wiring/capabilities.go:87`（`tools.max_output_bytes` 默认 8000、新增 `tools.max_spill_bytes` 默认 1 MiB）；`observe.go:182` attrs 增加 `raw_bytes`、`inline_bytes`、`spilled`；`internal/cli/gc.go:85-89` 删除会话目录（与删 transcripts 同处；store 不知道 SUAG_HOME）；`internal/cli/export.go` 增加 `--with-outputs`
- 动作: 实现与测试：MCP 大结果被外置、`file_read` 不截断不外置、`skill` 超阈值被外置、无 Spiller 退化、错误透传、元数据完整、call_id 含 `:`/`../` 时文件名安全、gc 后目录消失、导出带文件、上下文截断到 2000 档后 ref 仍在。
- 验证: 全量测试；`make smoke-chat`。
- 完成标准: 测试通过；手工用 fakellm 返回 50 KB 工具输出时，`entries` 中该结果不超过 8000 字节且文件存在。
- 依赖: T6

### T8 FTS 迁移、删除同步与回填（W1、D8）

- 文件: 新建 `internal/personal/store/migrations/<ts>-add-ledger-fts-memory-meta.sql`（GR105 命名）：`entries_fts(session_id UNINDEXED, seq UNINDEXED, role UNINDEXED, text, tokenize='trigram')`（FTS 自身 rowid）；`AFTER DELETE ON sessions` 触发器 `DELETE FROM entries_fts WHERE session_id = old.id`；存量回填 `INSERT INTO entries_fts SELECT … FROM entries`，文本表达式为 `json_extract(json,'$.Content')` 与 `json_extract(json,'$.ToolResult.Content')` 拼接（实施前先用一条真实 entries 行确认字段名，不符即停止并按 Operational Discovery 记录）；`memory_meta` 表；`store/entries.go:15`（`AppendEntries` 同事务 `INSERT INTO entries_fts SELECT <同一表达式> FROM entries WHERE session_id=? AND seq=?`，表达式定义一次供迁移与写入共用的说明写在 `store/fts.go`）
- 动作: 实现与测试：中文 ≥3 字命中、2 字走 LIKE、删会话（`DeleteSession`）与 gc（含 VACUUM）后搜不到且其余会话命中与 `session_id/seq` 定位正确、迁移在含历史数据的库上执行后可检索、迁移只执行一次（`schema_migrations`）。
- 验证: 全量测试；`go test ./internal/personal/store/ -run FTS -race -v`。
- 完成标准: 上述用例全部通过；`Open` 不新增任何回填扫描。
- 依赖: T7

### T9 `session_search` / `recall`（W1、D8、D18）

- 文件: `AC/tools/native/ledger.go`（`LedgerSearcher` 窄接口、两个工具，`ToolMeta{Category: search, ReadOnly: true, External: true}`）；`internal/personal/ledger.go`（实现：scope current/workspace/all，workspace 按 `sessions.cwd` 前缀；ref `<sid>/<seq>` 读消息窗口、`out:<sid>:<hex>` 读外置文件行窗口——ref 先用正则 `^out:sess_[0-9a-z]+:[0-9a-f]{16}$`（会话 ID 格式以 `personal/ids.go` 为准）校验，再经 `os.Root` 打开该会话 outputs 目录读取；`session_search(query, ref)` 对单个外置文件逐行扫描；查询逐词转义、按 rune 数决定 MATCH/LIKE；结果过 `redact.Clean`）；`internal/wiring/runtime.go:179-186` 注册
- 动作: 实现与测试（含 FTS 语法字符、跨工作区隔离、ref 不存在、ref 含 `../`、`:`、绝对路径时被拒、跨会话读取外置输出）。
- 验证: 全量测试。
- 完成标准: 测试通过；工具出现在只读注册清单中。
- 依赖: T8

### T10 污染标记与 `turn_context` 事件（W1、D7、D9）

- 文件: `internal/personal/observe.go:176-194`（工具结果返回后，`MetaOf(tool).External` 为真、或结果 `Metadata["external"] == true`（供 T13 的 gofer/exec 读结果按次声明）、或工具在 `memory.taint_tools` 中、或输出被外置（`OutputInfo.Ref` 非空）且工具在 `memory.taint_on_spill`（默认 `[shell_exec]`，空列表关闭）中，且 session meta 无 `tainted_at` 时经 `PatchSessionMeta` 写入）；`internal/personal/runner.go`（每回合开始记录 `turn_context`：model、provider、mode、工具名列表 hash 与数量、规则 hash、`memory_hash`（本任务取 session meta 中的值，T13 前为空）、是否污染）；`internal/cli/trace.go` 显示
- 动作: 实现与测试（跨回合仍为污染；结果元数据声明 external 时置位；`shell_exec` 默认不置位、配置后置位；`memory_hash` 为空时字段存在且为空）。
- 验证: 全量测试。
- 完成标准: 测试通过；`suag trace` 输出含 turn_context 行。
- 依赖: T9

### T11 `AC/memory` 与 `AC/memory/files`（W2、D3–D6）

- 文件: 新建 `AC/memory/{types.go,store.go,governed.go,router.go}`(+tests)、`AC/memory/files/{files.go,index.go}`(+tests)
- 动作: 类型（Kind 四类、Scope、Entry、Op、Provenance、Hit）；`Store` 接口与可选 `ProvenanceStore`；`Governed`：顺序为污染确认 → `Sanitize` → 墓碑指纹 → 预算（200 行/16 KB，超限报错）→ 后端 → `Observe` 事件，后端缺来源/墓碑能力时用注入的 `ProvenanceStore`；`Router` 按 Scope 分派；files 后端两种布局：目录模式（`<dir>/MEMORY.md` + `<dir>/<id>.md`）与仓库模式（`<repo>/MEMORY.md` + `<repo>/.suag/memory/<id>.md`），索引行格式 `- [标题](相对路径) — kind`，不符合格式的手写行原样保留、只读；搜索为逐文件匹配。
- 验证: 全量测试（含：污染时不调用后端、超预算报错、墓碑拒绝、手写行保留、两种布局读写往返）。
- 完成标准: 测试通过；守卫通过（memory 包只依赖标准库、api、redact）。
- 依赖: T10

### T12 gofer 后端与来源旁表（W2、D11、Q2、Q3）

- 文件: 新建 `internal/adapters/gofermem/gofermem.go`(+test)：调用 `gofer memory ls --json`、`show`、`set`（`--summary`、`--kind note`、`--tag suag,<kind>`、`--source suag:<session>`）、`rm`，只列出/修改/删除带 `suag` 标签的条目，可执行文件路径可注入以便测试；`internal/personal/store/memory_meta.go`（`ProvenanceStore` 实现）；`internal/wiring/memory.go`（项目后端选择：配置 > 仓库根 `MEMORY.md` > `.gofer/tracker` > `~/.suag/projects/<key>/memory/`）
- 动作: 实现与测试（用假 `gofer` 可执行脚本；不调用真实 gofer）。
- 验证: 全量测试。
- 完成标准: 测试通过；选择顺序表格测试覆盖四种情形。
- 依赖: T11

### T13 `memory` 工具、快照注入与确认（W2、D5–D7）

- 文件: `AC/tools/native/memory.go`（工具：add/replace/remove/get/search，说明含四类与不记清单、gofer note 会进入所有 agent 开场的提示；`ToolMeta{AgentState: true}`；构造参数 `Writable bool`——为 false 时写操作在进入 Governed 之前直接返回拒绝及原因；get/search 结果来自 gofer/exec 后端时在结果 `Metadata` 置 `external: true`，由 T10 的观测层置污染）；`internal/wiring/runtime.go`（`Options.MemoryReadOnly`（零值即可写，chat 无需改动）；在 `:179` 与 update_plan 同处注册 memory 工具）；`internal/cli/run.go`（CLI 传 `ReadOnly`）、`internal/cli/acp.go:33-45`（ACP 传 `readOnly`，并把确认回调传入 wiring）；`internal/acpserver/server.go:46`（`TurnGate` 增加记忆确认方法，经 `RequestPermission` 发出）与 `internal/acpserver/policy.go`；`internal/personal/runner.go:203-229`（新建会话写 `memory_index_content`/`memory_index_hash`；已有会话在首回合发现缺少快照时补写一次）、`compact.go:222` 之后刷新快照并同步更新内存中的 `rulesPromptBuilder.metadata`（`runner.go:362`），`runner.go:711` 在规则后注入记忆索引，`turn_context` 的 `memory_hash` 取快照 hash；`internal/wiring/memory.go`（`Policy`：`Tainted` 读 session meta；`Confirm` 在 chat 经 `tools.Asker` 询问 `[y]是 [N]否 [a]本会话不再问`、在 ACP 经 `TurnGate`、在 `run` 返回拒绝并提示）；`internal/chat/render.go`（回合结束显示“已记住：…”）；配置键 `memory.write`、`memory.write_tainted`、`memory.taint_tools`、`memory.project.backend`、`memory.user.backend`
- 动作: 实现与测试：表格测试覆盖 chat / `run` / `run --read-only` / ACP default / ACP read_only × 未污染 / 已污染 共 10 种组合的写入结果（只读两种一律拒绝写、允许读；其余未污染直接写，已污染按确认结果）；acpserver 测试覆盖确认的允许、拒绝、取消；`scripts/ptysmoke/chat_approval.py` 补“污染后写记忆询问”检查。
- 验证: 全量测试；`make smoke-chat`。
- 完成标准: 新会话系统提示词含记忆索引；同一会话内写入后提示词不变（缓存不破）；压缩后当回合即使用新索引；旧会话首回合补快照；10 种组合与 ACP 三例全部符合预期。
- 依赖: T12

### T14 exec 记忆后端（W3、D11）

- 文件: 新建 `AC/memory/execstore/execstore.go`(+test)：基于 `extproc.Call`，op 为 index/get/write/search；`internal/wiring/memory.go` 支持 `memory.<scope>.backend: exec` 与 `command`/`args`
- 动作: 实现与测试（测试用自身二进制模拟外部后端；输出过治理层清理）。
- 验证: 全量测试。
- 完成标准: 配置 exec 后端后 `memory` 工具读写经外部进程完成。
- 依赖: T13

### T15 `suag manifest`（W3、D15）

- 文件: 新建 `internal/cli/manifest.go`（文本与 `--json`），`internal/cli/root.go:47-56` 注册；`internal/wiring/manifest.go`（汇总：provider/模型、工具列表与元数据、MCP 服务器、skills 根、记忆各范围后端与是否可替换、输出治理阈值）；`AC/tools/native/manifest.go`（只读工具 `suag_manifest`，数据由 wiring 注入）
- 动作: 实现与测试。
- 验证: 全量测试；`go run ./cmd/suag manifest --json`（临时 `SUAG_HOME`）可解析。
- 完成标准: 输出包含上述各项。
- 依赖: T14

### T16 文档（W4）

- 文件: `README.md`、`README.zh-CN.md`（记忆、检索、输出治理、manifest、配置表）；`pkg/agentcore/README.md`（memory、redact、extproc、Output、History）；`config/config.yaml`（price、max_output_bytes、max_spill_bytes、memory.*）；`docs/usage/gofer-personal-agent.md` 工具一节；`AGENTS.md` 入口；`CHANGELOG.md` 未发布条目（含 `api.Memory` 改名、`tools.max_output_bytes` 默认值变化等不兼容项）
- 动作: 更新文档；`rg` 检查无 `api.Memory`、旧默认值残留。
- 验证: 文档中的命令在临时 `SUAG_HOME` 下逐条可运行。
- 完成标准: 文档与实现一致。
- 依赖: T15

### T17 全量与真实验证

- 文件: 无（发现问题时回到对应任务修复）
- 动作: 容器全量 race、vet、`make smoke-chat`；主机 `go test ./...` 与 staticcheck；主机确认 `suag ps` 为空后复制 `sessions.db` 与 `-wal`、`-shm` 备份，再 `make install`，`suag version` 等于 HEAD；主机运行 `go run ./scripts/eval --model opencode-go/deepseek-v4.1-flash --config-from <主机 ~/.suag>`；真实会话在主机的临时 git 仓库（`tmp/eval/real-<ts>/`，`git init`，临时 `SUAG_HOME`，配置强制 files 后端）中用 `suag run --allow-write` 跑一次：触发一次大输出外置、一次 `session_search`、一次 `memory` 写入，`suag trace` 核对事件，结束后删除该目录与临时 `SUAG_HOME`。
- 验证: 各命令退出码与输出记录到 gofer plan 备注；eval 结果文件路径记入。
- 完成标准: 全部通过；真实模型评测完成并记录基线数字（成功率、平均 token、缓存命中、每回合工具输出字节）。
- 依赖: T16

## 回滚与恢复

- 每个任务独立提交，可按任务 `git revert`；T1、T2、T3 为改名/移动，回滚即整体还原。
- 数据：迁移只新增表与触发器；旧版本二进制可继续打开新库（忽略未知表）。主机安装前的 `sessions.db` 备份可直接恢复。会话目录与记忆文件是纯文件，删除即清除。
- 主机安装回滚：按记录的上一个提交重新 `make install`，或恢复 `suag.exe.old`。
- 中断恢复：按 gofer plan 中最后完成的任务与 HEAD 继续（SR1407）。

## 人工 Gate

- 计划批准（本文件）。
- 当前执行请求：批准后另需一句明确的开始执行。
- 外部动作 Gate：push 与发版不在本计划授权内，完成后单独请求。
- 实施中出现 Semantic Amendment（改变接口语义、数据结构、确认规则或验收）时停止，回到计划 Gate。

## 可追溯性

| 设计条目 | 任务 | 验证 |
|---|---|---|
| D1 总纲 | 全部 | T17 |
| D2 两个实现才抽象 | T9（窄接口）、T11/T12/T14（三个后端） | T11–T14 测试 |
| D3 治理包装层 | T11 | Governed 测试 |
| D4 文件为事实源 + 旁表 | T11、T12 | 往返与来源测试 |
| D5 四类 + 不记清单 | T11、T13 | 工具说明与类型测试 |
| D6 按会话冻结快照 | T13 | 提示词不变/压缩刷新测试 |
| D7 写入默认生效、污染确认 | T4、T10、T13 | 污染与确认测试、冒烟 |
| D8 FTS trigram 检索 | T8、T9 | FTS 测试 |
| D9 turn_context | T10 | trace 测试 |
| D10 成本与评测 | T5、T6 | 成本单测、eval 运行 |
| D11 exec 协议、gofer 在应用层 | T3、T12、T14 | extproc/gofermem/execstore 测试 |
| D12 安全底线 | T2、T3、T7、T10、T13 | 脱敏、剥离、污染测试 |
| D13 History 改名 | T1 | rg 检查 |
| D14 下沉 redact/extproc | T2、T3 | 守卫 |
| D15 manifest | T15 | CLI 测试 |
| D16–D18 输出治理与统一 ref | T4、T7、T9 | 外置与回读测试 |
| Q1/Q2/Q5 项目后端顺序与仓库布局（CAP-13） | T11、T12 | 选择顺序表格测试 |
| Q3 写 gofer memory | T12 | 假 gofer 测试 |
| Q4 评测模型 | T6、T17 | 真实评测 |
| Q6 阈值 | T7 | 外置测试 |

## 完成 Gate 与剩余工作

完成条件：完成定义 1–6 全部满足，T17 记录齐全，gofer plan 全部 todo 完成。完成后提出（不在本计划内）：push 与是否发版；与 opencode/pi 的同模型对比评测（设计“继续的条件”）；下一期设计（分段与交接、只读子 agent、权限与信任档位、工具稳健性）。
