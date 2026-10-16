<!-- template_id: plan; template_version: 1.2.0 -->
# 写操作快照与 `:undo` 实施计划

> 状态：Draft 0.1（待用户审阅）

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-17 | suga（gofer job） | 依据已批准的写操作快照与 `:undo` 设计 Draft 0.3 起草实施计划；拆分 native 快照钩子、会话存储、回滚安全检查、通知观测、chat/CLI、清理与验收波次。 |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

### 思考模式与范围冻结

- thinking_mode：RIGOROUS。
- core_objective：让 `file_write`、`file_edit`、`file_remove` 的文件改动在回合结束后可审查、可确认、可安全回滚；回滚前列出恢复/删除/跳过/不可恢复项，回滚后以 `suga.undo` 通知模型。
- allowed_scope：`pkg/agentcore/tools/native` 的 `Env` 与三个写工具快照钩子；`internal/personal` 的快照存储、回滚、checkpoint 清理、通知与事件；`internal/wiring` 的装配和配置；`internal/chat` 的 `:checkpoints`/`:undo`；`internal/cli` 的 `suga undo`；`config/config.yaml`、README 中英、CHANGELOG、相关 tests 与 `scripts/ptysmoke`。
- non_goals：`shell_exec` 改动的自动回滚；`:redo`；按文件选择性回滚/任意历史时间旅行；ACP 内 undo 交互；整库备份/恢复；spill 压缩与 suga-zy9f 的全局备份/配额；改变 `file_remove` trash 7 天和忽略目录永久删除语义；`suga export` 集成；Gofer 侧改动。
- expansion_policy：DEFER_OR_REQUEST。快照格式、root 可信来源、冲突语义、锁顺序、失败模式、CLI 退出码和交互均冻结为设计 Draft 0.3；需要改变它们时停止并返回设计/计划 Gate。
- review budget：低暴露度；一次实现 + 单测 + 伪终端冒烟 + 一次反向验证。快照开关和 `on_error: continue` 是设计内的显式选项，不可用来掩盖默认 fail-closed。
- 停止条件：计划未获用户审阅批准不实施；任一恢复可能越 root、覆盖外部改动、绕过保护路径或破坏 trash 语义时停止。

## 目标与完成定义

目标：落地 `docs/design/2026-10-16-undo-checkpoints-design.md` Approved Draft 0.3 的 D1–D10、U1–U8 相关行为，并按 Q1–Q8 的建议选项定稿实现。

完成定义（全部满足）：

1. 三个文件写工具均在审批通过、实际 I/O 前按调用建立快照；`file_edit` 复用已读原文；同一回合同一路径多次写入按调用逆序可恢复；删除复用既有 trash 并正确报告 purged/混合删除。
2. 快照以 `$SUGA_HOME/sessions/<sid>/snapshots/<turn_id>/` 目录保存二进制原内容和自描述 `meta.json`，权限、root、hash、大小、状态、上限、清理和锁语义满足设计；不建表、不进模型上下文/transcript/export，老会话无需迁移即可读取。
3. `:checkpoints`、`:undo`、`:undo N`、`force` 和 `suga undo` 完成清单、确认、冲突/不可恢复处理、逆序恢复、幂等状态、pre-undo 与退出码；shell 仅提示 git 备选，不被误报为已回滚。
4. 回滚后写入待投递 `suga.undo` 注入条目和 `EventUndo`，不自动开回合；trace attrs 能说明恢复结果；后台任务风险被提示。
5. 容器 `go test -race ./...`、`make smoke-chat`（含新 `chat_undo.py` 与反向验证）通过；主机 Windows `go test -count=1 ./...`、`staticcheck ./...` 通过。
6. 至少用一个真实 provider 完成带写工具调用的对话，核对快照、undo 和 trace；无法执行时明确 `UNVERIFIED_REAL_ENV`，不把假模型结果写成真实验证。
7. 每个实施任务单独提交，提交前 `git status --short` 无夹带；只改计划范围文件。

## 范围、排除项与授权

- 范围：本计划“目标”与“思考模式与范围冻结”列出的实现、测试、文档和冒烟；Git root 为 `inhere-tools/suga`。
- 存储兼容：不增加 SQLite 表/列，不做历史数据迁移；快照目录按需创建。老会话没有 `snapshots/` 时报告“没有可回滚快照”，新二进制不改变 `outputs`/transcript/export；旧二进制忽略新目录。
- 排除：设计 non_goals；不改变写工具现有沙箱、`file_remove` trash/purge 判定、ACP 工具调用能力、不增加 `--undo-after` 或模型可调用 undo 工具。
- 授权：本计划批准后可按任务在本地 worktree 实施、运行容器与主机验证并本地提交；真实 provider 验证只使用已有配置。push、tag、release、Gofer 改动另行授权。
- `host_or_non_offline_action=REQUIRED`：真实 provider 与 Windows 主机 job 是完成 Gate 的验证动作，计划稿阶段不执行。

