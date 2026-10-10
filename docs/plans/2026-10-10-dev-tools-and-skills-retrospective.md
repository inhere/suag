# Suag 开发基础工具与 skills 跑通 复盘

> 2026-10-10 · 覆盖 gofer plan `plan-20261010-092442-10a528ce`（[计划 0.2](2026-10-10-dev-tools-and-skills-plan.md) T0–T13，[设计 0.2](../design/2026-10-10-dev-tools-and-skills-design.md)），以及实施期间用户追加的 chat 两次 Ctrl+C 退出和收尾时批准的 web_fetch 代理。

## 结果

| 项 | 数据 |
|---|---|
| 计划实施 | 14 个任务；功能与修正提交 8fae3b5..de79bd0（含一个修正提交 d3b50a4），另有 80e8863（两次 Ctrl+C）、a535adc（web_fetch 代理） |
| 新增/改进工具 | `file_grep`、`file_find`、`file_edit`、`web_fetch`、`skill`；`file_read` 分段、`file_list` 递归；`shell_exec` 重写；`web_search` 改为读工具 |
| 行为 | 统一输出上限 30000 字节；未启用工具按入口给开启方法并计数；MCP 单次 120 秒超时；进程级基础系统提示词；运行状态不再重复存消息 |
| 验证 | 容器 `go test -race ./...`；主机 Windows `go test ./...`（job 20261010-095002-b0b328f3、20261010-100705-bee20219）；`make smoke-chat` 26/26 + 新场景 9/9；主机 `make install` 0.7.0-20-geb275f8 |
| 真实任务 | 经 gofer 用 opencode-go：中文 shell 输出与退出码、分段读 3000 行文件、只读开启提示（阶段一）；修 bug 小项目（shell→list→read→grep→edit→shell，12.8 秒）、`suag` agent 检索 suag 仓库、skills 列出与加载、web_fetch（阶段二）；go.dev 经本机 mihomo 代理抓取成功 |
| 数据 | 3 回合 fakellm：`state_json` 666 字节且不含消息，entries 2013 字节（改动前 state_json 约再重复一份） |

## 有效的做法

1. **先做“一份工具清单”，后面的任务都变成加一行。** T1 把 run/chat/ACP 三处注册收口到 `native.Catalog`，此后 `web_search` 改读工具、未启用工具名单（清单减已注册）、grep/find/edit/fetch 的注册都只改清单一处，三种入口自动一致；“只读/可写工具集精确等于清单”的测试守住了这一点。
2. **关键性质做了反向验证。** 去掉 `WaitDelay` 后“后台子进程占住管道”用例挂满 30 秒失败；把未知工具名也计入拒绝后“调 bash 不计数”用例失败。两条测试都证明自己真的在守性质。
3. **Windows 行为直接在主机跑测试。** PowerShell 的 UTF-8 前缀、`exit $LASTEXITCODE` 后缀、job object 整组终止，第一次在主机跑就全部通过（中文、引号、退出码 3、失败 cmdlet 退出 1、`ping` 子进程被终止）。
4. **真实任务用 gofer 驱动，并用 `suag trace` 看工具调用序列。** 不只看最终答复，而是确认模型确实走了 grep→read→edit→shell；只读回合则从 trace 看出模型根本没调工具、靠系统提示词给出了开启方法。
5. **W7 先读代码证明前提再动手。** 续跑经 `LoadRunAuthority` 用 entries 覆盖 `state.Messages`，所以 `SaveRun` 只需在拷贝上清空消息，旧数据照常可读，没有写退路分支。

## 最值钱的发现

| 发现 | 来源 | 如果漏掉会怎样 |
|---|---|---|
| 主机直连不了 go.dev，而 web_fetch 按设计忽略环境代理 | 阶段二真实任务 | 模型在主机上抓不到大部分境外文档，只会报 TLS 超时；已加 `tools.web_fetch.proxy` |
| 真实模型不会调用不在工具列表里的工具 | 阶段一只读真实回合 + trace | 以为“未启用工具拒绝”路径经过了真实验证；实际只由单元测试覆盖，主要靠系统提示词起作用 |
| 主机 `~/.agents/skills` 有 40 多个 skill，全部列入提示词 | 阶段二 skills 真实任务 | 每轮多几 KB 输入；模型对数量的说法前后不一致 |
| 提示词不得提及 gofer（既有 cli 测试），ACP 只读是会话 mode 而非启动参数 | 全量测试 | 提示与拒绝文案把用户引向不存在的 `--read-only` 参数 |
| wiring 测试会读到开发者真实的 `~/.agents/skills` | T12 编写测试时 | 工具清单与提示词断言在不同机器上结果不同 |

## 不足与教训

1. **一次带着失败测试提交了。** T6 提交前用 `go test ... | grep -v ok | head` 看结果，管道吞掉了退出码，失败的 cli 测试混在日志里没拦住提交，随后以 d3b50a4 修正。**此后提交前一律检查测试命令自身的退出码（输出写入 `tmp/` 日志再 grep）。**
2. **文案照抄设计措辞，没对照代码。** 设计写的是“gofer 侧去掉 --read-only”，实际 ACP 只读是会话 mode `read_only`；加上提示词不得提 gofer 的约束，第一版文案两处都错。**面向用户的开启方法要从实际入口代码核对后再写。**
3. **计划里的验证命令有一处没跑过。** 计划 T8 写的是 `suag run --allow-write "…"`，实际 `run` 只接受 `--prompt`，执行时才发现。上一次复盘已经提出“计划里的命令要先实际跑一遍”，这次仍然漏了。
4. **设计决策没有对照主机网络现实。** “web_fetch 忽略环境代理”在安全上是对的，但设计阶段没核实主机能否直连常用文档站，直到真实任务才暴露。
5. **两次违反本地约定。** 一次用了 `pkill -f`（只匹配到本次 fakellm 和命令自身，无其他影响），一次往系统 `/tmp` 写了临时文件（已删除）。
6. **只读工具数量的精确断言改了三次。** `readonly_test.go` 的工具数从 2 改到 4 再到 5；更稳的写法是断言“等于清单中的读工具”，T1 新增的清单测试已经这样做，旧测试可以顺手改掉。

## 遗留

- `suag` 的 gofer run 模板加 `--allow-write`（由 gofer 会话落实）；不加时只读模式已可检索、读文件、抓网页、用 skills。
- 主机 `~/.suag/config.yaml` 如需抓境外站点，可设 `tools.web_fetch.proxy: http://127.0.0.1:10801`（本机 mihomo 端口，需代理软件运行）。
- skills 列表较长：可考虑按目录限制、只列工作区 skills 或折叠描述（另立候选）。
- Windows Terminal 下的 chat（含两次 Ctrl+C 退出）只在 Linux 伪终端冒烟中验证，待用户日常使用确认（`UNVERIFIED_REAL_ENV`）。
- 设计评审 F15（MCP 调用超时后发送取消通知）可选；会话存储结构调整另立候选。
