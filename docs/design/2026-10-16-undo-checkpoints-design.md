<!-- template_id: design; template_version: 1.1.1 -->
# 写操作快照与 `:undo`（回滚上一回合的文件改动）设计

> 状态：Draft 0.3 / 已批准（2026-10-16 用户：“全部按建议批准”；待确认事项均按文中建议选项定稿）

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-16 | suga（gofer job） | 初稿：写工具调用前快照（存放、粒度、上限、保留）、`:undo` / `:undo N` / `:checkpoints`、冲突检测、回滚后告知模型、`suga undo` CLI、shell_exec 边界与 git 备选比较、与 gc/suga-zy9f 的关系 |
| 0.2 | 2026-10-16 | suga（gofer job） | 关闭 U-C1–U-C3、U-M1–U-M7：改用待投递通知，补齐删除时机/混合删除、幂等状态、失败写入、全局清理、并发锁、root 与目标身份校验、DB 回合排序 |
| 0.3 | 2026-10-16 | suga（gofer job） | 关闭 U-M1、U-M4、U-M5、U-M6 及快照元数据/接口一致性缺陷：拦截 steering 命令，补跨进程会话锁、可信 root、Lstat 与现行钩子参数 |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

关联：issue suga-wl0l（本设计来源）、suga-zy9f（数据治理）、suga-oyby（`:add-dir`，快照需要涵盖附加目录）。调研依据：`docs/research/2026-10-16-feature-strengthening-analysis.md` §3 #11。独立评审：`local/reviews/2026-10-16-approval-undo-design-review-r1.md`（不入库）。

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

- 写工具直接涵盖文件：`file_write` 用 `O_WRONLY|O_CREATE|O_TRUNC` 写目标（`pkg/agentcore/tools/native/file.go:152-170`），返回值只有字节数（`:81`）；`file_edit` 读全文、在 `applyEditSpans` 成功后内存替换并**整文件重写**（`native/edit.go:63-90`）；`shell_exec` 在工作区里跑任意命令，返回输出与退出码（`native/code_exec.go:252,279-304`）。
- 唯一的可恢复路径是 `file_remove`：移到 `$SUGA_HOME/trash/<UTC 时间戳>-<6 hex>/`，保留 7 天，`origin.json` 记录来源（`native/remove.go:21-32,250-254,278-305`，装配 `internal/wiring/capabilities.go:154-162`）。被 `.gitignore` 命中的目录是**永久删除**（`remove.go:227-240`）。
- 全仓没有任何 undo/checkpoint/snapshot/revert 代码或遗留字段（`migrations/*.sql` 里也没有相关列）。
- 会话目录只有 `outputs/`：`<SUGA_HOME>/sessions/<sid>/outputs/<16hex>.txt`（`internal/personal/spill.go:23-96`、`internal/config/paths.go:29-37`），无配额、无压缩，只有 `suga gc` 删整个会话时一起删（`internal/cli/gc.go:99-104`）。

用户可见目标：

1. 模型的一次写操作可被撤销：`:undo` 回滚**上一回合**的全部文件改动（新建的文件被删掉、改过的文件恢复原内容、删除的文件从回收区回来）。
2. 回滚之前先看到清单：恢复几个文件、删除几个新建文件、哪些跳过（冲突 / 无法恢复），并且要确认才执行。
3. `:undo N` 回滚最近 N 个有改动的回合；`:checkpoints` 列出本会话的快照点。
4. 回滚后模型知道：下一回合它看到“用户回滚了这些改动”，不会把已撤销的东西再写回去。
5. 范围与代价写清楚：`shell_exec` 的改动不在快照内（命令可以改任何地方）；工作区是 git 仓库时给出可用的 git 备选与命令提示。

## 名词

