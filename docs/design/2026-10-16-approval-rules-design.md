<!-- template_id: design; template_version: 1.1.1 -->
# 审批规则文件（命令/路径 glob → allow|ask|deny）与更细的“一直允许” 设计

> 状态：Draft 0.3（按确认评审 r2 修订，待用户审阅）

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-16 | suga（gofer job） | 初稿：规则模型（来源、合并、优先）、命令/路径匹配语义、chat「一直允许」细化与写回、run/ACP/子 agent/MCP 并入同一模型、观测与验收 |
| 0.2 | 2026-10-16 | suga（gofer job） | 关闭 R-C1–R-C5、R-M1–R-M10：校正 ACP/MCP 与 run 语义，收紧 shell/工具/路径匹配，补齐会话持久性、项目完整性、子 agent、重载与写回并发边界 |
| 0.3 | 2026-10-16 | suga（gofer job） | 关闭 R-C2、N-A1、R-M1、R-M3、R-M8、R-M9 及全文一致性缺陷：补齐 shell 白名单、保护路径、项目信任钳制、会话规则生命周期与文件范围语义 |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

关联：issue suga-ob4a（本设计来源）、suga-1ojw（ACP 权限选项）、suga-oyby（`:add-dir`/越界授权，已实现，本设计不改其语义）。调研依据：`docs/research/2026-10-16-feature-strengthening-analysis.md` §3 #7、#8。独立评审：`local/reviews/2026-10-16-approval-undo-design-review-r1.md`（不入库）。

### 思考模式与范围冻结

- thinking_mode：RIGOROUS。
- core_objective：让“这一次工具调用要不要问人”由**可以写下来、可以审查、可以复现**的规则决定，而不是只有“工具名 + 单次回答”；并把会话内「一直允许」细到命令前缀/目录，且能选择写回规则文件。
- allowed_scope：`pkg/agentcore/tools`（新增规则匹配包与 `ApprovalReport` 扩展）、`internal/wiring`（加载/合并/判定、chat 审批器与写回）、`internal/config`（新配置键与项目级文件路径）、`internal/chat`（审批交互文案与选项）、`internal/acpserver`（权限选项与规则判定接入）、`internal/personal`（trace attrs）、`docs/`、`config/config.yaml`、`README.md`/`README.zh-CN.md`、skills 参考文档。
- non_goals：ACP `session/list|resume|close` 与 `fs/*` 客户端能力（suga-1ojw 的其余部分）；`set_config_option`/`authenticate`；MCP 的 HTTP/SSE transport；shell 的进程级隔离（调研 §3 #9）；`web_fetch` 代理下的 SSRF 加强（§3 #10）；危险删除守卫的判定规则本身；只读模式的工具集合；Gofer 侧改动。
- expansion_policy：DEFER_OR_REQUEST。
- review budget：低暴露度（个人工具、未投产、可用 `tools.approval_rules.enabled` 回退）；一次实现 + 单测 + 伪终端冒烟 + 一次反向验证。
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
4. 同一规则模型接入 chat、run、ACP 与子 agent；MCP 白名单按入口编译进同一模型，ACP 保持独立默认动作。
5. 可观测：`suga trace` 能看出“这次调用为什么没问 / 为什么被拒”——命中的是哪一条规则。

## 名词

- **规则（rule）**：一条 `{tool, action, command?, path?, root?, modes?}` 记录，外加来源与序号。无 `/` 的工具模式只匹配注册的原生工具；MCP 必须使用 `server/toolglob`。
- **动作（action）**：`allow`（不问，直接跑）、`ask`（询问，chat 在终端问、ACP 发权限请求、无终端则拒绝）、`deny`（直接拒绝，模型收到 tool error，回合继续）。
- **来源（source）**：`user`（`$SUGA_HOME/approval-rules.yaml`）、`project`（`<workspace>/.suga/approval-rules.yaml`）、`session`（chat 答 `a`/`d`、ACP 回 allow_always/reject_always 得到）。legacy MCP 规则仅在 chat/run 的独立编译路径存在。
- **默认动作（default action）**：没有任何规则命中时该入口的动作，按入口与模式决定（见 3.4）。
- **范围键（scope key）**：会话级规则里 `command`/`path` 的具体值。shell 使用命令前缀；`file_write`/`file_edit` 使用锚定 root 下的目标目录；`file_remove` 使用锚定 root 下的精确目标路径。
- **命中（hit）**：规则与本次调用匹配。判定的输出是“最强动作 + 一条代表性命中（来源、序号、规则摘要）”。

