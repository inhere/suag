<!-- template_id: design; template_version: 1.1.1 -->
# 写操作快照与 `:undo`（回滚上一回合的文件改动）设计

> 状态：Draft 0.1（待用户审阅）

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-16 | suga（gofer job） | 初稿：写工具调用前快照（存放、粒度、上限、保留）、`:undo` / `:undo N` / `:checkpoints`、冲突检测、回滚后告知模型、`suga undo` CLI、shell_exec 边界与 git 备选比较、与 gc/suga-zy9f 的关系 |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

关联：issue suga-wl0l（本设计来源）、suga-zy9f（数据治理：spill 配额与压缩、backup/restore，与本设计的保留/清理边界见 3.9）、suga-oyby（`:add-dir`，快照需要覆盖附加目录）。调研依据：`docs/research/2026-10-16-feature-strengthening-analysis.md` §3 #11（写文件与 `shell_exec` 的改动不可撤销，只有 `file_remove` 有 7 天 trash；全仓无 undo/checkpoint）。相关设计：`docs/design/2026-10-13-background-tasks-design.md`（会话目录 `outputs/` 的同族约定）、`docs/design/2026-10-15-interrupted-turn-replay-design.md`（中断回合的条目回放，本设计不改变它）。

### 思考模式与范围冻结

- thinking_mode：RIGOROUS。
- core_objective：模型对工作区的文件改动**可以在回合结束后被撤销**——看得到“将要恢复哪些文件”、知道哪些恢复不了（冲突、被永久删除、`shell_exec` 的改动），一次 `:undo` 回到上一回合开始时的文件状态。
- allowed_scope：`pkg/agentcore/tools/native`（写工具的快照钩子与 `Env` 字段）、`internal/personal`（快照器、回滚、`undo` 条目与事件、保留清理）、`internal/wiring`（装配与配置键）、`internal/chat`（`:undo`/`:undo N`/`:checkpoints` 与确认交互）、`internal/cli`（`suga undo` 子命令）、`docs/`、`config/config.yaml`、`README.md`/`README.zh-CN.md`。
- non_goals：`shell_exec` 造成改动的完整回滚（只做提示与 git 备选比较）；`:redo`；文件级 diff 预览（调研 §3 #12，另一个 issue）；ACP 侧的 undo 交互；整库备份/恢复与 spill 的全局配额/压缩（suga-zy9f）；`file_remove` 的 trash 保留期与忽略目录永久删除语义；Gofer 侧改动。
- expansion_policy：DEFER_OR_REQUEST。
- review budget：低暴露度（个人工具、未投产；快照可整体关掉、`:undo` 只写回快照记录的路径）；一次实现 + 单测 + 伪终端冒烟 + 一次反向验证。
- 停止条件：确认无 CORE_BLOCKING 后请用户批准本设计（Q1–Q6 有答复），再起草实施计划；批准设计不授权实施。

## 背景与目标

现状（2026-10-16 核对，基线 `626747f`）：

- 写工具直接覆盖文件：`file_write` 用 `O_WRONLY|O_CREATE|O_TRUNC` 写目标（`pkg/agentcore/tools/native/file.go:152-170`），返回值只有字节数（`:81`）；`file_edit` 读全文、内存替换后**整文件重写**（`native/edit.go:63-90`）；`shell_exec` 在工作区里跑任意命令，返回输出与退出码（`native/code_exec.go:252,279-304`）。
- 唯一的可恢复路径是 `file_remove`：移到 `$SUGA_HOME/trash/<UTC 时间戳>-<6 hex>/`，保留 7 天，`origin.json` 记录来源（`native/remove.go:21-32,250-254,278-305`，装配 `internal/wiring/capabilities.go:154-162`）。被 `.gitignore` 命中的目录是**永久删除**（`remove.go:227-240`）。
- 全仓没有任何 undo/checkpoint/snapshot/revert 代码或遗留字段（`migrations/*.sql` 里也没有相关列）。
- 会话目录只有 `outputs/`：`<SUGA_HOME>/sessions/<sid>/outputs/<16hex>.txt`（`internal/personal/spill.go:23-96`、`internal/config/paths.go:29-37`），无配额、无压缩，只有 `suga gc` 删整个会话时一起删（`internal/cli/gc.go:99-104`）。

用户可见目标：

1. 模型的一次写操作可被撤销：`:undo` 回滚**上一回合**的全部文件改动（新建的文件被删掉、改过的文件恢复原内容、删除的文件从回收区回来）。
2. 回滚之前先看到清单：恢复几个文件、删除几个新建文件、哪些跳过（冲突 / 无法恢复），并且要确认才执行。
3. `:undo N` 回滚最近 N 个有改动的回合；`:checkpoints` 列出本会话的快照点。
4. 回滚后模型知道：下一回合它看到“用户回滚了这些改动”，不会把已撤销的东西再写回去。
5. 覆盖范围与代价写清楚：`shell_exec` 的改动不在快照内（命令可以改任何地方）；工作区是 git 仓库时给出可用的 git 备选与命令提示。

## 名词

