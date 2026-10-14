# Windows 上 chat 回合中 Ctrl-C 的排查记录（suga-nrf2）

日期：2026-10-14 · 环境：Windows 10 IoT Enterprise LTSC 2024（amd64）、Go 1.25.10、主机上的 gofer job（无交互控制台）。

用户反馈：「chat 正在调用工具时按 Esc/Ctrl+C 无反应，不会终止执行」（在 Windows 主机上）。相关设计：`docs/design/2026-10-14-steering-design.md`（S2 要在此之前定位根因，并把回合中的中止改为按键路径）。

## 结论

**在能自动化到的 Windows 环境（普通控制台）里，设计的 Ctrl-C 路径是好的**：回合进行中控制台处于 cooked（`ENABLE_PROCESSED_INPUT` 置位），Ctrl-C 变成 `CTRL_C_EVENT` → Go `signal.NotifyContext` 取消进程 ctx → 回合结束、chat 退出 130、工具进程树被结束，全流程 21–30ms。

**用户环境是 Windows Terminal（ConPTY）**，这一条路径在本 job 里无法复现（见「限制」），因此**根因未确定**。已排除若干怀疑（见下），剩下三个可能，都需要现场数据才能落定：

1. **终端把按键吞掉**：Windows Terminal 默认把 `Ctrl+C` 绑到 `copy`，有选区（含 `copyOnSelect` 自动选区）时按键根本不进伪控制台，也就不存在 `0x03`。
2. **控制台当时不是 cooked**：`0x03` 只会在 `ENABLE_PROCESSED_INPUT` 置位时变成 `CTRL_C_EVENT`；置位时清位、且回合中没人读控制台（设计如此），按键就静静躺在输入缓冲区里，直到下一次提示符读行才被 lineedit 读到。控制台输入模式是**整台控制台共享**的，任何同控制台进程都可能改掉它而不恢复。
3. **进程收不到 CTRL_C**：`SetConsoleCtrlHandler(NULL, TRUE)`（忽略 Ctrl-C）会被之后创建的子进程继承；用 `CREATE_NEW_PROCESS_GROUP` 创建的进程收不到 `CTRL_C_EVENT`（只收 CTRL_BREAK）。两者都会让 Ctrl-C 静默失效。

## 实测证据（工具：`scripts/wincons`，代码与复现命令见该目录 README）

| 场景 | 结果 |
|---|---|
| classic 控制台，键入消息 → 工具跑 `sleep 60` → Ctrl-C | 控制台模式：提示符期 raw（`0x3f0`）、回合中 cooked（`0x1f7`）；Ctrl-C 后 21ms 退出，exit 130，`tool-finished.txt` 未出现 |
| 同上但用 `suga send` 投递开始回合（提示符读仍挂着，suag-jsha 场景） | 同上，30ms 退出 |
| 同上但 `--console-output`（彩色、spinner、输入高亮全开，最接近真人终端） | 同上，21ms 退出 |
| 对照（不发 Ctrl-C） | 工具照常跑到结束，chat 不退出 |

被这两组事实排除掉的怀疑：

- **回合中没恢复 cooked**：模式采样时间线只有「raw（提示符）→ cooked（回合）」，回合中没有 raw 窗口。
- **子 shell 改了控制台模式**：`pwsh -NoLogo -NoProfile -NonInteractive -Command ...`（就是 `shell_exec` 的形态，stdin 为空设备）、`powershell.exe`、`cmd /c` 前后控制台模式都是 `0x1f7`，全程无变化。
- **工具不响应 ctx**：`shell_exec` 在 ctx 取消后 20ms 级返回，工具的第二标记文件始终没出现。
- **颜色 / spinner / 输入高亮的代码改了控制台模式**：`--console-output` 与 piped+`NO_COLOR` 结果一致；`gookit/cliui`（spinner 用 `progress`）只在 `interact/backend/readline` 里 `term.MakeRaw`，chat 不用那条路；`gookit/color` 的 `EnableVirtualTerminalProcessing` 只写 `CONOUT$`/stdout。

## ConPTY（Windows Terminal）相关的事实

- 伪控制台把写入输入管道的字节当作**键盘输入**处理，再套用同一套控制台模式规则：`ENABLE_PROCESSED_INPUT` 置位 → 交给控制处理程序（不放进输入缓冲区）；清位 → 作为普通字符给读的一端。（Console-Docs `setconsolemode` / `console-input-buffer` / `console-control-handlers`；即「回合中 cooked」这个设计方向与文档语义一致。）
- ConPTY 下 Ctrl-C 的送达历史上坏过、后来修好：microsoft/terminal#10415（“Persist inbox conhost; delegate control activities to it via a pipe”，自述里 “this is a signal the operating system rejects from a PID that is not the owner … now relayed through the original owner”）、#10647（WT 1.20 验证恢复正常）。
- WT 默认 `Ctrl+C` = `copy`：有选区时按键被终端吃掉，程序收不到 `0x03`。
- conhost 里 Ctrl-C 的**派发时机与 console lock 解锁耦合**（microsoft/terminal#2141，仍 open）：应用正忙 / 持锁时可能延迟派发——这一点与本问题的「busy 时按了没反应」形态吻合，但没有定论性证据。

## 限制（本 job 内做不到的验证）

`scripts/wincons --console pty`（ConPTY）在 gofer job 里起不来：`CreateProcess` 带 `PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE` 时子进程立刻以 `0xC0000142`（STATUS_DLL_INIT_FAILED）退出，`cmd.exe` 作为子进程也一样；job 不允许 breakaway（`CREATE_BREAKAWAY_FROM_JOB` → Access denied），无法脱离 job 再试。因此在普通终端（Windows Terminal / conhost）里跑一次 `--console pty` 或 `--pty-probe`，是坐实 ConPTY 行为的最短路径。

## 下次在现场怎么判定

1. `suga trace <session-id> --json`：看这一回合的 turn 事件状态。`cancelled` → SIGINT 到了、ctx 被取消；`completed`/还在跑 → SIGINT 根本没到（问题在终端/控制台侧，即上面 1–3 之一）。
2. 若 turn 为 `cancelled` 而工具子进程仍在跑，则是工具侧的 ctx 传播问题（当前实测 `shell_exec` 20ms 级返回，未复现）。
3. 若怀疑控制台模式被别的进程改掉，可在出问题的终端里用 `scripts/wincons --console-output` 跑一轮，看「console input mode while the tool runs」是否仍是 `PROCESSED_INPUT`。

## 与 steering 设计（S2）的关系

- S2 把回合中的 Ctrl-C 改成**按键读取**（回合中保持 raw、lineedit 返回 abort 事件），正好绕开「依赖控制台产生 `CTRL_C_EVENT`」这一环：raw 下 `0x03` 直接落在输入缓冲区，谁读谁说了算，不再受 `ENABLE_PROCESSED_INPUT`、ConPTY 派发、终端键位影响。
- 但 S2 里的 **Esc 更可靠**：WT 默认把 `Ctrl+C` 绑到 `copy`，有选区时 `0x03` 到不了程序；Esc 不是默认键位。
- 另外提醒 S2：回合中保持 raw 后，终端不再向工具子进程发 SIGINT（设计里已写明）；Windows 上 raw 与 cooked 的切换是**整台控制台**的属性，同一个控制台里的其它程序（包括工具子进程）会看到 suga 的设置。
