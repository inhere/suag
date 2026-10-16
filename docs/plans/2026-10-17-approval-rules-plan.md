<!-- template_id: plan; template_version: 1.2.0 -->
# 审批规则（命令/路径 glob → allow|ask|deny）实施计划

> 状态：Draft 0.1（待用户审阅）

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-17 | suga（gofer job） | 依据已批准的审批规则设计 Draft 0.3 起草实施计划；拆分规则引擎、加载合并、入口接入、交互写回、ACP、观测与验收波次。 |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

### 思考模式与范围冻结

- thinking_mode：RIGOROUS。
- core_objective：将一次工具调用的审批收敛为可写、可审查、可复现的 `Decide(call) → allow|ask|deny + 命中规则`，并把 chat 的“本会话总是允许/拒绝”细化为命令前缀或路径范围。
- allowed_scope：`pkg/agentcore/tools/rules`、`pkg/agentcore/tools` 审批报告、`internal/wiring` 规则加载/合并/装配/写回、`internal/config` 配置与项目规则路径、`internal/chat` 审批交互、`internal/acpserver` 权限选项与判定、`internal/personal` trace、manifest、`config/config.yaml`、README 中英、skills 参考、CHANGELOG、`scripts/ptysmoke` 与其 Makefile 接入。
- non_goals：ACP `session/list|resume|close` 与 `fs/*` 客户端能力；`set_config_option`/`authenticate`；MCP HTTP/SSE transport；shell 进程级隔离；web_fetch 代理下的 SSRF 加强；危险删除守卫本身；只读模式工具集合；Gofer 侧改动；规则热加载以外的重载机制。
- expansion_policy：DEFER_OR_REQUEST。实现中若只增加路径/符号且不改变语义，记录操作性发现后继续；若改变默认动作、协议、状态机、范围或验收，停止并回到设计/计划 Gate。
- review budget：低暴露度；按设计的一次实现、单测、伪终端冒烟、一次反向验证。`tools.approval_rules.enabled: false` 是运行时回退开关，不代替验收。
- 停止条件：计划未获用户审阅批准不实施；任一安全匹配、项目信任、危险删除不变量或反向验证不成立时停止，不以放宽规则绕过。

## 目标与完成定义

目标：落地 `docs/design/2026-10-16-approval-rules-design.md` Approved Draft 0.3 的 D1–D11，并按其 Q1–Q8 的建议选项定稿实现。

完成定义（全部满足）：

1. 用户级、项目级、会话级规则按 `deny > ask > allow` 合并；工具身份、shell 命令、路径与 root 语义均由纯规则模块测试锁定。
2. chat、run、ACP、子 agent 使用同一规则模型但保留设计规定的入口默认动作；MCP legacy 白名单只在 chat/run 编译；危险删除守卫、沙箱、工具存在性与只读工具集合不被规则绕过。
3. chat 的 `a/d` 范围、`u/p/N` 写回、当前会话文案、非 TTY fail-closed 与重载失败保留上一份有效策略均可测试、可观测。
4. `ApprovalReport`、trace 与 `suga manifest --json` 能说明命中的来源、序号、规则摘要、scope 及被忽略项目规则。
5. 容器 `go test -race ./...`、`make smoke-chat`（含新 `chat_rules.py` 与反向验证）通过；主机 Windows `go test -count=1 ./...` 与 `staticcheck ./...` 通过。
6. 至少用一个真实 provider 完成一轮带工具调用的对话；ACP 用 `scripts/acpprobe/acp_probe.py` 或等价真实 ACP 客户端验证 allow/deny/always。无法执行时在交付记录中明确 `UNVERIFIED_REAL_ENV`，不宣称完成。
7. 配置、README 中英、skills 参考、CHANGELOG 同步，且每个实施任务均有独立提交，提交前只暂存本计划 owner 文件。

## 范围、排除项与授权

- 范围：本计划“目标”与“思考模式与范围冻结”列出的实现、测试、文档及冒烟；Git root 为 `inhere-tools/suga`。
- 排除：设计 non_goals；不修改 Gofer、不 push、不发版、不实现审批规则设计之外的 ACP 方法或新的安全隔离。
- 兼容：保留 `tools.approval`、`tools.auto_allow`、`tools.read_only_allow`、`chat.ask_outside_dirs`、`tools.allow_write`、`--read-only`、`--allow-write`、`--add-dir` 与 `SUGA_ADD_DIRS`；`enabled: false` 原样走旧入口路径。
- 授权：本计划批准后，按任务在本地 worktree 实施并提交；真实 provider/ACP 与主机验证只在已有配置和授权环境中执行。push、tag、release、Gofer 配置改动另行授权。
- `host_or_non_offline_action=REQUIRED`：真实 provider、ACP probe、Windows job 是完成 Gate 的验证动作，不在本地计划稿阶段执行。

