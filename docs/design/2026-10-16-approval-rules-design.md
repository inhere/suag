<!-- template_id: design; template_version: 1.1.1 -->
# 审批规则文件（命令/路径 glob → allow|ask|deny）与更细的“一直允许” 设计

> 状态：Draft 0.2（按独立评审 r1 修订，待用户审阅）

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-16 | suga（gofer job） | 初稿：规则模型（来源、合并、优先）、命令/路径匹配语义、chat「一直允许」细化与写回、run/ACP/子 agent/MCP 并入同一模型、观测与验收 |
| 0.2 | 2026-10-16 | suga（gofer job） | 关闭 R-C1–R-C5、R-M1–R-M10：校正 ACP/MCP 与 run 语义，收紧 shell/工具/路径匹配，补齐会话持久性、项目完整性、子 agent、重载与写回并发边界 |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

关联：issue suga-ob4a（本设计来源）、suga-1ojw（ACP 权限选项）、suga-oyby（`:add-dir`/越界授权，已实现，本设计不改其语义）。调研依据：`docs/research/2026-10-16-feature-strengthening-analysis.md` §3 #7、#8。独立评审：`local/reviews/2026-10-16-approval-undo-design-review-r1.md`（不入库）。

### 思考模式与范围冻结

- thinking_mode：RIGOROUS。
- core_objective：让“这一次工具调用要不要问人”由**可以写下来、可以审查、可以复现**的规则决定，而不是只有“工具名 + 单次回答”；并把会话内「一直允许」细到命令前缀/目录，且能选择写回规则文件。
- allowed_scope：`pkg/agentcore/tools`（新增规则匹配包与 `ApprovalReport` 扩展）、`internal/wiring`（加载/合并/判定、chat 审批器与写回）、`internal/config`（新配置键与项目级文件路径）、`internal/chat`（审批交互文案与选项）、`internal/acpserver`（权限选项与规则判定接入）、`internal/personal`（trace attrs）、`docs/`、`config/config.yaml`、`README.md`/`README.zh-CN.md`、skills 参考文档。
- non_goals：ACP `session/list|resume|close` 与 `fs/*` 客户端能力（suga-1ojw 的其余部分）；`set_config_option`/`authenticate`；MCP 的 HTTP/SSE transport；shell 的进程级隔离（调研 §3 #9）；`web_fetch` 代理下的 SSRF 加强（§3 #10）；危险删除守卫的判定规则本身；只读模式的工具集合；Gofer 侧改动。
- expansion_policy：DEFER_OR_REQUEST。
- review budget：低暴露度（个人工具、未投产、可用 `tools.rules.enabled` 回退）；一次实现 + 单测 + 伪终端冒烟 + 一次反向验证。
- 停止条件：确认无 CORE_BLOCKING 后请用户批准本设计（Q1–Q6 有答复），再起草实施计划；批准设计不授权实施。

## 背景与目标

现状（2026-10-16 核对，基线 `626747f`）：审批是**命令式**的，没有声明式规则。

- chat 的审批器按固定顺序判断：MCP 走白名单闸门 → 文件工具越界先问目录 → 只读/agent-state/`tools.approval: auto` 直接放行 → 会话内 `always[name]` → 询问（`internal/wiring/chat_approver.go:95-139`）。
- 「一直允许」只以**工具名**为键、只存在进程内存（`chat_approver.go:48-52,70,115-118,132-135`），所以答一次 `a` = 本进程内所有 `shell_exec` 都不再问，粒度太粗。
- 原生工具没有任何 allow/deny 规则；唯一的声明式白名单是 MCP 的 `tools.auto_allow` / `tools.read_only_allow`（`internal/wiring/cli_mcp.go:33-114`），**默认全拒**（`:37-38,113`）。
- run 的 approver 就是 MCP 闸门（`internal/wiring/runtime.go:165-175`），原生写工具全放行；ACP 每个 prompt 用 `permissionHook` 走 `session/request_permission`，选项只有 allow-once / reject-once（`internal/acpserver/policy.go:143-161`）。
- 项目级配置**不存在**：suga 只读 `$SUGA_HOME/config.yaml`（`internal/config/load.go:22-26,33-44`），工作区的配置文件明确不读。唯一的项目级文件先例是输出整理规则 `<workspace>/.suga/output-rules.yaml`（`internal/wiring/capabilities.go:119-139,151-152`）。

用户可见目标：

1. 能写规则：`命令/路径 glob → allow|ask|deny`，放在全局配置与项目级文件里，改规则不必改代码。
2. 规则可预期：**deny 恒优先**；同一条命令/路径的判定结果只由规则与默认动作决定，不依赖“先问过谁”。
3. 「一直允许」细到命令前缀与目录，并且可以（经询问）写回规则文件，成为下一次启动就生效的规则。
4. 同一套规则同时作用于 chat、run、ACP 与子 agent；MCP 白名单并入同一模型而不是并行第二套。
5. 可观测：`suga trace` 能看出“这次调用为什么没问 / 为什么被拒”——命中的是哪一条规则。

## 名词

- **规则（rule）**：一条 `{tool, action, command?, path?}` 记录，外加来源与序号。`tool` 是工具名或 glob；`command` 只对 `shell_exec` 有意义；`path` 只对文件工具（`file_write`/`file_edit`/`file_remove`）有意义。
- **动作（action）**：`allow`（不问，直接跑）、`ask`（询问，chat 在终端问、ACP 发权限请求、无终端则拒绝）、`deny`（直接拒绝，模型收到 tool error，回合继续）。
- **来源（source）**：`user`（`$SUGA_HOME/config.yaml` 的 `tools.rules` + `$SUGA_HOME/approval-rules.yaml`）、`project`（`<workspace>/.suga/approval-rules.yaml`）、`legacy`（`tools.auto_allow` / `tools.read_only_allow` 编译而成）、`session`（chat 答 `a`/`d`、ACP 回 allow_always/reject_always 得到）。
- **默认动作（default action）**：没有任何规则命中时该入口的动作，按入口与模式决定（见 3.4）。
- **范围键（scope key）**：会话级规则里 `command`/`path` 的具体值（命令前 N 个 token、目标所在目录、工具名），就是把「一直允许」写成一个规则时要填的东西。
- **命中（hit）**：规则与本次调用匹配。判定的输出是“最强动作 + 一条代表性命中（来源、序号、规则摘要）”。

## 范围与非目标

范围：