- **快照（snapshot）**：一次写工具调用**执行前**记录的目标状态（原文件字节、或“原本不存在”、或“已在回收区”）。
- **快照点（checkpoint）**：一个回合的全部快照，落在 `<sessions>/<sid>/snapshots/<turn_id>/`。
- **回滚（undo）**：按快照把目标恢复到该回合开始时的状态；同一路径被写多次时按调用**逆序**恢复，结果等于回合开始时的内容。
- **冲突（conflict）**：某个目标在快照之后又被改过（当前内容既不是快照前的、也不是这次写入后的），此时默认跳过而不是覆盖。
- **不可恢复（unrecoverable）**：快照本身拿不到原内容的情况——`file_remove` 直接永久删除的被忽略目录、超出单文件上限而未快照的大文件、快照目录已被清理。
- **`pre-undo` 快照**：回滚动作自身写入前记下的“回滚前状态”，让误回滚有据可查（本期不提供 `:redo`，但数据留着）。

## 范围与非目标

范围：

- 写工具（`file_write`、`file_edit`、`file_remove`）执行前的快照钩子，覆盖工作区与会话的 `:rw` 附加目录。
- 快照的存储布局、单文件/会话上限、保留期限与清理。
- chat：`:undo`、`:undo N`、`:checkpoints`，含清单渲染、确认、结果行。
- 回滚后的会话记录：`suga.undo` 元数据的 user 条目 + trace 事件。
- `suga undo <session>` CLI（`--list`/`--turn`/`--force`/`--json`）。
- 文档：`config/config.yaml`、README 中英、CHANGELOG。

非目标：

- `shell_exec` 的改动（含它启动的子进程、后台任务）不保证可回滚；只提示并给出 git 备选（D3）。
- 不改 `file_remove` 的 trash 语义（7 天、忽略目录永久删除）；本设计对删除的恢复以回收区为源（D4）。
- 不提供 `:redo`、不提供按文件/按路径选择性回滚、不提供回滚到任意历史版本（目录级时间旅行）。
- 不改 ACP 的交互面（ACP 客户端有自己的撤销能力）；ACP 会话的写操作**照常快照**，只是 U2 不提供 ACP 内的 `undo` 命令。
- 不把快照导出到 `suga export`（保持 export 只导条目与事件）。
- 不做备份/恢复、WAL checkpoint、spill 压缩与全局配额（suga-zy9f）。

## 已确认事实与规范

以下在 `626747f` 上核对，行号可能有偏移；引用时以符号名为准。

| 事实 | 位置 |
|---|---|
| `file_write`：路径自 `writeTargetPath`，解析先词法、后 `os.Root`；写是 `O_WRONLY\|O_CREATE\|O_TRUNC` 直接覆盖（非原子、无临时文件、无 fsync），返回 “wrote N bytes to path” | `pkg/agentcore/tools/native/file.go:68-83,92-125,130-173,175-181` |
| `file_edit`：先 `readFileInRoot` 读全文，内存替换后整文件重写；匹配为精确字符串（`replace_all` 或唯一匹配）；`ApplyEdit` 已导出供 ACP 复用 | `pkg/agentcore/tools/native/edit.go:63-90,98-101,204-236` |
| `file_remove`：`PlanRemoveWithDirs` 守卫（拒工作区根/附加目录根/`.git`/home/通配符，目录须 `recursive`，>1000 文件拒绝）；其余移入 `<SUGA_HOME>/trash/<stamp>-<6hex>/<basename>` + `origin.json`，保留 7 天；被忽略目录**永久删除**；`purgeTrash` 在每次 remove 前清理过期条目 | `pkg/agentcore/tools/native/remove.go:21-32,119-220,227-240,250-254,278-305` |
| trash 根由 wiring 注入；`TrashDir == ""` 时 `file_remove` 不注册 | `internal/wiring/capabilities.go:154-162`、`pkg/agentcore/tools/native/catalog.go:92-99` |
| `shell_exec`：`cmd.Dir = workspace`，超时默认 120s/上限 600s，超时杀整个进程树；返回输出与退出码，**不报告改动了什么** | `pkg/agentcore/tools/native/code_exec.go:174-176,216-226,252,258-267,279-304` |
| 会话目录根：`$SUGA_HOME/sessions`，唯一子目录是 `<sid>/outputs/`（0700/0600、`os.OpenRoot`、hex 文件名、`validateSessionID`） | `internal/config/paths.go:29-37`、`internal/personal/spill.go:23-96` |
| 日志在 `$SUGA_HOME/logs/suga.log`，transcript 在 `$SUGA_HOME/transcripts/<sid>.jsonl`（都不在 sessions 下） | `internal/wiring/logging.go:65-84`、`internal/chat/chat.go:195` |
| `suga gc`：删 `updated_at < cutoff` 的会话（含子 agent 会话）、其 events、`transcripts/<id>.jsonl` 与 `sessions/<id>/` 整目录；参数只有 `--older-than` 与 `--apply`，没有保留期配置键 | `internal/personal/store/gc.go:26-95`、`internal/cli/gc.go:21-38,96-104` |
| 表：sessions / turns / entries / events（+FTS、memory_meta）；entry 类型与 event kind 都是 TEXT，加新 kind 无需迁移 | `internal/personal/store/migrations/20261008-230000-create-core-store.sql:4-55`、`internal/personal/store/types.go:24-43,45-100` |
| turns 无序号列，顺序按 `started_at, id`；会话指向最近回合用 `sessions.last_turn_id` | `internal/personal/store/turns.go:61-70`、`internal/personal/store/types.go:104-113`、`internal/personal/runner.go:669` |
| 回合内/回合后往会话追加 user 消息的现成做法：`EntryUser` + metadata 标记（任务通知、插话），随后按 seq 回放 | `internal/personal/tasks.go:59-60,111-140`、`internal/personal/steer.go:47-56,323-356` |
| 注入条目用 `injectedItemKeys` 与真实用户消息区分（段交接的 recentRequests、压缩的注入条目处理都靠它） | `internal/personal/context.go:30-49` |
| `context_update` 条目在段内被采纳并作为 user 消息回放；失败/取消回合的更新不采纳（下一回合重算） | `internal/personal/context.go:150-195` |
| 工具调用能看到 ctx 里的会话与回合：`observeScope` 经 ctx 传递，导出的 `personal.CurrentTurn(ctx)` 可取 | `internal/personal/observe.go:37-48`、`internal/personal/subagent.go:171-176` |
| 工具能力层能拿到的东西集中在 `native.Env`（Workspace/AddDirs/TrashDir/ReadRoots/…），由 wiring 装配 | `pkg/agentcore/tools/native/catalog.go:15-50`、`internal/wiring/capabilities.go:167-190` |
| 附加目录集合是会话级、可运行时变更（`--add-dir`、`SUGA_ADD_DIRS`、`:add-dir`、越界授权）；写工具只认 `:rw` 的 | `pkg/agentcore/tools/native/adddirs.go:70-90`、`internal/wiring/sessionadddirs.go:24-120`、`internal/chat/chat_dirs.go:16-57` |
| chat 的命令分发是一个 `switch`（`:quit`/`:session`/`:compact`/目录命令…），`runner.Compact(ctx, sid)` 是命令调用 Runner 的现成例子 | `internal/chat/chat.go:390-430`（`:418`） |
| chat 的帮助是常量文案，没有命令表与 `:` 补全 | `internal/chat/layout.go:90-94` |
| CLI 用 `cflag/capp`，子命令集中注册；`openCoreStore()` 是现成的 store 句柄 | `internal/cli/root.go:48-61`、`internal/cli/stats.go:34` |
| ACP 已有“从参数算出 diff”的实现（`toolDiff`：`file_write` 用 content、`file_edit` 复用 `native.ApplyEdit`，上限 `maxDiffBytes`，只覆盖 cwd 内路径） | `internal/acpserver/policy.go:75-77,269-333` |
| issue suga-zy9f：spill 无配额与压缩、gc 无 events TTL/WAL/memory 覆盖、要做 backup/restore 与 gzip/LRU 配额 | `.gofer/tracker` 中 suga-zy9f（`suga gc`/spill 的现状与本节表一致） |