## 输入与批准证据

- 设计：`docs/design/2026-10-16-approval-rules-design.md`，Draft 0.3 / 已批准（2026-10-16 用户：“全部按建议批准”；待确认事项均按文中建议选项定稿）。
- 设计落地清单：设计 §“结论与人工计划 Gate”第 1–7 项（规则引擎、加载接入、chat、run/子 agent、ACP、观测文档、验收）。
- 设计关联：`suga-ob4a`；`suga-1ojw` 的 ACP 权限选项部分纳入本计划，ACP 其余非目标保持不动；`suga-oyby` 的 `:add-dir`/越界授权语义保持不变。
- 事实基线：设计 §“已确认事实与规范”及本计划“能力发现”；实施前重新记录 HEAD、分支和 dirty 状态，若事实与设计冲突，按“待确认/风险”停止。

## Capability Discovery

### Capability decisions

| capability_id | required_capability | searched_candidates | direct_reuse | thin_adapter_or_owner_extension | decision | proven_gap | duplication_and_lifecycle_risk |
|---|---|---|---|---|---|---|---|
| CAP-01 | 纯规则模型、命令/路径/MCP 匹配与动作归并 | `pkg/agentcore/tools/approval.go:32-170`、`pkg/agentcore/tools/meta.go:53-68`、`internal/wiring/cli_mcp.go:14-107`、native `code_exec.go:85-108` | 审批层、工具元数据、MCP glob 校验与默认拒绝可复用 | 新增 `pkg/agentcore/tools/rules` 纯逻辑 owner；wiring 只负责来源与装配 | MINIMAL_NEW_MODULE | 现有代码没有声明式原生规则、shell 安全白名单或项目信任 | 规则匹配只能有一个能力层 owner；禁止在 chat/run/ACP 各写一份 matcher |
| CAP-02 | 用户/项目/会话规则加载、信任、重载与写回 | `internal/wiring/capabilities.go:48-77,110-132` 的分层文件加载、`internal/config/load.go`、YAML v3、output-rules 先例 | 项目文件路径约定、配置解析、锁/原子写回工具可复用 | `internal/wiring/rules.go`、`rules_write.go` 作为 wiring owner | OWNER_EXTENSION | 无 approval-rules 文件、hash trust 或每回合保留上一份策略 | 规则解析不下沉到能力层；写回与读取必须共用 schema 校验 |
| CAP-03 | chat 的范围化 always/deny 与交互 | `internal/wiring/chat_approver.go:22-125`、`tools.Asker`、`chat_approval.py` | 询问/非 TTY/`tools.approval` 与 askOutside 流程可复用 | 会话 Policy 快照、scope key、文案和持久化回答原位扩展 | OWNER_EXTENSION | 现有 `map[string]bool` 只有工具名，不能表达命令/目录 | 不新建第二套 chat 审批器；保持外层危险守卫与越界授权顺序 |
| CAP-04 | run/ACP/子 agent 同模判定 | `internal/acpserver/policy.go:49-170`、`internal/wiring/subagent.go:107-117`、runtime 装配 | 入口 approver、ACP BeforeTool 通知、子 agent 创建参数可复用 | 各入口只做策略结果翻译；Policy 在创建时快照给子 agent | OWNER_EXTENSION | ACP 目前只有 allow-once/reject-once，子 agent 为显式 allow-all | 同一规则语义不复制；入口默认动作仍由各入口保有 |
| CAP-05 | 审批原因观测与清单 | `pkg/agentcore/tools/approval.go:134-170`、`internal/personal/observe.go`、`internal/personal/trace.go`、`internal/wiring/manifest.go:27-39,115-190,219-278` | 现有 attrs、trace JSON、manifest 双路径可复用 | 追加 `Rule`/`Scope` 与 approval_rules 清单 | OWNER_EXTENSION | 现有报告只有 Source/Wait/Danger，无法解释具体命中 | manifest 运行时和静态路径需共用摘要构造，防止漂移 |

### New module candidates