## 输入与批准证据

- 设计：`docs/design/2026-10-16-undo-checkpoints-design.md`，Draft 0.3 / 已批准（2026-10-16 用户：“全部按建议批准”；待确认事项均按文中建议选项定稿）。
- 设计落地清单：设计 §“结论与人工计划 Gate”第 1–5 项（native 钩子、personal 存储/回滚、chat/CLI、观测文档、验收）。
- 设计关联：`suga-wl0l`；`suga-zy9f` 只作为 spill/gc 配额边界参照，不在本计划实现；`suga-oyby` 的附加目录与 `:rw` 语义必须被快照 root 校验复用。
- 事实基线：设计 §“已确认事实与规范”及本计划“能力发现”；实施前重新记录 HEAD、分支和 dirty 状态，若代码事实与设计冲突，按“待确认/风险”停止。

## 能力发现

### Capability decisions

| capability_id | required_capability | searched_candidates | direct_reuse | thin_adapter_or_owner_extension | decision | proven_gap | duplication_and_lifecycle_risk |
|---|---|---|---|---|---|---|---|
| CAP-UN-01 | 写工具执行前后传递快照信息 | `native.Env`、`file.go`、`edit.go`、`remove.go`、`native.Catalog` | Env/工具 catalog、`ApplyEdit`、现有 trash/origin metadata 可复用 | Env 只持有导出的 Snapshotter 接口；三个工具各在既有 owner 内调用 | OWNER_EXTENSION | 当前工具无快照钩子，native 不能依赖 `internal/personal` | 不能在 wiring 与 native 各存一份快照；I/O 前后状态必须由同一调用记录更新 |
| CAP-UN-02 | 会话/回合/调用级安全落盘与清理 | `internal/personal/spill.go`、`process.SecureDir`、`validateSessionID`、sessions 路径 | 目录安全、hash 文件名、OpenRoot、权限、session scope 可复用 | 新 `SessionSnapshots` 作为与 `SessionSpiller` 同族的唯一存储 owner | OWNER_EXTENSION | 没有快照目录、meta 状态、大小/保留/全局清理 | 不建 DB BLOB；避免与 spill、suga-zy9f 建第二套全局配额语义 |
| CAP-UN-03 | 回合选择、冲突检测、逆序恢复和锁 | `Runner.Compact`、session gate、`process` 跨进程锁、store turns | Runner API、回合排序、锁顺序、session cwd/meta 可复用 | 新 `internal/personal/undo.go` 编排，快照模块只负责存取 | OWNER_EXTENSION | 无 undo、pre-undo、root/real path/hash 校验 | chat 与 CLI 必须调用同一 Runner/Undo 核心，不能各自恢复 |
| CAP-UN-04 | 回滚后模型通知与观测 | `EntryUser`/metadata、`injectedItemKeys`、task/steer 待投递、events/trace | 条目投递、上下文回放、event attrs、trace JSON 原样输出可复用 | 添加 `suga.undo` metadata 和 `EventUndo` | DIRECT_REUSE + OWNER_EXTENSION | 现有通知没有 undo 事实和恢复统计 | 不把 undo 误做 `context_update` 或真实用户请求；不进 export/transcript |
| CAP-UN-05 | chat/CLI 交互 | chat 本地命令 switch、帮助常量、`openCoreStore`、CLI root 注册 | `:compact`/目录命令、确认器、CLI session 前缀解析、store 句柄可复用 | 新 `internal/cli/undo.go`，chat 只做解析和渲染 | OWNER_EXTENSION | 无 checkpoint 清单/确认/退出码 | chat/CLI 共享 undo 核心与锁；不能复制恢复算法 |

### New module candidates

