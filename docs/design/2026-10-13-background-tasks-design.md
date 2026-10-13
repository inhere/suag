# 后台任务（2026-10-13）

关联：issue suag-ol03，plan-20261013-131834-555306b9（D7）。后续的子 agent（suag-dmv6）复用本设计的任务管理。参考：Claude Code 的 Bash `run_in_background` 加输出读取与结束通知，codex、opencode、Cline 的“长命令转后台 + wait/kill”，omp 的后台任务。

本文只做设计，不含实现。原先的待定项已由用户确认，见文末“已定”。

## 现状

- `shell_exec` 同步执行：Start、Wait、杀进程树绑在一次调用里，输出只进内存。超时默认 120s，上限 600s（`tools.shell_timeout_max`）。一次工具调用结束后，不会留下任何存活的子进程（`code_exec.go:182-247`）。进程树管理本身可以单独复用：Unix 用进程组，Windows 用带 KILL_ON_JOB_CLOSE 的 job object（`code_exec_unix.go`、`code_exec_windows.go`）。
- 大输出转存到 `<sessions>/<sid>/outputs/<hex>.txt`，ref 形如 `out:<sid>:<hex>`。`recall` 每次按行号从头扫描，所以对仍在增长的文件也能读到当时已写入的部分（`internal/personal/ledger.go:399`）。
- 注入点：
  - 每次模型请求前都会调用 `contextBuilder.BuildMessages` 加 `compose()`（`internal/personal/context.go:221/259`）。
  - 回合开始时用 `RunRequest.SystemContext`。
  - context update 管的是“上下文来源变了”，一次性通知不适合放在那里。
- 三种入口的生命周期不同：
  - `run`：一个回合跑完，进程就退出。
  - `chat`：一个进程对应一个 Runner。回合之间有空闲调度和 `suag send` 投递；投递在回合进行中只排队，空闲时作为注入回合运行。
  - ACP：进程常驻，但每个 prompt 都新建一套完整的 wiring runtime，回合结束就关闭（`acpserver/server.go:215/242`）。工具与 Runner 都不跨回合存活。
- `pkg/agentcore/schedule` 是进程内的通用定时器（After、Every、Idle），目前只有 chat 空闲整理在用。

## 目标

1. 模型能把耗时命令（构建、全量测试、服务、监听）放到后台。启动时立即拿到任务 id，同时继续做别的事，之后再读输出、等待或终止。
2. 任务结束时，模型在下一次请求前得到一条简短通知（退出码、耗时、输出量、ref），不需要轮询。
3. 生命周期清楚：任务属于会话，进程退出时整棵进程树一定被清理，不留孤儿进程。
4. 审批、只读模式、危险删除拦截与 `shell_exec` 保持一致。trace 能看到任务的完整过程。
5. 任务管理做成通用的：后续的监听类任务与子 agent 也挂在同一个管理器上。

不做：跨进程或重启后接管任务（进程退出即终止）；后台任务的交互输入（stdin 仍为空）。

## 方案

### 任务管理器（TaskManager）

放在新包 `pkg/agentcore/tasks`，不依赖 `internal/*`。职责如下：

- 按会话登记任务：id（`t1`、`t2`… 会话内递增）、种类（`shell`，以后加 `watch`、`agent`）、命令摘要、开始时间、状态（running、exited、killed、timed_out、lost）、退出码、输出文件、字节数。
- 启动与终止：复用 shell_exec 的 `newProcessTree`、`commandEnv` 与 shell 选择，把这部分抽成包内共享函数。进程的 stdout 和 stderr 合并写入输出文件，每行写入前先做 `redact.Secrets`，与转存一致。
- 等待：`Wait(id, timeout)` 返回任务结束或超时；`Notify` 通道把结束事件交给宿主。
- 上限：
  - `tasks.max_running`：同时运行的任务数，默认 4。
  - `tasks.max_duration`：单个任务最长运行时间，默认 2h，超过就终止，状态记为 timed_out。
  - `tasks.max_output_bytes`：输出文件上限，默认 64 MiB，超过后不再写入，并在文件末尾注明。

管理器的归属，即生命周期，由宿主决定。这是本设计的关键：