| candidate_id | capability_id | proposed_module | searched_candidates | direct_reuse_gap | thin_adapter_or_owner_extension_gap | proven_gap | unique_owner_and_lifecycle | deletion_or_merge_handling |
|---|---|---|---|---|---|---|---|---|
| MOD-AR-01 | CAP-01 | `pkg/agentcore/tools/rules` | `pkg/agentcore/tools/approval.go:32-170`、`internal/wiring/cli_mcp.go:14-107` | 需要无 `internal/*` 依赖的纯 matcher 与 `Policy.Decide` | wiring 只构造 Policy，不复制匹配逻辑 | 已由设计确认：原生/项目/会话规则没有现成实现 | 随 agentcore/tools 能力层生命周期 | 若实现证明与既有 approval 可合并，保留深接口但不再造平行 matcher；否则该模块是唯一 owner |

### Rejected new tools

| rejected_candidate | capability_id | deletion_test_and_reason |
|---|---|---|
| ACP 独立第二套规则引擎 | CAP-04 | 删除后 Policy 仍应覆盖 ACP；两套语义会使 deny/always 与 chat 分叉 |
| 项目级 `config.yaml` 内联规则 | CAP-02 | 设计选择独立 `approval-rules.yaml`；删除该入口可避免程序追加打乱用户配置与提权通道 |

## Preflight

- P0：确认 worktree 根为 `inhere-tools/suga`、分支为当前计划分支；记录 HEAD、`git status --short`；owner 路径无未归属改动，发现冲突即停止。
- P1：确认基线设计仍为 Draft 0.3 / 已批准，且 `pkg/agentcore/layering_test.go` 不允许 `pkg/agentcore` 依赖 `internal/*`；规则 matcher 不得读配置或文件系统。
- P2：跑一次 `go test -race ./pkg/agentcore/tools/... ./internal/wiring/... ./internal/acpserver/...` 与现有 `make smoke-chat`，记录原始 exit code 与输出；基线失败不进入实现波次。
- P3：所有测试的 `SUGA_HOME` 指向临时目录；规则写回测试必须使用临时用户级/项目级文件，不能修改真实配置或仓库规则。
- P4：每个 shell allow 测试明确实际 shell（sh/bash、PowerShell、cmd），不得用平台别名替代；路径测试覆盖 Windows 语义、符号链接、硬链接、特殊文件、不存在目标和 ADS/尾部点空格。
- P5：任何配置错误启动失败；项目 allow 未被 workspace 绝对路径+规则文件 hash 信任时必须忽略；规则解析/删除/hash 变化重载失败保留上一次有效策略并产生 warning/trace。
- P6：每个交互任务必须扩展 ptysmoke，并进行反向验证；临时删除/反转被测分支后，对应断言必须失败，不得以“人工可验证”替代。
- P7：单个 Go 文件不得超过 1000 行；新增 schema 必须保持所有工具 schema 的 `properties` 非空约束。

## 波次与依赖

| 波次 | 任务 | 依赖 | 执行方式 |
|---|---|---|---|
| W0 | T0 基线与安全契约 | 无 | 1 个 gofer job；只记录，不改产品代码 |
| W1 | T1 规则值对象/工具 glob/动作归并；T2 shell matcher；T3 path matcher | T0；T2/T3 可与彼此并行，均依赖 T1 | 最多 3 个并行 job；每个单独提交 |
| W2 | T4 配置解析、来源合并、项目信任与重载；T5 MCP legacy 编译与入口默认动作 | T1–T3；T5 依赖 T4 的 Policy 装配约定 | T4→T5 |
| W3 | T6 ApprovalReport/审批层装配；T7 chat 范围规则、文案与写回 | T4/T5；T7 依赖 T6 | T6→T7 |
| W4 | T8 run/子 agent；T9 ACP allow_always/reject_always | T6；T8/T9 可并行 | 2 个并行 job |
| W5 | T10 trace/manifest/config 输出；T11 chat 规则 ptysmoke 与反向验证 | T6–T9；T11 依赖 T7 | T10 与 T11 可并行 |
| W6 | T12 文档、全量回归、真实 provider/ACP/Windows 验证 | W5 | 监督会话 + 主机/真实环境 job |

## 任务

### T0 基线与安全契约

- 文件：无产品改动；验证记录写入 gofer job/交付报告。
- 动作：记录 HEAD、分支、dirty 状态；确认设计 D1–D11/Q1–Q8；运行现有审批/ACP 测试和 `make smoke-chat`，确定冒烟基线与临时 `SUGA_HOME` 约定。
- 验证命令：`go test -race ./pkg/agentcore/tools/... ./internal/wiring/... ./internal/acpserver/...`；`make smoke-chat`。
- 完成标准：两条命令 exit code 为 0；原始输出和环境记录可追溯；无 owner 冲突。
- 反向验证方法：不适用产品反向验证；以确认“基线失败不进入 W1”的 fail-closed 记录为替代。
- 预计改动规模：XS（无文件改动）。
- 依赖/并行：无。