- 新增规则匹配引擎（纯逻辑，可作为能力层的一部分，不依赖 `internal/*`）。
- 规则来源：`$SUGA_HOME/config.yaml` 的 `tools.rules`（内联）、`$SUGA_HOME/approval-rules.yaml`、`<workspace>/.suga/approval-rules.yaml`；启动校验与合并。
- 判定接入：chat 审批器（含「一直允许」细化、写回、询问文案）、run 的 approver、ACP 的 `permissionHook`、子 agent 的 approver、MCP 白名单的编译。
- 观测：`ApprovalReport` 扩展、trace attrs 与 `suga trace` 渲染、`suga manifest` 列出生效规则。
- 文档：`config/config.yaml` 注释、README（中英）、skills 参考、CHANGELOG。

非目标：

- 不改沙箱：`os.OpenRoot` 的边界、`tools.read_roots`、`--add-dir` 的可达性语义、`file_remove` 的路径拒绝清单（工作区根、`.git`、home、通配符）都不动。
- 不改危险删除守卫的判定与“chat 每次打 `yes`”的语义；规则不能放行它（D8）。
- 不改工具是否存在：写工具在只读模式下不注册的现状不变（规则对不存在的工具没有意义）。
- 不做规则的热加载之外的重载机制（`suga serve`/Gofer 侧不感知规则）。
- ACP 的其余工具方法补齐、`fs/*` 能力（suga-1ojw 范围内，但不在本设计）。

## 已确认事实与规范

以下在 `626747f` 上核对，行号可能有偏移；引用时以符号名为准。

| 事实 | 位置 |
|---|---|
| 审批层是注册表栈的一层，顺序 observe → visibility → approval → base；无 approver 时一律拒绝（fail-closed） | `pkg/agentcore/tools/approval.go:43-46,67-72,89-91` |
| 审批输入是 `(ToolCall, Tool)`；返回 error：`ErrDenied` = 拒绝（回合继续），其它 error = 中止回合 | `pkg/agentcore/tools/approval.go:25-34,92-97` |
| 审批来源词汇已存在：`rule` / `user` / `always` / `user_denied`；`ApprovalReport{Source, Wait, Danger}` 走 ctx | `pkg/agentcore/tools/approval.go:121-132,139-145,150-168` |
| 只读与 agent-state 标记来自 `tools.Meta`（`MetaOf`） | `pkg/agentcore/tools/meta.go:23-46,58-70` |
| 栈的装配点：`guardDangerous(approver)` 包在 approval 层外层 | `internal/wiring/runtime.go:345-351` |
| run 的 approver = `cliMCPGate`（不询问）；chat 的 approver = `chatApprover` 包住 gate | `internal/wiring/runtime.go:165-175,190-195` |
| `tools.allow_write` 仅 chat 生效、`--read-only` 优先；写工具不注册时经 `SetDisabled` 报“未启用 + 提示” | `internal/wiring/runtime.go:203-215`、`internal/wiring/native_tools.go:23-43`、`pkg/agentcore/tools/approval.go:56-60,82-88` |
| chat 判定顺序：MCP → 越界目录问 → `ReadOnly||AgentState||auto` 放行 → `always[name]` → 询问 | `internal/wiring/chat_approver.go:95-139` |
| 「一直允许」= `map[string]bool`，键是工具名，仅进程内存 | `internal/wiring/chat_approver.go:48-52,70,115-118,132-135` |
| 询问文案与答案解析：`[y]是 [N]否 [a]本会话总是允许该工具`；`y`/`yes` 一次、`a`/`always` 记 always、其它拒绝 | `internal/wiring/chat_approver.go:128-138,286-290` |
| 无终端时需确认的工具一律拒绝，并提示 `tools.approval: auto` | `internal/wiring/chat_approver.go:121-123` |
| `tools.approval` 只接受 `ask`/`auto`，其它值启动失败 | `internal/wiring/chat_approver.go:78-93` |
| MCP 匹配：`server/toolglob` 或裸 `toolglob`（`path.Match` 语义，`*` 不跨 `/`）；`read_only_allow` 必须 server-qualified | `internal/wiring/cli_mcp.go:18-28,61-99` |
| MCP 默认全拒：两个列表为空时每个 MCP 调用都被 `Denied("需要许可：…")` | `internal/wiring/cli_mcp.go:33-38,103-114` |
| 危险删除守卫在审批最外层：chat 每次要求输入 `yes`（`auto`/always 都不绕过），run/ACP 直接拒绝 | `internal/wiring/danger_guard.go:20-54,57-83` |
| 越界目录授权：`chat.ask_outside_dirs`（默认开）→ `授权访问 <dir>？[r] 只读 [w] 读写 [n] 拒绝`，授权记入会话（`source: ask`） | `internal/wiring/chat_approver.go:162-196,201-209,215-228`、`internal/config/chat.go:50-69` |
| 子 agent：只注册只读工具，approver 恒 nil（全放行） | `internal/wiring/subagent.go:41-44,107-116` |
| ACP：`permissionHook` 既是工具通知 hook 又是 approver；read_only 非只读工具直接拒；其余 `RequestPermission`，**选项只有 allow-once/reject-once** | `internal/acpserver/policy.go:52-73,114-182`（选项 `:158-161`） |
| ACP 的 runtime 用 `wiring.Open(Approver: gate)` 打开，即 ACP 不经 `cliMCPGate` | `internal/cli/acp.go:42-73` |
| 回合内拒绝上限 3 次结束回合 | `internal/personal/denial.go:14-16` |
| trace：工具事件 attrs `approval`（Source）、`approval_wait_ms`、`danger`、`call_id`；拒绝记 `kind=denial` | `internal/personal/observe.go:238-272`、`internal/personal/store/types.go:48-49` |
| 项目级文件先例：`$SUGA_HOME/output-rules.yaml` 然后 `<workspace>/.suga/output-rules.yaml`，`native.LoadOutputRules(files...)` | `internal/wiring/capabilities.go:119-139,151-152` |
| 配置只读 `$SUGA_HOME/config.yaml` 或 `SUGA_CONFIG`；工作区的 `config.yaml`/`.env` 明确不读 | `internal/config/load.go:22-26,33-44` |
| 项目规则文件（`AGENTS.md`）是**文本规则**，不是权限来源，也不是本设计的规则文件 | `internal/config/rules.go`、`config/config.yaml:165-169` |
| 会话、turns、entries、events 的存储与事件 kind 常量（加 kind 无需迁移） | `internal/personal/store/migrations/20261008-230000-create-core-store.sql:4-55`、`internal/personal/store/types.go:35-100` |