| candidate_id | capability_id | proposed_module | searched_candidates | direct_reuse_gap | thin_adapter_or_owner_extension_gap | proven_gap | unique_owner_and_lifecycle | deletion_or_merge_handling |
|---|---|---|---|---|---|---|---|---|
| MOD-UN-01 | CAP-UN-02 | `internal/personal/snapshots.go` | `internal/personal/spill.go` | 需要二进制 before、meta/state、容量与清理 | 复用 spill 安全写入约定，但不复用其脱敏/输出接口 | 设计确认当前没有 snapshot 实现 | 与 sessions 目录同生命周期，受 gc 覆盖 | 若存储能完全合入 spill 会损失语义；保留独立 owner，gc 只按目录涵盖 |
| MOD-UN-02 | CAP-UN-03 | `internal/personal/undo.go` | `runner.go`、`gate.go`、store turns | 需要逆序恢复和冲突矩阵 | Runner 上挂 `Undo`/`Checkpoints`，底层 root 校验共用 native helpers | 设计确认没有回滚入口 | chat/CLI 唯一执行 owner | 删除 chat/CLI 层恢复代码后核心仍可测试 |

### Rejected new tools

| rejected_candidate | capability_id | deletion_test_and_reason |
|---|---|---|
| git 基线自动回滚工具 | CAP-UN-03 | 删除后文件工具快照仍满足目标；git 会误含用户未提交修改、无法覆盖非 git/仓库外路径，设计选 G1 只提示 |
| 模型可调用的 undo tool | CAP-UN-05 | 撤销是用户动作；删除该工具可避免模型自行覆盖用户状态，chat/CLI 入口仍足够 |
| ACP 内 `undo` 方法 | CAP-UN-05 | ACP 客户端已有撤销面且不在范围；共享写工具仍可快照，CLI 可回滚会话 |

## 预检（preflight）与 fail-closed 条件

- P0：确认 worktree 根、分支、HEAD、`git status --short`，确认 owner 路径无他人改动；冲突即停止。
- P1：确认基线设计仍为 Draft 0.3 / 已批准；运行 `go test -race ./pkg/agentcore/tools/native/... ./internal/personal/... ./internal/chat/... ./internal/cli/...` 与现有 `make smoke-chat`，记录 exit code 和原始输出；基线失败不进入 W1。
- P2：确认 `pkg/agentcore` 分层约束：native 只能依赖导出的接口，不能导入 `internal/personal`；快照器经 `native.Env` 注入。
- P3：所有快照测试使用临时 `SUGA_HOME` 和临时 workspace/`:rw` 目录；测试必须覆盖文件不存在、二进制、symlink/hardlink/special、Windows 大小写/锁语义，并清理敏感副本。
- P4：默认 `tools.snapshots.on_error=deny` 的快照不可写、清理不确定、锁失败、root 不可信、before 校验失败均必须发生在写工具 I/O 前；`continue` 只能是显式配置并向模型说明不可撤销。
- P5：锁顺序固定为进程内 session gate → 会话目录跨进程文件锁；CLI、chat、gc 不得各自采用不同顺序；锁拿不到立即返回会话忙。
- P6：快照元数据不能进入模型上下文、transcript、export；`suga.undo` 只能是待投递注入条目且不自动开回合；shell 命令只出现在清单/结果提示。
- P7：每个交互任务必须有 ptysmoke 场景与反向验证；临时关掉快照写入后，恢复断言必须失败；不能以人工 chat 代替。
- P8：单个 Go 文件不得超过 1000 行；新增 tool schema 若有零参数工具也须有非空 `properties`，不引入空 schema。

## 波次与依赖

| 波次 | 任务 | 依赖 | 执行方式 |
|---|---|---|---|
| W0 | T0 基线与存储安全契约 | 无 | 1 个 gofer job；只记录 |
| W1 | T1 快照接口与 file_write/edit；T2 file_remove 两阶段钩子 | T0；T1/T2 可并行但 T2 依赖快照接口约定 | T1→T2 接口确认 |
| W2 | T3 SessionSnapshots 落盘、元数据、上限与清理 | T1/T2 | 1 个 gofer job |
| W3 | T4 checkpoint 选择/锁/清单；T5 冲突校验与逆序恢复 | T3；T4→T5 | 顺序执行 |
| W4 | T6 undo 通知、事件、trace；T7 wiring/config/gc | T5；T6/T7 可并行 | 2 个并行 job |
| W5 | T8 chat 命令/确认/ptysmoke；T9 CLI | T4–T7；T8/T9 可并行，均调用同一 undo 核心 | 2 个并行 job |
| W6 | T10 文档、真实验证与全量回归 | T8/T9 | 监督会话 + 主机/真实环境 job |

## 任务

### T0 基线与存储安全契约