| 入口 | 管理器挂在哪 | 何时清理 |
|---|---|---|
| chat | chat 进程（与 Runner 同生命周期） | 退出 chat（`/exit`、两次 Ctrl-C）时终止全部任务 |
| ACP | `acpserver.Agent` 的会话状态（`a.sessions[id]`），每个 prompt 新建 runtime 时传入 | 连接关闭或进程退出时清理；`session/cancel` 只取消当前回合，不动任务 |
| run | 本次 run | 回合结束、进程退出前终止全部任务（见下文“run 模式”） |

`wiring.Options` 新增 `Tasks *tasks.Manager`。为空时不注册后台能力。

### 工具

- `shell_exec` 新增参数 `background: true`。为 true 时立即返回 `任务 t3 已在后台启动：<命令摘要>；输出 ref out:<sid>:<hex>`，`timeout` 参数不再生效，改由 `tasks.max_duration` 约束。只在挂了管理器时出现在 schema 里，未挂时不暴露这个参数。
- 新工具 `task`，属于 AgentState 类，只管理本会话的任务：
  - `list`：列出任务及状态。
  - `output`：读输出。参数 `id`、`offset`、`limit`、`grep`，复用 `readOutputLines` 和 `searchOutput`，结果也可以用 ref 交给 `recall`。
  - `wait`：等待结束，参数 `id`、`timeout`（秒，上限是 `tools.shell_timeout_max`）。结束时返回尾部输出与退出码，超时则返回“仍在运行”和最新的几行。
  - `kill`：终止整棵进程树。
- 审批：`background: true` 的 shell_exec 走原有审批与危险删除守卫（`dangerGuard` 按 `command` 判断，不受 background 影响）。`task kill` 只作用于本会话自己启动的任务，不需要询问。只读模式没有 shell_exec，也就没有后台任务；`task` 工具在只读模式下只允许 list 和 output。

### 结束通知

任务结束时，管理器把通知放进会话的待通知队列。`contextBuilder.compose()` 在本次请求的消息末尾附加一条 user 消息，然后清空队列：

```
[后台任务通知] t3 已结束：exit=0，耗时 4m12s，输出 1.2 MB（out:sess_x:ab12…）。最后 5 行：
…
```

这条通知作为新的 entry 类型 `task_notice` 落库（与 hook 注入不同，hook 注入不落库），这样回放与分段摘要都能看到它，前缀缓存也稳定，因为通知只追加、不改写历史。同一次请求前结束的多个任务合并成一条通知。

回合之间的处理：

- chat 空闲时任务结束：默认像 `suag send` 投递一样，启动一个注入回合，提示文本是上面那条通知，让模型继续工作。`tasks.wake_on_exit: true|false` 控制，默认 true，见待定 1。如果设为 false，通知留到下一次用户输入的回合。
- ACP：回合之外不主动推送。目前没有这个先例，客户端是否在回合外渲染也不确定。通知留到下一个 prompt 的回合。需要时再加 `session/update` 推送，见待定 3。

### run 模式

`run` 回合结束时，进程就要退出了。模型准备给出最终答复时，如果还有任务在运行，Runner 追加一步收尾请求（与 MaxSteps 的 WrapUp 同一机制），提示“还有 N 个后台任务在运行（列出 id 和命令），先 wait 或 kill，再给出答复”。这样的提醒最多追加一次。收尾后仍有运行中的任务，就在退出前终止，并在结果里注明。stream-json 与 ACP 的结束行带上 `killed_tasks`。

### 观测

- trace 新增事件种类 `task`：
  - 启动时记 `task_id`、`arg`（命令摘要）、`kind`。
  - 结束时补 `status`、`exit_code`、`bytes`、`duration_ms`、`ref`。
  - 启动它的那个工具事件带上 `task_id`，两者能关联起来。
- `--analyze`：统计后台任务数、后台时长，以及与模型、工具时间重叠的部分（后台节省的等待时间）；另外统计 wait 次数与等待时长，用来发现“启动后台又立刻 wait”这种用法。
- `--web`：时间线把后台任务画成从启动到结束的横条，与前台事件重叠显示；任务通知 entry 挂在对应的回合上。

### 监听与 hook（第二阶段）

- 监听类任务 `watch`：把命令周期性执行（`every`）或在文件变化时执行（`paths` 的 glob），复用 `pkg/agentcore/schedule` 的 Every 与 Observer。结果与上一次不同时才产生通知，避免刷屏。
- hook 后台化：`hooks.<event>.background: true` 让耗时的 hook（例如 Stop 时跑全量测试）进入任务管理器；它的结果按上面的结束通知机制进入会话。
- 这两项依赖第一阶段的管理器和通知，不在第一阶段实现。