### T1 规则值对象、工具 glob 与动作归并

- 文件：`pkg/agentcore/tools/rules/`（新模块及测试）；必要时 `pkg/agentcore/tools/meta.go` 仅增加安全身份辅助。
- 动作：实现 `Rule`、`Target`、`Policy`、`Action`、来源/序号/模式；工具名 glob（原生工具无 `/`，MCP 必须 `server/toolglob`）；`deny > ask > allow` 的 `Decide` 与代表性命中；保持纯逻辑、无配置/文件系统依赖。
- 验证命令：`go test -race ./pkg/agentcore/tools/rules/... ./pkg/agentcore/...`。
- 完成标准：表驱动测试覆盖来源合并、模式过滤、默认动作、同强度多命中代表性规则稳定、MCP 身份；layering test 通过；schema/错误消息可供 wiring 使用。
- 反向验证方法：临时把动作归并改成首次命中，新增“后续 deny 覆盖 allow”用例必须失败；恢复实现后再通过。
- 预计改动规模：M（约 3–5 个 Go 文件、250–450 行含测试）。
- 依赖/并行：T0 后；T2、T3 依赖 T1，可并行。

### T2 shell 命令 token 与正向白名单 matcher

- 文件：`pkg/agentcore/tools/rules/command.go`（新）及测试；复用/对齐 `pkg/agentcore/tools/native/code_exec.go:85-108` 的 `resolveShell` 语义，不改 shell 执行器。
- 动作：按 sh/bash、PowerShell、cmd 的精确 ASCII 白名单、配对非嵌套引号、空格/token 规则实现 allow 匹配；仅末尾独立 `*`/`**` 扩展 token；Windows 忽略大小写及 `.exe`；控制符、替换/展开、未知 shell 不产生 allow；deny 仍可整串匹配；不提供包装器/解释器前缀放行。
- 验证命令：`go test -race ./pkg/agentcore/tools/rules/...`。
- 完成标准：覆盖合法/非法字符、换行/tab、引号、token 数、shell 差异、Windows 比较、别名/包装器拒绝；allow 与 deny 的安全差异固定。
- 反向验证方法：临时删除控制字符检查或把 token 比较改成 `strings.HasPrefix`，相应用例必须失败。
- 预计改动规模：M（2–4 个 Go 文件、250–400 行含测试）。
- 依赖/并行：T1 后，可与 T3 并行。

### T3 文件路径、root 与真实目标 matcher

- 文件：`pkg/agentcore/tools/rules/path.go`（新）及测试；与 `pkg/agentcore/tools/native/pathresolve.go:102-115`、`file.go:87-92` 对齐；必要时只增加导出适配，不改沙箱边界。
- 动作：实现文件工具限定身份（仅明确 `file_write`/`file_edit`/`file_remove`）、锚定 root 的 glob、词法路径与 `EvalSymlinks` 真实路径双检；硬链接、符号链接、特殊文件、解析失败、Windows 短名/ADS/尾部点空格不产生 allow；deny 解析失败按命中；`file_remove` 精确路径，写/编辑按目录范围。
- 验证命令：`go test -race ./pkg/agentcore/tools/rules/... ./pkg/agentcore/tools/native/...`。
- 完成标准：覆盖 workspace/附加 `:rw` root、目标不存在时最深已存在祖先、目录 glob 的 `*` 深度、root 越界和 file tool 身份，且不改变现有 os.Root 沙箱测试。
- 反向验证方法：临时只检查词法路径而跳过真实路径，symlink/hardlink allow 用例必须失败；临时把 `file_remove` 目录范围放宽，精确路径用例必须失败。
- 预计改动规模：L（3–5 个 Go 文件、350–600 行含测试）。
- 依赖/并行：T1 后，可与 T2 并行。

### T4 规则文件加载、校验、合并、项目信任与每回合重载