- 文件：无产品改动；验证记录写入 gofer job/交付报告。
- 动作：记录 HEAD、分支、dirty 状态；核对 `Env`、sessions、store、chat、CLI 当前边界；运行 native/personal/chat/CLI 测试与现有冒烟，确定临时目录、锁和权限测试策略。
- 验证命令：`go test -race ./pkg/agentcore/tools/native/... ./internal/personal/... ./internal/chat/... ./internal/cli/...`；`make smoke-chat`。
- 完成标准：两条命令 exit code 为 0；原始输出可追溯；无 owner 冲突。
- 反向验证方法：不适用产品反向验证；以基线失败不进入实现波次作为 fail-closed 证据。
- 预计改动规模：XS（无文件改动）。
- 依赖/并行：无。

### T1 native 快照接口与 `file_write`/`file_edit` 钩子

- 文件：`pkg/agentcore/tools/native/catalog.go:10-50,61-135`、`file.go`、`edit.go`、native 新测试/接口文件；必要时 `readroots.go` 只增加稳定的 `(root, rel, real_rel)` 适配。
- 动作：在 `Env` 增加导出的 `Snapshotter`/request-result 接口（nil 表示关闭）；在审批通过后、实际写盘前调用 `BeforeCall`。`file_write` 记录原内容或 `kind=absent`；`file_edit` 在 `ApplyEdit` 成功后复用已读原文，不能再次读盘；成功后 `AfterCall` 写 `sha256_after`/status；快照失败按配置阻止写入并把原因返回模型；不改变 os.Root 沙箱和工具 schema。
- 验证命令：`go test -race ./pkg/agentcore/tools/native/... ./pkg/agentcore/...`。
- 完成标准：fake Snapshotter 测试覆盖 before/after 顺序、二进制、不存在、同路径多调用、编辑唯一匹配失败不落错误快照、fail-closed 在写盘前返回；layering test 通过。
- 反向验证方法：临时把 `BeforeCall` 放到写盘后，注入失败测试必须发现目标已经被改动并失败；临时让 file_edit 重新读盘，读取计数测试必须失败。
- 预计改动规模：L（4–7 个 Go 文件、450–750 行含测试）。
- 依赖/并行：T0 后；T2 依赖接口约定，可先并行设计但不得先合并不兼容接口。

### T2 `file_remove` pending/trash/purged 快照钩子

- 文件：`pkg/agentcore/tools/native/remove.go`、`catalog.go`、remove 测试；与 `internal/wiring/capabilities.go:154-162` 的 TrashDir 装配保持兼容。
- 动作：删除前建立 `pending-remove` 元数据；成功后补回实际 trash entry/origin；忽略目录永久删除记 `kind=purged` 与计数；混合目录同时保留 trash/purged 信息；失败写入 `incomplete`；不改 7 天 trash、忽略目录永久删除、根/.git/home/通配符/目录递归守卫。
- 验证命令：`go test -race ./pkg/agentcore/tools/native/...`。
- 完成标准：普通文件、目录、混合删除、忽略目录、删除失败、trash 清理和 origin 校验测试通过；快照状态与工具结果一致。
- 反向验证方法：临时在删除成功后不补 trash entry，删除恢复元数据一致性用例必须失败；临时把 purged 标成可恢复，混合删除“部分恢复”用例必须失败。
- 预计改动规模：M/L（3–5 个 Go 文件、350–650 行含测试）。
- 依赖/并行：T1 的接口约定；可与 T1 实施并行，接口确认后合并。

### T3 `SessionSnapshots` 落盘、元数据、上限与清理

- 文件：`internal/personal/snapshots.go`（新）、测试；`internal/config/paths.go`（若需路径 helper）、`internal/personal/spill.go`（仅复用安全约定，不改变输出）；配置解析接口留给 T7。
- 动作：实现 `<sessions>/<sid>/snapshots/<turn_id>/<NN>-<sha256(call_id)[:16]>/meta.json` 与二进制 `before`；目录 0700、文件 0600、session id 校验、OpenRoot/SecureDir；meta 的 root/rel/real_rel/kind/hash/status/trash/purged/source 等字段；按调用序号保存多条记录；单文件 16 MiB、会话 256 MiB、全局上限接口、keep_turns 20/keep_days 7；写前本会话清理，启动/GC 全局清理；状态 open/undone/partial 与 pre-undo 目录；只清理已完成最旧 checkpoint。
- 验证命令：`go test -race ./internal/personal/...`。
- 完成标准：目录布局、权限、哈希文件名、二进制原样、大小/回合/日期清理、损坏/缺失目录、老 session 无目录、pre-undo 生成均有测试；不建表、不修改 migration。
- 反向验证方法：临时把 call_id 直接拼入路径，恶意 `../`/绝对 call_id 测试必须失败；临时清理 open/incomplete 回合，保留策略测试必须失败；临时把原始 bytes 当文本处理，二进制 round-trip 必须失败。
- 预计改动规模：L（2–5 个 Go 文件、550–850 行含测试）。
- 依赖/并行：T1/T2；不涉及数据库迁移，旧会话兼容必须先锁定。

