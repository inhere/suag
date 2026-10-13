# ptysmoke

在伪终端（PTY）里驱动真实的 `suag` 二进制做端到端冒烟，覆盖单元测试测不到的终端行为：raw/cooked 切换、提示符重绘、回合中提前输入、Ctrl-C、确认交互等。仅 Linux/macOS（依赖 Python `pty`）；Windows 控制台行为需人工确认。

| 文件 | 作用 |
|---|---|
| `fakellm.py` | OpenAI 兼容的 SSE 假模型：`--mode tool` 先返回 `shell_exec` 调用、工具结果后回复文本；`--mode text` 只回复文本；`--mode edit` 依次返回 `file_grep`、`file_edit` 调用再回复；`--mode memory` 先 `shell_exec` 再 `memory` add（用户消息含“直接”时跳过 shell）；`--delay` 控制每次响应前的等待；`--prompt-tokens` 设定上报的输入用量；`"stream": false` 的请求（如 compaction 摘要）回一个 JSON 响应；多线程服务（流式与非流式请求走不同连接） |
| `ptydrive.py` | PTY 驱动库：`mark()` 后只检查新输出、`until()` 等待模式出现再输入、`check()` 记录结果 |
| `chat_approval.py` | 现成场景：开场模型、❯ 提示符、写工具确认（y/n/a/Ctrl-C、回合中提前输入不放行）、提示符处单次 Ctrl-C 只提示、连按两次退出（汇总与续接命令）、非 TTY 拒绝 |
| `chat_edit.py` | `file_grep` 免确认、`file_edit` 确认显示原文/改为、确认后文件被修改、两次 Ctrl-C 退出 |
| `chat_idle.py` | 空闲整理：`context.idle_after: 2s` + 小 `context_window`，回合时长超过 idle_after 时回合中不触发；回合结束后出现“空闲整理中…”与“已整理上下文：X → Y token”；之后输入正常；整理进行中输入会抢占它（无成功提示、回合照常回答）；`suag trace` 可见 schedule 事件与 reason=idle 的分段 |
| `chat_observe.py` | 观测进入 trace：配置一个 hook 命令（UserPromptSubmit 注入上下文），在审批提示处停 1.2s 再答 y，退出后检查 `suag trace --json`：SessionStart / UserPromptSubmit（注入字符数）/ Stop 三个 hook 事件，shell_exec 的 `approval=user` 与等待时间，回合 context 事件计入 hook 注入 |
| `chat_memory.py` | 未污染会话直接写记忆并显示“已记住”；`shell_exec` 置污染（`memory.taint_tools`）后写记忆询问（显示范围/类型/标题/正文），y 写入、n 拒绝、a 本会话不再问；条目落在 SUAG_HOME 的 files 后端 |

运行：

```bash
make smoke-chat
# 或指定二进制与工作目录
python3 -I scripts/ptysmoke/chat_approval.py --bin tmp/smoke/suag --work tmp/smoke --keep
```

编写新场景的要点：

- 断言只看 `s.mark()` 之后的输出（`s.until(pattern, m)`、`s.contains(pattern, m)`），不要在整个缓冲里找——前面轮次的输出会让检查误判。
- 等待预期提示出现后再输入：chat 在确认询问前会清除未读输入，提前写入的回答会被丢弃。
- raw 模式下回车是 `\r`，Ctrl-C 是 `\x03`，Ctrl-U 是 `\x15`。
- 以结果行（如 `⎿ ok: ...`）判断工具是否执行，确认提示本身也会回显命令。
- 每轮结束以状态行（`tokens · `）为准再进入下一步；场景自己负责启动和停止假服务，不要依赖外部后台进程。
- 工作目录在本仓库内（有 `.gofer/tracker`），`write_home` 固定 `memory.project.backend: files`，冒烟不会调用真实 gofer。