- 文件：`internal/wiring/rules.go`（新）、`internal/config/` 规则路径/配置解析、`$SUGA_HOME/approval-rules.yaml` 与 `<workspace>/.suga/approval-rules.yaml` 的测试夹具、对应表驱动测试。
- 动作：读取 `tools.approval_rules`（enabled、always_prefix_tokens、persist_ask、project_trust）；加载用户/项目文件，限制 64 KiB/规则条数并禁用 YAML alias/anchor；校验字段互斥、tool/path/command/action/modes；合并 legacy 以外的来源；按 workspace 绝对路径+内容 hash过滤项目 allow；保护路径 allow 钳制；启动失败、每回合 size/mtime/hash 变化失败保留旧策略并 warning/trace；为写回提供共享 schema/锁/原子 rename 接口。
- 验证命令：`go test -race ./internal/wiring/... ./internal/config/... ./pkg/agentcore/tools/rules/...`。
- 完成标准：设计 3.1、3.3、3.10 的所有字段/限制均有单测；旧配置键仍被接受；`enabled:false` 完全走旧路径；项目 deny/ask/allow 信任矩阵与文件删除/hash 变化行为正确。
- 反向验证方法：临时将项目 allow 默认纳入或在 hash 变化时继续使用，信任矩阵用例必须失败；临时把坏文件替换成空策略，保留旧策略用例必须失败。
- 预计改动规模：L（4–7 个 Go 文件、500–800 行含测试）。
- 依赖/并行：T1–T3 后；T5 依赖 T4。

### T5 MCP legacy 编译与入口默认动作

- 文件：`internal/wiring/cli_mcp.go`、新/改 `internal/wiring/approval_policy.go`、runtime 装配测试；必要时 `internal/wiring/runtime.go`。
- 动作：将 chat/run 的 `tools.auto_allow`、`tools.read_only_allow` 编译为 `source=legacy` 的规则；保留原校验与 `--read-only` 语义；ACP 不读取 legacy；定义 chat/run/ACP/子 agent/MCP/只读/agent-state 的设计默认动作，并保证规则不能改变工具存在性、沙箱、危险删除或入口 read_only 钳制。
- 验证命令：`go test -race ./internal/wiring/... ./pkg/agentcore/tools/rules/...`。
- 完成标准：MCP 默认和白名单、显式 deny 覆盖 legacy、ACP 隔离、无终端 ask→deny 及旧配置兼容测试通过。
- 反向验证方法：临时让 ACP 读取 legacy 或让 project ask 放宽 default deny，隔离/钳制用例必须失败；临时移除 dangerous guard 外层，危险删除用例必须失败。
- 预计改动规模：M（3–5 个 Go 文件、300–550 行含测试）。
- 依赖/并行：T4 后。

### T6 ApprovalReport 与 chat/run/审批层装配

- 文件：`pkg/agentcore/tools/approval.go`；`internal/wiring/approval_policy.go`、`chat_approver.go`、`runtime.go`；对应 approval/danger/chat 测试。
- 动作：`ApprovalReport` 增加 `Rule`、`Scope`；将 Policy.Decide 翻译为 allow/ask/`tools.Denied`；显式规则先于只读/agent-state快捷放行，保留外层 `guardDangerous`、越界目录授权和 3 次拒绝上限；chat/run 共享规则判定，run 无终端 ask 按 deny；会话规则以正确生命周期挂在运行时。
- 验证命令：`go test -race ./pkg/agentcore/tools/... ./internal/wiring/... ./internal/personal/...`。
- 完成标准：allow 不询问、ask 只询问一次、deny 回合继续、report 记录命中与 scope；危险删除、越界目录、工具未注册、禁用写工具回归通过；`pkg/agentcore/layering_test.go` 通过。
- 反向验证方法：临时在内置 read-only 快捷路径前返回 allow，显式 deny/ask 用例必须失败；临时把 `ErrDenied` 当 abort，拒绝后回合继续用例必须失败。
- 预计改动规模：L（5–8 个 Go 文件、500–850 行含测试）。
- 依赖/并行：T4、T5 后；T7 依赖 T6。

### T7 chat 范围化 `a/d`、询问文案与规则写回