## 总体方案

主线一句话：**写工具在动手之前把“目标原来的样子”交给一个快照器，快照按“会话 → 回合 → 调用”落盘；`:undo` 就是对某个回合的快照做一次逆序恢复，先列清单、检测冲突、要确认。**

三条边界从一开始就定死，避免被误解为“万能的撤销”：

1. 只覆盖 `file_write` / `file_edit` / `file_remove` 三个工具的目标路径；`shell_exec` 的改动不记录（D3）。
2. 快照是**本机数据**，不进模型上下文、不进 `suga export`、不跨机器；`suga gc` 删会话时一起删。
3. 恢复只写快照记录过的 `(根, 相对路径)`，`:undo` 不接受路径参数；恢复写入仍走写工具的同一套 `os.Root` 沙箱与路径解析。

## 架构

```text
  模型 file_write/file_edit/file_remove
        │
        ▼  （native 工具内部，审批通过之后、I/O 之前）
  native.Env.Snapshots.BeforeCall(ctx, SnapshotRequest{Tool, Root, Rel, Kind})
        │                                  ▲ ctx 里的会话/回合来自 personal.CurrentTurn(ctx)
        ▼
  personal.SessionSnapshots  ──> <SUGA_HOME>/sessions/<sid>/snapshots/<turn_id>/<NN>-<hex>/
                                     meta.json      before（原文件字节，kind=file）
  写工具完成 ──> AfterCall(ctx, sha256_after)  记进 meta.json（冲突检测用）

  chat 提示符：:checkpoints → runner.Checkpoints(ctx, sid)（扫目录）
              :undo [N]    → runner.Undo(ctx, sid, N, force)
                               ├── 列清单（恢复 / 删除 / 跳过冲突 / 无法恢复）→ 确认
                               ├── 逆序恢复（同一沙箱；写前再记 pre-undo 快照）
                               ├── 追加 EntryUser{suga.undo} + trace 事件
                               └── 打印结果行（含 shell_exec 提示与 git 提示）
  CLI：suga undo <session> [--list] [--turn <id>] [--force] [--json]
```

组件划分（分层：`native` 只认得 `Env` 里的一个接口，不知道会话与存储）：

- `pkg/agentcore/tools/native`：`Env` 增加 `Snapshots Snapshotter` 字段（可为 nil）；写工具在 I/O 前调用 `BeforeCall`、成功后调用 `AfterCall`。工具不判断是否启用、不决定存哪。
- `internal/personal/snapshots.go`（新）：`SessionSnapshots`——与 `SessionSpiller` 同族的目录写入器（`os.OpenRoot(sessionsDir)`、`process.SecureDir`、0700/0600、hex 名、`validateSessionID`），负责落盘、读取某个回合、保留清理。
- `internal/personal/undo.go`（新）：`Undo` / `Checkpoints` / 冲突检测 / 逆序恢复 / 写 `suga.undo` 条目与事件；对外的两个方法挂在 `*Runner` 上（与 `Compact` 同风格）。
- `internal/wiring`：把 `personal.SessionSnapshots` 装进 `native.Env`（在 `nativeEnv` 里），并读 `tools.snapshots.*`。
- `internal/chat`：`:undo` / `:checkpoints` 的解析（`chat.go` 的 switch 或仿 `chat_dirs.go` 的判定函数 + 执行函数）、清单与确认渲染。
- `internal/cli/undo.go`（新）：`suga undo` 子命令，注册进 `root.go:48-61`。