### T4 checkpoint 选择、会话锁与回滚清单

- 文件：`internal/personal/undo.go`（新）、`runner.go`、`gate.go`、`process/lock.go`、store turns/session 读取及测试。
- 动作：按 DB `started_at,id` 选最近 N 个未 undone 且有快照的回合；shell-only 回合不静默跳过，在清单展示未恢复原因；普通 undo 排除 pre-undo；获取进程内 session gate 后立即获取跨进程会话文件锁；root 只接受 `sessions.cwd` 与持久化 `:rw` 目录等值，不能用当前 cwd/祖先包含关系；生成恢复/删除/冲突/不可恢复、shell 命令与后台 task 提示的清单；已 undone/partial 幂等状态明确。
- 验证命令：`go test -race ./internal/personal/...`。
- 完成标准：最近 N 语义、turn 排序、前缀 session、锁忙、只读会话允许回滚、shell-only 展示、root 可信矩阵和清单统计通过。
- 反向验证方法：临时按当前 CLI cwd 或祖先包含判断 root，篡改 session cwd 测试必须失败；临时跳过 shell-only 回合，清单连续回合语义测试必须失败；临时允许锁等待，锁立即失败测试必须失败。
- 预计改动规模：L（3–6 个 Go 文件、500–800 行含测试）。
- 依赖/并行：T3 后；T5 依赖清单模型。

### T5 冲突检测、pre-undo 与逆序恢复

- 文件：`internal/personal/undo.go`、native 路径/恢复辅助、测试；必要时仅增加可复用的导出 root 适配。
- 动作：按调用逆序恢复；当前内容等于 before 时幂等跳过，等于 after 时可恢复，否则默认 conflict 跳过；`--force` 只覆盖内容冲突，不绕过 root、rel、真实路径、文件类型、symlink/hardlink、protected path、before hash；恢复 absent 删除新文件；trash 恢复核对 origin、原位置存在即冲突；purged/过期 trash 列 unrecoverable；回滚写盘前建立独立 pre-undo 快照；符号链接按链接本身安全重建；逐文件失败保持 partial 并继续安全项。
- 验证命令：`go test -race ./internal/personal/... ./pkg/agentcore/tools/native/...`。
- 完成标准：同路径多写逆序、before/after/第三种内容三态、force、幂等、删除/恢复、symlink/hardlink/special、受保护路径、过期 trash、逐文件失败和 partial 状态全部通过；不会恢复到错误 root。
- 反向验证方法：临时默认 force，人工修改文件后默认 undo 用例必须失败；临时让 force 跳过 root/type 校验，越界/symlink 用例必须失败；临时正序恢复同路径多写，最终内容断言必须失败。
- 预计改动规模：XL（4–7 个 Go 文件、700–1000 行含测试；若接近 1000 行先拆分）。
- 依赖/并行：T4 后。

### T6 回滚通知、`EventUndo` 与 trace attrs

- 文件：`internal/personal/context.go`、`tasks.go`/`steer.go` 可复用的待投递队列、`store/types.go`、entries/events、`observe.go`、`trace.go` 及测试。
- 动作：回滚成功后写入会话待投递事实通知；下一回合创建 `suga.undo` metadata 的 injected entry 并加入 `injectedItemKeys`，按 seq 回放但不自动开回合；中断回合的工具回放完成后再注入；新增 `EventUndo` 及 attrs `turns/restored/deleted/skipped/unrecoverable/files/forced/entry_seq`；快照失败/清理单独事件按设计，不新增逐调用 snapshot event；通知中明确 shell 未涵盖、不要重做、需先重读。
- 验证命令：`go test -race ./internal/personal/...`；必要时 `go test -race ./internal/cli/...`。
- 完成标准：条目不被当作真实 user request、段交接/压缩保留注入属性、下一回合可见且不自动运行、事件 attrs 可由 `suga trace --json` 读取；无数据库迁移。
- 反向验证方法：临时把 undo 条目当真实 user message，recentRequests/injectedItemKeys 测试必须失败；临时自动启动回合，待投递不自动运行测试必须失败；临时不写 EventUndo，trace 断言必须失败。
- 预计改动规模：M/L（5–8 个 Go 文件、450–800 行含测试）。
- 依赖/并行：T5 后；T7 可并行。

