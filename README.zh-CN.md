# Suga

[![Unit-Tests](https://github.com/inhere/suga/actions/workflows/go.yml/badge.svg)](https://github.com/inhere/suga/actions/workflows/go.yml)

[English](README.md) | 简体中文

Suga 是用 Go 写的个人编码 Agent。同一个持久会话引擎提供两种入口：终端 CLI（交互式 `chat` 和单轮 `run`）和 ACP stdio 服务（`acp`）。[Gofer](https://github.com/inhere/gofer) 可以拉起这两种入口，并在其上提供作业、worker、日志和交互式审批。

- **开发工具**：按行分段读文件、列目录、搜索内容与文件名、按字符串精确修改、执行 shell 命令（Windows 上用 PowerShell）、抓取网页、网页搜索。
- **默认安全**：不开启就是只读；chat 写文件、执行命令前会先询问；文件工具限制在工作区内；网页抓取拒绝内网地址。
- **Skills**：读取工作区、`~/.suga/skills` 和 `~/.agents/skills` 下标准的 `SKILL.md`，按需列出或检索；内置配置与使用向导 skill `suga-guide`。
- **持久会话**：单个 SQLite 库，支持续接、自动压缩、计划、会话交接，以及用于 trace 和统计的事件记录。
- **MCP**：接入 stdio MCP 服务，支持放行名单，工具多时延迟加载。
- **持久记忆**：跨会话的用户与项目记忆，以 `MEMORY.md` 等文件为事实源，后端可换（files、gofer 或外部命令）。
- **历史检索**：全文检索过往会话，并可分页读取已保存的工具输出。
- **大输出外置**：超过阈值的工具结果存到会话目录，上下文只保留头尾与 ref。

## 安装

需要 Go 1.26 及以上。仓库是私有的，请从源码构建：

```bash
git clone https://github.com/inhere/suga.git && cd suga
make install            # 或：go install ./cmd/suga
```

`make install` 会构建（装了 `upx` 时顺带压缩）并安装到 `$(go env GOPATH)/bin`。在 Windows 上，如果旧的二进制还被打开着的 chat 占用，它会先把旧文件改名挪开再安装。配置好仓库访问权限（`GOPRIVATE=github.com/inhere/*`）后，也可以直接 `go install github.com/inhere/suga/cmd/suga@latest`。推送 `v*` 标签会触发 release 工作流，自动附上 Linux、macOS、Windows 的预编译包。确认安装目录在终端（以及 Gofer server，如果用到的话）的 `PATH` 中，然后运行 `suga version` 检查。

## 快速开始

创建 `~/.suga/config.yaml`（全部字段见 [config/config.yaml](config/config.yaml)）：

```yaml
model: deepseek/deepseek-chat   # <provider>/<model>
# openai、anthropic、deepseek 是内置 provider，分别读取
# OPENAI_API_KEY / ANTHROPIC_API_KEY / DEEPSEEK_API_KEY
tools:
  allow_write: true             # 允许 chat 写文件、执行命令（执行前会询问）
```

```bash
suga chat                                         # 在当前目录开启交互式对话
suga run --prompt "概括一下这个项目"               # 单轮，只读
suga run --allow-write --prompt "修好失败的测试"
suga run --resume <session-id> --prompt "继续"
suga run --max-steps 30 --prompt "长任务"         # 本轮步数上限
```

一轮最多用 `max_steps` 步（默认 240，按轮计，不是整个会话），到顶时模型被要求直接基于已获得的信息作答。`run --max-steps N`（N 至少 1）为本次调用覆盖该设置；环境变量 `SUGA_MAX_STEPS` 在两者之间生效；子 agent 用它们自己的 `agents.max_steps`。

chat 中：`:session` 显示会话 ID，`:compact` 开启新段（见[会话](#会话)），`:quit`（或 `:exit`）退出；`:add-dir <绝对目录>[:ro|:rw]` 为本会话追加授权目录，`:dirs` 列出工作区与全部附加目录（权限，以及来源：启动参数、环境变量、会话中追加、越界授权、早先会话沿用），`:rm-dir <路径>` 撤销其中一个；改动写入会话，因此本会话后续回合与续接都生效。这些命令只在本地键盘输入时生效：`suga send` 或 job runner 投递的文本一律是普通消息，投递的 `:add-dir`（或 `:quit`）不会作用于会话。在提示符处按一次 Ctrl+C 清掉已输入的内容，3 秒内再按一次才退出；Ctrl+D 直接退出。退出时会显示 token 用量和续接本会话的命令。续接已有会话（`--resume <id>`，或 `--session-id <id>` 指向已存在的会话）时，第一个提示符之前会先显示该会话最后一次有正文的模型回复，方便接着往下说：淡色“上一次回复：”作区分，过长只显示末尾 40 行并给出 `suga trace` 命令，上一回合被中断或失败时加一句“上一回合未正常结束”；只看不写，不调用模型、不产生会话条目。进程被强杀（kill -9、崩溃）后会话会停在 `running`，续接后的第一个提示符输入时先把它恢复为 `open`（显示“上一回合异常中断，已恢复会话”，被中断的回合记为 `cancelled`）再正常开始新回合；有活进程在跑该会话时行为不变，仍然拒绝。

## 配置

Suga 只读取 `$SUGA_HOME/config.yaml` 和 `$SUGA_HOME/.env`（默认 `~/.suga`）。工作目录下的配置和 `.env` 属于你的项目，不会被读取。`SUGA_CONFIG` 可以指定唯一的配置文件。密钥写进 `$SUGA_HOME/.env`（如 `BAILIAN_API_KEY=...`），配置里的 `${VAR}` 从这里或进程环境展开。最常用的配置：

| 配置项 | 含义 |
|---|---|
| `model` | 主模型，`<provider>/<model>`；可被 `SUGA_MODEL` 覆盖，再被 `--model` 覆盖 |
| `providers.<id>` | `type`（`openai` 即 chat/completions、`openai-responses` 即 Responses API、`anthropic`）、`base_url`、`api_key`、`headers`；`openai`、`anthropic`、`deepseek` 是内置预设，分别读取 `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` / `DEEPSEEK_API_KEY` |
| `providers.<id>.models.<model>` | `context_window`、`model_id`（与键不同时请求里发送的模型 id）、`type`/`base_url`（该模型可用自己的协议与地址）、`price`、`thinking` |
| `compact_model`、`fallback_models` | 生成摘要用的便宜模型；主模型失败时依次尝试的备用模型 |
| `tools.allow_write`、`tools.approval` | 仅 chat：启用写文件与 shell 工具（默认 false）；`ask`（默认）每次写/shell 调用前询问，`auto` 不询问 |
| `mcp.servers`、`tools.auto_allow`、`tools.read_only_allow` | MCP 服务，以及 MCP 放行名单（条目形如 `server/工具glob`）；两者都为空时所有 MCP 调用被拒绝 |
| `memory.user.backend`、`memory.project.backend`、`memory.write_tainted` | 各范围的记忆后端（`files`、项目可用 `gofer`、`exec`），以及读过外部内容的会话写记忆时的处理（`ask`、`deny`、`allow`） |
| `hooks.command`、`hooks.post_tool_use` | chat 的 hook 命令（argv 列表）与哪些工具调用发 PostToolUse；不设 `hooks.command` 即不启用 hooks |
| `skills.list_max`、`skills.always_load` | 带说明列出的 skills 数量；正文每次进提示词的 skills |
| `workspace` | 文件与 shell 工具的工作区根（默认：启动 suga 时所在的目录） |

其余配置（每一项的默认值）、provider 与模型的细节，以及分步配置向导（选 provider、写 `config.yaml`、把密钥放进 `.env`、验证第一次运行、看懂报错）都在内置 skill **suga-guide** 里：suga 首次运行会释放到 `~/.suga/skills/suga-guide/`。在 chat 里直接问怎么配置 suga 即可，模型会加载该 skill 并照着回答。

所有数据都在 `$SUGA_HOME/db/sessions.db`。

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
| `suga_manifest` | 读 | 以 JSON 返回本实例的能力清单（与 `suga manifest` 相同） |
| `memory` | 状态 | 对长期记忆 `add`、`replace`、`remove`、`get`、`search`（`scope` 为 project 或 user），见[记忆](#记忆) |
| `file_write` | 写 | 整文件写入 |
| `file_edit` | 写 | 替换一处唯一的字符串（`replace_all` 替换全部），或用 `edits` 一次替换多处（全部成功或全部不改）；保留 CRLF |
| `file_remove` | 写 | 删除文件，或带 `recursive` 删除目录：移入 `$SUGA_HOME/trash/` 保留 7 天（`origin.json` 记录原路径），单次最多 1000 个文件。被 `.gitignore` 忽略的目录（以及 `node_modules`、`vendor`、`dist`），如被忽略的 `tmp/` 下的测试仓库，视为可再生：不遍历、不计数、直接永久删除，并在结果中列出；被忽略的单个文件（如 `.env`）仍进回收区。拒绝工作区根、工作区自己的 `.git`、用户主目录、通配符与工作区外路径 |
| `shell_exec` | 写 | 执行命令（Windows 用 PowerShell，其他系统用 `sh`），带超时；返回输出与退出码。`run` 与 chat 里传 `background: true` 会把它作为后台任务启动，立即返回任务 id |
| `task` | 状态 | 本会话的后台任务：`list`、`output`（末尾若干行，可带 `offset`/`limit`、`grep`）、`wait`（可带 `timeout`）、`kill`（整个进程树） |

`file_grep`、`file_find` 与递归的 `file_list` 跳过 `.git`、`node_modules`、`vendor`、`dist` 和 `.gitignore` 匹配的路径（各级目录的 `.gitignore`，支持 `!` 与 `**`）；结果末尾一行注明跳过了什么，传 `no_ignore: true` 可包含它们（`.git` 除外）。

- **默认只读**：写工具只在 `run --allow-write`、chat 设置 `tools.allow_write: true`、或 ACP 会话处于 `default` 模式时才有；`--read-only` 永远优先。模型调用了未开启的工具时，会被告知开启方法，并计入拒绝次数。
- **附加目录**：`run` 与 `chat` 支持可重复参数 `--add-dir <绝对目录>[:ro|:rw]`（默认 `:ro`），环境变量 `SUGA_ADD_DIRS` 可再加（各项用系统的路径列表分隔符分隔，Windows 是 `;`，适合 job runner 透传）。`:ro` 目录加入本会话的只读根（与 `tools.read_roots` 并列），适合 `cwd` 落在某个子仓、却要读项目根或兄弟模块文档的场景；`:rw` 目录同时允许 `file_write`、`file_edit`、`file_remove` 写入，审批、危险删除拦截、拒绝工作区根/自带 `.git`/主目录等规则与工作区完全一致，`shell_exec` 的 cwd 仍是工作区。目录写入会话元数据（`add_dirs`），`--resume` / `--session-id` 续接沿用，同路径重给时以本次的权限为准。`--read-only` 下它们只以只读生效（给出提示，会话里保留你要求的权限；会话中途也只能授权只读目录）。环境段会列出每个附加目录及其权限，模型因此直接用文件工具读取，不必绕 shell；会话中途追加的目录从下一次工具调用起就能用，下一回合的上下文更新把它告诉模型（环境段在段上下文里，不放在稳定系统提示，避免前缀缓存失效）。只接受已存在的绝对目录，会清理 `..`；工作区与附加目录之外的路径照旧被拒绝。ACP 暂不支持附加目录（`session/new` 带 `additionalDirectories` 会被拒绝）。
- **会话中途要目录**：chat 里用 `:add-dir` / `:rm-dir` 改附加目录（见上）。不给命令时模型也能问：文件工具要访问工作区、只读根与附加目录之外的路径时，chat 先问一行 `授权访问 <目录>？[r] 只读 [w] 读写 [n] 拒绝`（`chat.ask_outside_dirs`，默认开），问的是工具真正解析出的那个目录（跟随符号链接），不是模型嘴里的路径。答 `r` 或 `w` 就把该目录写进会话（来源 `ask`）并让这次调用照常跑；答 `w` 的写工具随后仍会照常确认一次。`--read-only` 会话不提供 `[w]`。关掉这个开关、没有终端、或 `run`/ACP 下，越界调用照旧直接拒绝。
- **chat 先询问**：`tools.approval: ask` 时，每次写文件或执行命令前都会询问，并显示完整命令、路径或改动前后的文本。回答 `y` 允许一次，`a` 在本进程内对该工具始终允许，其他任何回答都是拒绝。一轮里被拒 3 次就结束这一轮。
- **危险删除**：递归删除的 `shell_exec` 命令（`rm -r`、`Remove-Item -Recurse`、`rd /s`，以及 `sh -c`、`cmd /c`、`pwsh -Command` 里的同类命令）在审批前就会被拦下：目标是 `/`、`~` 或 `$HOME`、盘符根、工作区或其上级目录，或路径以变量开头、变量为空时会落到根目录（`"$X/"`、`"$dir\*"`）时；对这些目录用通配符（`rm ~/*`）时没有 `-r` 也算。chat 每次都询问，`tools.approval: auto` 或之前回答过的 `a` 也不例外，只有手动输入 `yes` 才执行；`run` 和 ACP 直接拒绝，并提示模型改用 `file_remove`。trace 里记下原因（`danger=…`）。这能挡住常见的误操作，挡不住刻意构造的命令：它不是安全边界。
- **后台任务**：`shell_exec` 传 `background: true`（`run`、chat 与 ACP 的写模式）后，长时间构建、完整测试、服务或监听进程可以在模型继续工作的同时运行。输出逐行脱敏写入会话的 outputs（`recall` 也能读它的 ref）。任务在模型看到之前就结束时，会留下一条提示，在下一次模型请求前加入（退出码、耗时、大小、末尾几行）；正等待输入的 chat 会被它唤醒并开始一轮（`tasks.wake_on_exit`，默认 true，运行中的任务发来通知时同样唤醒）。`run` 在回答后仍有任务在跑时提醒模型一次，退出时终止它们并在 stderr 中列出名字。ACP 下任务属于 `suga acp` 进程：跨 prompt 继续运行，结束提示进入该会话的下一个 prompt；`session/cancel` 不影响它们，连接关闭时终止。上限：`tasks.max_running`（每会话 4）、`tasks.max_duration`（2h）、`tasks.max_output_bytes`（64 MiB）。trace 里每个任务记一条 `task` 事件（带种类，watch 还带运行与变化次数），`--analyze` 按种类统计，包括 watch 任务数与后台 Stop hook 次数。设计见 docs/design/2026-10-13-background-tasks-design.md。
- **监听任务**：`shell_exec` 传 `background: true` 并带 `every`（秒，至少 10）和/或 `watch_paths`（相对工作区的 glob，`**` 跨目录，不含 `/` 的模式匹配任意深度的同名文件）时命令变成监听：启动时先跑一次，之后每个周期到点、以及被监听文件变化（每 2 秒轮询，变化停止后的下一次轮询才触发）时再跑。运行之间不会重叠，每次运行受调用的 `timeout` 限制（运行自身写入被监听的文件不算变化）。任务的输出文件里每轮以 `=== run N (触发原因) HH:MM:SS ===` 开头、`=== exit X ===` 结尾。只有退出码或输出（忽略耗时与时间戳）与上一次不同才通知模型，基线运行只在失败时通知，所以模型可以改代码、再从下一轮通知得知测试是否转绿。对监听调用 `task wait` 会在下一轮运行结束时返回，并带上这一轮的报告；`task list` 显示运行次数与最近一次退出码，`task kill` 停止监听。
- **后台 Stop hook**：配置 `hooks.stop_background: true`（仅 chat）后，Stop hook 作为 `hook` 类任务运行，不再在提示符处等待：用户输入、投递、任务唤醒都不会中断它。hook 返回 block 加 reason 时产生通知 `Stop hook asks to continue: <reason>`：空闲的 chat 被唤醒并开始注入回合，回合进行中则在下一次模型请求前带上。新的 Stop 到来时终止正在跑的那个。设了 `hooks.stop_wait_sec` 时 hook 超时为它加 2 秒，否则由 `tasks.max_duration` 限制；没有空闲任务槽位时退回原来的等待。
- **并发读取**：同一次回复里有多个 `file_read`、`file_list`、`file_grep`、`file_find` 调用时并发执行（`tools.parallel_reads`，默认开）；每个调用仍按顺序经过审批、污染追踪与 trace 记录，只有读取本身重叠。
- **大输出**：工具结果超过 `tools.max_output_bytes` 时，脱敏后的原文存到 `$SUGA_HOME/sessions/<id>/outputs/`，上下文只留头尾（各 2000 字节）和 ref（形如 `out:<session>:<hex>`）；用 `recall` 读取，用 `session_search` 检索。在此之前，shell 结果会先整理（`tools.minimize_output`，默认开）：去掉颜色码与进度重绘，折叠连续相同的行，测试与构建输出按输出规则裁剪——全部通过时只留各包结果和一行汇总，有失败时只留失败用例及其信息与日志；原文保存为 ref，结果中注明。内置规则覆盖 `go test`、`cargo test`、jest/vitest/mocha 与 `npm|pnpm|yarn|bun test`、pytest、Maven、Gradle、`git log`/`git diff`/`git status` 与 `rg`/`grep` 的命中；一条规则可以丢行、折叠重复噪声、改写整理结果（`replace`）、按捕获的键分组（`group_by`）、保留首尾 N 行，并给自己设更低的门槛（字段与格式见 [rules/](pkg/agentcore/tools/native/rules/)）；`$SUGA_HOME/output-rules.yaml` 与其后的 `<workspace>/.suga/output-rules.yaml` 可按名字替换规则、新增规则，或用 `enabled: false` 关掉某条。命令本身用管道过滤了输出（`| grep`、`| Select-String`、`| tail` 等）时不套用这些规则，保留它筛出的内容。`file_read` 自己分页（每个文件一页最多 `tools.read_page_bytes`，默认 32 KiB；`paths` 一次最多四页），不会被外置。
- **边界**：文件工具经 `os.OpenRoot` 访问，不会顺着 symlink 跑出工作区；`shell_exec` 超时会终止整个进程树；`web_fetch` 检查实际连接的 IP，跳转也一样受限；所有工具结果都有上限。
- **MCP**：用 `--mcp-config <file>`（Claude 风格的 `mcpServers` JSON）或配置里的 `mcp.servers` 接入。MCP 调用默认一律拒绝，需在 `tools.auto_allow`（`--read-only` 时用 `tools.read_only_allow`）中放行。工具很多时会延迟加载，模型通过 `tool_search` 取用。

完整语义见 [Gofer 接入与工具说明](docs/usage/gofer-personal-agent.md#内置工具与相关配置)。

## 子 agent

`agent` 工具把一件独立的任务交给子 agent：它是一个在自己的会话里运行的模型，只有只读工具，最终回答就是带回给主 agent 的报告。要大量阅读的任务——追调用链、比对多个实现、查清一个依赖的用法——因此只花掉父上下文一份报告，而不是每个读过的文件。

- **两种类型**：`explore`（默认）拿到工作区只读工具（`file_read`、`file_list`、`file_grep`、`file_find`）；`research` 再加 `web_fetch` 与 `web_search`。子 agent 不能写文件、不能执行命令、不能询问用户，也不能再派子 agent，并且看不到本会话的任何内容。调用里的 `description` 就是它在 chat、ACP 与 trace 里的名字。
- **后台**：传 `background: true` 时它作为任务（种类 `agent`）运行：调用立刻返回任务 id，`task wait` 等它结束，报告就是该任务的输出（结束时还有一条带报告开头的通知）。可以一次派几个并行探索（与 shell 任务共用 `tasks.max_running` 槽位）。只读会话也有 `task` 工具，因为它只用到 `list`/`output`/`wait`/`kill`（`shell_exec` 仍只在写模式后台运行）。
- **上限**：`agents.max_steps`（30）、`agents.max_tokens`（400000，按未命中缓存的输入加输出累计，长上下文命中缓存不会很快耗尽它）、`agents.timeout`（10m，软截止，之后 60 秒宽限）。到任一个上限时子 agent 被要求收尾，基于已有信息给出报告；报告末尾一行是 `[sub-agent <会话> · N steps · in … / out … · <耗时> · stop=<原因>]`，原因有 `final`、`max_steps`、`max_tokens`、`timeout`、`cancelled`、`failed`。`agents.model` 可让子 agent 跑在更便宜的模型上；`agents.enabled: false` 则不注册该工具。
- **用量与污染**：子 agent 的模型调用计入调用它的会话当前进行中的 run（stream-json 与 ACP 的 `usage_update` 都含它，ACP 在 prompt 进行中上报），也计入会话累计——会话累计 = 父会话加它的全部子会话（`suga stats --session`）。子 agent 读过的外部内容会和其它外部读取一样把父会话标记为已污染：`research` 类型的子 agent（或读到了引用网页内容的文件）会让之后的写记忆需要确认。
- **子会话**：每次调用都在一个自己的会话里运行——`kind: agent`，带 `parent_session_id`、`agent_type` 以及发起它的回合与工具调用，标题就是 description。`suga sessions` 与 `session_search` 默认不列出子会话；`suga sessions --agents` 会列出并标明各自的父会话。`suga trace <会话>` 可以照常查看某一个子会话，`suga gc` 随父会话一起删除它。

## Skills

启动时按顺序扫描 `<工作区>/.agents/skills`、`<工作区>/skills`、`<SUGA_HOME>/skills`（默认 `~/.suga/skills`）与 `~/.agents/skills`；同名时先扫到的优先。`<SUGA_HOME>/skills/suga-guide` 是内置的配置与使用向导：首次运行会写到这里，升级后只要目录里每个文件仍是 suga 写入的那份就会刷新。它旁边的 `.suga-builtin.json` 记录 suga 写了什么（版本与每个文件的哈希）：你改过、加过文件的目录，或没有这个标记的目录（你自己的同名 skill）永远不被动。skills 数量不超过 `skills.list_max` 时全部列在系统提示词里；超过时只列工作区 skills 的说明，其余只列名字（名字总计最多 3000 字节，再多只给数量），模型可以按名字加载任意一个，或用 `skill(query=…)` 检索。要让某个 skill 的正文每次都放进提示词，可在其 frontmatter 写 `metadata: {always_load: true}`，或把名字加进 `skills.always_load`。

## 记忆

跨会话共享的长期记忆，由模型通过 `memory` 工具写入。

- **四类条目**：`user`（用户是谁、偏好）、`feedback`（纠正与确认过的做法）、`project`（代码里看不出的目标、决定、进展）、`reference`（外部资源在哪）。不记：能从代码或 git 推出的内容、规则文件已有的内容、本次会话的临时细节、密钥。流程类内容应写成 skill。
- **范围与后端**：用户记忆在 `$SUGA_HOME/memory/user`（files）。项目记忆按顺序选后端：`memory.project.backend` 的配置；仓库根有 `MEMORY.md`（files，仓库模式）；仓库根有 `.gofer/tracker`（gofer）；否则用 `$SUGA_HOME/projects/<key>/memory` 下的 files，`<key>` 为仓库目录名加完整路径的短哈希（如 `suga-5cb0fa32`，也显示为作用域 `project:<key>`）。仓库模式下索引就是 `MEMORY.md`，每条记忆放在 `.suga/memory/<id>.md`，可随项目提交；根目录的 `MEMORY.md` 不会由 suga 创建。gofer 后端中每条项目记忆是带 `suga` 标签的 gofer memory，只管理这些条目。`suga manifest` 会显示选了哪个后端及原因。
- **冻结的索引**：记忆索引（仅标题）放在段上下文消息里，本段内不变；本会话写入的内容下一段才可见，开启新段时刷新。正文用 `memory get` 读取。
- **污染与确认**：会话一旦读过外部内容（`web_fetch`、`web_search`、MCP 工具、`session_search`、`recall`，以及 `memory.taint_tools` / `memory.taint_on_spill` 中的工具），就永久标记为已污染，此后写记忆需要确认（`memory.write_tainted`）：chat 在终端询问（`y`、`n`，`a` 表示本会话不再问），ACP 发权限请求，`run` 直接拒绝；`deny` 永不写入，`allow` 不询问。`--read-only`（及 ACP `read_only`）下该工具只读。普通 `run` 可以写记忆：它是 suga 自身的状态，不属于工作区写入。

## 会话

- **续接与交接**：`--session-id` 创建或复用指定 ID 的会话；`--resume` 要求会话已存在，不存在时以退出码 4 退出。`run --from <session>` 新建一个会话，继承来源会话的摘要、计划和已加载的工具。
- **分段**：会话是一串段。每段以一条段起始记录开头（原因：开始、阈值、`:compact`、空闲、`--from`），记录里有上下文消息；第一段之后还有交接包：面向当前任务的摘要、最近的用户请求原文和仍相关的 ref。上下文接近模型窗口或执行 `:compact` 时开启新段，原样保留的只有最后一组完整的工具调用/结果。旧段仍在库里，用 `session_search` 和 `recall` 取回。
- **中断回合**：以 `failed` / `cancelled` 结束的回合已经把效果落到了工作区（写文件、跑命令、提交），因此它的记录不再被整体丢弃：下一回合回放它到“最后一个完整的工具调用/结果”为止，并附一条小结，写明它执行过哪些工具、为何停下；只截掉悬空尾部（没有结果的工具调用及之后的内容），小结会说明截掉了多少条。这类回合的压缩与上下文更新记录仍然不生效（下一回合会重算），插话则退回输入行、不随回放。
- **历史折叠**：每次请求都会重发此前的对话，其中大部分是模型自己的思考内容和整文件写入。设置 `context.fold_every`（如 10；默认 0，关闭）与 `context.fold_keep`（默认 5）开启后，最近 5 步之前的步骤发送时去掉思考内容，超过 1000 字符的字符串参数（整文件写入、长编辑）换成一句说明，提示需要时重读文件。折叠点每 10 步才前移一次，期间缓存前缀保持有效。存储的会话不变；折叠点前移时 `suga trace` 显示一条 `fold`。T9 对比中每次请求的上下文减半、prompt 总量下降 59%（见 docs/research/2026-10-11-suag-vs-omp-loop-analysis.md）。默认关闭：P5 复测中每次折叠后重建缓存前缀约占未命中输入的三分之一。
- **空闲整理（chat）**：超过 `context.idle_after`（默认 15m）没有输入，且上下文已达阈值的 `context.idle_min_ratio`（默认 0.65）时，chat 在后台开启新段。回合进行中不会触发，开始输入即取消。它打印的行以 `✦` 开头，与其他提示相区分。
- **中断本轮（chat）**：`chat.steering`（默认开）让进行中的回合继续读取键盘：`Esc` 或单次 `Ctrl-C` 中断本轮、chat 留在提示符；3 秒内再按一次 `Ctrl-C` 退出 chat。按键以字节到达，因此不依赖控制台自己的 Ctrl-C 事件（ConPTY：Zed、Windows Terminal）——以前那条路上回合中按 `Ctrl-C` 没有反应（suga-nrf2）。回合中键入的其他内容不回显也不保留（与旧行为一致），本轮暂不插入消息（suag-mbwu）。`chat.steering: false` 时回合保持 cooked、`Ctrl-C` 走 SIGINT 并直接退出整个 chat（旧行为）。
- **多行输入（chat）**：`Alt+Enter` 在输入里插入换行而不是提交（`Ctrl+J` 不行：Unix 下预输入的行也以裸 LF 结尾）；`Enter` 把整段作为一条消息提交。输入里含换行时，`Up`/`Down` 在行内移动光标（保持屏幕列、目标行较短则落在行尾），单行输入时上下键仍是历史。
- **计划**：`update_plan` 工具记录模型的计划。stream-json 中输出为 `plan` 事件，ACP 中转为计划更新。
- **系统提示词与段上下文**：系统提示词只含稳定内容（身份与工作约定），同一模式下跨会话、跨工作区字节相同。环境（系统、shell、工作区）、项目规则（每级一个文件，`rules.file`，默认 `AGENTS.md`，另有 `~/.suga/AGENTS.md`）、`--append-system-prompt`、记忆索引、skills 列表和延迟工具列表放在每段开头的上下文消息里。段内发生变化（如规则文件被改）时，追加一条“上下文更新”消息，不改写已有内容。`--no-project-rules` 跳过项目规则。
- **投递与 hooks**：`suga send --session <s> <文本>`（或 `--text <文本>`，二选一）把文本送进正在运行的 chat。`<s>` 可用会话 ID、ID 前缀或短名称：每个 chat 注册时会取一个 `<工作目录名>-<两位随机后缀>` 的名字（目录名转小写、只留 `[a-z0-9-]`，如 `suga-k7`），横幅与 `suga ps` 都显示它；名字存在会话元数据里，续接沿用（被另一个活进程占用时临时换一个并在终端说明）。完整 ID 优先于短名称，短名称优先于 ID 前缀；匹配到多个进程时退出码 2。`hooks.command` 以 stdin 上的 JSON 接收 chat 的生命周期事件。

## 配合 Gofer 使用

Gofer 拉起 `suga run`（CLI 作业），或 `suga acp`（常驻会话，通过 `permission_policy` 逐次审批）。agent 配置、只读与可写两种 run、stream-json 事件和拒绝语义见 [docs/usage/gofer-personal-agent.md](docs/usage/gofer-personal-agent.md)。

## 观测

```text
suga trace <session> [--turn <id>] [--json]       # 时间线：模型调用、工具、拒绝、计划
suga sessions [关键词] [-n 10] [--since 7d] [--until 2026-10-01] [--cwd .] [--agents] [--json]  # 会话列表，最新在前；关键词匹配标题、id、目录、模型或对话内容；--agents 连子会话一起列出
suga trace <session> --analyze [--json]          # 循环分析：步数、并行调用、重复读、重复命令、首次通过后的测试、子 agent
suga trace <session> --web [--addr 127.0.0.1:0]  # 本机网页：时间线（后台任务横条、任务通知标注、子 agent 行及其子会话 trace 命令）、详情、上下文组成、分析、本机环境（Ctrl-C 结束）
suga stats [--session <id>] [--since 7d] [--json] # 调用次数、耗时、token、缓存命中
suga export <session> [--redact] [-o <file>]      # 以 jsonl 导出会话内容与事件
suga gc --older-than 30d [--apply]                # 列出（默认）或删除长期未活动的会话
suga ps                                           # 正在运行的 chat 进程（含短名称）
suga manifest [--json] [--model <p/m>]            # 模型、工具、MCP、skills、记忆后端、输出上限
```

工具行显示退出码（shell）、进入上下文的字节数、转存标记与脱敏后的参数摘要；`agent` 行（子 agent 调用）还会显示它运行的会话、类型、步数（连带子 agent 自己的工具调用数）、token 与成本、以及结束原因（`sub=<会话> type=explore steps=12 calls=5 in=85k(cache 60k) out=3k cost=$0.0123 stop=final`），而后台子 agent 的 task 行显示 `agent steps=N calls=M`；模型行显示这次回复发出了几个工具调用（`calls=N`）、被输出上限截断时的 `truncated`、请求重试时的 `retries=N(reason, waited Xs)`、命中缓存的输入 token（`cached=N`），以及请求没有延续上一次时的原因和重新发送的早先内容量（`prefix=fold(rebuilt=12k)`，原因有 fold、segment、system、tools、rewrite）。`--analyze` 从同一组事件得出：步数与并行步数、各工具次数与失败、读了多次的文件和写完又回读的次数、每个文件的编辑次数、重复执行的相同命令、测试运行次数及首次通过之后又跑了几次（输出报告了失败的不算通过，不论退出码）、各阶段的模型调用数（首次写入前的探索、首次写入到测试首次通过、通过之后）、编辑步数及其中只改一处的步数、退出码为 0 但有 stderr 输出的 shell 次数、未命中缓存的输入按来源拆分（上次请求之后新增的内容、前缀断开后重新发送的早先内容（按原因）、前缀本可命中但 provider 没有命中的部分，按请求大小估算）、转存/折叠/压缩/模型重试次数、最慢的几次模型调用，以及本会话子 agent 的开销（调用次数、步数、工具调用数、token、成本与耗时，见 `agent_calls`、`agent_steps`、`agent_tool_calls`、`agent_input_tokens`、`agent_cache_read_tokens`、`agent_output_tokens`、`agent_cost_usd`、`agent_ms`；`agent_stops` 记录各结束原因的分布）。有后台任务时还会补充：任务运行期间循环用于模型与工具调用的时间（`background_overlap_ms`，即省下的等待，只计后台命令，不含监听与 Stop hook；重叠的前台调用只算一次）、`task wait` 的次数与阻塞时长，以及其中在任务启动后立刻 wait 的次数（`immediate_waits`，把后台当前台用）；按种类还会补充本会话的数字：watch 任务数及其运行与变化次数（`watch_tasks`、`watch_runs`、`watch_changes`），以及后台 Stop hook 的运行次数（`stop_hooks`）。`task` 行（后台任务结束）会显示任务的种类：watch 带 `runs=N changes=M` 或最近一次状态，Stop hook 显示它的摘要，agent 任务显示子 agent 跑的步数。工具行还会显示编辑的增删行数（`+新增/-删除`），问过人时显示 `approval=user(wait=3.2s)`。每回合有一行 `context`，列出该回合第一次请求由哪些部分组成（系统提示词、工具及其数量与 MCP 占比、环境、规则、记忆索引、skills、延迟工具、交接摘要、历史、hook 注入、本轮输入的字符数）以及估算 token 与窗口；每次 hook 运行记一行 `hook`（事件、耗时、是否 block、注入的上下文）。回合行记录输入长度及是否为注入而非人工输入。`suga trace` 每回合还会显示一行 `turn_context`（模型、模式、工具集哈希、规则与记忆索引哈希、是否已污染），每次分段显示一行（`segment=#N reason=... tokens=前->后`），以及 `schedule` 事件（chat 空闲整理：fired、done、cancelled、skipped）。`session recovered` 行记录被死进程留在 `running` 的会话被本次运行恢复为 `open`（`turn=` 是那回合未结束的 id，已记为 `cancelled`）：`chat --resume` 与 `run --resume` 靠它才能开始新回合。`suga stats` 显示各模型成本（provider 返回，或按配置的 `price` 计算）；加 `--json` 还有工具输出字节（`raw_bytes`、`inline_bytes`、`spilled`）。

`run --output-format stream-json` 在每次模型调用后输出一行 `usage`，为本次 run 的累计值：`input_tokens`（不含缓存）、`output_tokens`、`cache_read_tokens`、`cache_write_tokens`、`total_tokens`、`cost_usd`、`tool_calls`。`turn_end` 与 `result` 带同名 `usage`，调用方可在运行中途做预算控制。模型请求因瞬时故障重试时会输出一行 `retry`（`reason`、`attempt`、`wait_ms`、`restart`）；`restart: true` 表示这条消息此前输出的 `message_delta` 文本作废，会从头重新流式输出。chat 把这种重试显示为一行 `↻` 提示，`run` 打到 stderr，ACP 作为 thought 块下发。ACP 在同一时机发 `usage_update`（[说明](docs/usage/gofer-personal-agent.md#acp-用量上报)）。因上限收尾而非正常作答的回合会在最后的 `result` 行上带 `wrap_up`（到步数上限时为 `"max_steps"`，正常答复不带该字段）；文本模式把该原因打到 stderr，短答复因此不会被当成完成，也不会被当成超时。

进程诊断日志只写 `$SUGA_HOME/logs/suga.log`（0600、各次运行都追加、不轮转），不再出现在终端：需要让人看到的信息由运行本身打印（会话 id、模型、重试、收尾原因），而 Gofer 把 stdout 当协议读，自由文本的日志行只会让终端或 job 日志变吵。`SUGA_DEBUG=1` 才会打开 debug 记录并往 stderr 再写一份（在 Gofer 的 agent 里就是 `env: {SUGA_DEBUG: "1"}`）；日志文件打不开时无论如何都回退到 stderr。

## 开发

```bash
go test -race ./...     # 单元测试
make smoke-chat         # 伪终端端到端 chat 测试，含 chat_memory 与 chat_idle（Linux/macOS，需 Python 3）
go run ./scripts/eval --fake --bin tmp/smoke/suga   # 任务评测，见 scripts/eval/README.md
make build-all          # 交叉编译到 dist/
```

评测支持多轮任务（`task.yaml` 的 `prompts`，同一会话依次发送），并报告 `first_call_uncached`（表格 FIRST 列：首个主循环模型调用的未缓存输入）。`--probe-prefix` 跑三组前缀缓存探针：同工作区、跨工作区、系统提示词带随机 nonce 的对照组。`--agent omp` 用 omp 跑同样的任务做对比。

- `cmd/suga`：程序入口；`internal/cli`：每个命令一个文件。
- `internal/wiring`：为 CLI 与 ACP 组装 provider、工具、skills、系统提示词和 runner。
- `internal/personal`：回合、分段、交接和事件；`internal/personal/store`：SQLite 存储。
- `internal/chat`：终端对话；`internal/acpserver`：ACP 服务。
- `pkg/agentcore`：应用层之下可复用的各层，不依赖 `internal/*`（[README](pkg/agentcore/README.md)）：内核（模型/工具循环、run 状态、transcript），以及 `llm`（各 provider）、`schedule`（进程内 After/Every/Idle 定时）、`tools`（注册表、审批、延迟加载）、`tools/native`（内置工具）、`tools/mcp`（MCP 客户端）、`skills`、`memory`（存储接口、治理、路由；后端 `memory/files`、`memory/execstore`）、`redact`（密钥与隐形文本清理）和 `extproc`（外部命令的 exec 协议 v1 与凭据剥离）。

版本变更见 [CHANGELOG.md](CHANGELOG.md)；设计与计划文档在 [docs/](docs/README.md)。CI 在 Ubuntu 和 Windows 上用 Go 1.26 和 stable 跑测试。旧的控制面（team、cron、orchestrator）保留在 `v0.1.0` 标签。