## 关键设计

### 3.1 快照的存储布局与内容

```text
<SUGA_HOME>/sessions/<session_id>/
  outputs/                                  # 现状：外置工具输出
  snapshots/
    <turn_id>/
      001-9f3ac21b/
        meta.json
        before                              # 原文件字节（kind=file 时存在）
      002-4d0e77aa/
        meta.json
```

- `<NN>`：该回合内的调用序号（3 位，按 seq 顺序，1 起）。
- `<hex>`：`sha256(call_id)` 前 16 hex——模型给的 `call_id` 可以含 `:`、`/`、`..` 或绝对路径，所以绝不进路径（与 spill 同一理由，`internal/personal/spill.go:37-44`）。
- 权限：`snapshots/` 与子目录 0700、文件 0600，沿用 `process.SecureDir`（`spill.go:58-78`）。
- `meta.json`：

```json
{
  "session_id": "sess_2610161...",
  "turn_id": "turn_1itu4g0p9l50021",
  "call_id": "call_2",
  "seq": 1,
  "tool": "file_edit",
  "root": "D:/work/proj",          // 工作区或某个 :rw 附加目录的绝对路径
  "rel": "src/a.go",               // 该根下的相对路径（slash 分隔）
  "kind": "file",                  // file | absent | trash | purged
  "size": 2143,                    // 原文件字节数
  "sha256_before": "…",            // 调用前内容（kind=file）
  "sha256_after": "…",             // 本次写入后内容（AfterCall 填；kind=absent 时为空）
  "mode": "0644",
  "file": "before",                // kind=file 时的内容文件名
  "trash_dir": "",                 // kind=trash：回收区条目目录
  "created_at": "2026-10-16T14:32:11Z",
  "source": "call"                 // call | pre-undo
}
```

设计取舍：

- **不建数据库表**：目录本身就是索引（哪个回合有快照 = 哪个目录存在），`meta.json` 自描述。好处是零迁移、`suga gc` 天然覆盖、用户可以直接看和删。代价是不能用 SQL 查询——`:checkpoints` 只扫一个会话的目录，量级很小。
- **`sha256_before` / `sha256_after` 都要**：前者用于“已经恢复过（幂等）”的判断，后者用于冲突检测（见 3.4）。
- **二进制按字节存**：不读成文本、不做换行转换、不脱敏（回滚需要原文；spill 是给模型看的，快照不是）。

### 3.2 何时快照、快照什么

| 工具 | 快照动作 | 额外 I/O |
|---|---|---|
| `file_write` | 读目标现有内容 → `before` + `sha256_before`；目标不存在 → `kind=absent` | 一次读盘（原来没有） |
| `file_edit` | 复用工具**已经读入内存的原文**（`native/edit.go:69`）→ `before`；文件不存在时工具本来就报错 | 无（只写快照文件） |
| `file_remove` | 不复制内容：记录 `kind=trash` 与回收区条目目录（`origin.json` 的父目录） | 无（回收区已有副本） |
| 被忽略目录的永久删除 | `kind=purged`（只记路径与计数，不可恢复） | 无 |

时机：**审批通过之后、I/O 之前**（在工具内部），所以“没有快照就不写”可以作为一个硬保证（3.3）。回合内同一路径写多次会有多条快照——这是正确的语义（逆序恢复得到回合开始状态，见 3.4）。

### 3.3 上限、失败处理与保留

新增配置（`tools.snapshots.*`）：

| 键 | 默认 | 含义 |
|---|---|---|
| `tools.snapshots.enabled` | `true` | 关掉后写工具完全不快照（`:undo`/`:checkpoints` 报告“本会话没有快照”） |
| `tools.snapshots.max_file_bytes` | `16777216`（16 MiB） | 单个文件快照上限；超过则不快照，meta 记 `kind=oversize`（`:undo` 列为“无法恢复”） |
| `tools.snapshots.max_session_bytes` | `268435456`（256 MiB） | 本会话快照总量上限；超过时按**最旧回合**先清理 |
| `tools.snapshots.keep_turns` | `20` | 保留最近多少个有快照的回合 |
| `tools.snapshots.keep_days` | `7` | 与 trash 一致的保留期；超期回合整体删除 |
| `tools.snapshots.on_error` | `deny` | 快照写失败时的行为：`deny` = 拒绝执行这次写入（fail-closed，默认）、`continue` = 照旧写但标记未快照 |
| `chat.undo_confirm` | `true` | `:undo` 执行前是否要确认（与 `chat.markdown` 同一种开关取值，`internal/config/chat.go:61-69`）；`false` 时直接回滚（仍打印清单与结果行） |

- 清理时机：每次新开一个写调用前（与 `purgeTrash` 的时机相同，`remove.go:288-305`），按 `keep_turns`/`keep_days`/`max_session_bytes` 从最旧回合开始删；被删的回合在 trace 里留一条 `snapshot` 事件（attrs `pruned`、回合、字节）。
- 失败消息要具体：`快照失败，已拒绝写入 <path>：<原因>；如需在快照失败时仍然写入，设置 tools.snapshots.on_error: continue`。
- 只读模式、写工具不存在时不会有快照，无需特判。