### T7 wiring 配置、快照装配与 gc 统计

- 文件：`internal/wiring/capabilities.go`、`runtime.go`、`internal/config/`、`config/config.yaml`、`internal/personal/store/gc.go`、`internal/cli/gc.go`、配置测试。
- 动作：新增 `tools.snapshots.enabled`、`max_file_bytes`、`max_session_bytes`、`max_global_bytes`、`keep_turns`、`keep_days`、`on_error`（deny/continue）与 `chat.undo_confirm` 默认值；读取并校验上限/枚举；将 `SessionSnapshots` 注入 native.Env，run/chat/ACP 共享写工具快照，子 agent 无写工具无快照；启动和 `suga gc` 触发全局清理；gc 报告包含 snapshots 字节；关闭开关不写快照但命令仍存在。
- 验证命令：`go test -race ./internal/wiring/... ./internal/config/... ./internal/personal/... ./internal/cli/... ./pkg/agentcore/tools/native/...`。
- 完成标准：默认值与设计一致、坏配置启动失败、enabled false 旧行为、on_error continue 明确不可撤销、gc 删除 session 时涵盖 snapshots、run/ACP/chat 装配一致；无 migration 文件改动。
- 反向验证方法：临时将默认 on_error 改 continue，快照目录不可写测试必须失败；临时只给 chat 注入快照器，run/ACP 共享装配测试必须失败；临时把 keep_days 延长 trash 保留，删除恢复窗口测试必须失败。
- 预计改动规模：M/L（5–8 个 Go/config 文件、450–750 行含测试）。
- 依赖/并行：T3/T5 后；可与 T6 并行。

### T8 chat `:checkpoints`/`:undo`、确认交互与 ptysmoke

- 文件：`internal/chat/chat.go` 命令分发、`chat_commands.go`/`layout.go` 帮助、chat undo 渲染/确认测试、`scripts/ptysmoke/chat_undo.py`（新）、`scripts/ptysmoke/fakellm.py`（仅必要 edit 模式扩展）、`scripts/ptysmoke/chat_edit.py`、`Makefile`、ptysmoke README。
- 动作：只在提示符处识别 `:checkpoints`、`:undo`、`:undo N`、`force`；回合进行中以 `:` 开头的整行退回输入框并提示等待，绝不 steering；外部 send/hook/task 文本永不当命令；清单展示恢复/删除/冲突/不可恢复/shell/git；确认默认取消，`chat.undo_confirm` 默认开；调用 Runner 核心并打印结果。`chat_undo.py` 用 `PtySession` 驱动真实二进制/fakellm edit：写文件→checkpoints→undo 清单→确认→磁盘恢复→下一回合收到 `suga.undo`→`suga trace --json` 断言 EventUndo/attrs；覆盖新建、编辑、删除、冲突、force、shell 提示、无快照与回合中命令。
- 验证命令：`python scripts/ptysmoke/chat_undo.py`；`make smoke-chat`。
- 完成标准：新场景与 `chat_edit.py`、`chat_approval.py` 全通过；文件内容、命令语义、提示符、trace、通知和返回状态可证伪；Windows 不以 Linux PTY 结果冒充，另记主机验证。
- 反向验证方法：临时关闭/跳过快照 `BeforeCall`，`chat_undo.py` 的恢复断言必须失败；临时让回合中 `:undo` 进入 steering，场景必须发现模型收到该文本；临时把确认默认改为执行，回车取消断言必须失败；新场景正式纳入 Makefile 前先去掉被测逻辑做一次反向运行。
- 预计改动规模：L（5–9 个 Go/Python/Make 文件、550–900 行）。
- 依赖/并行：T4–T7；可与 T9 并行实施，但合入前共享 Runner API。

### T9 `suga undo` CLI