## 总体方案

主线一句话：**把“这一次工具调用要不要问人”收敛成一次纯函数判定** —— `Decide(call) → {allow | ask | deny, 命中的规则}`，规则来自“用户级 + 项目级 + 会话级”三个来源，取最强动作（deny > ask > allow）；chat 的「一直允许」与 ACP 的 allow_always/reject_always 都编译成**同构的会话级规则**，MCP 白名单编译成 `legacy` 来源的 allow 规则。

这样做的三个直接结果：

- 规则、会话内的临时放宽、ACP 的选项、MCP 白名单**共用一个语义**，不再有“这里按工具名、那里按 glob”的分裂。
- deny 恒优先，所以“项目级只能收紧”和“危险删除不可放行”这两条安全性质是同一套规则的推论，而不是额外补丁。
- trace 里能报出“命中的是哪条规则”，用户能自己解释一次调用为什么没问。

## 架构

```text
                     $SUGA_HOME/config.yaml: tools.rules ─┐
              $SUGA_HOME/approval-rules.yaml ────────────┤  期望：加载 → 校验 → 编译
        <workspace>/.suga/approval-rules.yaml ───────────┤  （项目级 allow 默认忽略）
        tools.auto_allow / read_only_allow（legacy）──────┘        │
                                                                    ▼
   会话内放宽（chat 答 a/d，ACP allow_always/reject_always）─> rules.Policy（Rules[] + 默认动作）
                                                                    │
                                                                    ▼
   工具调用 ──> Approval 层 ──> guardDangerous ──> <入口 approver> ──> Policy.Decide(call)
                    │                                   │                    │
      （工具不存在→Denied）                    chat：Ask / ACP：RequestPermission  │
                                                                    ▼
                                              allow → 跑；deny → tools.Denied(规则说明)
                                                         │
                                                         ▼
                                      ApprovalReport{Rule: 来源+序号+摘要} → trace
```

组件划分（保持分层规矩：能力层不依赖 `internal/*`，见 `pkg/agentcore/layering_test.go`）：

- `pkg/agentcore/tools/rules`（新）：`Rule{Source, Index, Tool, Command, Path, Action}`、`Policy{Rules, Default}`、匹配函数（工具名、命令 token、路径 glob）与判定 `Decide(Target) (Action, *Rule)`。纯逻辑、纯单测，不读配置、不碰文件系统。
- `internal/wiring/rules.go`（新）：读取 `tools.rules` 与两个规则文件、校验、把项目级 `allow` 按开关过滤、把 MCP 白名单编译成 legacy 规则，产出 `*rules.Policy`；供 chat/run/ACP/子 agent 共用。
- `internal/wiring/approval_policy.go`（新，或并入 `chat_approver.go` 的重写）：入口 approver 的判定核心——把 `Policy.Decide` 的结果翻译成“放行 / 询问 / `tools.Denied`”，并在 ctx 里上报命中的规则。
- `internal/chat/approval_reply.go`（新）：答案解析（`y`/`a`/`d`/`n` 与持久化回答）、范围键计算、文案渲染。
- 写回：`internal/wiring/rules_write.go`（新）：向用户级/项目级规则文件追加一条规则（读-改-写，保留其余文本）。
- ACP：`internal/acpserver/policy.go` 的 `permissionHook` 改为先问 `Policy`（allow → 不问；deny → `deniedError`；ask → 现有 `RequestPermission`），选项集加上 `allow_always`/`reject_always`。

## 关键设计

### 3.1 规则形状

`$SUGA_HOME/approval-rules.yaml`（用户级）的内容：

```yaml
rules:
  - tool: shell_exec                 # 工具名或 glob；MCP 写 "server/toolglob"
    command: ["go test *", "go build *"]   # 只对 shell_exec：命令匹配
    action: allow
  - tool: file_write
    path: ["docs/**", "README*.md"]  # 只对文件工具：目标路径匹配（相对工作区）
    action: allow
  - tool: "*"                        # 任意工具
    path: ["**/.env", "**/*.pem"]
    action: deny
  - tool: "gofer/gofer_*_delete"
    action: deny                     # MCP 工具也可以被显式拒绝
```

规则字段：

| 字段 | 必填 | 说明 |
|---|---|---|
| `tool` | 否（缺省 `*`） | 工具名或 glob（`path.Match` 语义，`*` 不跨 `/`）；MCP 用 `server/toolglob`，与 `tools.auto_allow` 同一种写法 |
| `command` | 否 | 字符串或列表；仅对 `shell_exec` 生效；只要声明了它，本次调用的工具不是 `shell_exec` 就不匹配 |
| `path` | 否 | 字符串或列表；仅对 `file_write`/`file_edit`/`file_remove` 生效；相对工作区/附加目录的 glob，或绝对路径 glob |
| `action` | 是 | `allow` / `ask` / `deny` |

一条规则同时写 `command` 与 `path` 是配置错误（启动失败）：它们的适用工具集合不相交，写在一起说明作者意图不明，宁可报错也不猜。

### 3.2 匹配语义

**工具名**：`path.Match(pattern, name)`；MCP 工具先用同样的 `server/toolglob` 规则匹配（复用 `mcpPatternMatches` 的语义，`internal/wiring/cli_mcp.go:18-28`）。

**命令（`shell_exec`）**：判定输入是模型给的 `command` 参数原文。