- **快照（snapshot）**：一次写工具调用**执行前**记录的目标状态（原文件字节、或“原本不存在”、或“已在回收区”）。
- **快照点（checkpoint）**：一个回合的全部快照，落在 `<sessions>/<sid>/snapshots/<turn_id>/`。
- **回滚（undo）**：按快照把目标恢复到该回合开始时的状态；同一路径被写多次时按调用**逆序**恢复，结果等于回合开始时的内容。
- **冲突（conflict）**：某个目标在快照之后又被改过（当前内容既不是快照前的、也不是这次写入后的），此时默认跳过而不是涵盖。
- **不可恢复（unrecoverable）**：快照本身拿不到原内容的情况——`file_remove` 直接永久删除的被忽略目录、超出单文件上限而未快照的大文件、快照目录已被清理。
- **`pre-undo` 快照**：回滚动作自身写入前记下的“回滚前状态”，让误回滚有据可查（本期不提供 `:redo`，但数据留着）。

## 范围与非目标

范围：

- 写工具（`file_write`、`file_edit`、`file_remove`）执行前的快照钩子，涵盖工作区与会话的 `:rw` 附加目录。
- 快照的存储布局、单文件/会话/全局上限、保留期限与清理。
- chat：`:undo`、`:undo N`、`:checkpoints`，含清单渲染、确认、结果行。
- 回滚后的会话记录：待投递的 `suga.undo` 通知、元数据与 trace 事件。
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
| `file_write`：路径自 `writeTargetPath`，解析先词法、后 `os.Root`；写是 `O_WRONLY\|O_CREATE\|O_TRUNC` 直接涵盖（非原子、无临时文件、无 fsync），返回 “wrote N bytes to path” | `pkg/agentcore/tools/native/file.go:68-83,92-125,130-173,175-181` |
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
| ACP 已有“从参数算出 diff”的实现（`toolDiff`：`file_write` 用 content、`file_edit` 复用 `native.ApplyEdit`，上限 `maxDiffBytes`，只涵盖 cwd 内路径） | `internal/acpserver/policy.go:75-77,269-333` |
| issue suga-zy9f：spill 无配额与压缩、gc 无 events TTL/WAL/memory 涵盖、要做 backup/restore 与 gzip/LRU 配额 | `.gofer/tracker` 中 suga-zy9f（`suga gc`/spill 的现状与本节表一致） |

## 总体方案

主线一句话：**写工具在动手之前把“目标原来的样子”交给一个快照器，快照按“会话 → 回合 → 调用”落盘；`:undo` 就是对某个回合的快照做一次逆序恢复，先列清单、检测冲突、要确认。**

三条边界从一开始就定死，避免被误解为“万能的撤销”：

1. 只涵盖 `file_write` / `file_edit` / `file_remove` 三个工具的目标路径；`shell_exec` 的改动不记录（D3）。
2. 快照是**本机数据**，不进模型上下文、不进 `suga export`、不跨机器；`suga gc` 删会话时一起删。
3. 恢复只写快照记录过的 `(根, 相对路径)`，`:undo` 不接受路径参数；恢复写入仍走写工具的同一套 `os.Root` 沙箱与路径解析。

## 架构

```text
  模型 file_write/file_edit/file_remove
        │
        ▼  （native 工具内部，审批通过之后、I/O 之前）
  native.Env.Snapshots.BeforeCall(ctx, SnapshotRequest{Tool, Root, Rel, Kind, Before})
        │                                  ▲ ctx 里的会话/回合来自 personal.CurrentTurn(ctx)
        ▼
  personal.SessionSnapshots  ──> <SUGA_HOME>/sessions/<sid>/snapshots/<turn_id>/<NN>-<hex>/
                                     meta.json      before（原文件字节，kind=file）
  写工具完成 ──> AfterCall(ctx, SnapshotResult{AfterSHA256, TrashEntries, PurgedDirs, Status})  记进 meta.json（冲突检测用）

  chat 提示符：:checkpoints → runner.Checkpoints(ctx, sid)（扫目录）
              :undo [N]    → runner.Undo(ctx, sid, N, force)
                               ├── 列清单（恢复 / 删除 / 跳过冲突 / 无法恢复）→ 确认
                               ├── 逆序恢复（同一沙箱；写前再记 pre-undo 快照）
                               ├── 写入待投递 undo 通知 + trace 事件
                               └── 打印结果行（含 shell_exec 提示与 git 提示）
  CLI：suga undo <session> [--list] [--turn <id>] [--force] [--json]
```