- 文件：`internal/wiring/chat_approver.go`；`internal/chat/approval_reply.go`（新）；`internal/wiring/rules_write.go`（新）；`internal/config` 配置读取；`internal/wiring/chat_approver_test.go`、写回测试；`scripts/ptysmoke/chat_rules.py`（初稿可在 T11 完善）。
- 动作：按工具类型生成 shell 前缀/写编辑目录/删除精确路径/其他工具名 scope；仅安全 shell 命令显示 `[a]`；增加 `[d]`；回答后按 `persist_ask` 询问 `u/p/N`，回车仅会话；项目仅允许 deny 写回；YAML v3 Node 保留注释，文件锁、临时文件原子 rename、回读校验；写失败只警告但会话规则继续生效；同一运行时/会话并发串行化。
- 验证命令：`go test -race ./internal/wiring/... ./internal/chat/...`。
- 完成标准：文案、答案别名、默认拒绝、非 TTY、范围 key、写回后新进程生效、项目 allow 禁止写回、坏写回不丢会话规则均通过；旧审批关闭开关回归通过。
- 反向验证方法：临时把范围 key 恢复成工具名，连续两个不同命令/目录的隔离用例必须失败；临时将默认写回改为允许项目 allow，安全用例必须失败；pty 反向在 T11 完成。
- 预计改动规模：L（5–8 个 Go/Python 文件、500–850 行含测试）。
- 依赖/并行：T6 后；与 T8/T9 不并行。

### T8 run 与子 agent 的 Policy 快照

- 文件：`internal/wiring/runtime.go`、`internal/wiring/subagent.go`、子 agent 相关测试。
- 动作：run 使用设计 default allow 与显式 deny/ask（无终端 ask→deny）；创建子 agent 时传递父 Policy 与会话规则快照，子 agent 的 ask 按 deny；修正当前显式 allow-all approver 事实，不误以为 nil；规则变化不实时污染已创建子 agent。
- 验证命令：`go test -race ./internal/wiring/... ./internal/personal/...`。
- 完成标准：run、子 agent 的 allow/deny、快照传播、无写工具集合与现有 task/agent 行为通过；旧 run 不带规则时保持现状。
- 反向验证方法：临时让子 agent 每次读取父实时 Policy，快照一致性测试必须失败；临时把 ask 视为 allow，非终端收紧测试必须失败。
- 预计改动规模：M（3–5 个 Go 文件、300–550 行含测试）。
- 依赖/并行：T6 后，可与 T9 并行。

### T9 ACP Policy 与 `allow_always/reject_always`

- 文件：`internal/acpserver/policy.go`、`server.go`（仅 session state 生命周期接入）、`policy_table_test.go`；必要时 ACP 适配类型文件。
- 动作：保留 `allow`/`reject` OptionId，增加 `allow_always`/`reject_always`；permissionHook 先判 Policy，allow 不发 permission 但照发 `tool_call`，deny 返回 tool error；always 规则放入同进程 session state，`session/load` 同进程保留快照，跨进程不恢复，不写规则文件；read_only 与 ACP 默认动作保持设计语义。
- 验证命令：`go test -race ./internal/acpserver/... ./internal/wiring/...`；`python scripts/acpprobe/acp_probe.py --help`（再按环境运行真实 probe）。
- 完成标准：协议选项序列化、allow/deny/ask、session/load、MCP legacy 隔离、通知顺序与取消行为有单测；若 SDK 不接受新增 option kind，先停在协议 Gate，不私自改协议。
- 反向验证方法：临时删除 `allow_always` 的 session 规则保存，同类第二次调用必须重新询问；临时把 allow 路径也发 permission，no-prompt 断言必须失败。
- 预计改动规模：M/L（4–6 个 Go 文件、400–700 行含测试）。
- 依赖/并行：T6 后，可与 T8 并行；真实 ACP 在 T12。

### T10 trace、manifest 与配置可诊断性

- 文件：`internal/personal/observe.go`、`internal/personal/trace.go`、`internal/cli/trace.go`、`internal/wiring/manifest.go`、`internal/cli/manifest.go`、对应 JSON/文本测试。
- 动作：工具/denial attrs 增加 `approval_rule`、`approval_scope`；保留 Source 旧值（会话规则仍显示 `always`）；trace 文本按设计渲染；manifest JSON 增加各来源路径、有效条数、忽略原因和 `tools.approval` 默认动作；静态/运行时构造共用摘要，敏感内容按既有输出策略处理。
- 验证命令：`go test -race ./internal/personal/... ./internal/wiring/... ./internal/cli/...`；`go run ./cmd/suga trace --help`、`go run ./cmd/suga manifest --help`。
- 完成标准：规则命中、项目 allow 被忽略、session scope、旧 trace 字段兼容和 manifest 两路径一致测试通过。
- 反向验证方法：临时不写 `approval_rule` attr，trace 解释性断言必须失败；临时只改运行时 manifest，静态/运行时一致性测试必须失败。
- 预计改动规模：M（5–8 个 Go 文件、350–650 行含测试）。
- 依赖/并行：T6 后，可与 T11 并行。

