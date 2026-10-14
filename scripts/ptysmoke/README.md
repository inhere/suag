# ptysmoke

在伪终端（PTY）里驱动真实的 `suga` 二进制做端到端冒烟，覆盖单元测试测不到的终端行为：raw/cooked 切换、提示符重绘、回合中提前输入、Ctrl-C、确认交互等。仅 Linux/macOS（依赖 Python `pty`）；Windows 控制台行为需人工确认。

| 文件 | 作用 |
|---|---|
| `fakellm.py` | OpenAI 兼容的 SSE 假模型：`--mode tool` 先返回 `shell_exec` 调用、工具结果后回复文本；`--mode text` 只回复文本；`--mode edit` 依次返回 `file_grep`、`file_edit` 调用再回复；`--mode memory` 先 `shell_exec` 再 `memory` add（用户消息含“直接”时跳过 shell）；`--delay` 控制每次响应前的等待；`--prompt-tokens` 设定上报的输入用量；`"stream": false` 的请求（如 compaction 摘要）回一个 JSON 响应；多线程服务（流式与非流式请求走不同连接）；`--background` 让 tool 模式的 shell_exec 带 `background: true`（收到 `[background task` 开头的通知或提醒只回文本）；`--fail-first N` 前 N 个请求回 HTTP 503，`--break-first M` 随后 M 个流只发一半回复就断开连接（重试冒烟用） |
| `ptydrive.py` | PTY 驱动库：`mark()` 后只检查新输出、`until()` 等待模式出现再输入、`check()` 记录结果 |
| `chat_approval.py` | 现成场景：开场模型、❯ 提示符、写工具确认（y/n/a/Ctrl-C、回合中提前输入不放行）、提示符处单次 Ctrl-C 只提示、连按两次退出（汇总与续接命令）、非 TTY 拒绝 |
| `chat_edit.py` | `file_grep` 免确认、`file_edit` 确认显示原文/改为、确认后文件被修改、两次 Ctrl-C 退出 |
| `chat_idle.py` | 空闲整理：`context.idle_after: 2s` + 小 `context_window`，回合时长超过 idle_after 时回合中不触发；回合结束后出现“空闲整理中…”与“已整理上下文：X → Y token”；之后输入正常；整理进行中输入会抢占它（无成功提示、回合照常回答）；`suga trace` 可见 schedule 事件与 reason=idle 的分段 |
| `chat_observe.py` | 观测进入 trace：配置一个 hook 命令（UserPromptSubmit 注入上下文），在审批提示处停 1.2s 再答 y，退出后检查 `suga trace --json`：SessionStart / UserPromptSubmit（注入字符数）/ Stop 三个 hook 事件，shell_exec 的 `approval=user` 与等待时间，回合 context 事件计入 hook 注入 |
| `chat_retry.py` | 模型重试：`retry` 配成 200ms 级等待，假模型先回 503、再断一次流、再完整回复；检查 chat 显示 503 重试提示与“将重新输出”的断流提示、之后完整回复，`suga trace --json` 的 model 事件 `retries=2`、`retry_reason=stream broken` 与等待时间 |
| `chat_danger.py` | 危险删除：`tools.approval: auto`、HOME 指向临时目录，假模型调用 `rm -rf ~`；检查仍会询问并显示危险原因、答 `y` 被拒且临时目录还在、答 `yes` 才执行且目录被删，trace 两条 shell_exec 事件带 `danger` 与审批来源 |
| `chat_tasks.py` | 后台任务：假模型以 `background: true` 启动 `sleep 1.5; echo TASK_DONE` 后直接回复；检查回合先结束、任务结束后空闲的 chat 被自动唤醒并显示通知（退出码与输出），trace 有带 `task_id` 的 shell_exec 与 `task` 事件；再用 `suga run` 启动 `sleep 30`：提醒一次（两个回合）、退出时终止任务并在 stderr 注明、不等满 30s |
| `chat_watch.py` | watch 与后台 Stop hook：假模型启动监听 `a.txt` 的 watch，脚本把文件改到命令失败，空闲 chat 被通知唤醒（`exit 0 -> 3`），再写入相同内容只运行不通知，trace 的任务事件有 runs/changes；`hooks.stop_background` 下 hook 睡 3 秒后 block，第一轮后立即输入第二句：hook 从不收到 Interrupt，第一个 Stop 任务被第二个终止，block 以通知唤醒 chat，随后的 Stop 带 stop_hook_active。`--no-stop-background` 用于反向验证 |
| `chat_agent.py` | 子 agent：假模型调用 `agent` 工具，子 agent（按系统提示词识别）grep 一次后交报告。同步：agent 调用下显示 `  · file_grep` 进度行、报告作为结果、trace 的 agent 事件带子会话 id 与 stop=final、`suga sessions` 隐藏子会话而 `--agents` 列出；Ctrl-C 打断慢的子 agent 时 chat 正常退出且子会话不停在 running；后台：立即返回任务 id，子 agent 结束后以报告通知唤醒空闲 chat。`--agents-off` 用于反向验证 |
| `chat_memory.py` | 未污染会话直接写记忆并显示“已记住”；`shell_exec` 置污染（`memory.taint_tools`）后写记忆询问（显示范围/类型/标题/正文），y 写入、n 拒绝、a 本会话不再问；条目落在 SUGA_HOME 的 files 后端 |
| `chat_spinner.py` | “思考中…”等待行：本场景显式不带 `NO_COLOR`（该行只在有色终端显示），假模型 `--delay 2` 让提示可见；检查首个回合前不显示、等待模型首个消息时出现（带已用秒数）、工具调用行替换它、工具结果后再次出现、答案前最后一条提示已被 `ESC[2K` 清除（看原始缓冲，`text()` 会剥掉 ANSI）；另一个 `chat.spinner: false` 的会话完全不显示该行 |
| `chat_resume.py` | 续接会话显示上一次回复：假模型（text 模式，回复 51 行）先答一轮，第二轮在模型回复前 SIGKILL 掉 chat（会话停在 running）；再用 `--resume <id>` 与 `--session-id <id>`（gofer 走的路径）续接，检查首个 ❯ 提示符之前出现淡色“上一次回复：”、正文最后 40 行（保留 `▣` 标记）、头部被截断并提示 `已截断前 N 行，完整记录见 suga trace <id>`、被杀回合显示“上一回合未正常结束”；两次续接都不调用模型（假模型请求数不变、trace 里 model/turn 事件仍各 1 条） |

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