### 子 agent（suag-dmv6）的衔接

子 agent 也是一种任务（kind `agent`）：输出是它的最终报告，结束通知方式相同，`task wait` 可以等待它。区别在于它不是子进程，而是同进程内的另一个 Runner。管理器的接口按“可等待、可取消、有输出”抽象，不绑定进程。

## 分阶段

1. **P1**：`pkg/agentcore/tasks` 管理器（shell 种类）、shell_exec 的 `background`、`task` 工具、`task_notice` 通知、trace 的 `task` 事件。先在 chat 和 run 两个入口落地，包括 run 的收尾提醒。测试：单测（Unix 与 Windows 的进程树终止、输出上限、超时）、伪终端冒烟（chat 启动后台任务，空闲时任务结束后自动开始注入回合）、真实模型跑一次“后台跑测试，同时读代码”。
2. **P2**：ACP（挂在会话状态上、连接关闭时清理），`--analyze` 与 `--web` 的后台视图。
3. **P3**：watch 任务与后台 hook。
4. **P4**：子 agent（suag-dmv6）接入同一个管理器。

## 已定（2026-10-13 用户确认按建议）

1. chat 空闲时任务结束自动开始一个注入回合，`tasks.wake_on_exit` 默认 true。
2. run 模式：最终答复前若有运行中的任务，提醒一次先 wait 或 kill；之后仍在运行的在退出前终止并注明。
3. ACP 先不在回合外推送，通知留到下一个 prompt 的回合。
4. 默认上限：同时运行 4 个、单个最长 2h、输出 64 MiB。

## 实施记录

- P1（v0.16.0）：管理器、`shell_exec` 的 `background`、`task` 工具、通知、trace 的 `task` 事件，chat 与 run 两个入口。
- P2 ACP（2026-10-13）：管理器挂在 ACP 进程上（`RunPersonalACP` 创建，按会话 id 分开），每个 prompt 新建的 runtime 通过 `wiring.Options.Tasks` 拿到它，所以任务跨回合存活，结束通知进入该会话的下一个 prompt。`session/cancel` 只取消回合；连接关闭后 `Serve` 返回，再终止仍在运行的任务。任务结束事件改由 `wiring.NewTaskManager` 设置的回调自己打开 store 写入（`personal.RecordTaskEndIn`），不再经过回合的 Runner：ACP 两个回合之间、进程退出时没有打开的 Runner，chat 与 run 也走同一条路径。只读模式的回合不注册后台能力，也不取通知，通知留到下一个写模式回合。

## P3 细化：watch 任务与后台 Stop hook（2026-10-13）

### 现状要点

- 任务管理器只接受 `*exec.Cmd`（`tasks.Spec.Cmd`），通知只在任务结束时产生，每个任务至多一条。
- 外部命令 hook 只有 chat 接入，全局一条 `hooks.command`，事件为 SessionStart、UserPromptSubmit、PostToolUse、Stop、SessionEnd。Stop 已是异步（`StartStop`）：chat 立刻回到提示符；返回 block 加 reason 时，reason 作为注入输入开始新回合。但用户一打字、socket 投递或任务唤醒都会中断它（发 Interrupt 后取消进程），耗时的 Stop（例如跑全量测试）因此常被打断。
- 仓库没有文件监听依赖，也没有轮询文件的代码；`pkg/agentcore/schedule` 有 Every 与 Idle，但目前只给 chat 空闲整理用。

### 1. 管理器扩展：函数任务与运行中通知

- `tasks.Spec` 增加 `Run func(ctx context.Context, out io.Writer, n Notifier) (exitCode int, err error)`，与 `Cmd` 二选一。函数任务没有进程树：Kill 和 `max_duration` 取消它的 ctx。`Kind` 由调用方填（`shell`、`watch`、`hook`，P4 的 `agent` 也走这条路）。
- 运行中通知：`Notifier.Notify(text)` 让仍在运行的任务产生一条通知（watch 的结果变化）。待通知队列从“任务 id”改为“任务 id 加文本”；`TakeNotices` 返回的 `Info` 新增 `Notice` 字段，为空时表示结束通知（文本仍是 Describe 加最后几行）。同一个任务最多保留一条未取走的运行中通知，新的替换旧的，避免刷屏。Wait 与 Kill 只清掉结束通知。
- 唤醒：运行中通知与结束通知一样触发 `OnExit`（改名为 `OnNotice`），所以空闲的 chat 会被唤醒。