### 3.4 回滚语义与冲突检测

`Undo(sessionID, turns, force)` 的算法：

1. 取最近 `N`（默认 1）个**有快照目录**的回合，按时间从新到旧。
2. 每个回合内按调用序号**逆序**处理（`003` → `002` → `001`）。
3. 对每条快照算当前状态（同一个 `(root, rel)` 上）：

| 当前状态 | 判定 | 默认动作 |
|---|---|---|
| `sha256(当前) == sha256_after` | 快照后没人动过 | 恢复 |
| `sha256(当前) == sha256_before`（或 `kind=absent` 且目标不存在） | 已经是回合开始的样子 | 跳过（幂等，说明“已是恢复后的状态”） |
| 其它（内容不同、类型变了、目标不存在但应有、应有但不存在） | **冲突** | 跳过并列出；`--force`/`undo force` 才覆盖 |
| `kind=trash` 且回收区条目不存在 | 回收区已过期（7 天）或已被清理 | 列为“无法恢复” |
| `kind=purged` / `kind=oversize` | 从未保留内容 | 列为“无法恢复” |

4. 恢复动作：`kind=file` → 把 `before` 写回 `(root, rel)`（必要时 `MkdirAll` 父目录，权限尽量按 `mode`，Windows 忽略）；`kind=absent` → 删除该文件（目录为空时保留空目录，不递归删父目录）；`kind=trash` → 从回收区条目复制回原位置。
5. **回滚自身也留痕**：每次真正要覆盖/删除目标之前，先按 3.2 的格式记一份 `source: pre-undo` 快照（同一目录、序号前缀 `u`）。本期不提供 `:redo`，但数据在，误回滚有据可查（D10）。
6. 恢复写入用写工具的同一套沙箱：**不绕过 `os.Root`**；`:rw` 附加目录用会话当前的目录集合，若解析失败（目录已被 `:rm-dir` 移除、盘符不存在）→ 冲突并可跳过。
7. 结果统计返回给调用方（恢复数、删除数、跳过数、不可恢复数、涉及路径清单），用于渲染与 `--json`。

### 3.5 chat 的命令与文案

`:checkpoints`：

```text
快照点（最近的回合在前；快照在写操作执行前记录）：
  turn_1itu4g0p9l50021  10-16 14:32  3 次写调用 / 5 个文件 / 12.4 KB  [已回滚 2 个文件]
  turn_1ittz8x2k100077  10-16 14:05  1 次写调用 / 1 个文件 / 1.2 KB
合计 6 个文件 / 13.6 KB；保留最近 20 个回合或 7 天（tools.snapshots）
```

`:undo`：

```text
将回滚上一回合 turn_1itu4g0p9l50021 的文件改动（3 次写调用）：
  恢复    src/a.go          2.1 KB
  删除    docs/new.md       （该文件由本回合新建）
  跳过    tmp/x.log         冲突：快照之后被改动过
  无法恢复  dist/app.bin     超过单文件快照上限（16 MiB）
执行？[y] 回滚 [N] 取消：
```

结果行：

```text
已回滚 2 个文件、删除 1 个新建文件；跳过 1 个冲突、1 个无法恢复。
本回合还运行了 2 条 shell 命令，它们的改动不在快照范围内：suga trace <session> --turn turn_1itu4g0p9l50021
工作区是 git 仓库：可用 git -C <workspace> status --short / git diff 核对其余改动。
```

规则：

- 只在**提示符处**可用；回合进行中拒绝（`回合进行中，等它结束再 :undo`）；外部投递的文本（`suga send`、hook 回复、任务通知）永不当作命令（现有语义，`internal/chat/chat.go:399-403`）。
- `:undo` 不接受路径参数；`:undo N` 的 `N` 是“最近 N 个有改动的回合”（`1..keep_turns`），不是条目序号；`:undo force` 与 `:undo N force` 表示覆盖冲突。
- 没有快照时：`没有可回滚的快照（本会话没有写工具改动，或快照已按 tools.snapshots 清理）。`
- 确认默认是“取消”（回车取消），与审批询问的默认一致。
- 帮助文案（`internal/chat/layout.go:90-94`）加上 `:undo / :checkpoints`。

### 3.6 回滚后告知模型

回滚不是用户的“新请求”，而是一条**事实注入**：写一条 `EntryUser` + metadata `suga.undo`（做法与任务通知、插话的注入条目一致：`tasks.go:111-140`、`steer.go:323-356`），并把 key 加进 `injectedItemKeys`（`context.go:30-49`），这样：

- 段交接的 recentRequests 与压缩的注入处理都不会把它当成用户请求；
- 下一回合模型能看到它（按 seq 回放）；
- **不自动开回合**（与 steering 退回输入行同一原则：用户没发话就不动），chat 只打印结果行。

文案：

```text
[用户回滚了上一回合的文件改动（这不是新请求）：已恢复 src/a.go，已删除 docs/new.md（本回合新建）；
跳过 tmp/x.log（快照之后被改动过）。不要再重做这些改动，除非用户明确要求；需要时先重新读取文件确认当前内容。]
```

同时写一条 trace 事件（`EventUndo`，新 kind，`store/types.go:45-100` 加常量，无需迁移）：`name=undo`，attrs `turns`、`restored`、`deleted`、`skipped`、`unrecoverable`、`files`（截断列表）、`forced`、`entry_seq`。

