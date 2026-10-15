# keyprobe

把终端**实际**发给程序的按键字节打印出来（raw 模式），用来回答「某个键在这台终端上到底是什么」——比查文档可靠。起因是 `suga-nrf2`/多行输入那几轮：Ctrl+Enter、Shift+Enter、Alt+Enter 在各终端编码不同，猜错就会写出不生效的键位。

## 用法

```powershell
go run ./scripts/keyprobe
```

在目标终端里运行（Windows Terminal、Zed、conhost、Linux 终端都行），按顺序按键，每按一次打印一行：

```
stdin is a terminal: true
stdout size: 120x30 (err=<nil>)
press keys (Ctrl+C or Ctrl+D quits):
read  1 byte(s): 0a   "\n"
read  1 byte(s): 0d   "\r"
```

- `read N byte(s)` —— 一次读到几个字节（转义序列通常一次全到）；
- `% x` —— 十六进制，`%q` —— 可读形式；
- Ctrl+C / Ctrl+D 退出（raw 模式下它们就是 `03`/`04` 字节，不会杀进程）。

## 已实测记录

| 终端 | 按键 | 字节 | 结论 |
|---|---|---|---|
| Windows Terminal 1.24（scoop 安装，本机 2026-10-15） | `Ctrl+Enter` | `0a` | **与 Ctrl+J 完全相同**，所以 chat 的「裸 LF = 输入里换行」已经覆盖它，无需任何 WT 配置 |
| 同上 | `Shift+Enter` | `0d` | 与 Enter 同码，终端层面无法区分；要用得在 WT `settings.json` 里 `sendInput` 绑定 |
| 同上 | `Alt+Enter` | （收不到） | WT 默认把它绑成 `toggleFullscreen`，按键不到程序 |

相关：`docs/design/2026-10-14-windows-chat-ctrl-c-findings.md`（Windows 终端行为的调查记录）、`scripts/wincons`（在 Windows 控制台上驱动真实 chat 的 harness）。