组件划分（分层：`native` 只认得 `Env` 里的一个接口，不知道会话与存储）：

- `pkg/agentcore/tools/native`：`Env` 增加 `Snapshots Snapshotter` 字段（可为 nil）；写工具在 I/O 前调用 `BeforeCall`、成功后调用 `AfterCall`。工具不判断是否启用、不决定存哪。
- `internal/personal/snapshots.go`（新）：`SessionSnapshots`——与 `SessionSpiller` 同族的目录写入器；全局清理由启动与 gc 触发（`os.OpenRoot(sessionsDir)`、`process.SecureDir`、0700/0600、hex 名、`validateSessionID`），负责落盘、读取某个回合、保留清理与全局清理。
- `internal/personal/undo.go`（新）：`Undo` / `Checkpoints` / 冲突检测 / 逆序恢复 / 写入待投递 `suga.undo` 通知与 `undo` 事件；对外的两个方法挂在 `*Runner` 上（与 `Compact` 同风格）。
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
  "real_rel": "src/a.go",          // EvalSymlinks 后相对 root 的真实路径
  "kind": "file",                  // file | absent | trash | purged | special
  "size": 2143,                    // 原文件字节数
  "sha256_before": "…",            // 调用前内容（kind=file）
  "sha256_after": "…",             // 本次写入后内容（AfterCall 填；kind=absent 时为空）
  "status": "complete",            // pending | complete | incomplete
  "mode": "0644",
  "file": "before",                // kind=file 时的内容文件名
  "trash_dir": "",                 // kind=trash：回收区条目目录
  "created_at": "2026-10-16T14:32:11Z",
  "trash_entries": [],              // file_remove 成功后回传的 trash 条目
  "purged_dirs": [],                // 被永久删除的目录清单
  "source": "call"                 // call；pre-undo 位于独立 pre-undo/ 子目录
}
```

设计取舍：

- **不建数据库表**：目录本身就是索引（哪个回合有快照 = 哪个目录存在），`meta.json` 自描述。好处是零迁移、`suga gc` 天然涵盖、用户可以直接看和删。代价是不能用 SQL 查询——`:checkpoints` 只扫一个会话的目录，量级很小。
- **`sha256_before` / `sha256_after` 都要**：前者用于“已经恢复过（幂等）”的判断，后者用于冲突检测（见 3.4）。
- **二进制按字节存**：不读成文本、不做换行转换、不脱敏（回滚需要原文；spill 是给模型看的，快照不是）。

### 3.2 何时快照、快照什么

| 工具 | 快照动作 | 额外 I/O |
|---|---|---|
| `file_write` | 读目标现有内容 → `before` + `sha256_before`；目标不存在 → `kind=absent` | 一次读盘（原来没有） |
| `file_edit` | 先执行 `applyEditSpans`，成功后复用工具已读入内存的原文（`native/edit.go:69`）→ `before`，再调用 `writeFileInRoot` | 无（只写快照文件） |
| `file_remove` | 先记 pending-remove；成功后由结果 metadata 补回实际 trash entry，并记录 `purged_dirs` | 无（回收区已有副本） |
| 被忽略目录的永久删除 | `kind=purged`（只记路径与计数，不可恢复）；混合删除额外记录 `purged_dirs`，恢复时只恢复 trash 并报告部分恢复 | 无 |

时机：审批通过之后、I/O 之前；`file_remove` 的 trash 路径在成功后补全，写入失败记录 `incomplete`，因此不能把“没有完整快照就不写”当作绝对保证。回合内同一路径写多次会有多条快照——这是正确的语义（逆序恢复得到回合开始状态，见 3.4）。

### 3.3 上限、失败处理与保留

新增配置：`tools.snapshots.enabled`、`max_file_bytes`（16 MiB）、`max_session_bytes`（256 MiB）、`max_global_bytes`、`keep_turns`（20）、`keep_days`（7）、`on_error`（默认 `deny`）与 `chat.undo_confirm`。每次写调用前清理本会话；启动和 `suga gc` 前全局扫描 `sessions/*/snapshots`，按过期日和全局上限清理最旧已完成回合。run 默认 fail-closed；目录不可写、锁失败或清理状态不确定时拒绝写入。

快照失败必须具体说明并拒绝写入；`continue` 是显式例外。oversize 也要写入工具结果和模型消息“本次写入不可撤销”，不能伪装成没有快照。快照信息作为现有 tool event attr（不新增逐调用 `snapshot` 事件），只有失败和清理单独记 event；`suga gc` 报告计入 snapshots 字节。

### 3.4 回滚语义与冲突检测

`Undo` 按 DB 的 `started_at, id` 顺序选择最近 N 个**未 undone 且有快照**的回合；shell-only 回合不会静默跳过，清单先显示其位置与未恢复原因。checkpoint 有 `state.json`：`open`、`undone` 或 `partial`；已回滚回合不计入 N，普通 undo 永远排除独立 `pre-undo/` 子目录。每个文件按调用逆序处理，冲突默认跳过，`--force` 只强制写回内容冲突，不绕过 root、类型、哈希、受保护路径校验。

undo 先取得进程内 session gate，再取得会话目录的跨进程文件锁；锁拿不到返回“会话忙”。恢复前只接受会话记录 `sessions.cwd` 本身或会话 meta 中持久化的 `:rw` 附加目录，root 必须与这些值等值匹配（不按包含关系判断），rel 必须是规范相对路径，before 哈希必须等于 `sha256_before`，并再次核对真实路径、文件类型、符号链接目标和硬链接身份。恢复 trash 前核对 `origin.json` 与 `(root, rel)`，原位置已存在即冲突；受保护路径（规则文件、`.suga/**`、`.git/**`、`$SUGA_HOME/**`）永不由 undo 写入；符号链接按链接本身重建。`file_remove` 采用 pending-remove/成功后补全的两阶段快照；混合 trash + purged 只恢复 trash 并报告部分恢复，trash 保留期固定 7 天。失败写入是 `incomplete`：若当前仍等于 before 则幂等跳过，否则允许恢复并单独列出。

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

- 只在**提示符处**可用；回合进行中，`turnInput.insert` 先识别以 `:` 开头的本地命令（包括 `:undo`、`:checkpoints` 和现有目录/整理命令），把整行退回输入框并提示“回合进行中，等它结束再执行本地命令”，绝不作为 steering 发给模型。外部投递的文本（`suga send`、hook 回复、任务通知）永不当作命令。
- `:undo` 不接受路径参数；`:undo N` 的 `N` 是“最近 N 个有改动的回合”（`1..keep_turns`），不是条目序号；`:undo force` 与 `:undo N force` 表示涵盖内容冲突。
- 没有快照时：`没有可回滚的快照（本会话没有写工具改动，或快照已按 tools.snapshots 清理）。`
- 确认默认是“取消”（回车取消），与审批询问的默认一致。
- 帮助文案（`internal/chat/layout.go:90-94`）加上 `:undo / :checkpoints`。

### 3.6 回滚后告知模型

回滚不是用户的“新请求”，而是一条**待投递事实通知**：摘要写入会话 meta 的队列；下一回合由 contextBuilder 创建带 `suga.undo` metadata 的注入条目并加入 `injectedItemKeys`。这样：

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
suga undo <session> --force --yes  # 涵盖内容冲突
suga undo <session> --list --json  # 供脚本
```

- `<session>` 接受完整 id 或前缀（与 `suga trace`/`suga sessions` 的既有习惯一致）。
- 退出码：`0` 成功（或无剩余改动）、`3` 没有快照 / 全部冲突 / 全部无法恢复、`4` 会话不存在、`5` 会话忙（无法取得跨进程会话文件锁）。
- 不调用模型：`--yes` 时也写入待投递 `suga.undo` 通知；不调用模型不等于不记录。CLI 需要 store 句柄（`openCoreStore()`，`internal/cli/stats.go:34`）。
- run 与 ACP：写操作照常快照（共享运行时），`suga undo <session>` 对它们产生的会话同样可用；ACP 内部不提供 undo 交互（U2）。CLI 与 chat 都只写待投递通知，不伪造旧回合的 TurnID。

### 3.8 `shell_exec` 的边界与 git 备选（比较）

事实：`shell_exec` 在工作区里跑任意命令（`cmd.Dir = t.workspace`，`native/code_exec.go:252`），可写工作区、附加目录、也能写工作区之外（用户自己配的 `!cmd` 与模型的 `shell_exec` 都如此），工具返回输出与退出码，**不报告改了哪些文件**。所以“用快照涵盖 shell 的改动”在原理上做不到。

三种备选：

| 方案 | 能涵盖 | 代价与风险 |
|---|---|---|
| **G1 只提示（本设计采用）** | 文件工具的改动 | `shell_exec` 造成的改动要靠用户自己（`:undo` 结果行给 `suga trace --turn` 与 git 命令提示） |
| **G2 回合前后用 git 取基线**：回合开始 `git stash create`（生成 commit 对象、**不改工作区**）或在干净树上记 HEAD，回合结束 `git diff` 得到本回合改过的文件；undo 用 `git checkout -- <files>` / 应用反向 patch | 文件工具 + `shell_exec` + 其它进程删改的文件 | 要求工作区是 git 仓库且有 `git`；用户的未提交改动会与“本回合改动”混在一起，误回滚会把用户自己的改动一键回退；非 git 仓库、`.gitignore` 里的文件（如 `tmp/`）、仓库外路径（`--add-dir` 到别的目录）都无法处理；一个仓库多个会话并发时基线会互相污染 |
| **G3 回合前后快照整个工作区** | 一切 | 大仓库不可行（时间与磁盘）；忽略目录、依赖目录会拖垮；不做 |

本设计取 **G1**：安全、可解释、不碰用户自己的工作区状态。G2 的价值（涵盖 shell 改动）确实存在，但没有会话级的“谁改的”隔离，风险大于收益；若用户需要，建议在实施后用 `suga undo` 之外的独立 issue 做“回合 diff 报告”（只报告不回滚）。这一点列为 Q4。

G1 的具体补偿：`:checkpoints` 与 `:undo` 都会列出该回合运行的 shell 命令（截断到 5 条、已脱敏），并在结果行固定给出 trace 与 git 提示，让用户自己判断。

### 3.9 保留、清理与 gc / suga-zy9f 的关系

- 本设计管**回合级与全局**保留：`keep_turns` / `keep_days` / `max_session_bytes` / `max_global_bytes`，由写调用前、启动和 gc 清理。
- `suga gc` 已经删 `sessions/<id>/` 整目录（`internal/cli/gc.go:99-104`），所以删会话时快照随之消失——本设计**不新增** gc 参数，但会在 `suga gc` 的列表/报告里把 `snapshots/` 的字节计入该会话的占用（让“删了能省多少”看得到）。
- 留给 suga-zy9f 的是压缩、backup/restore 与 export 集成；本设计已经定义全局上限与过期清理，不把活跃会话作为清理前提。
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
- 与 trash 的关系：`file_remove` 的 7 天保留期**不变**；快照只引用回收区，所以删除的恢复窗口也是 7 天；`keep_days` 不能延长 trash 的固定保留期，到期后 `:undo` 报“无法恢复”。

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
       → 写入待投递通知 + undo 事件 + 结果行
下一回合由待投递队列注入 `suga.undo` 通知，模型看到 [用户回滚了上一回合的文件改动…]，不再重做
```

### 冲突

```text
:undo 时 tmp/x.log 的当前内容 ≠ sha256_after（用户在回合后又手工改了它）
  → 默认跳过并列出：跳过 tmp/x.log（冲突：快照之后被改动过）
  → :undo force 才涵盖（涵盖前仍记 pre-undo 快照）
```

### 快照失败（fail-closed）

```text
磁盘满 / snapshots 目录不可写 + tools.snapshots.on_error=deny（默认）
  → 写工具返回错误：快照失败，已拒绝写入 <path>：<原因>
  → 模型看到 tool error，回合继续；工作区没有被改
```

## 安全、数据、运维与回滚

- **安全**：快照目录使用 0700/0600 与 `os.OpenRoot`；快照含敏感内容，不进模型上下文、export 或 transcript。undo 必须持有进程内 session gate 和跨进程会话文件锁，只恢复经 root、rel、哈希、类型和真实目标校验的记录；`--force` 不绕过这些校验。后台任务可能随后改回文件，结果行列出仍运行的任务与 task id。
- **数据**：不建表、无迁移；checkpoint 目录含 state 与快照，待投递 undo 通知写入会话 meta/队列，随下一回合消费并加入 `injectedItemKeys`。中断回合的工具回放完成后再注入通知。
- **运维**：单文件、会话、全局上限和全局清理共同控制磁盘；Windows 锁文件、逐文件失败和大小写变体分别报告/合并。关闭开关或删除快照目录不影响旧会话。
- **回滚**：`shell_exec` 改动仍只提示 git 备选；ACP/run 的写操作照常快照，ACP 不提供内部 undo。

## 决策

- **D1 快照放在 `<SUGA_HOME>/sessions/<sid>/snapshots/`**。备选：工作区 `.suga/snapshots/`（会污染用户仓库、可能被提交、被 `.gitignore` 忽略反而更糟）；数据库 BLOB（体积、备份、导出都变差）；只依赖 git（见 G2，非 git 仓库/仓库外路径无法处理）。选会话目录：与 `outputs/` 同族、`suga gc` 已涵盖、不进仓库。
- **D2 粒度按调用（每次写前）而不是按回合**。备选：回合开始时预扫“可能要改的文件”（预知不了：目标由模型在回合里决定）。按调用还能让 `file_edit` 零额外 I/O（复用已读原文）。
- **D3 `shell_exec` 只提示，不做自动 git 回滚**（G1，3.8 比较）。备选：G2（git 基线）涵盖更全，但会把用户自己的未提交改动卷进回滚、且对非 git 仓库/仓库外路径无效。
- **D4 `file_remove` 复用回收区，不复制第二份**。备选：独立复制快照（删除后 7 天仍可恢复，但一份内容存两处，删除大目录时磁盘翻倍）。选复用 + 明确提示“回收区过期后无法恢复”。
- **D5 冲突默认跳过，`--force` 才强制写回**。备选：默认强制写回（用户的手工改动会丢）；遇到冲突中止整次回滚（一个冲突挡住其它可安全恢复的文件）。
- **D6 入口是 chat `:` 命令 + `suga undo` CLI，ACP 不做**。备选：做成模型可调用的工具（模型能自己回滚自己的改动——但“撤销”是用户的权利，不该由被撤销者执行）；ACP 内提供（客户端已有自己的撤销面，且 ACP 交互全是工具调用，塞进 `session/update` 反而不好用）。
- **D7 快照失败默认拒绝写入（fail-closed）**。备选：总是继续写、只标记未快照（可用性更好，但用户会以为“能回滚”而实际上回不去）。默认 fail-closed，`on_error: continue` 可换。
- **D8 回滚后写 `suga.undo` 注入条目 + undo 事件，不自动开回合**。备选：作为 `context_update` 条目（语义上不对：它不是“本回合环境变化”，且会被段内重算逻辑涵盖）；不告知模型（模型会重做/困惑）。
- **D9 保留期与 trash 对齐（7 天）+ 20 回合 + 会话总量上限**。备选：无期限（磁盘无界）；只按回合数（长期不动的会话也留）。三个条件谁先到谁生效。
- **D10 回滚前记 `pre-undo` 快照，但本期不做 `:redo`**。备选：不做 `pre-undo`（省一半写入，但误回滚无法追回）；做 `:redo`（交互面变大，语义（“撤销撤销”与再撤销的栈）需要更多设计，先留数据）。

## 待确认事项

- **Q1 命令名与开关**：建议 `:undo` / `:undo N` / `:undo force` / `:checkpoints` 与 `tools.snapshots.enabled`；备选 `:rewind` 或 `:rollback`，但现有命名更贴合“撤销”。
- **Q2 上限与保留**：建议单文件 16 MiB、会话 256 MiB、全局上限可配置、20 回合/7 天；备选更保守的 4/64 MiB，理由是降低敏感副本和磁盘风险，或更宽松的 1 GiB，理由是大仓库可用性。
- **Q3 只读会话**：建议允许并明确“撤销会写回文件”；备选禁用，理由是严格遵守只读语义。
- **Q4 shell 改动**：建议继续 G1 只提示；备选另立 issue 做只报告的回合 diff，评审与原稿都反对直接 git 回滚用户未提交改动。
- **Q5 删除恢复**：建议复用固定 7 天 trash，并接受混合删除的部分恢复；备选另存快照（keep_days 可独立控制但磁盘翻倍），或不支持删除恢复。
- **Q6 CLI**：建议完整参数集，`--force` 必须与 `--yes` 同时出现或二次确认，且必须拿会话锁；备选只做只读 `--list`，理由是缩小 CLI 的破坏面。会话忙使用独立退出码 5。
- **Q7 会话锁**：建议 CLI 使用会话目录跨进程文件锁，拿不到立即退出并提示会话忙；备选等待固定时长后重试，理由是改善短暂竞争，但会让破坏性命令的时序不透明。
- **Q8 root 可信来源**：建议只接受会话记录的 `sessions.cwd` 和 `:rw` 附加目录等值；备选按当前 CLI cwd 或祖先包含关系判断，理由是使用方便，但会重新打开篡改 root 的写入风险。

## 结论与人工计划 Gate

方案的核心是“写工具在动手前把目标原来的样子交出去”，落到 `<SUGA_HOME>/sessions/<sid>/snapshots/<turn_id>/`；`:undo` 按回合逆序恢复，冲突默认跳过、确认后执行，回滚本身也留 `pre-undo` 快照。范围诚实地停在文件工具上：`shell_exec` 只提示并给 git 命令，不自动碰用户的工作区状态。风险主要由“上限 + 保留期 + 冲突检测 + 开关”兜住。

落地清单（批准后进计划阶段细化）：

1. `native.Env.Snapshots` 钩子 + 三个写工具的接入（`file_edit` 复用已读原文）+ 单测（快照内容、`kind=absent`、`kind=trash`、fail-closed）。
2. `personal.SessionSnapshots`（落盘、读取、保留清理）+ `Runner.Undo` / `Runner.Checkpoints` + 冲突检测 + `suga.undo` 条目、`undo` 事件与 tool 事件快照属性 + 单测（逆序恢复、同路径多写、幂等、冲突三种、清理、目录/字节上限）。
3. chat：`:undo`/`:undo N`/`:checkpoints` 与确认交互、`layout.go` 帮助、`chat.undo_confirm`（默认开）；`suga undo` CLI 子命令。
4. 观测与文档：trace 渲染、`config/config.yaml`、README 中英、CHANGELOG。
5. 验收：`make smoke-chat` 新增 `scripts/ptysmoke/chat_undo.py`（fakellm edit 模式写文件 → `:checkpoints` → `:undo` 确认 → 断言文件内容恢复、trace 有 快照属性/`undo` 事件、下一回合由待投递队列注入 `suga.undo` 条目），并做一次反向验证（临时关掉快照写入，`chat_undo.py` 的恢复断言必须失败）；`chat_edit.py` 与 `chat_approval.py` 全量回归（会话目录里多了 `snapshots/`）；Windows 路径/权限行为经 gofer exec job 在主机跑 `go test ./...`。

同类产品对照（参考，不照搬）：Claude Code 等同类工具也提供“编辑前后记录快照 + 会话内回退（rewind）”，并把回退做在编辑器/工具层而不是靠版本控制；本设计与之共享“写前快照 + 会话内回滚”的形态，差异在：`file_remove` 复用已有回收区而不复制第二份、冲突默认跳过而不是涵盖、`shell_exec` 明确排除在外（只提示 + git 备选）、回滚后以注入条目告知模型。这些对照只作形态参考，具体行为一律以本仓代码与本设计为准。