### 3.7 `suga undo`（CLI）

```text
suga undo <session>                # 同 :undo，带确认（非终端输入时拒绝，需 --yes）
suga undo <session> --list         # 同 :checkpoints（只读）
suga undo <session> --turn <id>    # 指定回合（默认最近一个有快照的）
suga undo <session> -n 2 --yes     # 最近 2 个回合，不等确认
suga undo <session> --force --yes  # 覆盖冲突
suga undo <session> --list --json  # 供脚本
```

- `<session>` 接受完整 id 或前缀（与 `suga trace`/`suga sessions` 的既有习惯一致）。
- 退出码：`0` 成功（或无剩余改动）、`3` 没有快照 / 全部冲突 / 全部无法恢复、`4` 会话不存在（对齐现有会话命令的约定）。
- 不调用模型：`--yes` 时不写 `suga.undo` 条目？**写**——不写条目，用户在 chat 里 `--resume` 时会看到模型不知道文件被回滚了。写条目需要 store 句柄（`openCoreStore()`，`internal/cli/stats.go:34`）。
- run 与 ACP：写操作照常快照（共享运行时），`suga undo <session>` 对它们产生的会话同样可用；ACP 内部不提供 undo 交互（U2）。

### 3.8 `shell_exec` 的边界与 git 备选（比较）

事实：`shell_exec` 在工作区里跑任意命令（`cmd.Dir = t.workspace`，`native/code_exec.go:252`），可写工作区、附加目录、也能写工作区之外（用户自己配的 `!cmd` 与模型的 `shell_exec` 都如此），工具返回输出与退出码，**不报告改了哪些文件**。所以“用快照覆盖 shell 的改动”在原理上做不到。

三种备选：

| 方案 | 能覆盖 | 代价与风险 |
|---|---|---|
| **G1 只提示（本设计采用）** | 文件工具的改动 | `shell_exec` 造成的改动要靠用户自己（`:undo` 结果行给 `suga trace --turn` 与 git 命令提示） |
| **G2 回合前后用 git 取基线**：回合开始 `git stash create`（生成 commit 对象、**不改工作区**）或在干净树上记 HEAD，回合结束 `git diff` 得到本回合改过的文件；undo 用 `git checkout -- <files>` / 应用反向 patch | 文件工具 + `shell_exec` + 其它进程删改的文件 | 要求工作区是 git 仓库且有 `git`；用户的未提交改动会与“本回合改动”混在一起，误回滚会把用户自己的改动一键回退；非 git 仓库、`.gitignore` 里的文件（如 `tmp/`）、仓库外路径（`--add-dir` 到别的目录）都覆盖不到；一个仓库多个会话并发时基线会互相污染 |
| **G3 回合前后快照整个工作区** | 一切 | 大仓库不可行（时间与磁盘）；忽略目录、依赖目录会拖垮；不做 |

本设计取 **G1**：安全、可解释、不碰用户自己的工作区状态。G2 的价值（覆盖 shell 改动）确实存在，但没有会话级的“谁改的”隔离，风险大于收益；若用户需要，建议在实施后用 `suga undo` 之外的独立 issue 做“回合 diff 报告”（只报告不回滚）。这一点列为 Q4。

G1 的具体补偿：`:checkpoints` 与 `:undo` 都会列出该回合运行的 shell 命令（截断到 5 条、已脱敏），并在结果行固定给出 trace 与 git 提示，让用户自己判断。

### 3.9 保留、清理与 gc / suga-zy9f 的关系

- 本设计管**回合级**保留：`keep_turns` / `keep_days` / `max_session_bytes`，在写调用前清理（3.3）。
- `suga gc` 已经删 `sessions/<id>/` 整目录（`internal/cli/gc.go:99-104`），所以删会话时快照随之消失——本设计**不新增** gc 参数，但会在 `suga gc` 的列表/报告里把 `snapshots/` 的字节计入该会话的占用（让“删了能省多少”看得到）。
- 留给 suga-zy9f（issue 原文：spill 配额与压缩、backup/restore、events TTL、WAL checkpoint）：`snapshots/` 的 gzip 压缩、全局（跨会话）总量配额与 LRU、随整库备份一起打包、`suga export --with-snapshots`。本设计只保证“有上限、能按回合删”，不与它冲突。
- 快照不进模型上下文、不进 `suga export`、不进 transcript（`transcripts/<id>.jsonl` 里只有条目与事件）。

### 3.10 run / ACP / 子 agent

- **run**：写工具自动快照（无交互）；`suga undo <session>` 是它的回滚入口。不提供 `--undo-after` 之类自动回滚开关（自动回滚会让“失败即回滚”变得危险：模型可能已经跑过测试、用户可能已经在看结果）。
- **ACP**：写工具自动快照（ACP 的 runtime 与 run/chat 共用 `wiring.Open`，`internal/cli/acp.go:42-73`）；不提供 ACP 内的 undo（U2）。ACP 已有的 `toolDiff`（`internal/acpserver/policy.go:269-333`）展示的是本次调用的 diff，与本设计不冲突（未来可复用 `native.ApplyEdit` 给 chat 做 diff 预览，属调研 §3 #12）。
- **子 agent**：只读（不注册写工具，`internal/wiring/subagent.go:41-44`），没有快照。
- **`:undo` 在只读会话**：允许（它恢复的是这个会话自己造成的改动，方向是减少工作区改动）；只读会话通常没有快照，若有（例如先可写运行、之后 `--resume` 只读）也能回滚。列为 Q3。