## 范围与非目标

范围：

- 新增规则匹配引擎（纯逻辑，可作为能力层的一部分，不依赖 `internal/*`）。
- 规则来源：`$SUGA_HOME/config.yaml` 的 `tools.approval_rules`（仅开关与选项）、`$SUGA_HOME/approval-rules.yaml`、`<workspace>/.suga/approval-rules.yaml`；启动校验与合并。
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
| 子 agent：只注册只读工具，approver 恒 nil（全放行） | `internal/wiring/subagent.go:41-44,107-116`；创建子 agent 时传入父 Policy 与会话级规则快照，不能使用写死的 nil approver |
| ACP：`permissionHook` 既是工具通知 hook 又是 approver；read_only 非只读工具直接拒；其余 `RequestPermission`，**选项只有 allow-once/reject-once** | `internal/acpserver/policy.go:52-73,114-182`（选项 `:158-161`） |
| ACP 的 runtime 用 `wiring.Open(Approver: gate)` 打开，即 ACP 不经 `cliMCPGate` | `internal/cli/acp.go:42-73` |
| 回合内拒绝上限 3 次结束回合 | `internal/personal/denial.go:14-16` |
| trace：工具事件 attrs `approval`（Source）、`approval_wait_ms`、`danger`、`call_id`；拒绝记 `kind=denial` | `internal/personal/observe.go:238-272`、`internal/personal/store/types.go:48-49` |
| 项目级文件先例：`$SUGA_HOME/output-rules.yaml` 然后 `<workspace>/.suga/output-rules.yaml`，`native.LoadOutputRules(files...)` | `internal/wiring/capabilities.go:119-139,151-152` |
| 配置只读 `$SUGA_HOME/config.yaml` 或 `SUGA_CONFIG`；工作区的 `config.yaml`/`.env` 明确不读 | `internal/config/load.go:22-26,33-44` |
| 项目规则文件（`AGENTS.md`）是**文本规则**，不是权限来源，也不是本设计的规则文件 | `internal/config/rules.go`、`config/config.yaml:165-169` |
| 会话、turns、entries、events 的存储与事件 kind 常量（加 kind 无需迁移） | `internal/personal/store/migrations/20261008-230000-create-core-store.sql:4-55`、`internal/personal/store/types.go:35-100` |

## 总体方案

主线一句话：**把“这一次工具调用要不要问人”收敛成一次纯函数判定** —— `Decide(call) → {allow | ask | deny, 命中的规则}`，规则来自“用户级 + 项目级 + 会话级”三个来源，按 deny > ask > allow 取动作，并执行项目来源与入口默认动作的限制；legacy MCP 白名单只在 chat/run 编译，ACP 保持自己的入口默认动作；chat 的「一直允许」与 ACP 的 allow_always/reject_always 都编译成**同构的会话级规则**，MCP 白名单编译成 `legacy` 来源的 allow 规则。

这样做的三个直接结果：

- 原生规则与会话规则共用一个语义；MCP legacy 白名单只在 chat/run 使用，ACP 保持入口隔离。
- deny 优先用于规则池；项目 deny 不可放宽；已信任的项目 allow 可在 ask 默认下生效，危险删除不可放行是外层守卫的独立不变式。
- trace 里能报出“命中的是哪条规则”，用户能自己解释一次调用为什么没问。

## 架构