### 2. watch 任务

- 入口：`shell_exec` 在 `background: true` 时再接受 `every`（秒，至少 10）与/或 `watch_paths`（相对工作区的 glob，支持 `**`）。审批、只读模式、危险删除拦截都沿用 `shell_exec`；不带 `background` 时这两个参数报错。没有挂管理器时 schema 里不出现。
- 运行：先立即跑一次作为基线，之后在每个触发点再跑：
  - `every` 到时；
  - `watch_paths` 匹配的文件有变化：每 2 秒轮询一次匹配文件的 mtime 与大小，跳过 `.git`，最多 5000 个文件，检测到变化后再等 1 秒没有新变化才触发（去抖）。
  - 两次运行不会重叠，运行期间的多次触发合并成运行结束后的一次。
- 每次运行的超时用 `tools.shell_timeout`（受 `shell_timeout_max` 限制）。输出追加到任务的输出文件，每次运行前写一行 `=== run N (every|files: a.go, b.go) ===`，结束写 `=== exit X ===`。
- 通知：退出码或归一化后的输出与上一次不同才通知。归一化会去掉耗时与时间戳（如 `0.123s`、`12ms`、RFC3339 时间），让 `ok pkg 0.4s` 这类每次都变的数字不算变化。基线运行只在失败时通知。通知文本形如 `t3 watch run 5 (files: calc.go): exit 0 -> 1`，再加最后几行。
- 生命周期：计入 `max_running`；`max_duration` 到时结束（timed_out）；`task kill` 或会话结束时终止。`task list` 显示运行次数与最近一次退出码。
- trace：结束事件的 attrs 加 `runs`、`changes`；每条通知本身作为 user entry 落库（同 P1）。

### 3. 后台 Stop hook

- 配置 `hooks.stop_background: true`，默认 false，只在 chat 生效（run 与 ACP 没有外部 hook）。开启后，Stop hook 作为 `hook` 类任务（摘要 `Stop hook`）进入管理器：
  - 用户输入、投递、任务唤醒都不再中断它，用户可以继续对话；
  - 返回 block 加 reason 时产生一条通知 `[background task notice] Stop hook asks to continue: <reason>`。chat 空闲时唤醒并开始注入回合（与现在的 block 效果相同），回合进行中则在下一次模型请求前带上；
  - 其他结果（不 block、失败、超时）不通知，只记 trace 的 hook 事件；stderr 写进任务输出。
  - 超时：设了 `hooks.stop_wait_sec` 时用它加 2 秒，否则用 `tasks.max_duration`。
  - 新的 Stop 到来时，如果上一个后台 Stop 还在跑，就终止旧的再启动新的（只关心最新状态）。
  - 由这条通知触发的回合，其 Stop 事件带 `stop_hook_active=true`，防止循环，语义同现在。
- UserPromptSubmit 与 PostToolUse 仍然同步：它们的结果必须在本回合生效。

### 4. 观测与文档

- trace、`--analyze`、`--web` 显示任务种类（watch、hook）、watch 的运行次数与变化次数（交给 suag job）。
- README 中英、配置说明。

### 5. 测试

- 单测：函数任务的取消与超时、运行中通知的替换与唤醒；watch 的轮询、去抖、合并触发、归一化比较（短间隔加真实文件）。
- 伪终端冒烟（加进 `make smoke-chat`，并做反向验证）：
  - 后台 Stop hook：hook 脚本睡 2 秒后 block；期间用户输入不中断它，结果到达后开始注入回合；
  - watch：fakellm 启动监听某文件的 watch，脚本改这个文件，空闲的 chat 被通知唤醒。
- 真实模型评测：新增 `watch-tests`，模型用 watch 监听测试，修代码后从通知得知测试转绿。

### 已定（2026-10-13 用户确认按建议）

1. watch 的入口放在 `shell_exec`（复用审批与危险删除拦截）；另一种做法是 `task` 工具新增 action，但 `task` 不经审批，需要另加。
2. 文件变化用轮询（2 秒，无新依赖，跨平台行为一致）；另一种做法是引入 fsnotify（更及时，但要处理递归目录与各平台差异）。
3. 基线运行只在失败时通知；另一种做法是总通知一次。
4. 后台 Stop 默认关闭，需配置开启；新的 Stop 到来时终止旧的。
5. 比较输出前做归一化，忽略耗时与时间戳的变化。
