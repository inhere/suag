# ptysmoke

在伪终端（PTY）里驱动真实的 `suag` 二进制做端到端冒烟，覆盖单元测试测不到的终端行为：raw/cooked 切换、提示符重绘、回合中提前输入、Ctrl-C、确认交互等。仅 Linux/macOS（依赖 Python `pty`）；Windows 控制台行为需人工确认。

| 文件 | 作用 |
|---|---|
| `fakellm.py` | OpenAI 兼容的 SSE 假模型：`--mode tool` 先返回 `shell_exec` 调用、工具结果后回复文本；`--mode text` 只回复文本；`--mode edit` 依次返回 `file_grep`、`file_edit` 调用再回复；`--delay` 控制每次响应前的等待 |
| `ptydrive.py` | PTY 驱动库：`mark()` 后只检查新输出、`until()` 等待模式出现再输入、`check()` 记录结果 |
| `chat_approval.py` | 现成场景：开场模型、❯ 提示符、写工具确认（y/n/a/Ctrl-C、回合中提前输入不放行）、提示符处单次 Ctrl-C 只提示、连按两次退出（汇总与续接命令）、非 TTY 拒绝 |
| `chat_edit.py` | `file_grep` 免确认、`file_edit` 确认显示原文/改为、确认后文件被修改、两次 Ctrl-C 退出 |

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