```text
                     规则文件与 tools.approval_rules 选项 ─┐
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

- `pkg/agentcore/tools/rules`（新）：`Rule{Source, Index, Tool, Command, Path, Root, Modes, Action}`、`Policy{Rules, Default}`、匹配函数（工具名、命令 token、路径 glob）与判定 `Decide(Target) (Action, *Rule)`。纯逻辑、纯单测，不读配置、不碰文件系统。
- `internal/wiring/rules.go`（新）：读取规则文件与 `tools.approval_rules` 选项、校验、按 workspace/hash 信任过滤项目 allow；chat/run 另编译 MCP legacy 规则，ACP 使用自己的入口默认动作。
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
  - tool: file_write
    path: ["**/.env", "**/*.pem"]  # 只保护写入/编辑/移除，不阻止读工具读取
    action: deny
  - tool: "gofer/gofer_*_delete"
    action: deny                     # MCP 工具也可以被显式拒绝
```

规则字段：

| 字段 | 必填 | 说明 |
|---|---|---|
| `tool` | 否（缺省 `*`） | 工具名或 glob（`path.Match` 语义，`*` 不跨 `/`）；MCP 用 `server/toolglob`，与 `tools.auto_allow` 同一种写法 |
| `command` | 否 | 字符串或列表；仅对 `shell_exec` 生效；只要声明了它，本次调用的工具不是 `shell_exec` 就不匹配 |
| `path` | 否 | 字符串或列表；仅对明确的 `file_write`/`file_edit`/`file_remove` 生效；相对绑定 root 的 glob，或绝对路径 glob。带 `path` 的 `tool` 不得是 glob。 |
| `action` | 是 | `allow` / `ask` / `deny` |

带 `path` 的 `tool` 必须是明确的文件写工具名；一条规则同时写 `command` 与 `path` 是配置错误（启动失败）：它们的适用工具集合不相交，写在一起说明作者意图不明，宁可报错也不猜。

### 3.2 匹配语义

**工具身份**：无 `/` 的模式只匹配注册的原生工具；MCP 工具必须使用 `server/toolglob`。`command`/`path` 条件还要按工具身份校验，不能靠同名 MCP 工具冒充 `shell_exec` 或文件工具。

**命令（`shell_exec`）**：先按实际 `resolveShell` 分类。allow 只对整条原文执行对应 shell 的正向字符白名单；未知 shell、`\r`/`\n`/Unicode 行分隔符、控制操作符、替换与展开语法一律不匹配 allow，deny 仍做整串匹配。白名单是以下 ASCII 字符的精确集合（空格是唯一允许的空白；不允许 tab）并要求引号成对、不可嵌套：`sh`/`bash` 允许 `A-Z a-z 0-9 _ . / : = , + @ % -`、空格和单/双引号；引号只用于分组，反斜杠、`$`、反引号、分号、管道、重定向、括号、花括号、波浪号和感叹号均不允许。PowerShell 允许 `A-Z a-z 0-9 _ . / \ : = , + @ % -`、空格和单/双引号；引号只用于分组，`$`、反引号、`;`、`&`、`|`、重定向、括号、花括号和 `!` 均不允许。`cmd.exe` 允许 `A-Z a-z 0-9 _ . / \ : = , + @ -`、空格和双引号；双引号只用于分组，`%`、`^`、`!`、`&`、`|`、重定向、括号、花括号和单引号均不允许。字符白名单不承诺命令语义安全，只是 allow 的必要条件。token 数默认必须相等；只有末尾单独的 `*` 或 `**` 表示其后可有任意数量 token，token 内通配符不把 `/` 当特殊分隔符。Windows 比较大小写不敏感并忽略 `.exe`，不对别名、包装器、解释器或 `git -C` 做语义归一化。

内置包装器/解释器（`sh`、`bash`、`pwsh`、`python`、`node`、`env`、`sudo`、`npx`、`xargs`、`find`、`go run` 等）不提供按前缀的 `a`；只能对完整安全命令作一次性 allow，并在提示中显示实际保存的模式。