### 3.11 兼容与迁移

- 无数据迁移（不建表、不加列）；老会话没有 `snapshots/` 目录，`:undo` 报“没有可回滚的快照”。
- 老二进制读新会话目录：不认识 `snapshots/`，不影响任何现有路径（`spill`/`gc` 都只按自己的名字寻址）。
- 新二进制读老会话：同上，无差别。
- 关闭开关即回到现状：`tools.snapshots.enabled: false` 后写工具不写快照、`:undo` 只报告没有快照（命令仍存在，避免“命令不见了”的困惑）。
- 与 trash 的关系：`file_remove` 的 7 天保留期**不变**；快照只引用回收区，所以删除的恢复窗口也是 7 天（到期后 `:undo` 报“无法恢复”，这正是 Q5 要确认的点）。

## 关键流程

### 一个回合的写与回滚

```text
用户：把 README 的分节改一下
  turn_1itu4… 开始
   file_edit README.md       → 快照 001：before=旧 README（sha256_before/after）
   file_write docs/x.md      → 快照 002：kind=absent
   shell_exec go test ./...  → 无快照（结果行会提示）
  turn 结束（completed）
用户：:checkpoints → 显示 turn_1itu4…：2 次写调用 / 2 个文件
用户：:undo → 清单（恢复 README.md、删除 docs/x.md、提示 shell 的 1 条命令）
       y → 逆序恢复：002 删除 docs/x.md（先记 pre-undo 快照）、001 写回旧 README.md
       → EntryUser{suga.undo} + undo 事件 + 结果行
下一回合模型看到 [用户回滚了上一回合的文件改动…]，不再重做
```

### 冲突

```text
:undo 时 tmp/x.log 的当前内容 ≠ sha256_after（用户在回合后又手工改了它）
  → 默认跳过并列出：跳过 tmp/x.log（冲突：快照之后被改动过）
  → :undo force 才覆盖（覆盖前仍记 pre-undo 快照）
```

### 快照失败（fail-closed）

```text
磁盘满 / snapshots 目录不可写 + tools.snapshots.on_error=deny（默认）
  → 写工具返回错误：快照失败，已拒绝写入 <path>：<原因>
  → 模型看到 tool error，回合继续；工作区没有被改
```

## 安全、数据、运维与回滚

- **安全**：快照目录权限与 spill 相同（0700/0600、`process.SecureDir`、`os.OpenRoot(sessionsDir)`、`validateSessionID`）；恢复只写快照记录的 `(root, rel)`，走写工具的同一套沙箱与路径解析，`:undo` 不接受路径参数，所以“回滚”不能被用来写到别处；快照含敏感内容（`.env` 等），因此**不进模型上下文、不进 export、不进 transcript**。`file_remove` 的守卫（工作区根、`.git`、home、通配符）在回滚路径上同样适用——恢复不会把内容写进这些位置（因为快照不可能记录它们）。
- **数据**：不建表、无迁移；快照是会话目录下的普通文件，随会话删除；`meta.json` 自描述、可人工检查；一个回合一个目录，`suga gc` 语义不变。
- **运维**：磁盘占用可控（单文件 16 MiB、会话 256 MiB、20 回合/7 天）；`tools.snapshots.enabled: false` 一键关闭；快照的写入与清理各写一条 trace 事件（可回答“这个回合为什么没有快照”）。
- **回滚（本功能自身的回退）**：改动分四块（native 钩子、快照器、chat 命令、CLI 命令），关开关即回到现状；快照目录是纯数据，删掉不影响会话（`:undo` 报没有快照）。
- **风险**：① 误以为 shell 改动也能回滚 → 命令文案、结果行、README 三处都写明；② 误回滚（用户手工改动被覆盖）→ 冲突检测默认跳过 + 确认 + `pre-undo` 快照；③ 磁盘放大 → 上限 + 保留期 + 只对写目标快照 + `file_edit` 复用已读内容；④ `file_remove` 的恢复依赖回收区（7 天）→ 明确提示（Q5）。

## 决策