- 文件：`internal/cli/undo.go`（新）、`internal/cli/root.go`、CLI session 解析/`stats.go` 可复用、CLI 测试、必要的 trace/help 文案。
- 动作：注册 `suga undo <session>`，支持 `--list`/`--turn <id>`/`-n`/`--force`/`--yes`/`--json`；session 完整 id/前缀；破坏性操作无 `--yes` 时非终端拒绝；`--force` 必须与 `--yes` 或二次确认绑定；复用 store/Runner undo，不调用模型但写 `suga.undo` 待投递通知；退出码 0/3/4/5 按设计；会话忙立即返回。
- 验证命令：`go test -race ./internal/cli/... ./internal/personal/...`；临时 SUGA_HOME 下运行 `go run ./cmd/suga undo <sid> --list --json` 与 `--yes`。
- 完成标准：list/json、指定回合、N、force/yes、前缀、无快照、会话不存在、忙锁退出码及通知均通过；CLI 与 chat 结果一致。
- 反向验证方法：临时允许无 `--yes` 的破坏性调用，安全测试必须失败；临时让 CLI 绕过 session lock，忙锁测试必须失败；临时不写通知，CLI 后续回合注入测试必须失败。
- 预计改动规模：M（3–5 个 Go 文件、300–550 行含测试）。
- 依赖/并行：T4–T7；可与 T8 并行。

### T10 文档、真实验证与全量回归

- 文件：`README.md`、`README.zh-CN.md`、`config/config.yaml`（若 T7 未完成注释）、`CHANGELOG.md`、相关 usage/skills 参考；不修改设计正文。
- 动作：说明快照目录/敏感性、支持的写工具、上限/保留、冲突/force、删除/trash、shell 边界/git 提示、chat 命令与确认、CLI 参数/退出码、只读会话、ACP/run 共享快照、关闭开关和无迁移兼容；以真实 provider 跑一轮带 file_write/file_edit/file_remove 的对话并检查 trace/磁盘；执行容器、主机和 pty 全量验证，汇总未执行环境。
- 验证命令：容器 `gofmt -l $(git ls-files "*.go")`、`go vet ./...`、`go test -race ./...`、`make smoke-chat`；主机 `go test -count=1 ./...`、`staticcheck ./...`；真实 provider 带工具调用；`suga trace <sid> --json` 检查 undo；Windows 通过 gofer exec job。
- 完成标准：所有命令 exit code 与原始输出为成功；文档不承诺 shell 自动回滚或延长 trash；真实验证有会话/trace 证据，无法执行则明确 `UNVERIFIED_REAL_ENV`。
- 反向验证方法：不新增业务逻辑；以 T8 的快照关闭反向运行、T9 的安全参数反向运行和全量兼容测试作为 Gate，任何反向检查不失败都阻止完成。
- 预计改动规模：M（5–8 个文档/配置文件；无新业务代码）。
- 依赖/并行：T8、T9、T7 全部完成。

## 回滚与恢复

- 每个任务在独立 worktree/分支完成并按功能点单独提交；未通过验收不合入，禁止 `git reset`/`stash` 覆盖他人改动。
- W1/W2 的 native hook 和存储提交可独立 revert；W3/W4 的 undo/通知/配置提交按逆序 revert；W5 chat/CLI 与 W6 文档最后处理。
- 运行时回退：`tools.snapshots.enabled: false` 停止新快照，`:undo`/`suga undo` 仍存在但报告无可回滚快照；不删除旧会话、不改变旧写工具行为。
- 存储回滚：本期没有数据库迁移；删除新版本后，旧二进制忽略 `sessions/*/snapshots`，`outputs`、transcript、store 仍可用。恢复前保留快照目录，不能用清理命令代替版本回滚。
- 写回/恢复失败：每个目标单独记录，安全目标继续恢复并将 checkpoint 标为 partial；绝不为了“全绿”强制覆盖冲突或跳过 root/type/hash 校验。
- 恢复点：每波记录合入后的 HEAD、测试/冒烟原始输出、快照目录兼容结论及 `UNVERIFIED_REAL_ENV`；语义变化返回设计 Gate。

## 人工 Gate

1. 用户审阅并批准本 Draft 0.1；批准计划不授权扩展到 shell 自动回滚、ACP 内 undo 或备份恢复。
2. T3–T5 完成后进行安全存储 Gate：核对权限、root 可信来源、锁顺序、冲突默认跳过、force 不越界、trash 7 天不变。
3. T8 完成后审阅 ptysmoke 场景和反向验证；交互行为不留给人工 chat 验证。
4. T10 的真实 provider、Windows 主机 job 是外部环境 Gate；无法执行时交付说明必须列出 `UNVERIFIED_REAL_ENV`。
5. push、发版、Gofer 配置变更不在本计划内，另行授权。

## 可追溯性

