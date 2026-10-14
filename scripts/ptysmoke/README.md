# ptysmoke

在伪终端（PTY）里驱动真实的 `suga` 二进制做端到端冒烟，覆盖单元测试测不到的终端行为：raw/cooked 切换、提示符重绘、回合中提前输入、Ctrl-C、确认交互等。仅 Linux/macOS（依赖 Python `pty`）；Windows 控制台行为需人工确认。

| 文件 | 作用 |
|---|---|
| `fakellm.py` | OpenAI 兼容的 SSE 假模型：`--mode tool` 先返回 `shell_exec` 调用、工具结果后回复文本；`--mode text` 只回复文本；`--mode edit` 依次返回 `file_grep`、`file_edit` 调用再回复；`--mode memory` 先 `shell_exec` 再 `memory` add（用户消息含“直接”时跳过 shell）；`--mode adddir` 依次返回 `file_read`（`--read-path`）、`file_write`（`--write-path`、`--write-body`）调用再回复；`--delay` 控制每次响应前的等待；`--prompt-tokens` 设定上报的输入用量；`"stream": false` 的请求（如 compaction 摘要）回一个 JSON 响应；多线程服务（流式与非流式请求走不同连接）；`--background` 让 tool 模式的 shell_exec 带 `background: true`（收到 `[background task` 开头的通知或提醒只回文本）；`--fail-first N` 前 N 个请求回 HTTP 503，`--break-first M` 随后 M 个流只发一半回复就断开连接（重试冒烟用）；`--fail-from N [--fail-count M]` 从第 N 个请求起失败 M 次（M<=0 表示此后一直失败，中断回合冒烟用）。每个请求在 stdout 打一行 JSON：`user` 是最后一条 user 消息，`seen` 是请求里每条消息的简短标记（`user:<前 40 字符>`、`a:file_write`、`tool:edited …`），让场景能断言模型实际收到的消息与顺序 |
| `ptydrive.py` | PTY 驱动库：`mark()` 后只检查新输出、`until()` 等待模式出现再输入、`drain()` 把新输出读进缓冲供 `contains()` 判断、`check()` 记录结果、`resize(cols, rows)` 改窗口大小（发 SIGWINCH）|
| `chat_approval.py` | 现成场景：开场模型、❯ 提示符、写工具确认（y/n/a/Ctrl-C、回合中提前输入不放行）、提示符处单次 Ctrl-C 只提示、连按两次退出（汇总与续接命令）、非 TTY 拒绝；窗口设为 100 列，并检查输入行按终端宽度折行（90 个字符仍在同一行；去掉 `SetSize` 调用时该检查会失败），以及运行中把窗口改到 60 列后编辑器按新宽度排版（用宽字符在行尾的填充空格判断；去掉 resize 监听时该检查会失败）|
| `chat_edit.py` | `file_grep` 免确认、`file_edit` 确认显示原文/改为、确认后文件被修改、两次 Ctrl-C 退出 |
| `chat_idle.py` | 空闲整理：`context.idle_after: 2s` + 小 `context_window`，回合时长超过 idle_after 时回合中不触发；回合结束后出现“✦ 空闲整理中…”与“✦ 已整理上下文：X → Y token”；之后输入正常；整理进行中输入会抢占它（无成功提示、回合照常回答）；`suga trace` 可见 schedule 事件与 reason=idle 的分段 |
| `chat_observe.py` | 观测进入 trace：配置一个 hook 命令（UserPromptSubmit 注入上下文），在审批提示处停 1.2s 再答 y，退出后检查 `suga trace --json`：SessionStart / UserPromptSubmit（注入字符数）/ Stop 三个 hook 事件，shell_exec 的 `approval=user` 与等待时间，回合 context 事件计入 hook 注入 |
| `chat_retry.py` | 模型重试：`retry` 配成 200ms 级等待，假模型先回 503、再断一次流、再完整回复；检查 chat 显示 503 重试提示与“将重新输出”的断流提示、之后完整回复，`suga trace --json` 的 model 事件 `retries=2`、`retry_reason=stream broken` 与等待时间 |
| `chat_interrupt.py` | 中断回合的回放（suga-y0sk）：假模型 `--fail-from 3 --fail-count 3`＋`retry.max_attempts: 3`，回合先跑 `file_grep`、再 `file_edit`（target.txt 已改动），随后应答请求连续 503 到回合 failed；检查 chat 显示重试与 `Error: … request failed after 3 attempts`、磁盘上的改动确实在；再发一句后，模型请求（fakellm 的 `seen` 摘要）里重新带有那次 `file_grep`/`file_edit` 调用与结果、顺序正确、并带中断小结，`suga trace` 有 failed/后续 completed 两个回合、`suga export` 里有 `suga.turn_interrupted` 条目。反向验证见脚本 docstring |
| `chat_danger.py` | 危险删除：`tools.approval: auto`、HOME 指向临时目录，假模型调用 `rm -rf ~`；检查仍会询问并显示危险原因、答 `y` 被拒且临时目录还在、答 `yes` 才执行且目录被删，trace 两条 shell_exec 事件带 `danger` 与审批来源 |
| `chat_tasks.py` | 后台任务：假模型以 `background: true` 启动 `sleep 1.5; echo TASK_DONE` 后直接回复；检查回合先结束、任务结束后空闲的 chat 被自动唤醒并显示通知（退出码与输出），trace 有带 `task_id` 的 shell_exec 与 `task` 事件；再用 `suga run` 启动 `sleep 30`：提醒一次（两个回合）、退出时终止任务并在 stderr 注明、不等满 30s |
| `chat_watch.py` | watch 与后台 Stop hook：假模型启动监听 `a.txt` 的 watch，脚本把文件改到命令失败，空闲 chat 被通知唤醒（`exit 0 -> 3`），再写入相同内容只运行不通知，trace 的任务事件有 runs/changes；`hooks.stop_background` 下 hook 睡 3 秒后 block，第一轮后立即输入第二句：hook 从不收到 Interrupt，第一个 Stop 任务被第二个终止，block 以通知唤醒 chat，随后的 Stop 带 stop_hook_active。`--no-stop-background` 用于反向验证 |
| `chat_agent.py` | 子 agent：假模型调用 `agent` 工具，子 agent（按系统提示词识别）grep 一次后交报告。同步：agent 调用下显示 `  · file_grep` 进度行、报告作为结果、trace 的 agent 事件带子会话 id 与 stop=final、`suga sessions` 隐藏子会话而 `--agents` 列出；Ctrl-C 打断慢的子 agent 时被中断的回合显示“已中断本轮”（不出现原样的 `context canceled`）、chat 正常退出且子会话不停在 running；后台：立即返回任务 id，子 agent 结束后以报告通知唤醒空闲 chat。`--agents-off` 用于反向验证 |
| `chat_memory.py` | 未污染会话直接写记忆并显示“已记住”；`shell_exec` 置污染（`memory.taint_tools`）后写记忆询问（显示范围/类型/标题/正文），y 写入、n 拒绝、a 本会话不再问；条目落在 SUGA_HOME 的 files 后端 |
| `chat_spinner.py` | “思考中…”等待行：本场景显式不带 `NO_COLOR`（该行只在有色终端显示），假模型 `--delay 2` 让提示可见；检查首个回合前不显示、等待模型首个消息时出现（带已用秒数）、工具调用行替换它、工具结果后再次出现、答案前最后一条提示已被 `ESC[2K` 清除（看原始缓冲，`text()` 会剥掉 ANSI）；同时还覆盖终端标题（`chat.terminal_title`）：启动时先 `ESC[22;0t` 保存再把标题设为 `suga · <短名称>`，首条消息后标题带上会话标题，Ctrl-D 退出时标题清空再 `ESC[23;0t` 恢复；另一个 `chat.spinner: false` / `chat.highlight_input: false` / `chat.terminal_title: false` 的会话完全不显示该行、不加背景、不写标题序列 |
| `chat_steer.py` | 回合中插话与“只中止本轮”（`chat.steering`）：工具运行中输入并回车，插话送达**同一回合**（假模型请求日志里插话紧跟工具结果之后、trace 有 `steer_delivered`、本回合只有一个 turn 事件、状态行显示插话次数）；模型已给出最终回复时到达的插话成为紧接着的下一回合（trace `steer_returned` 的 `completed_untaken`/`next_turn`）；Esc 与单次 Ctrl-C 只中止本轮（显示“已中断本轮，N 条插话已放回输入行”、chat 仍在、插话回到输入行且回车才运行），两次 Ctrl-C 退出；审批提问期间键入的是答案而不是插话；`chat.steering: false` 回到旧行为。反向验证见脚本 docstring |
| `chat_tool_ctrlc.py` | 前台工具调用中按 Ctrl-C / Esc（suga-nrf2）：假模型 `--command` 先写 `tool-started` 再 `sleep 60`。一次运行两组：默认（`chat.steering: true`）检查单次 Ctrl-C 与 Esc 各只中断本轮且 chat 留在提示符、`已中断本轮`、命令的第二标记 `tool-finished` 从未出现、确认窗口内两次 Ctrl-C 退出；`chat.steering: false` 检查按 `\x03` 后回合被中断（“已中断”）、chat 退出并打印汇总、进程结束。Windows 对应物是 `scripts/wincons`（Windows 控制台无法用 Python 驱动） |
| `chat_resume.py` | 续接被强杀的会话：假模型（text 模式，回复 51 行）先答一轮，第二轮在模型回复前 SIGKILL 掉 chat（会话停在 running）；再用 `--resume <id>` 与 `--session-id <id>`（gofer 走的路径）续接，检查首个 ❯ 提示符之前出现淡色“上一次回复：”、正文最后 40 行（保留 `▣` 标记）、头部被截断并提示 `已截断前 N 行，完整记录见 suga trace <id>`、被杀回合显示“上一回合未正常结束”；两次续接都不调用模型（假模型请求数不变、trace 里 model/turn 事件仍各 1 条）。随后 `--session-id` 会话发一条消息：先显示“已恢复会话”再照常回答（会话从 running 恢复为 open），再把自己的下一轮杀掉留成 running，用 `suga run --resume` 走另一条入口（Run 自己取锁）：stderr 显示恢复提示、stdout 是回答；`suga trace --json` 有两条 `session recovered` 事件（带被杀回合 id 及 `previous_status=running`、`turn_status=cancelled`） |
| `chat_shortname.py` | 会话短名称：工作目录名 `Smoke_WS` → 横幅显示 `smokews-xy`（小写、清洗过），`suga ps` 与 `--json` 都列出该名称，`suga send --session <短名称>` 投递成功并开启一轮，`:session` 同时显示 id 与名称，退出摘要的续接命令后带名称；首条消息正文不再作为名字（`--session 第一句话` 退出码 3，且进程记录仍是短名称）；`--resume` 续接沿用同一名称，chat 退出后该地址不再可达（退出码 3） |
| `chat_adddir.py` | 会话级附加目录（`--add-dir`）：启动 `suga chat --add-dir <工作区外目录> --add-dir <共享目录>:rw`，假模型先 `file_read` 工作区外文件（只读目录生效）、再 `file_write` 写进 `:rw` 目录（`tools.approval: auto`，不询问），检查工具结果、磁盘上的文件，以及 `suga trace --json` 的 `turn_context` 事件 `add_dirs` 为 `["<目录>:ro", "<目录>:rw"]`；`--keep` 保留工作目录便于反向验证（去掉附加目录注入后读/写在 chat 里应失败） |
| `chat_adddir_cmds.py` | 会话中途改授权目录：不带 `--add-dir` 起 chat，模型读工作区外文件时出现 `授权访问 <目录>？[r] 只读 [w] 读写 [n] 拒绝`（答 `r` 后同一步就读到内容），`:dirs` 标出 `越界授权`，`:add-dir <目录>:rw` 后下一次 `file_write` 成功，`:rm-dir` 后同一写入又被拒（磁盘上无文件），`:add-dir docs` 只报错+用法；`suga trace --json` 有 3 条 `add_dir` 事件（路径/权限/来源：ask 与 keyboard），`turn_context.add_dirs` 逐回合跟上授权变化。第二个 chat（text 模式）里 `suga send` 投递 `:add-dir <目录>` 只当普通消息（显示 `[来自 cli] …`、模型作答、`:dirs` 里没有它），证明投递文本改不了授权；反向验证见脚本 docstring |
| `chat_paste.py` | 多行粘贴（suga-b1md）：`text` 模式的假模型；检查行编辑期间终端收到 `ESC[?2004h`；`ESC[200~第一行\n第二行ESC[201~` 后两行都出现在输入行且**不**开始回合（假模型没被调用），回车后模型收到的**一条** user 消息正是 `第一行\n第二行`（`fakellm.py` 每个请求的 stdout JSON 带上 `user`）；CR 换行的粘贴同样；随后手打回车仍照常提交一行。反向验证：去掉粘贴处理（lineedit 里粘贴内换行又变成提交）后三条检查都失败 |

运行：

```bash
make smoke-chat
# 或指定二进制与工作目录
python3 -I scripts/ptysmoke/chat_approval.py --bin tmp/smoke/suga --work tmp/smoke --keep
```

编写新场景的要点：

- 断言只看 `s.mark()` 之后的输出（`s.until(pattern, m)`、`s.contains(pattern, m)`），不要在整个缓冲里找——前面轮次的输出会让检查误判。
- 等待预期提示出现后再输入：chat 在确认询问前会清除未读输入，提前写入的回答会被丢弃。
- raw 模式下回车是 `\r`，Ctrl-C 是 `\x03`，Ctrl-U 是 `\x15`。
- 以结果行（如 `⎿ ok: ...`）判断工具是否执行，确认提示本身也会回显命令。
- 每轮结束以状态行（`tokens · `）为准再进入下一步；场景自己负责启动和停止假服务，不要依赖外部后台进程。
- 工作目录在本仓库内（有 `.gofer/tracker`），`write_home` 固定 `memory.project.backend: files`，冒烟不会调用真实 gofer。