- **D1 快照放在 `<SUGA_HOME>/sessions/<sid>/snapshots/`**。备选：工作区 `.suga/snapshots/`（会污染用户仓库、可能被提交、被 `.gitignore` 忽略反而更糟）；数据库 BLOB（体积、备份、导出都变差）；只依赖 git（见 G2，非 git 仓库/仓库外路径覆盖不到）。选会话目录：与 `outputs/` 同族、`suga gc` 已覆盖、不进仓库。
- **D2 粒度按调用（每次写前）而不是按回合**。备选：回合开始时预扫“可能要改的文件”（预知不了：目标由模型在回合里决定）。按调用还能让 `file_edit` 零额外 I/O（复用已读原文）。
- **D3 `shell_exec` 只提示，不做自动 git 回滚**（G1，3.8 比较）。备选：G2（git 基线）覆盖更全，但会把用户自己的未提交改动卷进回滚、且对非 git 仓库/仓库外路径无效。
- **D4 `file_remove` 复用回收区，不复制第二份**。备选：独立复制快照（删除后 7 天仍可恢复，但一份内容存两处，删除大目录时磁盘翻倍）。选复用 + 明确提示“回收区过期后无法恢复”。
- **D5 冲突默认跳过，`--force` 才覆盖**。备选：默认覆盖（用户的手工改动会丢）；遇到冲突中止整次回滚（一个冲突挡住其它可安全恢复的文件）。
- **D6 入口是 chat `:` 命令 + `suga undo` CLI，ACP 不做**。备选：做成模型可调用的工具（模型能自己回滚自己的改动——但“撤销”是用户的权利，不该由被撤销者执行）；ACP 内提供（客户端已有自己的撤销面，且 ACP 交互全是工具调用，塞进 `session/update` 反而不好用）。
- **D7 快照失败默认拒绝写入（fail-closed）**。备选：总是继续写、只标记未快照（可用性更好，但用户会以为“能回滚”而实际上回不去）。默认 fail-closed，`on_error: continue` 可换。
- **D8 回滚后写 `suga.undo` 注入条目 + 事件，不自动开回合**。备选：作为 `context_update` 条目（语义上不对：它不是“本回合环境变化”，且会被段内重算逻辑覆盖）；不告知模型（模型会重做/困惑）。
- **D9 保留期与 trash 对齐（7 天）+ 20 回合 + 会话总量上限**。备选：无期限（磁盘无界）；只按回合数（长期不动的会话也留）。三个条件谁先到谁生效。
- **D10 回滚前记 `pre-undo` 快照，但本期不做 `:redo`**。备选：不做 `pre-undo`（省一半写入，但误回滚无法追回）；做 `:redo`（交互面变大，语义（“撤销撤销”与再撤销的栈）需要更多设计，先留数据）。

## 待确认事项

- **Q1 命令名与开关**：建议 `:undo` / `:undo N` / `:undo force` / `:checkpoints`，全局开关 `tools.snapshots.enabled`。备选命名：`:rewind`（更贴 Claude Code 的用语）、`:rollback`。
- **Q2 默认上限与保留期**：建议单文件 16 MiB、会话 256 MiB、20 回合 / 7 天。备选：更保守（单文件 4 MiB、会话 64 MiB）或更宽松（单文件不限、会话 1 GiB）。
- **Q3 只读会话是否允许 `:undo`**：建议允许（恢复=减少改动），并在文案里说明。备选：`--read-only` 下禁用（只读会话一律不改工作区）。
- **Q4 是否要做 git 辅助（G2）**：建议本期不做，只给提示；若需要，另立 issue 做“回合 diff 报告”（只报告、不回滚），或在确认“工作区只由 suga 使用”时再考虑自动 git 基线。
- **Q5 `file_remove` 的恢复窗口**：建议复用回收区（7 天，与 trash 一致）。备选：删除时额外复制一份快照（保留期由 `keep_days` 决定，磁盘约翻倍）；或干脆不支持删除的恢复（只支持“新建文件的回滚 + 已改文件的恢复”）。
- **Q6 `suga undo` 的范围**：建议 `--list` / `--turn` / `-n` / `--yes` / `--force` / `--json`，退出码 0/3/4 如上。备选：只做 `--list`（回滚只在 chat 里做，CLI 保持只读）。

## 结论与人工计划 Gate

方案的核心是“写工具在动手前把目标原来的样子交出去”，落到 `<SUGA_HOME>/sessions/<sid>/snapshots/<turn_id>/`；`:undo` 按回合逆序恢复，冲突默认跳过、确认后执行，回滚本身也留 `pre-undo` 快照。覆盖范围诚实地停在文件工具上：`shell_exec` 只提示并给 git 命令，不自动碰用户的工作区状态。风险主要由“上限 + 保留期 + 冲突检测 + 开关”兜住。

落地清单（批准后进计划阶段细化）：

1. `native.Env.Snapshots` 钩子 + 三个写工具的接入（`file_edit` 复用已读原文）+ 单测（快照内容、`kind=absent`、`kind=trash`、fail-closed）。
2. `personal.SessionSnapshots`（落盘、读取、保留清理）+ `Runner.Undo` / `Runner.Checkpoints` + 冲突检测 + `suga.undo` 条目与 `undo`/`snapshot` 事件 + 单测（逆序恢复、同路径多写、幂等、冲突三种、清理、目录/字节上限）。
3. chat：`:undo`/`:undo N`/`:checkpoints` 与确认交互、`layout.go` 帮助、`chat.undo_confirm`（默认开）；`suga undo` CLI 子命令。
4. 观测与文档：trace 渲染、`config/config.yaml`、README 中英、CHANGELOG。
5. 验收：`make smoke-chat` 新增 `scripts/ptysmoke/chat_undo.py`（fakellm edit 模式写文件 → `:checkpoints` → `:undo` 确认 → 断言文件内容恢复、trace 有 `snapshot`/`undo` 事件、下一回合请求体里出现 `suga.undo` 条目），并做一次反向验证（临时关掉快照写入，`chat_undo.py` 的恢复断言必须失败）；`chat_edit.py` 与 `chat_approval.py` 全量回归（会话目录里多了 `snapshots/`）；Windows 路径/权限行为经 gofer exec job 在主机跑 `go test ./...`。

同类产品对照（参考，不照搬）：Claude Code 等同类工具也提供“编辑前后记录快照 + 会话内回退（rewind）”，并把回退做在编辑器/工具层而不是靠版本控制；本设计与之共享“写前快照 + 会话内回滚”的形态，差异在：`file_remove` 复用已有回收区而不复制第二份、冲突默认跳过而不是覆盖、`shell_exec` 明确排除在外（只提示 + git 备选）、回滚后以注入条目告知模型。这些对照只作形态参考，具体行为一律以本仓代码与本设计为准。