### T11 chat 规则伪终端冒烟与反向验证

- 文件：`scripts/ptysmoke/chat_rules.py`（新）、`scripts/ptysmoke/fakellm.py`（仅必要的请求/工具序列扩展）、`scripts/ptysmoke/chat_approval.py`（回归）、`Makefile`、`scripts/ptysmoke/README.md`。
- 动作：用 `ptydrive.PtySession` 驱动真实 `suga chat` 与 fakellm，覆盖：命中 allow 不提问；命中 deny 产生 tool error 且回合继续；首次询问 `a` 后按命令前缀/目录不再问；`d` 后同 scope 不再问；`u` 用户级写回并在新进程生效；`p` 仅 deny 可写回；不安全 shell 不显示 `[a]`；`suga trace --json` 和 manifest 可见原因；`enabled:false` 回到旧行为。纳入 `make smoke-chat`，不依赖人工输入。
- 验证命令：`make smoke-chat`；单场景按 `python scripts/ptysmoke/chat_rules.py` 运行；临时 `SUGA_HOME`/fake provider 日志必须可复核。
- 完成标准：所有断言、trace attrs、规则文件内容及新进程加载通过；现有 `chat_approval.py` 无回归；PTY 仅 Linux/macOS，Windows 行为另由 T12 主机测试记录。
- 反向验证方法：临时把 matcher 改为首次命中，deny 覆盖 allow 场景必须失败；临时跳过 chat approver 的 Policy.Decide，allow“不提问”或 deny 断言必须失败；临时去掉写回，跨进程生效断言必须失败。
- 预计改动规模：M（3–6 个 Python/Make/Go 测试文件、250–500 行）。
- 依赖/并行：T7、T10 后；与 T10 可并行但合入前需同步。

### T12 文档、全量回归与真实环境 Gate

- 文件：`config/config.yaml`、`README.md`、`README.zh-CN.md`、`skills/suga-guide/references/config.md`、`CHANGELOG.md`；不改设计正文。
- 动作：说明规则文件格式、动作优先级、shell/path 安全限制、项目 trust/hash、chat `a/d/u/p/N`、ACP always、入口默认动作、manifest/trace、`enabled:false` 回退与已知限制；运行容器/主机/真实 provider/ACP 验证，汇总 `UNVERIFIED_REAL_ENV`。
- 验证命令：容器 `gofmt -l $(git ls-files "*.go")`、`go vet ./...`、`go test -race ./...`、`make smoke-chat`；主机 `go test -count=1 ./...`、`staticcheck ./...`；真实 provider 一轮带工具调用；`scripts/acpprobe/acp_probe.py` 一轮 always/deny；Windows 由 gofer exec job 跑主机测试。
- 完成标准：命令 exit code 和原始输出均为成功；文档与默认配置一致；真实验证有 trace/日志证据，不能执行的项目明确列出而不伪造通过。
- 反向验证方法：本任务不新增产品分支；以 T11 反向验证记录、关闭 `enabled` 的兼容回归和全量测试作为反向证据，若任一反向检查不失败则 Gate 不通过。
- 预计改动规模：M（6–8 个文档/配置文件；无新业务代码）。
- 依赖/并行：T10、T11、T8、T9 全部完成。

## 回滚与恢复

- 每个任务在独立 worktree/分支完成并按功能点单独提交；未通过验收的任务不合入，保留 worktree 供修复，禁止用 `git reset`/`stash` 覆盖他人改动。
- W1–W2 的纯规则与加载提交可独立 revert；W3–W4 的入口接入提交按任务逆序 revert；文档/冒烟提交最后 revert 不影响旧代码。
- 运行时回退：`tools.approval_rules.enabled: false` 必须恢复 2026-10-16 旧路径；旧配置键与旧 MCP 白名单仍可用。
- 写回文件恢复：测试只使用临时目录；实际写回采用原子 rename，失败保留原文件与会话内规则。
- 恢复点：每波次记录合入后的 HEAD、测试/冒烟原始输出和 `UNVERIFIED_REAL_ENV` 项；若设计语义需改变，停止并返回设计 Gate。

## 人工 Gate

1. 用户审阅并批准本 Draft 0.1；批准计划不等于批准扩大设计范围。
2. T4/T5 完成后进行一次安全语义 Gate：确认项目 trust、shell/path fail-closed、危险删除守卫和 MCP/ACP 隔离仍与设计一致。
3. T11 完成后由监督方审阅 ptysmoke 断言及反向验证结果；不得以人工 chat 操作为替代。
4. T12 的真实 provider、ACP probe、Windows 主机 job 是外部环境 Gate；不能执行时必须在交付说明写 `UNVERIFIED_REAL_ENV`。
5. push、发版、Gofer 配置变更不在本计划内，另行授权。

