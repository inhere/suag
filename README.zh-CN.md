# Suag

[![Unit-Tests](https://github.com/inhere/suag/actions/workflows/go.yml/badge.svg)](https://github.com/inhere/suag/actions/workflows/go.yml)

[English](README.md) | 简体中文

Suag 是用 Go 写的个人编码 Agent。同一个持久会话引擎提供两种入口：终端 CLI（交互式 `chat` 和单轮 `run`）和 ACP stdio 服务（`acp`）。[Gofer](https://github.com/inhere/gofer) 可以拉起这两种入口，并在其上提供作业、worker、日志和交互式审批。

- **开发工具**：按行分段读文件、列目录、搜索内容与文件名、按字符串精确修改、执行 shell 命令（Windows 上用 PowerShell）、抓取网页、网页搜索。
- **默认安全**：不开启就是只读；chat 写文件、执行命令前会先询问；文件工具限制在工作区内；网页抓取拒绝内网地址。
- **Skills**：读取工作区和 `~/.agents/skills` 下标准的 `SKILL.md`，按需列出或检索。
- **持久会话**：单个 SQLite 库，支持续接、自动压缩、计划、会话交接，以及用于 trace 和统计的事件记录。
- **MCP**：接入 stdio MCP 服务，支持放行名单，工具多时延迟加载。
- **持久记忆**：跨会话的用户与项目记忆，以 `MEMORY.md` 等文件为事实源，后端可换（files、gofer 或外部命令）。
- **历史检索**：全文检索过往会话，并可分页读取已保存的工具输出。
- **大输出外置**：超过阈值的工具结果存到会话目录，上下文只保留头尾与 ref。

## 安装

需要 Go 1.26 及以上。仓库是私有的，请从源码构建：

```bash
git clone https://github.com/inhere/suag.git && cd suag
make install            # 或：go install ./cmd/suag
```

`make install` 会构建（装了 `upx` 时顺带压缩）并安装到 `$(go env GOPATH)/bin`。在 Windows 上，如果旧的二进制还被打开着的 chat 占用，它会先把旧文件改名挪开再安装。配置好仓库访问权限（`GOPRIVATE=github.com/inhere/*`）后，也可以直接 `go install github.com/inhere/suag/cmd/suag@latest`。推送 `v*` 标签会触发 release 工作流，自动附上 Linux、macOS、Windows 的预编译包。确认安装目录在终端（以及 Gofer server，如果用到的话）的 `PATH` 中，然后运行 `suag version` 检查。

## 快速开始

创建 `~/.suag/config.yaml`（全部字段见 [config/config.yaml](config/config.yaml)）：

```yaml
model: deepseek/deepseek-chat   # <provider>/<model>
# openai、anthropic、deepseek 是内置 provider，分别读取
# OPENAI_API_KEY / ANTHROPIC_API_KEY / DEEPSEEK_API_KEY
tools:
  allow_write: true             # 允许 chat 写文件、执行命令（执行前会询问）
```

```bash
suag chat                                         # 在当前目录开启交互式对话
suag run --prompt "概括一下这个项目"               # 单轮，只读
suag run --allow-write --prompt "修好失败的测试"
suag run --resume <session-id> --prompt "继续"
```

chat 中：`:session` 显示会话 ID，`:compact` 开启新段（见[会话](#会话)），`:quit` 退出。在提示符处按一次 Ctrl+C 清掉已输入的内容，3 秒内再按一次才退出；Ctrl+D 直接退出。退出时会显示 token 用量和续接本会话的命令。

## 配置

Suag 只读取 `$SUAG_HOME/config.yaml` 和 `$SUAG_HOME/.env`（默认 `~/.suag`）。工作目录下的配置和 `.env` 属于你的项目，不会被读取。`SUAG_CONFIG` 可以指定唯一的配置文件。

| 配置项 | 含义 |
|---|---|
| `model` | 主模型，`<provider>/<model>`；可被 `SUAG_MODEL` 覆盖，再被 `--model` 覆盖 |
| `providers.<id>` | `type`（openai 即 chat/completions、openai-responses 即 Responses API、anthropic）、`compat`（网关适配，见 docs/usage/llm-config-v0.md）、`base_url`、`api_key`、`headers`、`models.<model>.context_window` |
| `providers.<id>.models.<model>.price` | 每百万 token 的美元价格（`input`、`output`、`cache_read`、`cache_write`）；provider 不返回成本时，`suag stats` 据此计算 |
| `providers.<id>.thinking`、`providers.<id>.models.<model>.thinking` | 思考档位 `off\|minimal\|low\|medium\|high\|xhigh\|max\|auto`，`openai` 类型作为 `reasoning_effort` 发送，`anthropic` 类型作为 extended thinking 发送（adaptive + `output_config.effort`，Claude 4.5 及更早用 `budget_tokens`）；不设、`auto` 或 `off` 不发送；`run`/`chat --thinking` 可覆盖主模型的档位 |
| `providers.<id>.context_window` | 该 provider 所有模型的默认上下文窗口（模型级 `models.<model>.context_window` 优先；都没有时按模型名推断） |
| `providers.<id>.models.<model>.model_id` | 请求里发送的模型 id（与键不同时），如键 `deepseek-v4.1-flash`、`model_id: deepseek/deepseek-v4.1-flash`；其余地方都用键 |
| `providers.<id>.models.<model>.type`、`base_url` | 该模型自己的协议与地址，覆盖 provider 的；如网关的 Claude 模型写 `type: anthropic`，其余模型仍走 OpenAI 兼容 |
| `compact_model`、`fallback_models` | 生成摘要用的便宜模型；主模型失败时依次尝试的备用模型 |
| `retry.max_attempts`、`retry.base_delay`、`retry.max_delay`、`retry.reset_after` | 模型请求因网络错误、HTTP 408/429/5xx 或流中断后的重试（所有 provider）：含首次在内的尝试次数（默认 6，`1` 关闭重试）；等待从 `base_delay`（`1s`）起逐次翻倍，最多 `max_delay`（`60s`），另有 ±20% 抖动；服务端返回的 `Retry-After` 会被遵守；距上次失败超过 `reset_after`（`2m`）后等待回落到 `base_delay` |
| `context.fold_every`、`context.fold_keep` | 历史折叠（默认关闭；`fold_every: 10`、`fold_keep: 5` 开启）：较早步骤发送时去掉思考内容，过长的字符串参数换成一句说明。只改发给模型的请求，存储的历史保持完整 |
| `context.idle_after`、`context.idle_min_ratio` | 仅 chat：安静超过此时长（默认 `15m`，`0` 关闭），且上下文已达分段阈值的此比例（默认 `0.5`）时，在后台开启新段 |
| `rules.file` | 每一级目录读取的项目规则文件名（每级只读一个，从 Git 根或 cwd 到 cwd），默认 `AGENTS.md`，可改成 `CLAUDE.md`；只接受文件名，不能带路径。`~/.suag/AGENTS.md` 仍会读取 |
| `providers.<id>.prompt_cache` | 仅 Claude（`anthropic` 类型），默认关：加 `cache_control` 断点（system 变为块数组）；部分网关不接受 |
| `tools.allow_write` | 仅 chat：启用写文件和 shell 工具（默认 false） |
| `agents.enabled`、`agents.model`、`agents.max_steps`、`agents.max_tokens`、`agents.timeout` | `agent` 工具（见[子 agent](#子-agent)）：是否注册（默认 true）、子 agent 使用的模型（默认主模型）、步数上限（30）、token 上限（400000，按未命中缓存的输入加输出计）、软时间上限（10m） |
| `tasks.max_running`、`tasks.max_duration`、`tasks.max_output_bytes`、`tasks.wake_on_exit` | 后台任务：每个会话同时在跑的数量（4）、单次最长运行时间（`2h`）、输出文件上限（64 MiB）、任务结束时是否唤醒空闲的 chat（true；运行中的任务发来通知时同样唤醒，见监听任务） |
| `tools.approval` | 仅 chat：`ask`（默认）执行写/shell 工具前询问，`auto` 不询问 |
| `tools.max_output_bytes`、`tools.max_spill_bytes` | 内联阈值：单次工具结果完整写入会话的上限（默认 8000，超出时原文存 `$SUAG_HOME/sessions/<id>/outputs/`，会话只留 ref 与头尾片段）；单次工具原始输出上限（默认 1 MiB） |
| `tools.shell`、`tools.shell_timeout_max` | 指定 shell（默认 Windows 用 pwsh/powershell，其他系统用 sh）；单条命令最长秒数（600） |
| `tools.dependency_roots`、`tools.read_roots` | 工作区外的只读目录，`file_read`/`file_list`/`file_grep`/`file_find` 可用绝对路径读取：自动识别的 Go 模块缓存、Go 标准库源码与 Rust crate 源码（默认 `true`，`false` 关闭识别），以及额外目录列表（支持 `~`）。写入仍只限工作区；这些目录会列在提示词的环境一节 |
| `tools.web_fetch.proxy`、`tools.web_fetch.allow_private` | `web_fetch` 使用的代理；允许访问内网地址 |
| `tools.auto_allow`、`tools.read_only_allow` | MCP 放行名单，条目形如 `server/工具glob` |
| `tools.deferred` | MCP 工具 schema 是否延迟加载：`auto`（默认）、`always`、`never` |
| `mcp.servers`、`mcp.call_timeout_sec` | 默认 MCP 服务；单次 MCP 调用超时（120） |
| `skills.list_max`、`skills.always_load` | 带说明列出的 skills 数量（15；超过后只列工作区 skills，其余只列名字）；预加载正文的 skills |
| `hooks.*`、`hooks.stop_background` | chat 的 hook 命令（SessionStart、UserPromptSubmit、PostToolUse、Stop 等）；`hooks.stop_background` 让 Stop hook 作为后台任务运行，不再在提示符处等待 |
| `memory.write` | `auto`（默认）允许 `memory` 工具写入；`off` 时只读 |
| `memory.write_tainted` | 读过外部内容的会话写记忆时的处理：`ask`（默认）、`deny`、`allow` |
| `memory.user.backend`、`memory.project.backend` | `files`、`gofer`（仅项目）或 `exec`。不设时：用户记忆用 files，项目记忆自动选择（见[记忆](#记忆)） |
| `memory.<scope>.command`、`.args`、`.timeout_sec` | 该范围后端为 `exec` 时要运行的外部命令（此时必填） |
| `memory.taint_tools`、`memory.taint_on_spill` | 额外会让会话变为“已污染”的工具：每次结果都算，或仅在输出被存成文件时才算（默认 `[shell_exec]`） |
| `serpapi_key` | 配置后启用 `web_search` |

所有数据都在 `$SUAG_HOME/db/sessions.db`。模型与 provider 的细节见 [LLM 配置](docs/usage/llm-config-v0.md)。

## 工具与权限

| 工具 | 类型 | 作用 |
|---|---|---|
| `file_read` | 读 | 带行号分段读取（`offset`/`limit`，默认 2000 行）；`paths` 一次读最多 10 个文件；二进制文件只报告不输出 |
| `file_list` | 读 | 列目录，`depth`（≤ 5）可递归 |
| `file_grep` | 读 | RE2 正则搜索，支持 `glob`、`ignore_case`、`output: content\|files` |
| `file_find` | 读 | 支持 `**` 的 glob 查找，按修改时间从新到旧 |
| `web_fetch` | 读 | 抓取网页正文（保留标题、链接、代码块）；拒绝内网和本机地址 |
| `web_search` | 读 | SerpAPI 搜索（需 `serpapi_key`） |
| `skill` | 读 | 加载 skill、读取其文件，或检索 skills（`query`；`"*"` 列出全部名字） |
| `session_search` | 读 | 全文检索过往消息、工具结果与摘要（`scope`：workspace、current、all）；带 `ref=out:...` 时在那份已保存的输出内检索 |
| `recall` | 读 | 按 ref 读取命中附近内容：`<session>/<seq>` 起的消息，或已保存输出 `out:<session>:<hex>` 的带行号文本 |
| `agent` | 读 | 把一件独立的任务交给子 agent：它在自己的上下文里大量读取，只把报告带回来（`description`、`prompt`、`type`：`explore` 或 `research`、`background`）；见[子 agent](#子-agent) |
| `suag_manifest` | 读 | 以 JSON 返回本实例的能力清单（与 `suag manifest` 相同） |
| `memory` | 状态 | 对长期记忆 `add`、`replace`、`remove`、`get`、`search`（`scope` 为 project 或 user），见[记忆](#记忆) |
| `file_write` | 写 | 整文件写入 |
| `file_edit` | 写 | 替换一处唯一的字符串（`replace_all` 替换全部），或用 `edits` 一次替换多处（全部成功或全部不改）；保留 CRLF |
| `file_remove` | 写 | 删除文件，或带 `recursive` 删除目录：移入 `$SUAG_HOME/trash/` 保留 7 天（`origin.json` 记录原路径），单次最多 1000 个文件。被 `.gitignore` 忽略的目录（以及 `node_modules`、`vendor`、`dist`），如被忽略的 `tmp/` 下的测试仓库，视为可再生：不遍历、不计数、直接永久删除，并在结果中列出；被忽略的单个文件（如 `.env`）仍进回收区。拒绝工作区根、工作区自己的 `.git`、用户主目录、通配符与工作区外路径 |
| `shell_exec` | 写 | 执行命令（Windows 用 PowerShell，其他系统用 `sh`），带超时；返回输出与退出码。`run` 与 chat 里传 `background: true` 会把它作为后台任务启动，立即返回任务 id |
| `task` | 状态 | 本会话的后台任务：`list`、`output`（末尾若干行，可带 `offset`/`limit`、`grep`）、`wait`（可带 `timeout`）、`kill`（整个进程树） |

`file_grep`、`file_find` 与递归的 `file_list` 跳过 `.git`、`node_modules`、`vendor`、`dist` 和 `.gitignore` 匹配的路径（各级目录的 `.gitignore`，支持 `!` 与 `**`）；结果末尾一行注明跳过了什么，传 `no_ignore: true` 可包含它们（`.git` 除外）。

- **默认只读**：写工具只在 `run --allow-write`、chat 设置 `tools.allow_write: true`、或 ACP 会话处于 `default` 模式时才有；`--read-only` 永远优先。模型调用了未开启的工具时，会被告知开启方法，并计入拒绝次数。
- **chat 先询问**：`tools.approval: ask` 时，每次写文件或执行命令前都会询问，并显示完整命令、路径或改动前后的文本。回答 `y` 允许一次，`a` 在本进程内对该工具始终允许，其他任何回答都是拒绝。一轮里被拒 3 次就结束这一轮。
- **危险删除**：递归删除的 `shell_exec` 命令（`rm -r`、`Remove-Item -Recurse`、`rd /s`，以及 `sh -c`、`cmd /c`、`pwsh -Command` 里的同类命令）在审批前就会被拦下：目标是 `/`、`~` 或 `$HOME`、盘符根、工作区或其上级目录，或路径以变量开头、变量为空时会落到根目录（`"$X/"`、`"$dir\*"`）时；对这些目录用通配符（`rm ~/*`）时没有 `-r` 也算。chat 每次都询问，`tools.approval: auto` 或之前回答过的 `a` 也不例外，只有手动输入 `yes` 才执行；`run` 和 ACP 直接拒绝，并提示模型改用 `file_remove`。trace 里记下原因（`danger=…`）。这能挡住常见的误操作，挡不住刻意构造的命令：它不是安全边界。
- **后台任务**：`shell_exec` 传 `background: true`（`run`、chat 与 ACP 的写模式）后，长时间构建、完整测试、服务或监听进程可以在模型继续工作的同时运行。输出逐行脱敏写入会话的 outputs（`recall` 也能读它的 ref）。任务在模型看到之前就结束时，会留下一条提示，在下一次模型请求前加入（退出码、耗时、大小、末尾几行）；正等待输入的 chat 会被它唤醒并开始一轮（`tasks.wake_on_exit`，默认 true，运行中的任务发来通知时同样唤醒）。`run` 在回答后仍有任务在跑时提醒模型一次，退出时终止它们并在 stderr 中列出名字。ACP 下任务属于 `suag acp` 进程：跨 prompt 继续运行，结束提示进入该会话的下一个 prompt；`session/cancel` 不影响它们，连接关闭时终止。上限：`tasks.max_running`（每会话 4）、`tasks.max_duration`（2h）、`tasks.max_output_bytes`（64 MiB）。trace 里每个任务记一条 `task` 事件（带种类，watch 还带运行与变化次数），`--analyze` 按种类统计，包括 watch 任务数与后台 Stop hook 次数。设计见 docs/design/2026-10-13-background-tasks-design.md。
- **监听任务**：`shell_exec` 传 `background: true` 并带 `every`（秒，至少 10）和/或 `watch_paths`（相对工作区的 glob，`**` 跨目录，不含 `/` 的模式匹配任意深度的同名文件）时命令变成监听：启动时先跑一次，之后每个周期到点、以及被监听文件变化（每 2 秒轮询，变化停止后的下一次轮询才触发）时再跑。运行之间不会重叠，每次运行受调用的 `timeout` 限制（运行自身写入被监听的文件不算变化）。任务的输出文件里每轮以 `=== run N (触发原因) HH:MM:SS ===` 开头、`=== exit X ===` 结尾。只有退出码或输出（忽略耗时与时间戳）与上一次不同才通知模型，基线运行只在失败时通知，所以模型可以改代码、再从下一轮通知得知测试是否转绿。对监听调用 `task wait` 会在下一轮运行结束时返回，并带上这一轮的报告；`task list` 显示运行次数与最近一次退出码，`task kill` 停止监听。
- **后台 Stop hook**：配置 `hooks.stop_background: true`（仅 chat）后，Stop hook 作为 `hook` 类任务运行，不再在提示符处等待：用户输入、投递、任务唤醒都不会中断它。hook 返回 block 加 reason 时产生通知 `Stop hook asks to continue: <reason>`：空闲的 chat 被唤醒并开始注入回合，回合进行中则在下一次模型请求前带上。新的 Stop 到来时终止正在跑的那个。设了 `hooks.stop_wait_sec` 时 hook 超时为它加 2 秒，否则由 `tasks.max_duration` 限制；没有空闲任务槽位时退回原来的等待。
- **并发读取**：同一次回复里有多个 `file_read`、`file_list`、`file_grep`、`file_find` 调用时并发执行（`tools.parallel_reads`，默认开）；每个调用仍按顺序经过审批、污染追踪与 trace 记录，只有读取本身重叠。
- **大输出**：工具结果超过 `tools.max_output_bytes` 时，脱敏后的原文存到 `$SUAG_HOME/sessions/<id>/outputs/`，上下文只留头尾（各 2000 字节）和 ref（形如 `out:<session>:<hex>`）；用 `recall` 读取，用 `session_search` 检索。在此之前，shell 结果会先整理（`tools.minimize_output`，默认开）：去掉颜色码与进度重绘，折叠连续相同的行，测试与构建输出按输出规则裁剪——全部通过时只留各包结果和一行汇总，有失败时只留失败用例及其信息与日志；原文保存为 ref，结果中注明。内置规则覆盖 `go test`、`cargo test`、jest/vitest/mocha 与 `npm|pnpm|yarn|bun test`、pytest、Maven、Gradle（格式见 [output_rules.yaml](pkg/agentcore/tools/native/output_rules.yaml)）；`$SUAG_HOME/output-rules.yaml` 与其后的 `<workspace>/.suag/output-rules.yaml` 可按名字替换规则、新增规则，或用 `enabled: false` 关掉某条。命令本身用管道过滤了输出（`| grep`、`| Select-String`、`| tail` 等）时不套用这些规则，保留它筛出的内容。`file_read` 自己分页（每个文件一页最多 `tools.read_page_bytes`，默认 32 KiB；`paths` 一次最多四页），不会被外置。
- **边界**：文件工具经 `os.OpenRoot` 访问，不会顺着 symlink 跑出工作区；`shell_exec` 超时会终止整个进程树；`web_fetch` 检查实际连接的 IP，跳转也一样受限；所有工具结果都有上限。
- **MCP**：用 `--mcp-config <file>`（Claude 风格的 `mcpServers` JSON）或配置里的 `mcp.servers` 接入。MCP 调用默认一律拒绝，需在 `tools.auto_allow`（`--read-only` 时用 `tools.read_only_allow`）中放行。工具很多时会延迟加载，模型通过 `tool_search` 取用。

完整语义见 [Gofer 接入与工具说明](docs/usage/gofer-personal-agent.md#内置工具与相关配置)。

## 子 agent

`agent` 工具把一件独立的任务交给子 agent：它是一个在自己的会话里运行的模型，只有只读工具，最终回答就是带回给主 agent 的报告。要大量阅读的任务——追调用链、比对多个实现、查清一个依赖的用法——因此只花掉父上下文一份报告，而不是每个读过的文件。

- **两种类型**：`explore`（默认）拿到工作区只读工具（`file_read`、`file_list`、`file_grep`、`file_find`）；`research` 再加 `web_fetch` 与 `web_search`。子 agent 不能写文件、不能执行命令、不能询问用户，也不能再派子 agent，并且看不到本会话的任何内容。调用里的 `description` 就是它在 chat、ACP 与 trace 里的名字。
- **后台**：传 `background: true` 时它作为任务（种类 `agent`）运行：调用立刻返回任务 id，`task wait` 等它结束，报告就是该任务的输出（结束时还有一条带报告开头的通知）。可以一次派几个并行探索（与 shell 任务共用 `tasks.max_running` 槽位）。只读会话也有 `task` 工具，因为它只用到 `list`/`output`/`wait`/`kill`（`shell_exec` 仍只在写模式后台运行）。
- **上限**：`agents.max_steps`（30）、`agents.max_tokens`（400000，按未命中缓存的输入加输出累计，长上下文命中缓存不会很快耗尽它）、`agents.timeout`（10m，软截止，之后 60 秒宽限）。到任一个上限时子 agent 被要求收尾，基于已有信息给出报告；报告末尾一行是 `[sub-agent <会话> · N steps · in … / out … · <耗时> · stop=<原因>]`，原因有 `final`、`max_steps`、`max_tokens`、`timeout`、`cancelled`、`failed`。`agents.model` 可让子 agent 跑在更便宜的模型上；`agents.enabled: false` 则不注册该工具。
- **用量与污染**：子 agent 的模型调用计入调用它的会话当前进行中的 run（stream-json 与 ACP 的 `usage_update` 都含它，ACP 在 prompt 进行中上报），也计入会话累计——会话累计 = 父会话加它的全部子会话（`suag stats --session`）。子 agent 读过的外部内容会和其它外部读取一样把父会话标记为已污染：`research` 类型的子 agent（或读到了引用网页内容的文件）会让之后的写记忆需要确认。
- **子会话**：每次调用都在一个自己的会话里运行——`kind: agent`，带 `parent_session_id`、`agent_type` 以及发起它的回合与工具调用，标题就是 description。`suag sessions` 与 `session_search` 默认不列出子会话；`suag sessions --agents` 会列出并标明各自的父会话。`suag trace <会话>` 可以照常查看某一个子会话，`suag gc` 随父会话一起删除它。

## Skills

启动时扫描一次 `<工作区>/.agents/skills`、`<工作区>/skills` 和 `~/.agents/skills`，同名时工作区的优先。skills 数量不超过 `skills.list_max` 时全部列在系统提示词里；超过时只列工作区 skills 的说明，其余只列名字（名字总计最多 3000 字节，再多只给数量），模型可以按名字加载任意一个，或用 `skill(query=…)` 检索。要让某个 skill 的正文每次都放进提示词，可在其 frontmatter 写 `metadata: {always_load: true}`，或把名字加进 `skills.always_load`。

## 记忆

跨会话共享的长期记忆，由模型通过 `memory` 工具写入。

- **四类条目**：`user`（用户是谁、偏好）、`feedback`（纠正与确认过的做法）、`project`（代码里看不出的目标、决定、进展）、`reference`（外部资源在哪）。不记：能从代码或 git 推出的内容、规则文件已有的内容、本次会话的临时细节、密钥。流程类内容应写成 skill。
- **范围与后端**：用户记忆在 `$SUAG_HOME/memory/user`（files）。项目记忆按顺序选后端：`memory.project.backend` 的配置；仓库根有 `MEMORY.md`（files，仓库模式）；仓库根有 `.gofer/tracker`（gofer）；否则用 `$SUAG_HOME/projects/<key>/memory` 下的 files，`<key>` 为仓库目录名加完整路径的短哈希（如 `suag-5cb0fa32`，也显示为作用域 `project:<key>`）。仓库模式下索引就是 `MEMORY.md`，每条记忆放在 `.suag/memory/<id>.md`，可随项目提交；根目录的 `MEMORY.md` 不会由 suag 创建。gofer 后端中每条项目记忆是带 `suag` 标签的 gofer memory，只管理这些条目。`suag manifest` 会显示选了哪个后端及原因。
- **冻结的索引**：记忆索引（仅标题）放在段上下文消息里，本段内不变；本会话写入的内容下一段才可见，开启新段时刷新。正文用 `memory get` 读取。
- **污染与确认**：会话一旦读过外部内容（`web_fetch`、`web_search`、MCP 工具、`session_search`、`recall`，以及 `memory.taint_tools` / `memory.taint_on_spill` 中的工具），就永久标记为已污染，此后写记忆需要确认（`memory.write_tainted`）：chat 在终端询问（`y`、`n`，`a` 表示本会话不再问），ACP 发权限请求，`run` 直接拒绝；`deny` 永不写入，`allow` 不询问。`--read-only`（及 ACP `read_only`）下该工具只读。普通 `run` 可以写记忆：它是 suag 自身的状态，不属于工作区写入。

## 会话

- **续接与交接**：`--session-id` 创建或复用指定 ID 的会话；`--resume` 要求会话已存在，不存在时以退出码 4 退出。`run --from <session>` 新建一个会话，继承来源会话的摘要、计划和已加载的工具。
- **分段**：会话是一串段。每段以一条段起始记录开头（原因：开始、阈值、`:compact`、空闲、`--from`），记录里有上下文消息；第一段之后还有交接包：面向当前任务的摘要、最近的用户请求原文和仍相关的 ref。上下文接近模型窗口或执行 `:compact` 时开启新段，原样保留的只有最后一组完整的工具调用/结果。旧段仍在库里，用 `session_search` 和 `recall` 取回。
- **历史折叠**：每次请求都会重发此前的对话，其中大部分是模型自己的思考内容和整文件写入。设置 `context.fold_every`（如 10；默认 0，关闭）与 `context.fold_keep`（默认 5）开启后，最近 5 步之前的步骤发送时去掉思考内容，超过 1000 字符的字符串参数（整文件写入、长编辑）换成一句说明，提示需要时重读文件。折叠点每 10 步才前移一次，期间缓存前缀保持有效。存储的会话不变；折叠点前移时 `suag trace` 显示一条 `fold`。T9 对比中每次请求的上下文减半、prompt 总量下降 59%（见 docs/research/2026-10-11-suag-vs-omp-loop-analysis.md）。默认关闭：P5 复测中每次折叠后重建缓存前缀约占未命中输入的三分之一。
- **空闲整理（chat）**：超过 `context.idle_after`（默认 15m）没有输入，且上下文已达阈值的 `context.idle_min_ratio`（默认 0.5）时，chat 在后台开启新段。回合进行中不会触发，开始输入即取消。
- **计划**：`update_plan` 工具记录模型的计划。stream-json 中输出为 `plan` 事件，ACP 中转为计划更新。
- **系统提示词与段上下文**：系统提示词只含稳定内容（身份与工作约定），同一模式下跨会话、跨工作区字节相同。环境（系统、shell、工作区）、项目规则（每级一个文件，`rules.file`，默认 `AGENTS.md`，另有 `~/.suag/AGENTS.md`）、`--append-system-prompt`、记忆索引、skills 列表和延迟工具列表放在每段开头的上下文消息里。段内发生变化（如规则文件被改）时，追加一条“上下文更新”消息，不改写已有内容。`--no-project-rules` 跳过项目规则。
- **投递与 hooks**：`suag send --session <s> --text <t>` 把文本送进正在运行的 chat；`hooks.command` 以 stdin 上的 JSON 接收 chat 的生命周期事件。

## 配合 Gofer 使用

Gofer 拉起 `suag run`（CLI 作业），或 `suag acp`（常驻会话，通过 `permission_policy` 逐次审批）。agent 配置、只读与可写两种 run、stream-json 事件和拒绝语义见 [docs/usage/gofer-personal-agent.md](docs/usage/gofer-personal-agent.md)。

## 观测

```text
suag trace <session> [--turn <id>] [--json]       # 时间线：模型调用、工具、拒绝、计划
suag sessions [关键词] [-n 10] [--since 7d] [--until 2026-10-01] [--cwd .] [--agents] [--json]  # 会话列表，最新在前；关键词匹配标题、id、目录、模型或对话内容；--agents 连子会话一起列出
suag trace <session> --analyze [--json]          # 循环分析：步数、并行调用、重复读、重复命令、首次通过后的测试、子 agent
suag trace <session> --web [--addr 127.0.0.1:0]  # 本机网页：时间线（后台任务横条、任务通知标注、子 agent 行及其子会话 trace 命令）、详情、上下文组成、分析、本机环境（Ctrl-C 结束）
suag stats [--session <id>] [--since 7d] [--json] # 调用次数、耗时、token、缓存命中
suag export <session> [--redact] [-o <file>]      # 以 jsonl 导出会话内容与事件
suag gc --older-than 30d [--apply]                # 列出（默认）或删除长期未活动的会话
suag ps                                           # 正在运行的 chat 进程
suag manifest [--json] [--model <p/m>]            # 模型、工具、MCP、skills、记忆后端、输出上限
```

工具行显示退出码（shell）、进入上下文的字节数、转存标记与脱敏后的参数摘要；`agent` 行（子 agent 调用）还会显示它运行的会话、类型、步数（连带子 agent 自己的工具调用数）、token 与成本、以及结束原因（`sub=<会话> type=explore steps=12 calls=5 in=85k(cache 60k) out=3k cost=$0.0123 stop=final`），而后台子 agent 的 task 行显示 `agent steps=N calls=M`；模型行显示这次回复发出了几个工具调用（`calls=N`）、被输出上限截断时的 `truncated`、请求重试时的 `retries=N(reason, waited Xs)`、命中缓存的输入 token（`cached=N`），以及请求没有延续上一次时的原因和重新发送的早先内容量（`prefix=fold(rebuilt=12k)`，原因有 fold、segment、system、tools、rewrite）。`--analyze` 从同一组事件得出：步数与并行步数、各工具次数与失败、读了多次的文件和写完又回读的次数、每个文件的编辑次数、重复执行的相同命令、测试运行次数及首次通过之后又跑了几次（输出报告了失败的不算通过，不论退出码）、各阶段的模型调用数（首次写入前的探索、首次写入到测试首次通过、通过之后）、编辑步数及其中只改一处的步数、退出码为 0 但有 stderr 输出的 shell 次数、未命中缓存的输入按来源拆分（上次请求之后新增的内容、前缀断开后重新发送的早先内容（按原因）、前缀本可命中但 provider 没有命中的部分，按请求大小估算）、转存/折叠/压缩/模型重试次数、最慢的几次模型调用，以及本会话子 agent 的开销（调用次数、步数、工具调用数、token、成本与耗时，见 `agent_calls`、`agent_steps`、`agent_tool_calls`、`agent_input_tokens`、`agent_cache_read_tokens`、`agent_output_tokens`、`agent_cost_usd`、`agent_ms`；`agent_stops` 记录各结束原因的分布）。有后台任务时还会补充：任务运行期间循环用于模型与工具调用的时间（`background_overlap_ms`，即省下的等待，只计后台命令，不含监听与 Stop hook；重叠的前台调用只算一次）、`task wait` 的次数与阻塞时长，以及其中在任务启动后立刻 wait 的次数（`immediate_waits`，把后台当前台用）；按种类还会补充本会话的数字：watch 任务数及其运行与变化次数（`watch_tasks`、`watch_runs`、`watch_changes`），以及后台 Stop hook 的运行次数（`stop_hooks`）。`task` 行（后台任务结束）会显示任务的种类：watch 带 `runs=N changes=M` 或最近一次状态，Stop hook 显示它的摘要，agent 任务显示子 agent 跑的步数。工具行还会显示编辑的增删行数（`+新增/-删除`），问过人时显示 `approval=user(wait=3.2s)`。每回合有一行 `context`，列出该回合第一次请求由哪些部分组成（系统提示词、工具及其数量与 MCP 占比、环境、规则、记忆索引、skills、延迟工具、交接摘要、历史、hook 注入、本轮输入的字符数）以及估算 token 与窗口；每次 hook 运行记一行 `hook`（事件、耗时、是否 block、注入的上下文）。回合行记录输入长度及是否为注入而非人工输入。`suag trace` 每回合还会显示一行 `turn_context`（模型、模式、工具集哈希、规则与记忆索引哈希、是否已污染），每次分段显示一行（`segment=#N reason=... tokens=前->后`），以及 `schedule` 事件（chat 空闲整理：fired、done、cancelled、skipped）。`suag stats` 显示各模型成本（provider 返回，或按配置的 `price` 计算）；加 `--json` 还有工具输出字节（`raw_bytes`、`inline_bytes`、`spilled`）。

`run --output-format stream-json` 在每次模型调用后输出一行 `usage`，为本次 run 的累计值：`input_tokens`（不含缓存）、`output_tokens`、`cache_read_tokens`、`cache_write_tokens`、`total_tokens`、`cost_usd`、`tool_calls`。`turn_end` 与 `result` 带同名 `usage`，调用方可在运行中途做预算控制。模型请求因瞬时故障重试时会输出一行 `retry`（`reason`、`attempt`、`wait_ms`、`restart`）；`restart: true` 表示这条消息此前输出的 `message_delta` 文本作废，会从头重新流式输出。chat 把这种重试显示为一行 `↻` 提示，`run` 打到 stderr，ACP 作为 thought 块下发。ACP 在同一时机发 `usage_update`（[说明](docs/usage/gofer-personal-agent.md#acp-用量上报)）。

## 开发

```bash
go test -race ./...     # 单元测试
make smoke-chat         # 伪终端端到端 chat 测试，含 chat_memory 与 chat_idle（Linux/macOS，需 Python 3）
go run ./scripts/eval --fake --bin tmp/smoke/suag   # 任务评测，见 scripts/eval/README.md
make build-all          # 交叉编译到 dist/
```

评测支持多轮任务（`task.yaml` 的 `prompts`，同一会话依次发送），并报告 `first_call_uncached`（表格 FIRST 列：首个主循环模型调用的未缓存输入）。`--probe-prefix` 跑三组前缀缓存探针：同工作区、跨工作区、系统提示词带随机 nonce 的对照组。`--agent omp` 用 omp 跑同样的任务做对比。

- `cmd/suag`：程序入口；`internal/cli`：每个命令一个文件。
- `internal/wiring`：为 CLI 与 ACP 组装 provider、工具、skills、系统提示词和 runner。
- `internal/personal`：回合、分段、交接和事件；`internal/personal/store`：SQLite 存储。
- `internal/chat`：终端对话；`internal/acpserver`：ACP 服务。
- `pkg/agentcore`：应用层之下可复用的各层，不依赖 `internal/*`（[README](pkg/agentcore/README.md)）：内核（模型/工具循环、run 状态、transcript），以及 `llm`（各 provider）、`schedule`（进程内 After/Every/Idle 定时）、`tools`（注册表、审批、延迟加载）、`tools/native`（内置工具）、`tools/mcp`（MCP 客户端）、`skills`、`memory`（存储接口、治理、路由；后端 `memory/files`、`memory/execstore`）、`redact`（密钥与隐形文本清理）和 `extproc`（外部命令的 exec 协议 v1 与凭据剥离）。

版本变更见 [CHANGELOG.md](CHANGELOG.md)；设计与计划文档在 [docs/](docs/README.md)。CI 在 Ubuntu 和 Windows 上用 Go 1.26 和 stable 跑测试。旧的控制面（team、cron、orchestrator）保留在 `v0.1.0` 标签。