| 设计条目 / 定稿选项 | 任务 | 验证 |
|---|---|---|
| D1 会话目录存储、Q1 `:undo`/`:checkpoints`/开关 | T3、T7、T8、T9 | 布局/权限测试；chat/CLI；旧 session 兼容 |
| D2 每次写调用前快照、同路径逆序、file_edit 复用原文 | T1、T2、T3、T5 | fake hook、重复写/逆序/读取计数测试 |
| D3 shell 改动只提示、Q4 G1 | T4、T8、T10 | 清单/结果行 shell 与 trace/git 提示；真实写命令回归不误报 |
| D4 file_remove 复用 trash、Q5 部分恢复/固定 7 天 | T2、T5、T7、T10 | trash/origin、purged/混合/过期测试；文档 |
| D5 冲突默认跳过、`--force` 仅内容冲突 | T5、T8、T9 | before/after/第三内容矩阵；force 不越界反向 |
| D6 chat `:` 与 CLI，ACP 不做，Q6 CLI 完整参数/退出码 | T8、T9 | ptysmoke、CLI 表驱动与退出码 |
| D7 快照失败默认 deny、`on_error: continue` 例外 | T1、T2、T7 | 不可写/清理/锁失败在 I/O 前拒绝；continue 明示不可撤销 |
| D8 `suga.undo` 通知 + EventUndo、不自动开回合 | T6、T8、T9 | context 回放/injectedItemKeys、trace JSON、下一回合模型消息 |
| D9 20 回合/7 天/会话与全局上限，Q2 16/256 MiB | T3、T7 | 清理/上限/GC 统计测试；文档 |
| D10 pre-undo、不做 redo | T3、T5、T8 | pre-undo 目录与状态测试；无 redo 命令断言 |
| root/安全校验、Q8 可信 cwd 与 `:rw` 等值 | T4、T5、T7 | 篡改 cwd、symlink/hardlink、protected path、附加目录矩阵 |
| Q3 只读会话允许 undo | T4、T7、T8 | 只读运行仍可恢复自身快照，非自身路径拒绝 |
| 存储/兼容：无 DB 迁移、不进上下文/export/transcript | T3、T6、T7、T10 | migration 未改；导出/trace/transcript 检查；老 session 测试 |
| 运行时范围：run/ACP 写照常快照，ACP 无内置 undo，子 agent 只读 | T7、T9、T10 | wiring 模式矩阵；文档；CLI 回滚会话 |
| 设计 non_goals 与 suga-zy9f 边界 | 全部任务、T10 | 范围审阅；无 shell git 回滚/backup/spill 压缩改动 |

## 待确认/风险（设计已批准的选项，不在实施中重新改语义）

- 当前 native 路径解析 helper 未提供完整稳定的 `(root, rel, real_rel)` 外部契约；T1/T5 必须先建立最小导出适配。若会导致改变沙箱语义，停止返回设计 Gate。
- `file_remove` 的 trash entry 只有成功后才知道，且忽略目录可能永久删除；T2/T5 必须保持 pending/incomplete 与混合部分恢复的一致性，不能用“复制第二份快照”绕开设计。
- 设计要求 Runner gate→跨进程锁，但当前实现/锁文件细节若与此不一致，先核对 `runner.go`/`process/lock.go`，不自行改成等待或反序。
- `suga gc` 当前删除整个 sessions 目录但不统计 snapshots 字节；T7 只增加统计，不能顺手引入 suga-zy9f 的压缩、backup/restore 或新的 GC 参数。
- 后台 task 可能在 undo 后继续改文件；清单/结果必须列 task id 或风险，不能声称 undo 覆盖 shell/background 改动。
- Windows 的文件锁、权限、大小写和 symlink 能力可能与 Linux 不同；主机 job 失败需记录具体输出并标 `UNVERIFIED_REAL_ENV` 或返回修复 Gate，不删掉安全校验。

## 完成 Gate

- T0–T10 的依赖任务全部完成并按功能点提交；最终 `git status --short` 无夹带，`git diff --check` 无输出。
- 三个写工具、SessionSnapshots、Runner undo、通知/trace、chat/CLI、配置/gc 均有直接测试；每个反向验证都确认移除被测逻辑后会失败。
- 容器 `gofmt -l`、`go vet`、`go test -race ./...`、`make smoke-chat`；主机 `go test -count=1 ./...`、`staticcheck ./...`；真实 provider 带工具调用或明确 `UNVERIFIED_REAL_ENV`，所有原始输出可追溯。
- 设计每个 D/Q、兼容规则、存储边界、非目标和安全不变量均能定位到任务与验证；不存在数据库迁移或未批准的协议/产品语义。
- 完成后汇报每个波次、任务清单、验证原始输出、风险与未验证环境；push/发版另行请求。