## 可追溯性

| 设计条目 / 定稿选项 | 任务 | 验证 |
|---|---|---|
| D1 独立用户/项目规则文件、Q1 `tools.approval_rules` 对象 | T4、T7、T12 | 配置/路径/写回测试；新进程读取；文档 |
| D2 `deny > ask > allow` | T1、T4、T6、T11 | 表驱动归并；PTY deny 覆盖 allow；反向首次命中失败 |
| D3 shell token + shell 正向白名单、Q3 严格匹配 | T2、T7、T11 | shell matcher 矩阵；不安全命令无 `[a]`；反向控制符/前缀失败 |
| D4 项目 allow 默认忽略，Q2 workspace+hash trust | T4、T11 | trust/hash 矩阵；hash 变化保留旧策略；跨进程场景 |
| D5 工具类型 scope、Q6 file_remove 精确路径 | T3、T7、T11 | path/scope 单测；不同目录/路径隔离冒烟 |
| D6 `a/d` 后追问，Q4 保留 `[d]`，Q5 默认会话/用户可写/项目只 deny | T7、T11 | 答案解析/写回矩阵；PTY `u/p/N` 与文件检查 |
| D7 MCP legacy 合并、chat/run 使用、ACP 隔离 | T5、T9 | MCP 表驱动；ACP 不读 legacy；显式 deny 覆盖白名单 |
| D8 规则不能放行危险删除/沙箱/工具存在性 | T3、T5、T6 | danger/path/disabled 回归；反向移除外层守卫失败 |
| D9 ACP always 会话级、不写规则文件，Q7 同进程 load | T9、T12 | ACP policy 表；acp_probe；session/load 与跨进程重新询问 |
| D10 每回合重载、失败保留上一份有效策略 | T4、T10 | size/mtime/hash/delete 失败测试；warning/trace/manifest |
| D11 显式规则可改写只读/agent-state 默认 | T5、T6、T8 | read-only/agent-state ask/deny 测试；入口默认矩阵 |
| 入口默认动作与子 agent Q8 创建时 Policy 快照 | T5、T8、T9 | run/ACP/subagent 测试；快照传播反向验证 |
| `ApprovalReport.Rule/Scope`、trace、manifest | T6、T10、T11 | trace JSON/文本、manifest JSON 一致性；PTY 断言 |
| 设计 non_goals 与兼容/无迁移 | T4、T9、T12 | layering、旧配置回归、全量测试、范围审阅 |

## 待确认/风险（设计已批准的选项，不在实施中重新改语义）

- ACP SDK 是否接受 `allow_always/reject_always` 对应的 option kind/OptionId：T9 先做协议单测；若 SDK/协议事实冲突，停止并返回用户 Gate，不自创兼容语义。
- 设计称子 agent approver “恒 nil”，当前代码事实是显式 allow-all `ApproverFunc`（`internal/wiring/subagent.go:107-117`）；实施按“创建时 Policy 快照、ask→deny”设计定稿，不能照搬过时事实。
- manifest 既有静态与运行时构造路径（`internal/wiring/manifest.go:27-39,115-190,219-278`），必须共用摘要生成；若不能统一，列为阻断而非只更新一条路径。
- PTY smoke 仅 Linux/macOS；Windows 控制台差异、真实 provider 与真实 ACP 结果可能为 `UNVERIFIED_REAL_ENV`，但不把未执行写成通过。

## 完成 Gate

- T0–T12 的依赖任务全部完成并按功能点提交；`git status --short` 仅允许无关为空，最终 `git diff --check` 无输出。
- 规则、入口、交互、观测、文档均有表驱动/集成/ptysmoke 证据，且每个新增反向验证曾确认“去掉被测逻辑后检查失败”。
- 容器 `gofmt -l`、`go vet`、`go test -race ./...`、`make smoke-chat`；主机 `go test -count=1 ./...`、`staticcheck ./...`；provider/ACP 真实验证或明确 `UNVERIFIED_REAL_ENV` 全部留痕。
- 设计每个 D/Q、兼容项、非目标和安全不变量都能在本计划任务或 Gate 中定位；未解决的语义变化不合入。
- 完成后由监督方汇报波次、任务、验证原始输出与风险；push/发版另行请求。