1. 保守 token 化：按空白切分，**不解析引号**。引号里的空格会切错，切错只会导致“少匹配”（规则不生效 → 落默认动作），不会误放行。
2. 逐 token 做 `path.Match`（`*` 只在同一 token 内匹配），且 pattern 的 token 数必须 ≤ 命令的 token 数，逐位匹配。`go test *` 因此匹配 `go test ./...`、`go test -race ./...`，不匹配 `go test`（要匹配就写 `go test`）。
3. **含 shell 控制符的命令不参与 `allow` 匹配**：命令里出现引号外的 `;`、`&&`、`||`、`|`、`&`、`>`、`<`、`` ` ``、`$(` 或换行时，`allow` 类规则一律不匹配（`ask`/`deny` 仍然匹配）。
   理由：否则 `go test *` 这条 allow 会让 `go test ./... && rm -rf ~` 一起过。这是“规则不是 shell 解析器”的代价，用 fail-closed 兜住：这类命令回到默认动作（chat 会问）。
4. 判定输入不含 shell 别名/函数展开、不含环境变量求值；`sh -c "…"` 里的命令就是 `sh` 开头的命令本身，不递归解析内部（写 `sh -c *` 的规则要自己承担）。

**路径（文件工具）**：

1. 调用先按工具自己的解析得到目标：`file_write`/`file_edit` 用 `native.writeTargetPath`（`pkg/agentcore/tools/native/file.go:175-181`），`file_remove` 用 `native.PlanRemoveWithDirs`（`native/remove.go:119-220`）之后的目标路径——即**工具真正会动的那个路径**，不是模型写的字符串（与越界问题同一原则，`chat_approver.go:143-146`）。
2. 对每个根（workspace、会话里 `:rw` 的附加目录）算出相对路径（`filepath.ToSlash`）；规则里不含 `/` 开头的 `path` 按“某个根下的相对路径”匹配，含绝对路径（Unix `/…`、Windows `C:/…`）的按规范化绝对路径匹配。
3. glob 语义与 `file_find`/README 一致：`*` 不跨 `/`，`**` 跨目录，`?` 单字符。
4. 解析失败（越界、目标不存在且工具会拒绝、路径含通配符的 `file_remove`）→ 路径规则不匹配 → 落默认动作。
5. Windows 上路径匹配**大小写不敏感**（与 cwd 比较的既有做法一致，`README.md:147`）；命令匹配大小写敏感。

### 3.3 来源、合并与优先级

- **来源与顺序**：`user`（`config.yaml` 的 `tools.rules`，然后 `$SUGA_HOME/approval-rules.yaml`）→ `project`（`<workspace>/.suga/approval-rules.yaml`）→ `legacy`（MCP 白名单）→ `session`（本次进程内答出来的）。加载后合成一个池子，每条规则带 `(source, index)`。
- **动作取最强**：`deny` > `ask` > `allow`。不建议“第一条命中说了算”：那样规则顺序会成为隐蔽的语义，且用户新增一条 `deny` 时可能因为没有放在前面而失效。
- **项目级只能收紧**：`<workspace>/.suga/approval-rules.yaml` 里的 `allow` 条目**默认被忽略**，启动时打一条警告并在 trace 里可见；需要项目级放行时必须显式设置 `tools.rules.trust_project_allow: true`（默认 false）。
  理由：这个文件在工作区内、模型通过 `file_write` 就能写它；允许它放行等于把审批权交给模型（一次提权后自我维持）。`deny`/`ask` 没有这个问题——模型写进去只会收紧自己，最坏是它把自己卡住，用户可读可改。
- **会话级规则的合并**：与文件规则同池，动作同样取最强。所以“本会话总是允许 X”不能放宽文件里的一条 `deny` X——这正是用户写下 `deny` 时期望的。
- **校验时机**：`config.yaml` 的 `tools.rules`、两个规则文件在启动时读取并校验；语法错误、未知 `action`、空 `tool`、同时写 `command` 与 `path`、glob 非法 → **启动失败**（与 `tools.approval`/`tools.allow_write` 的既有做法一致：配置错误不猜、fail-closed）。文件不存在或为空不算错；文件里 `enabled: false` 时整个文件跳过。
- **重载**：每个回合开始前按 mtime 重读两个规则文件（文件很小；用户改规则不必重启 chat）。`config.yaml` 的内联规则仍以启动时读到的为准（配置本身不热加载）。

### 3.4 默认动作（每个入口）

| 入口 | 默认动作 | 说明 |
|---|---|---|
| chat | `tools.approval` 的值：`ask`（默认）/ `auto` → `allow` | 保留现有键作为“默认动作”，不新增第二个同义键；`tools.approval` 非这两值是启动错误（现状） |
| run | `allow` | 与现状一致：run 没有逐次确认，写工具直接跑 |
| ACP（default 模式） | `ask` | 走 `RequestPermission`；read_only 模式下非只读工具直接 `deny`（现状 `policy.go:143-145`） |
| 子 agent | `allow` | 只注册只读工具；规则可以收紧（D8） |
| MCP 工具（任何入口） | `deny` | 现状：白名单为空即全拒；`tools.auto_allow`/`read_only_allow` 编译成 `legacy` allow 规则后，语义等价 |
| 只读工具（`Meta.ReadOnly`）与 agent-state 工具 | `allow` | 现状的快捷放行；规则可以把它改写成 `ask` 或 `deny`（见 3.7） |

`allow` 规则在 run/ACP 里也不绕过其他闸门：工具是否存在（`SetDisabled` 的“未启用”提示）、危险删除守卫、沙箱边界都照旧。

### 3.5 chat「一直允许」细化到命令前缀/目录

回答 `a` 时记下的不再是一个工具名，而是一条**会话级规则**，范围键按工具类型计算：

| 工具 | 范围键 | 记下的规则 | 询问文案 |
|---|---|---|---|
| `shell_exec` | 命令前 `N` 个 token（`tools.rules.always_prefix_tokens`，默认 2；命令只有 1 个 token 时按 1 个） | `{tool: shell_exec, command: "go test *", action: allow}` | `[a]本会话总是允许“go test”开头的命令` |
| `file_write` / `file_edit` | 目标文件所在目录（相对工作区）；工作区根下的文件用 `*`（不跨目录） | `{tool: file_write, path: "docs/**", action: allow}` | `[a]本会话总是允许写入 docs/ 目录` |
| `file_remove` | 目标所在目录（同一算法） | `{tool: file_remove, path: "tmp/**", action: allow}` | `[a]本会话总是允许删除 tmp/ 下的内容` |
| 其余（MCP、将来新增的写工具） | 工具名 | `{tool: "server/tool", action: allow}` | `[a]本会话总是允许该工具` |

配套两点：

- 命令含控制符（3.2 第 3 条）时**不提供** `a`：文案里省略 `[a]`，并说明“含 `&&`/`|` 等的命令不能总是允许”。否则会话级 allow 会绕开刚立的规矩。
- 新增 `[d]本会话总是拒绝`：记 `action: deny` 的会话级规则，范围键与 `a` 相同。它对应 ACP 的 `reject_always`（suga-1ojw）与文件规则里的 `deny`，让“这次之后别再问我”两个方向都有出口。

询问文案（chat，未命中任何规则时）：

```text
允许执行 shell_exec？[y]是 [N]否 [a]本会话总是允许“go test”开头的命令 [d]本会话总是拒绝：
命令：go test -race ./...
```

```text
允许执行 file_write？[y]是 [N]否 [a]本会话总是允许写入 docs/ 目录 [d]本会话总是拒绝：
路径：D:\work\...\docs\design\x.md
内容：1240 字节
```

答案解析：`y`/`yes` 一次；`a`/`always` → 会话级 allow 规则；`d`/`deny` → 会话级 deny 规则；其它（含回车）= 拒绝，回合继续（现状 `chat_approver.go:128-138` 不变）。

**是否持久化**：答 `a`/`d` 之后追一句询问（`tools.rules.persist_ask`，默认开）：

```text
把这条规则写进规则文件？[u] 用户级 ~/.suga/approval-rules.yaml [p] 项目级 .suga/approval-rules.yaml [N] 只本会话：
```

- 直接回车 = 只本会话（默认，最不易造成意外）；写文件要显式输入 `u` 或 `p`。
- `[p]` 只有在 `tools.rules.trust_project_allow: true`（放行 `allow`）或规则动作为 `deny` 时才出现；否则只给 `[u]`，并在文案里说明原因。
- 写回是**追加**：读原文件（不存在则写一个带注释头的模板）、`rules:` 列表尾部追加一条，保留其余文本与注释；写失败只警告（规则已在会话内生效），trace 记 `approval_rule_persist` 事件（attrs 文件、动作、结果）。
- 写回成功后规则标记 `source: user`（或 `project`），下一次启动由文件加载得到同一条。

### 3.6 MCP 白名单并入同一模型

- `tools.auto_allow` / `tools.read_only_allow` 在加载时**编译**成 `legacy` 来源的 allow 规则（`tool: <server>/<toolglob>`），语义与 `mcpPatternMatches` 一致；两个键保留、校验保留（`read_only_allow` 仍要求 server-qualified，`:61-99`），不新增同义键。
- `--read-only` / ACP read_only 仍只用 `read_only_allow`：编译时按模式选择来源（现状 `newCLIMCPGate(readOnly)`，`cli_mcp.go:44-57`）。
- MCP 工具的默认动作是 `deny`（不是 `ask`）：没有规则命中时直接拒绝，模型收到 `Denied("需要许可：…")`（现状文案与语义，`:113`）。用户想让某个 MCP 工具走询问，可以写 `{tool: "srv/tool", action: ask}`；chat 会问，ACP 会发权限请求。
- `legacy` 规则的优先级与其它来源相同（取最强），所以文件里的一条 `deny` 能盖掉 `auto_allow`。

### 3.7 与既有闸门的关系（顺序与不变式）

判定顺序（外到内）：

1. **工具是否存在**：`Approval.ExecuteTool` 先查注册表；不存在的工具走 `SetDisabled` 提示（`approval.go:82-88`、`native_tools.go:23-43`）。规则不参与。
2. **危险删除守卫**：`guardDangerous` 在 approver 之外（`runtime.go:348-349`），命中就 chat 要求输入 `yes`、run/ACP 直接拒绝（`danger_guard.go:20-54`）。**规则不能放行**，也不参与它的判断。
3. **规则判定**：`deny` → `tools.Denied("规则 <来源>#<n> 拒绝：…")`，回合继续；命中 `allow`/`ask` 则继续往下。
4. **越界目录授权（chat）**：文件调用越过工作区与附加目录时，现行为不变地问目录（`chat_approver.go:162-196`）——这是“可达性授权”，不是单次许可。规则**不能替代它**：路径在工作区外时，`allow` 规则也要先通过它（沙箱根本没放开）。
5. **逐次确认**：`ask`（或默认动作是 ask）→ chat 询问、ACP 权限请求、无终端则 `Denied`；`allow` → 直接跑。
6. **只读/agent-state 快捷放行**：默认放行（`chat_approver.go:110-112`、`policy.go:137-142`），但**规则匹配先于它**：写了 `{tool: web_fetch, action: deny}` 就真的会被拒。这是“用户显式写下”与“内置默认”的区别。

不变式（实现与测试都要守）：

- 规则只决定“问不问 / 放不放行”；**不扩大沙箱、不改变工具存在性、不放行危险删除、不授予工作区以外的可达性**。
- `--read-only` 下写工具不注册（`runtime.go:203-215`），所以写工具的规则无效；MCP 只认 `read_only_allow`。
- 同一次调用的判定是纯函数（规则池 + 调用 + 默认动作），不依赖“之前问过谁”。
- 一个回合内 3 次拒绝仍结束回合（`internal/personal/denial.go:14-16`）；规则造成的拒绝也算一次（与 `core` 的计数点一致，`observe.go:248-251`）。

### 3.8 ACP 的 allow_always / reject_always（suga-1ojw 的映射）

- 权限选项集从两种扩到四种，`OptionId` 保持现有 `allow` / `reject` 不变，新增 `allow_always` / `reject_always`（`policy.go:158-161`）。
- `allow_always` → 会话级 `allow` 规则（范围键算法与 chat 的 `a` 完全相同，包括“含控制符的命令不提供”）；`reject_always` → 会话级 `deny` 规则。
- 规则命中 `allow` 时**不发** `session/request_permission`，但 `BeforeTool` 的 `tool_call` 通知照常发（客户端仍看到这次调用）；命中 `deny` 时直接返回 `deniedError`（模型收到 tool error，回合继续，与 read_only 拒绝同一路径，`policy.go:143-145`）。
- ACP 不写规则文件：持久化是客户端/用户的决定（它有自己的配置面）。ACP 所需的其余方法（`session/list|resume|close`、`fs/*`）仍属 suga-1ojw，不在本设计。

### 3.9 观测

- `ApprovalReport` 增加 `Rule string`（形如 `user#3 shell_exec command="go test *" action=allow`）与 `Scope string`（会话级规则的范围键，非会话级为空）。
- trace：现有 `tool`/`denial` 事件（`internal/personal/observe.go:238-272`）的 attrs 增加 `approval_rule`、`approval_scope`；`approval` 值不变（`rule`/`user`/`always`/`user_denied`），会话级 allow 仍报 `always` 以保持旧读法可用。
- `suga trace` 渲染：`approval=rule(user#3: shell_exec "go test *")`；会话级显示 `approval=always(shell_exec "go test")`。
- `suga manifest --json` 增加 `approval_rules`：每个来源的文件路径、生效条数、被忽略的条目（项目级 `allow`、未信任的文件）、`tools.approval` 的默认动作。这是用户排查“为什么没问/为什么被拒”的第一站。
- 拒绝文案带规则来源，模型据此自我纠正（例：`规则 project#2 拒绝：路径 **/.env`）。

### 3.10 兼容与迁移

- 全部现有配置键保留且语义等价：`tools.approval`（默认动作）、`tools.auto_allow`/`tools.read_only_allow`（编译成 legacy 规则）、`chat.ask_outside_dirs`（越界授权）、`tools.allow_write`、`--read-only`、`--allow-write`、`--add-dir`、`SUGA_ADD_DIRS`。
- 无数据迁移：规则是配置，不落库。会话级规则只在进程内存（与现状 `always` map 相同），chat 退出即失。
- 行为变化只有两处是有意的：① 项目级规则文件的 `allow` 默认被忽略（只有设置了开关才生效）；② 只读/agent-state 工具可以被规则改成 `ask`/`deny`。
- 新增键汇总：

| 键 | 类型/默认 | 含义 |
|---|---|---|
| `tools.rules` | 规则列表，默认空 | 内联规则（用户级） |
| `tools.rules.enabled` | bool，默认 `true` | 关掉后不做规则判定（等价于现状） |
| `tools.rules.trust_project_allow` | bool，默认 `false` | 允许项目级文件里的 `allow` 生效 |
| `tools.rules.always_prefix_tokens` | int，默认 `2`（`>= 1`） | `a` 记命令前缀的 token 数 |
| `tools.rules.persist_ask` | bool，默认 `true` | 答 `a`/`d` 后是否追问写回文件 |
| `tools.rules.project_write` | bool，默认 `false` | 是否把规则写进项目级文件（写用户级始终允许） |

（`tools.rules` 既是列表又要带子键会与 YAML 冲突，实现时按 `tools.rules: {enabled: …, rules: […]}` 的嵌套写法定稿，见 Q1。）

## 关键流程

### 命中文件规则放行（chat）

```text
模型调用 shell_exec{"command": "go test -race ./..."}
  Approval → guardDangerous（无危险）→ chatApprover
  Policy.Decide: tool=shell_exec 命中 user#3 {command: "go test *", allow}
     命令无控制符、token 前缀匹配 → 动作 allow
  → 工具直接执行；ApprovalReport{Rule: "user#3 …"} → trace approval=rule(user#3: …)
```

### 没命中 → 默认 ask → 答 `a` → 追问持久化

```text
模型调用 shell_exec{"command": "go build ./cmd/suga"}
  无规则命中 → 默认动作 ask → 询问
  文案：允许执行 shell_exec？[y]是 [N]否 [a]本会话总是允许“go build”开头的命令 [d]本会话总是拒绝：
  用户答 a → 会话级规则 {tool: shell_exec, command: "go build *", action: allow}
  追问：把这条规则写进规则文件？[u] 用户级 … [p] 项目级 … [N] 只本会话：
  用户答 u → 追加到 $SUGA_HOME/approval-rules.yaml；本回合调用执行；trace approval=always(shell_exec "go build")
```

### 项目级规则收紧

```text
<workspace>/.suga/approval-rules.yaml:
  rules:
    - tool: file_write
      path: ["**/*.env", "**/*.pem"]
      action: deny
模型调用 file_write{"path": "config/prod.env"}
  项目级 deny 命中（即使 config.yaml 里有 allow 规则也拦不住）→ Denied("规则 project#1 拒绝：路径 **/*.env")
```

### ACP allow_always

```text
ACP 客户端收到 session/request_permission（选项 allow / allow_always / reject / reject_always）
  客户端回 allow_always → 会话级 allow 规则（范围键同 chat）
  同一工具的后续同类调用不再发权限请求；tool_call 通知照常
```

## 安全、数据、运维与回滚

- **安全**：规则是安全相关配置，因此全部 fail-closed——配置错误启动失败、项目级 `allow` 默认忽略、含控制符的命令不参与 `allow`、路径匹配用工具真实解析的目标、规则不绕过危险删除守卫与沙箱与越界授权。规则文件本身不脱敏（它不含密钥），但它出现在 `suga manifest` 输出里（与已有的配置输出同级）。
- **数据**：无新表、无迁移；会话级规则只在内存；写回文件是用户可见的 YAML（可能同时被用户手工编辑：写回用追加 + 读-改-写，冲突时以磁盘内容为准并把新规则追加到末尾）。
- **运维**：`tools.rules.enabled: false` 一键回到现状；规则改动每回合生效；`suga manifest --json` 可查生效规则与忽略原因；启动警告写入 `$SUGA_HOME/logs/suga.log`（与其它启动警告一致）。
- **回滚**：三条改动相互独立（加载/合并、判定接入、chat 交互与写回）；关开关或去掉新文件即回到 2026-10-16 的行为，无数据迁移。
- **风险**：误配 `allow` 扩大权限（缓解：默认动作不变、项目级不能 allow、`deny` 恒优先、manifest 可见、trace 记规则）；命令 token 化的保守性导致“以为放行了其实还在问”（缓解方向是安全的，文档写明语义并提供 `suga manifest` 与 trace 证据）。

## 决策

- **D1 规则放在独立文件而不是 `config.yaml`**。备选：全部内联进 `config.yaml`（与 `tools.*` 一致，但那是用户手写带注释的文件，程序追加会打乱它）；只读用户级、不做项目级（少一层，但项目需要“这个仓库不许改 .env”这类规则）。选：用户级 `$SUGA_HOME/approval-rules.yaml` + 内联 `tools.rules` 两者都支持、项目级单独文件（位置对齐 `output-rules.yaml` 的先例）。
- **D2 优先级取最强（deny > ask > allow），而不是首次命中或按具体度排序**。备选：首次命中（顺序成为隐蔽语义，新增 deny 容易失效）；按 glob 具体度排序（需要定义“更具体”，解释成本高且容易被绕）。选最强：唯一规则、fail-closed 方向明确。
- **D3 命令匹配用 token 前缀 glob + 控制符豁免**。备选：纯字符串前缀（简单，但不能表达 `go test -run *`，且 `go test*` 会误匹配 `go testing`）；正则（表达力强，但带来 ReDoS 风险与写法门槛，且用户写错的后果是放行）。选 token 前缀 glob，并**必须**带控制符豁免。
- **D4 项目级 `allow` 默认忽略**（`tools.rules.trust_project_allow` 可开）。备选：项目级完全支持 allow（方便，但把审批权交给模型可写的文件）；项目级文件根本不读（最简单、最安全，但失去“仓库自带收紧规则”的价值）。选“可收紧、默认收紧”。
- **D5 `a` 的范围键按工具类型细化**（命令前缀 2 token / 目录 / 工具名）。备选：全部按工具名（现状，粒度粗）；全部按“精确命令/文件”（安全但不实用：几乎每行命令都要重新答）。选细化 + `always_prefix_tokens` 可调。
- **D6 答 `a`/`d` 后追问是否写回，默认不写**。备选：直接写回用户级（少一次交互，但用户会意外积累规则）；从不写回（每次重启都重新答）。选追问，回车即“只本会话”。
- **D7 MCP 白名单编译进同一模型（`legacy` 来源）**，保留两个键与校验。备选：并行保留两套判定（MCP 独立闸门 + 原生规则，判定顺序复杂、无法用一条 deny 覆盖白名单）。选并入。
- **D8 规则不能放行危险删除**。备选：允许显式 `allow` 放行（会削弱守卫存在的意义：它拦的正是“确定性删除”；确认过的一次就不该再确认）。选不放行；用户确实要跑时仍在 chat 里手输 `yes`。
- **D9 ACP 的 allow_always/reject_always 落成会话级规则，不写文件**。备选：ACP 侧也写规则文件（超出客户端语义、且没人看得到写在哪）；不做（suga-1ojw 的验收会缺一半）。选会话级。
- **D10 规则文件每回合按 mtime 重载**。备选：只在启动读（改规则要重启 chat）。选每回合一次（文件小，成本可忽略）。
- **D11 只读/agent-state 工具的默认放行可被规则改写**。备选：规则只作用于“需要确认的工具”（保持内置快捷路径不可动摇，但用户就无法用规则禁掉 `web_fetch`）。选可改写：显式规则优先于内置默认。

## 待确认事项

- **Q1 规则文件与键的形状**：建议
  `tools.rules: {enabled: true, trust_project_allow: false, always_prefix_tokens: 2, persist_ask: true, project_write: false, rules: [...]}`（嵌套），
  以及文件 `$SUGA_HOME/approval-rules.yaml` / `<workspace>/.suga/approval-rules.yaml` 里顶层就是 `rules:` 列表（可带 `enabled: false`）。
  备选：写成 `tools.approval_rules`（与 `tools.approval` 更近）或把 `tools.rules` 直接当列表、子配置另立键。
- **Q2 项目级规则文件的位置与信任开关**：建议 `<workspace>/.suga/approval-rules.yaml`（对齐 `output-rules.yaml`），`project` 只能收紧，`trust_project_allow` 默认 false。备选：允许项目级 allow 默认生效（更顺手、但把审批交给模型可写文件）；项目级文件改放仓库根 `APPROVAL.md`（更像项目规则、但不适合机器追加）。
- **Q3 命令匹配语义**：建议 token 前缀 glob（`go test *` 匹配 `go test ./...`，不匹配 `go test`）+ 含控制符的命令不允许 `allow`。备选：字符串前缀 `go test` 也匹配 `go test`（更宽松，需同时定义“前缀到哪结束”）；纯正则。
- **Q4 是否加 `[d]本会话总是拒绝`**：建议加（与 ACP `reject_always`、文件 `deny` 三处对齐）。备选：不加（交互面更小，但“别问了、直接拒绝”没有出口）。
- **Q5 写回的目标与默认**：建议默认“只本会话”，`[u]` 用户级始终可写、`[p]` 项目级需 `project_write: true` 且（对 `allow`）`trust_project_allow: true`。备选：只允许用户级（更简单，项目想固化就自己编辑文件）。
- **Q6 `file_remove` 的 `a` 是否也按目录细化**：建议按目录（与 `file_write` 一致，文案写明“总是允许删除该目录下的内容”）。备选：`file_remove` 只按工具名（最保守，但删除往往就集中在 `tmp/`，每次问很吵）。

## 结论与人工计划 Gate

方案把审批从“工具名 + 单次回答”换成“规则池 + 默认动作 + 取最强动作”：用户级/项目级文件写长期规则，chat 的 `a`/`d` 与 ACP 的 allow_always/reject_always 写会话级规则，MCP 白名单编译成 legacy 规则；`deny` 恒优先、项目级默认只能收紧、危险删除守卫与沙箱不受影响。主要风险是误配 `allow` 与命令匹配的保守性，用“默认动作不变 + manifest/trace 可查 + 开关回退”兜住。

落地清单（批准后进计划阶段细化）：

1. `pkg/agentcore/tools/rules` 匹配引擎 + 单测（命令 token 化、控制符豁免、路径 glob、工具名 glob、动作取最强）。
2. `internal/wiring` 加载/合并/校验 + `tools.rules` 键 + 两个文件 + MCP 白名单编译；`approval_rule`/`approval_scope` 上报。
3. chat：判定接入、`a`/`d` 范围键、文案、写回与追问；`chat_approval.py` 同步更新。
4. run 与子 agent：默认动作与规则接入（收紧方向）。
5. ACP：选项集 + 规则判定（suga-1ojw 的这部分）。
6. 观测与文档：`suga manifest --json`、trace 渲染、`config/config.yaml`、README 中英、skills 参考、CHANGELOG。
7. 验收：单测（含 `internal/wiring/approval_table_test.go` 风格的表驱动）；伪终端冒烟新增 `scripts/ptysmoke/chat_rules.py`（allow 命中不再问、deny 命中被拒、写回文件后新进程仍生效）并做一次反向验证（临时改成“首次命中”，deny 用例必须失败）；`make smoke-chat` 全量回归；ACP 用 `scripts/acpprobe/acp_probe.py` 或 acpserver 单测验证 allow_always，真实 ACP 客户端行为记 UNVERIFIED_REAL_ENV。

同类产品对照（参考，不照搬）：Claude Code 的 permission rules 也是“工具 + 模式”的 allow/ask/deny 列表，并有项目级配置文件与“本次会话总是允许”这类选项；Codex 一类工具把“审批模式 + 沙箱”分开配置。本设计与它们共享“声明式规则 + 更强动作优先”的形态，差异在：命令按 token 前缀 glob 且含 shell 控制符时不允许放行、项目级默认只能收紧、危险删除守卫不可被规则绕过。这些对照只作形态参考，具体行为一律以本仓代码与本设计为准。


## Draft 0.2 修订条款（取代前文冲突表述）

本节是对独立评审 r1 的规范性回应；与前文冲突处以本节为准。代码核对依据包括 `internal/acpserver/policy.go:135-176`、`pkg/agentcore/tools/native/code_exec.go:85-108`、`internal/wiring/subagent.go:41-116`、`internal/wiring/chat_approver.go:104-106` 与 `pkg/agentcore/tools/native/pathresolve.go:94-172`。

### 4.1 ACP、MCP 与入口隔离（R-C1、R-M1、R-M7）

- `cliMCPGate` 的 legacy 白名单只编译并用于 chat/run；ACP 不读取这组 legacy 规则。ACP 非只读模式下，MCP 默认仍为 `ask` 并调用 `RequestPermission`；ACP read_only 下 MCP 一律 `deny`，`read_only_allow` 不会放行它们。
- ACP 的显式 `approval_rules` 仍可对 MCP 使用完整的 `server/tool` 名称；默认动作只在没有显式命中时生效。ACP 的 allow_always/reject_always 规则存放在 `a.sessions[id]` 的 session state，后续 prompt 复用；`session/load` 恢复该状态，未知或过期 session 不恢复。不得再写成每个 prompt 的临时 runtime 内存。
- 每条规则新增可选 `modes: [chat, run, acp]`，缺省为三者。无终端的 run 命中 `ask` 时按 `deny` 处理并记录原因，不把 chat 的交互等待带入 gofer job；该行为是本设计的有意变化并写入 3.10。项目规则动作先与入口默认动作取更严格者，项目文件不能把 `deny` 默认放宽成 `ask`。
- `tools.rules.enabled: false` 原样走旧的各入口路径，不构造空规则池；因此 legacy MCP 与 ACP 的旧差异保持不变。

### 4.2 工具身份与命令匹配（R-C2、R-C3、R-C5、R-M5、R-M6）

- 原生工具规则要求工具确为注册的原生工具；不含 `/` 的 `tool` 模式永不匹配 MCP。MCP 规则必须写 `server/toolglob`；legacy 裸模式只在其原有 MCP 白名单编译路径解释。`command`/`path` 条件也按工具身份校验，不能靠同名参数冒充。
- 命令规则先按实际 `resolveShell` 分类。allow 只接受该 shell 的正向安全字符白名单并检查**整条原文**：POSIX sh、PowerShell、显式 fish/cmd 各自定义允许字符；未知 shell、换行（含 `\r`、Unicode 行分隔）、控制操作符、替换/展开语法一律不匹配 allow。deny 也做整串匹配，但命令 deny 是建议性收紧而非安全边界，不能替代沙箱或危险守卫。
- token 语法默认精确：token 数必须相等；只有末尾单独的 `*` 或 `**` 表示其后可有任意数量 token。token 内通配符匹配任意非空字符（不把 `/` 当特殊分隔符），不再直接套 `path.Match`。Windows 命令比较大小写不敏感并忽略 `.exe`；别名、包装器、解释器和 `git -C` 等不作语义归一化。
- 内置包装器/解释器首 token（`sh`、`bash`、`pwsh`、`python`、`node`、`env`、`sudo`、`npx`、`xargs`、`find`、`go run` 等）不提供按前缀的 `a`；只能对完整安全命令作一次性 allow，提示中展示实际保存的模式。`a` 与规则 allow 共用同一 shell 白名单。

### 4.3 路径、安全根与项目文件（R-C4、R-M2、R-M3、R-M4、R-M8、R-M9）

- `path` 规则只接受明确的写工具名（`file_write`、`file_edit`、`file_remove`）或仅由这些工具组成的 glob；带 `path` 的可能读工具 glob 启动时报错。密钥示例改为写入保护，不宣称阻止 `file_read`/`file_grep` 读取；prefetch 仍可在审批前读，未来若要控制读需另立设计。
- 每条相对 path 规则绑定 `root`，文件规则默认只绑定 workspace；会话级规则保存产生它的绝对 root。先完成越界目录授权，再以更新后的 root 集合解析目标。规则同时检查词法路径与 `EvalSymlinks` 后的真实路径：deny 任一命中即命中，allow 要求两者都命中；符号链接、硬链接（`nlink > 1`）、特殊文件、解析失败或 Windows 的短名/ADS/末尾点空格均不产生 allow。deny 在解析失败时按命中处理，allow 按不命中处理。glob 必须锚定 root，`*` 不代表任意深度；根文件的 `a` 保存转义后的精确相对路径，`file_remove` 不因目录回答 `a` 而获得递归/永久删除权限。
- 受保护路径（规则文件、`.suga/**`、`.git/**`、`$SUGA_HOME/**`）的 allow 永远不生效；危险删除守卫仍是独立外层守卫。原有“项目级只能收紧”改为：项目 `allow` 默认忽略，项目 `ask`/`deny` 也不能放宽入口默认动作。
- `trust_project_allow` 不再是全局布尔信任开关。改为用户配置中的 workspace 绝对路径条目及其规则文件内容 hash；路径未获信任、文件 hash 变化或仓库切换时，项目 allow 继续忽略并要求用户重新确认。文件上限 64 KiB、规则条数有限制，禁用 YAML alias/anchor 展开。
- 回合重载用 size + mtime + hash。中途解析失败、文件被删除或 hash 不一致时保留上一份有效规则，发 warning 与 trace；项目文件的既有 deny 在本会话继续有效，直至用户确认新内容。两进程写回以锁保护，使用 yaml.v3 Node 保留注释/流式列表，临时文件 + 原子 rename，回读校验；规则生成时转义 glob 元字符与 Windows `\\`。
- 子 agent 获得与父会话相同的 policy/规则快照，不能使用恒 nil approver 绕过；子 agent 内命中 `ask` 等同 `deny`。`d`/deny 对主 agent 经 `agent` 工具传递到子 agent。

### 4.4 兼容、验收与待用户决定

本节关闭 R-C1–R-C5、R-M1–R-M10；R-m1–R-m10 同步按上述导出解析 API、拒绝来源、命名、环境变量、prefetch、复合指纹、外层守卫、旧路径回退、示例、文件上限处理。特别是内联配置采用 `tools.approval_rules` 对象（不再让 `tools.rules` 同时表示列表和选项），规则正文只放规则文件；若保留兼容别名，启动时明确其 `${…}` 会被 `ParseEnv` 展开。

仍待用户选择：Q1 采用 `tools.approval_rules` 还是兼容旧名；Q2 哪些 workspace 路径先进入 hash 信任清单；Q3 各 shell 白名单的精确字符集；Q4 是否接受“命令 deny 仅建议性”；Q5 `[p]` 永不写入 allow、仅允许项目 deny；Q6 `file_remove` 只允许精确路径的 `[a]`。评审不同意原稿的全局 trust 开关、项目 allow 写回和按目录删除放宽，理由是它们会把可写仓库/递归删除变成提权通道。