**路径（文件工具）**：先完成越界目录授权，再在更新后的根集合上解析目标；规则同时检查词法路径和 `EvalSymlinks` 后的真实路径。deny 任一命中即命中，allow 要求两者都命中；符号链接、硬链接（`nlink > 1`）、特殊文件、解析失败以及 Windows 短名/ADS/末尾点空格均不产生 allow，解析失败对 deny 按命中处理。相对规则绑定绝对 `root`，普通文件规则默认只绑定 workspace；glob 锚定 root，`*` 不代表任意深度。目标不存在时，取最深的已存在祖先目录做 `EvalSymlinks`，再拼接剩余词法部分；目标不存在（ENOENT）不算解析失败，只有祖先解析失败或越出 root 才算失败。根文件的会话规则保存转义后的精确相对路径；`file_write`/`file_edit` 的会话范围是目录，`file_remove` 的会话范围是精确路径，`file_remove` 的 `a` 不授予递归或永久删除权限。

### 3.3 来源、合并与优先级

- 用户级规则文件、项目级规则文件与会话级规则合并；legacy MCP 白名单只在 chat/run 的编译路径使用，ACP 不读取它。
- 动作取最强：`deny > ask > allow`。项目 `deny` 永远生效；项目 `ask` 不得把入口默认 `deny` 放宽成询问；已获 workspace 路径与规则文件 hash 信任的项目 `allow` 可以在入口默认 `ask` 时生效，但不能绕过保护路径、危险删除或沙箱。未信任或 hash 变化的项目 `allow` 忽略。
- 项目 `allow` 默认忽略；信任改为用户配置中的 workspace 绝对路径及规则文件内容 hash。路径未获信任、hash 变化或仓库切换时继续忽略项目 allow。文件上限 64 KiB、规则条数有限制，禁用 YAML alias/anchor 展开。规则文件、`.suga/**`、`.git/**` 与 `$SUGA_HOME/**` 是内置受保护路径：任何来源的 `allow` 对它们不生效；chat/ACP 按 `ask`，无终端的 run/子 agent 按 `deny`，显式 `deny` 仍按 `deny`。用户级规则文件也受此保护，不能借一次放行的写工具自我放权。
- 每条规则可带 `modes: [chat, run, acp]`，缺省为三者；无终端的 run 命中 `ask` 按 deny 处理。`tools.approval_rules.enabled: false` 原样走旧的各入口路径。
- 启动校验失败即退出；回合重载组合检查 size、mtime、hash。中途解析失败、文件删除或 hash 不一致时保留上一份有效规则并发 warning/trace；既有项目 deny 继续有效，直到用户确认新内容。
- 写回使用文件锁、yaml.v3 Node、临时文件原子 rename 与回读校验；生成规则时转义 glob 元字符与 Windows `\`。

### 3.4 默认动作（每个入口）

| 入口 | 默认动作 | 说明 |
|---|---|---|
| chat | `tools.approval` 的值：`ask`（默认）/ `auto` → `allow` | 保留现有键作为“默认动作”，不新增第二个同义键；`tools.approval` 非这两值是启动错误（现状） |
| run | `allow` | 与现状一致：run 没有逐次确认，写工具直接跑 |
| ACP（default 模式） | `ask` | 走 `RequestPermission`；read_only 模式下非只读工具直接 `deny`（现状 `policy.go:143-145`） |
| 子 agent | `allow` | 只注册只读工具；继承创建时的 Policy 与会话规则快照，子 agent 中 `ask` 等同 `deny` |
| MCP 工具（chat/run） | `deny` | legacy 白名单可编译为 allow；ACP 不使用该默认值 |
| MCP 工具（ACP default） | `ask` | 非只读模式走 `RequestPermission`；ACP read_only 下为 `deny` |
| 只读工具（`Meta.ReadOnly`）与 agent-state 工具 | `allow` | 现状的快捷放行；规则可以把它改写成 `ask` 或 `deny`（见 3.7） |

`allow` 规则在 run/ACP 里也不绕过其他闸门：工具是否存在（`SetDisabled` 的“未启用”提示）、危险删除守卫、沙箱边界都照旧。

### 3.5 chat「一直允许」细化到命令前缀/目录

回答 `a` 时记下的不再是一个工具名，而是一条**会话级规则**，范围键按工具类型计算：

| 工具 | 范围键 | 记下的规则 | 询问文案 |
|---|---|---|---|
| `shell_exec` | 命令前 `N` 个 token（`tools.approval_rules.always_prefix_tokens`，默认 2；命令只有 1 个 token 时按 1 个） | `{tool: shell_exec, command: "go test *", action: allow}` | `[a]本会话总是允许“go test”开头的命令` |
| `file_write` / `file_edit` | 目标目录的锚定相对路径与 root | `{tool: file_write, root: "<workspace>", path: "docs/design/**", action: allow}` | `[a]本会话总是允许写入 docs/design/ 目录` |
| `file_remove` | 目标的精确相对路径 | `{tool: file_remove, root: "<workspace>", path: "tmp/x.log", action: allow}` | `[a]本会话总是允许该目标` |
| 其余（MCP、将来新增的写工具） | 工具名 | `{tool: "server/tool", action: allow}` | `[a]本会话总是允许该工具` |

配套两点：

- 命令不满足对应 shell 的正向白名单时**不提供** `a`：文案里省略 `[a]`，并说明“含 `&&`/`|` 等的命令不能总是允许”。否则会话级 allow 会绕开刚立的规矩。
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

**是否持久化**：答 `a`/`d` 之后追一句询问（`tools.approval_rules.persist_ask`，默认开）：

```text
把这条规则写进规则文件？[u] 用户级 ~/.suga/approval-rules.yaml [p] 项目级 .suga/approval-rules.yaml [N] 只本会话：
```

- 直接回车 = 只本会话（默认，最不易造成意外）；写文件要显式输入 `u` 或 `p`。
- `[p]` 只对 `deny` 出现；项目 `allow` 永不由 chat 写回，即使 workspace 已信任。
- 写回是**追加**：读原文件（不存在则写一个带注释头的模板）、`rules:` 列表尾部追加一条，保留其余文本与注释；写失败只警告（规则已在会话内生效），trace 记 `approval_rule_persist` 事件（attrs 文件、动作、结果）。
- 写回成功后规则标记 `source: user`（或 `project`），下一次启动由文件加载得到同一条。

### 3.6 MCP 白名单并入同一模型

- `tools.auto_allow` / `tools.read_only_allow` 仅在 chat/run 加载时编译成 legacy allow 规则（`tool: <server>/<toolglob>`），语义与 `mcpPatternMatches` 一致；两个键保留、校验保留（`read_only_allow` 仍要求 server-qualified，`:61-99`），不新增同义键。
- `--read-only` 的 chat/run 仍只用 `read_only_allow`；ACP read_only 不读取 legacy 白名单，MCP 一律 deny。
- chat/run 的 MCP 默认动作是 `deny`；ACP 非只读默认是 `ask`，read_only 一律 `deny`。显式 ACP 规则可写完整 `server/tool` 名称，但 legacy 白名单不影响 ACP。
- `legacy` 规则的优先级与其它来源相同（取最强），所以文件里的一条 `deny` 能盖掉 `auto_allow`。

### 3.7 与既有闸门的关系（顺序与不变式）

子 agent 使用创建时的 Policy 快照，继承父会话规则；其入口没有终端，任何 `ask` 均按 `deny` 处理。

判定顺序为：工具存在性 → 危险删除外层守卫 → chat 越界目录授权 → 规则判定 → 逐次确认/默认动作 → 只读与 agent-state 默认放行。显式规则先于内置快捷放行；规则不能扩大沙箱、改变工具存在性、放行危险删除或授予工作区外可达性。路径解析失败对 deny fail-closed。一个回合内 3 次拒绝仍结束回合，规则造成的拒绝也计数。

### 3.8 ACP 的 allow_always / reject_always（suga-1ojw 的映射）

- 权限选项集从两种扩到四种，`OptionId` 保持现有 `allow` / `reject` 不变，新增 `allow_always` / `reject_always`（`policy.go:158-161`）。
- `allow_always` → 保存在 `a.sessions[id]`、由后续 prompt 复用的会话级 allow；`reject_always` → 会话级 deny。`session/load` 整体替换会话状态，故 load 时保留同一进程中已有的规则快照；规则不写入 session/meta，跨进程重启或进程间 load 不恢复，客户端需重新选择。
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
- 无数据迁移：规则是配置。chat 会话级规则在进程内存；ACP 会话级规则保存在 session state。同一进程的 `session/load` 保留已有规则快照，跨进程不恢复。
- 有意变化包括：项目 allow 按 workspace/hash 信任；只读/agent-state 可被规则改成 ask/deny；无终端 run 的 ask 按 deny；ACP 不读取 chat/run 的 legacy MCP 白名单而保持 MCP ask/deny 入口语义。
- 新增键汇总：

| 键 | 类型/默认 | 含义 |
|---|---|---|
| `tools.approval_rules` | 对象，默认启用 | 开关、选项；规则正文只在规则文件 |
| `tools.approval_rules.enabled` | bool，默认 `true` | 关掉后不做规则判定（等价于现状） |
| `tools.approval_rules.always_prefix_tokens` | int，默认 `2`（`>= 1`） | `a` 记命令前缀的 token 数 |
| `tools.approval_rules.persist_ask` | bool，默认 `true` | 答 `a`/`d` 后是否追问写回文件 |
| `tools.approval_rules.project_trust` | workspace 路径与内容 hash 列表，默认空 | 只有已确认且 hash 未变的项目文件 allow 才生效 |

（`tools.approval_rules` 是对象，规则正文只放在规则文件，避免内联规则与选项混用。）

## 关键流程

### 命中文件规则放行（chat）

```text
模型调用 shell_exec{"command": "go test -race ./..."}
  Approval → guardDangerous（无危险）→ chatApprover
  Policy.Decide: tool=shell_exec 命中 user#3 {command: "go test *", allow}
     命令满足 sh/PowerShell/cmd 对应白名单、token 匹配 → 动作 allow
  → 工具直接执行；ApprovalReport{Rule: "user#3 …"} → trace approval=rule(user#3: …)
```

### 没命中 → 默认 ask → 答 `a` → 追问持久化

```text
模型调用 shell_exec{"command": "go build ./cmd/suga"}
  无规则命中 → 默认动作 ask → 询问
  文案：允许执行 shell_exec？[y]是 [N]否 [a]本会话总是允许“go build”开头的命令 [d]本会话总是拒绝：
  用户答 a → 会话级规则 {tool: shell_exec, command: "go build *", action: allow}（包装器/解释器命令只记录完整安全命令）
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

- **安全**：规则是安全相关配置，因此全部 fail-closed——配置错误启动失败、项目 allow 受 workspace/hash 信任约束、allow 使用按 shell 的正向白名单、路径同时检查词法和真实目标、规则不绕过危险删除守卫与沙箱。规则文件本身不脱敏（它不含密钥），但它出现在 `suga manifest` 输出里（与已有的配置输出同级）。
- **数据**：无新表、无迁移；chat 规则在进程内存，ACP 规则在 session state；写回用锁、Node 编辑、原子 rename 与回读校验。
- **运维**：`tools.approval_rules.enabled: false` 一键回到现状；规则改动每回合生效；`suga manifest --json` 可查生效规则与忽略原因；启动警告写入 `$SUGA_HOME/logs/suga.log`（与其它启动警告一致）。
- **回滚**：三条改动相互独立（加载/合并、判定接入、chat 交互与写回）；关开关或去掉新文件即回到 2026-10-16 的行为，无数据迁移。
- **风险**：误配 `allow` 扩大权限（缓解：workspace/hash 信任、正向白名单、manifest 可见、trace 记规则）；命令 token 化的保守性导致“以为放行了其实还在问”（缓解方向是安全的，文档写明语义并提供 `suga manifest` 与 trace 证据）。

## 决策

- **D1 规则放在独立文件而不是 `config.yaml`**。备选：全部内联进 `config.yaml`（与 `tools.*` 一致，但那是用户手写带注释的文件，程序追加会打乱它）；只读用户级、不做项目级（少一层，但项目需要“这个仓库不许改 .env”这类规则）。选：用户级 `$SUGA_HOME/approval-rules.yaml` + `tools.approval_rules` 选项、项目级单独文件（位置对齐 `output-rules.yaml` 的先例）。
- **D2 优先级取最强（deny > ask > allow），而不是首次命中或按具体度排序**。备选：首次命中（顺序成为隐蔽语义，新增 deny 容易失效）；按 glob 具体度排序（需要定义“更具体”，解释成本高且容易被绕）。选最强：唯一规则、fail-closed 方向明确。
- **D3 命令匹配用 token 语法 + 按 shell 的正向字符白名单**。备选：纯字符串前缀（简单，但不能表达 `go test -run *`，且 `go test*` 会误匹配 `go testing`）；正则（表达力强，但带来 ReDoS 风险与写法门槛，且用户写错的后果是放行）。选 token 前缀 glob，并对 allow 强制执行整串正向白名单；不满足白名单的调用回到默认动作。
- **D4 项目级 `allow` 默认忽略**（按 workspace 路径与内容 hash 信任）。备选：项目级完全支持 allow（方便，但把审批权交给模型可写的文件）；项目级文件根本不读（最简单、最安全，但失去“仓库自带收紧规则”的价值）。选“可收紧、默认收紧”。
- **D5 `a` 的范围键按工具类型细化**（命令前缀 2 token / `file_write`、`file_edit` 目录 / `file_remove` 精确路径 / 其它工具名）。备选：全部按工具名（现状，粒度粗）；全部按“精确命令/文件”（安全但不实用：几乎每行命令都要重新答）。选细化 + `always_prefix_tokens` 可调。
- **D6 答 `a`/`d` 后追问是否写回，默认不写**。备选：直接写回用户级（少一次交互，但用户会意外积累规则）；从不写回（每次重启都重新答）。选追问，回车即“只本会话”。
- **D7 MCP 白名单编译进同一模型（`legacy` 来源）**，保留两个键与校验。备选：并行保留两套判定（MCP 独立闸门 + 原生规则，判定顺序复杂、无法用一条 deny 涵盖白名单）。选并入。
- **D8 规则不能放行危险删除**。备选：允许显式 `allow` 放行（会削弱守卫存在的意义：它拦的正是“确定性删除”；确认过的一次就不该再确认）。选不放行；用户确实要跑时仍在 chat 里手输 `yes`。
- **D9 ACP 的 allow_always/reject_always 落成会话级规则，不写文件**。备选：ACP 侧也写规则文件（超出客户端语义、且没人看得到写在哪）；不做（suga-1ojw 的验收会缺一半）。选会话级。
- **D10 规则文件每回合按 size/mtime/hash 重载，失败保留上一份有效规则**。备选：只在启动读（改规则要重启 chat）。选每回合一次（文件小，成本可忽略）。
- **D11 只读/agent-state 工具的默认放行可被规则改写**。备选：规则只作用于“需要确认的工具”（保持内置快捷路径不可动摇，但用户就无法用规则禁掉 `web_fetch`）。选可改写：显式规则优先于内置默认。

## 待确认事项

- **Q1 形状**：建议 `tools.approval_rules` 对象，规则正文只放在用户级/项目级规则文件；备选是保留旧 `tools.rules` 别名（兼容方便，但与既有命名混淆且需说明 `ParseEnv` 展开）。
- **Q2 项目级信任**：建议按 workspace 绝对路径与内容 hash 信任，项目 allow 默认忽略；备选是预先信任固定仓库（简单但换仓库或文件变更容易误信任）。
- **Q3 命令匹配**：建议按 shell 的正向白名单、精确 token 与末尾 `*`/`**`；备选是更宽松的字符串前缀（实用但无法安全区分包装器与控制语法）。命令 deny 仅建议性收紧是评审要求，原稿把 deny 恒优先视作安全边界的理由不再采用。
- **Q4 `[d]`**：建议保留，与 ACP `reject_always` 对齐；备选是不加，交互更小但没有“直接拒绝且不再问”的出口。
- **Q5 写回**：建议默认仅本会话，用户级可写，项目级只允许 deny；备选是允许项目 allow 写回（原稿理由是固化仓库规则更方便，评审反对理由是可写仓库形成提权通道）。
- **Q6 `file_remove`**：建议 `[a]` 只保存精确路径，不按目录放宽；备选是锚定目录且只允许直接文件，理由是减少重复询问，但递归/永久删除风险仍不值得。
- **Q7 ACP session/load**：建议只在同一进程内保留 session state 中的规则快照，跨进程重新询问；备选是把规则写入 session meta，理由是跨进程恢复方便，但会扩大持久化敏感状态和协议兼容面。
- **Q8 子 agent 规则传播**：建议创建时传入父 Policy 与会话规则快照，子 agent 的 `ask` 按 deny；备选是每次调用实时读取父策略，理由是能立即响应规则变化，但会引入并发和生命周期不一致。

## 结论与人工计划 Gate

方案把审批从“工具名 + 单次回答”换成“规则池 + 默认动作 + 取最强动作”：用户级/项目级文件写长期规则，chat 的 `a`/`d` 与 ACP 的 allow_always/reject_always 写会话级规则，MCP 白名单编译成 legacy 规则；`deny` 恒优先、项目级默认只能收紧、危险删除守卫与沙箱不受影响。主要风险是误配 `allow` 与命令匹配的保守性，用“默认动作不变 + manifest/trace 可查 + 开关回退”兜住。

落地清单（批准后进计划阶段细化）：

1. `pkg/agentcore/tools/rules` 匹配引擎 + 单测（命令 token 化、控制符豁免、路径 glob、工具名 glob、动作取最强）。
2. `internal/wiring` 加载/合并/校验 + `tools.approval_rules` 选项 + 两个文件；chat/run 另编译 MCP legacy 白名单；`approval_rule`/`approval_scope` 上报。
3. chat：判定接入、`a`/`d` 范围键、文案、写回与追问；`chat_approval.py` 同步更新。
4. run 与子 agent：默认动作与规则接入（收紧方向）。
5. ACP：选项集 + 规则判定（suga-1ojw 的这部分）。
6. 观测与文档：`suga manifest --json`、trace 渲染、`config/config.yaml`、README 中英、skills 参考、CHANGELOG。
7. 验收：单测（含 `internal/wiring/approval_table_test.go` 风格的表驱动）；伪终端冒烟新增 `scripts/ptysmoke/chat_rules.py`（allow 命中不再问、deny 命中被拒、写回文件后新进程仍生效）并做一次反向验证（临时改成“首次命中”，deny 用例必须失败）；`make smoke-chat` 全量回归；ACP 用 `scripts/acpprobe/acp_probe.py` 或 acpserver 单测验证 allow_always，真实 ACP 客户端行为记 UNVERIFIED_REAL_ENV。

同类产品对照（参考，不照搬）：Claude Code 的 permission rules 也是“工具 + 模式”的 allow/ask/deny 列表，并有项目级配置文件与“本次会话总是允许”这类选项；Codex 一类工具把“审批模式 + 沙箱”分开配置。本设计与它们共享“声明式规则 + 更强动作优先”的形态，差异在：命令按 token 前缀 glob 且含 shell 控制符时不允许放行、项目级默认只能收紧、危险删除守卫不可被规则绕过。这些对照只作形态参考，具体行为一律以本仓代码与本设计为准。
