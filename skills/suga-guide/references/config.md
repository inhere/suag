# suga 完整配置参考

本文是 `$SUGA_HOME/config.yaml` 的完整键表（默认值、取值、作用范围），外加环境变量、目录布局与常用命令。键名与默认值取自当前代码；样例文件另有仓库里的 `config/config.yaml`（带注释的字段样例）。

## 文件位置与优先级

- suga **只加载** `$SUGA_HOME/config.yaml` 与 `$SUGA_HOME/.env`，`SUGA_HOME` 默认 `~/.suga`。当前工作目录的 `config.yaml` / `.env` 不会被读取。
- `SUGA_CONFIG` 指定唯一的配置文件（此时不读 `$SUGA_HOME/config.yaml`）。
- `.env` 里的值会进入进程环境，供配置里的 `${VAR}` 展开；`SUGA_HOME`、`SUGA_CONFIG` 例外，只认进程环境里最初的值（避免 `.env` 改写数据根）。
- 主模型优先级：`--model` > `SUGA_MODEL` > `model`。
- 值类型写错通常是**启动报错**，而不是回退默认值（如 `tools.allow_write`、`tools.approval`、`context.fold_keep`、`memory.write`、`retry.*`、`agents.*`、`tasks.*`）。

## 模型与 provider

| 键 | 默认 | 含义 |
|---|---|---|
| `model` | 无（必填，或用 SUGA_MODEL/--model） | 主模型，`<provider>/<model>` |
| `compact_model` | 不设（用当前回合的模型） | 生成分段摘要（自动压缩、`:compact`、空闲整理）的模型，引用规则同 `model`，裸 id 用主模型的 provider |
| `fallback_models` | 不设 | 主模型失败后按顺序尝试的模型列表（会先重试 `retry` 次） |
| `providers.<id>` | 内置预设 openai / anthropic / deepseek | provider 定义，逐字段覆盖同名预设；其它 id 必须写 `type` |
| `providers.<id>.type` | 预设自带；其它 id 必填 | `openai`（`/chat/completions`）、`openai-responses`（Responses API `/responses`）、`anthropic`（`/v1/messages`） |
| `providers.<id>.compat` | 按 base_url/模型 id 自动匹配 | 网关适配名列表，自动匹配不上时在此追加 |
| `providers.<id>.base_url` | 预设自带 | 地址，支持 `${VAR}` |
| `providers.<id>.api_key` | 预设读对应环境变量 | 密钥，支持 `${VAR}` |
| `providers.<id>.headers` | `User-Agent: suga` | 附加请求头，值支持 `${VAR}` 与 `{{session_id}}` |
| `providers.<id>.prompt_cache` | `false` | 仅 `anthropic` 类型：加 `cache_control` 断点（system 变块数组），部分网关不接受 |
| `providers.<id>.context_window` | 按模型名推断 | 该 provider 所有模型的默认上下文窗口 |
| `providers.<id>.thinking` | 不设（不发送） | 思考档位（provider 级默认） |
| `providers.<id>.models.<model>.context_window` | 按模型名推断 | 该模型的上下文窗口（优先级：模型级 > provider 级 > 名字推断） |
| `providers.<id>.models.<model>.model_id` | 不设（发送键名） | 请求里实际发送的模型 id，便于用短名引用长 id |
| `providers.<id>.models.<model>.type`、`base_url` | 继承 provider | 该模型自己的协议与地址（同一网关的 Claude 模型可单独走 Anthropic 端点） |
| `providers.<id>.models.<model>.price` | 不设 | `{input, output, cache_read, cache_write}`，每百万 token 的美元价；provider 不返回成本时 `suga stats` 用它算 |
| `providers.<id>.models.<model>.thinking` | 不设 | 该模型的思考档位 |

思考档位取值：`off`、`minimal`、`low`、`medium`、`high`、`xhigh`、`max`、`auto`。优先级 `--thinking`（只作用于主模型）> 模型级 > provider 级；不设或 `auto` 不发送。细节与网关示例见 `references/providers.md`。

## 重试与上下文

| 键 | 默认 | 含义 |
|---|---|---|
| `retry.max_attempts` | `6` | 一次模型请求的总尝试次数（含首次）；`1` 关闭重试 |
| `retry.base_delay` | `1s` | 退避起始等待（每次翻倍，±20% 抖动） |
| `retry.max_delay` | `60s` | 退避等待上限（服务端 `Retry-After` 同样受此限制） |
| `retry.reset_after` | `2m` | 连续这么久没有失败就把等待档位回到 `base_delay` |
| `context.fold_every` | `0`（关） | 每 N 步前移一次历史折叠点（如 `10` 开启） |
| `context.fold_keep` | `5`（≥1） | 最近多少步保持完整 |
| `context.idle_after` | `15m`（`0` 关） | 仅 chat：空闲这么久且上下文达到阈值比例时在后台开启新段 |
| `context.idle_min_ratio` | `0.5` | 空闲整理要求的上下文占比（0..1） |

- 重试覆盖所有 provider，触发条件：网络错误、HTTP 408/429/5xx、响应流中断。
- 历史折叠只改发给模型的请求（去掉较早步骤的思考内容、把超过 1000 字符的字符串参数换成说明），存储的历史完整；主模型为 `anthropic` 类型时不生效（签名绑定请求前缀）。

## 其它顶层键

| 键 | 默认 | 含义 |
|---|---|---|
| `workspace` | `""`（当前工作目录） | 文件与 shell 工具的工作区根 |
| `rules.file` | `AGENTS.md` | 每级目录读取的项目规则文件名（只接受文件名，不能带路径），如 `CLAUDE.md`；`$SUGA_HOME/AGENTS.md` 始终读取 |
| `max_steps` | `240` | 一次任务（一轮，不是整个会话）的最大 ReAct 循环步数；可被环境变量 `SUGA_MAX_STEPS` 与 `run --max-steps N` 覆盖（子 agent 用 `agents.max_steps`），到顶时要求模型收尾并在答复开头写明是步数上限 |
| `serpapi_key` | `""` | 配置后注册 `web_search`（支持 `${VAR}`） |
| `transcripts` | 开 | 会话记录（transcript）写到 `$SUGA_HOME/transcripts`；`false` 或 `"off"` 关闭（键值接受布尔或字符串） |

## `chat.*`（仅 chat 的终端显示）

| 键 | 默认 | 含义 |
|---|---|---|
| `chat.markdown` | `true` | 在终端渲染模型输出的 markdown（标题、粗体、行内代码、代码块、列表、引用、链接） |
| `chat.spinner` | `true` | 等待模型输出时显示“思考中… Ns”，正文/工具调用/审批问题之前清除 |
| `chat.highlight_input` | `true` | 提示符提交的那行（含粘贴的多行）与外部投递的消息（`suga send`、Stop hook 回复、任务通知）整行加低饱和背景，与模型回答区分；两者背景色不同 |

三个键都接受布尔或字符串（`"off"` / `"false"` 关闭）。都只在彩色终端生效：非终端、重定向输出或 `NO_COLOR` 时按原文显示，不加任何转义。

## `tools.*`

| 键 | 默认 | 含义 / 作用范围 |
|---|---|---|
| `tools.allow_write` | `false` | **仅 chat**：启用写文件与命令工具。`run` 需 `--allow-write`；ACP 看会话 mode；`--read-only` 优先 |
| `tools.approval` | `ask` | **仅 chat**：`ask` 时每个非只读、非 MCP 工具调用前在终端询问；`auto` 不问。无终端时需确认的工具被拒 |
| `tools.deferred` | `auto` | MCP 工具 schema 延迟加载：`auto`（schema 超过上下文窗口约 10% 时延迟）、`always`、`never`；被延迟的工具用 `tool_search` 取回 |
| `tools.max_output_bytes` | `8000` | 单次工具结果内联进会话的上限；超出时脱敏原文存 `$SUGA_HOME/sessions/<id>/outputs/`，会话只留 ref 与头尾各 2000 字节 |
| `tools.max_spill_bytes` | `1048576`（1 MiB） | 单次工具（含 MCP）原始输出上限 |
| `tools.parallel_reads` | `true` | 同一次回复里的多个本地读调用（file_read/list/grep/find）并发执行 |
| `tools.read_page_bytes` | `32768` | `file_read` 每页字节数（它自己分页，不受 `max_output_bytes` 限制） |
| `tools.minimize_output` | `true` | shell 结果进上下文前整理（去色、折叠重复行、按输出规则裁剪测试/构建输出），原文留 ref |
| `tools.shell` | `""`（Windows：pwsh 优先，否则 powershell；其它：sh） | `shell_exec` 使用的 shell（命令名或路径） |
| `tools.shell_timeout_max` | `600` | `shell_exec` 单次 `timeout` 的上限秒数（单次默认 120） |
| `tools.dependency_roots` | `true` | 自动识别工作区外的只读目录（Go 模块缓存、`GOROOT/src`、Rust crate 源码） |
| `tools.read_roots` | `[]` | 额外只读目录列表（绝对路径，支持 `~`）。只影响 file_read/list/grep/find |
| `tools.web_fetch.allow_private` | `false` | 允许 `web_fetch` 访问私网/本机地址 |
| `tools.web_fetch.proxy` | `""` | `web_fetch` 的代理（`http://`、`https://`、`socks5://`）；只影响 web_fetch，不影响模型请求 |
| `tools.auto_allow` | `[]` | MCP 放行名单（普通运行），条目 `server/toolglob`（裸 `toolglob` 也接受，会匹配任意 server） |
| `tools.read_only_allow` | `[]` | `--read-only` 时**唯一**使用的放行名单，条目必须写成 `server/toolglob` |

输出规则文件：内置规则 → `$SUGA_HOME/output-rules.yaml` → `<workspace>/.suga/output-rules.yaml`（同名替换、新名追加、`enabled: false` 关闭）。

## `mcp.*`

| 键 | 默认 | 含义 |
|---|---|---|
| `mcp.servers` | `[]` | 默认 MCP 服务列表；每项 `id`、`command`、`args`、`env` |
| `mcp.call_timeout_sec` | `120` | 单次 MCP 工具调用超时（秒） |

命令行也可用 `--mcp-config <Claude 风格 JSON>` 临时接入（可重复）。MCP 子进程继承启动环境，但 `*_API_KEY` / `*_AUTH_TOKEN` 与 suga 读取的 LLM 密钥变量被剔除。

## `skills.*`

| 键 | 默认 | 含义 |
|---|---|---|
| `skills.list_max` | `15` | 带描述列出的 skill 数量上限；超过后只列工作区 skills（带描述），其余只列名字（名字合计上限 3000 字节） |
| `skills.always_load` | `[]` | 正文每次进提示词的 skill 名列表（等价于 SKILL.md 的 `metadata: {always_load: true}`），每个正文上限 8000 字节 |

扫描根目录顺序：`<工作区>/.agents/skills`、`<工作区>/skills` → `$SUGA_HOME/skills` → `~/.agents/skills`；同名先到者胜。

## `agents.*`（子 agent）

| 键 | 默认 | 含义 |
|---|---|---|
| `agents.enabled` | `true` | 是否注册 `agent` 工具 |
| `agents.model` | 不设（会话自身的模型） | 子 agent 使用的模型，写法同 `model`（裸 id 用主模型 provider） |
| `agents.max_steps` | `30` | 步数上限，达到后要求收尾（不受 `SUGA_MAX_STEPS` / `run --max-steps` 影响） |
| `agents.max_tokens` | `400000` | 未命中缓存的输入 + 输出上限，达到后要求收尾 |
| `agents.timeout` | `10m` | 软时间上限（之后 60 秒宽限） |

## `tasks.*`（后台任务）

| 键 | 默认 | 含义 |
|---|---|---|
| `tasks.max_running` | `4` | 每会话同时运行的任务数 |
| `tasks.max_duration` | `2h` | 单个任务最长运行时间 |
| `tasks.max_output_bytes` | `67108864`（64 MiB） | 任务输出文件上限 |
| `tasks.wake_on_exit` | `true` | chat 等待输入时任务结束/监听结果变化的通知会唤醒并开始一轮 |

## `memory.*`

| 键 | 默认 | 含义 |
|---|---|---|
| `memory.write` | `auto` | `auto` 允许 memory 工具写入；`off` 让工具整体只读 |
| `memory.write_tainted` | `ask` | 读过外部内容的会话写记忆时：`ask`（chat 询问 / ACP 发权限请求 / run 拒绝）、`deny`、`allow` |
| `memory.taint_tools` | `[]` | 额外把会话标记为已污染的工具（每次结果都算） |
| `memory.taint_on_spill` | `[shell_exec]` | 仅当输出被外置成文件时才污染会话的工具；显式空列表可关闭 |
| `memory.user.backend` | `files` | `files`（`$SUGA_HOME/memory/user`）或 `exec` |
| `memory.project.backend` | 自动（见下） | `files`、`gofer` 或 `exec` |
| `memory.<scope>.command` | 无（`exec` 时必填） | 外部命令 |
| `memory.<scope>.args` | `[]` | 外部命令参数 |
| `memory.<scope>.timeout_sec` | 不设 | 外部命令超时秒数 |

`memory.project.backend` 不设时的选择顺序：仓库根有 `MEMORY.md` → `files`（仓库模式，条目在 `.suga/memory/<id>.md`，可随项目提交）；否则仓库根有 `.gofer/tracker` → `gofer`；否则 `$SUGA_HOME/projects/<key>/memory` 的 `files`（`<key>` = 仓库目录名 + 路径短哈希）。`suga manifest` 显示结果与原因。

## `hooks.*`（仅 chat）

| 键 | 默认 | 含义 |
|---|---|---|
| `hooks.command` | 不设（不启用） | hook 命令，argv 列表（不经 shell）；事件 JSON 写在 stdin 上 |
| `hooks.timeout_ms` | `3000` | 普通事件的超时 |
| `hooks.stop_args` | `[]` | 只追加到 Stop 事件 argv；**必须**与 `stop_wait_sec` 同时配，否则 Stop 按 `timeout_ms` 结束 |
| `hooks.stop_wait_sec` | 不设 | 设置了它时 Stop 最多等 `stop_wait_sec+2` 秒 |
| `hooks.stop_background` | `false` | 把 Stop 作为后台任务运行（打字、投递、任务唤醒都不打断它）；此时超时由 `tasks.max_duration` 限制，除非设了 `stop_wait_sec` |
| `hooks.post_tool_use.tools` | `[]`（不发） | 只对这些 glob 匹配的工具发送 PostToolUse |
| `hooks.post_tool_use.name` | 不设 | 设置了就把 PostToolUse 事件里的工具名改成它 |

事件：SessionStart、UserPromptSubmit、PostToolUse、Stop、SessionEnd、Interrupt。`SUGA_HOOKS=off` 整体关闭。

## 环境变量

| 变量 | 含义 |
|---|---|
| `SUGA_HOME` | 数据与配置根，默认 `~/.suga`（会话库 `$SUGA_HOME/db/sessions.db` 等都在其下） |
| `SUGA_CONFIG` | 指定唯一的配置文件路径（替代 `$SUGA_HOME/config.yaml`） |
| `SUGA_MODEL` | 主模型，`<provider>/<model>`；被 `--model` 覆盖，覆盖配置 `model` |
| `SUGA_HOOKS` | `off` 时整体关闭 chat hooks |
| `SUGA_APPEND_SYSTEM_PROMPT` | 默认的追加系统提示（命令行的 `--append-system-prompt` 优先；非空时也覆盖会话已保存的那份） |
| `SUGA_DEBUG` | 非空时打开 debug 日志（含 `DEBUG MODE ENABLED` 行） |
| `SUGA_PROVIDER` | **已移除**：仍有值时启动报错，改用 `SUGA_MODEL=<provider>/<model>` |
| `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` / `DEEPSEEK_API_KEY` | 三个内置预设的默认密钥变量 |
| `{VAR}` | 配置里 `api_key`、`base_url`、`headers` 的值支持 `${VAR}`，来自进程环境或 `$SUGA_HOME/.env` |

密钥不要写进命令行参数（会被进程列表、job 请求记录持久化）；放进 `.env` 或进程环境。

## 目录布局（`$SUGA_HOME` 下）

| 路径 | 内容 |
|---|---|
| `config.yaml`、`.env` | 配置与密钥 |
| `db/sessions.db` | 全部会话（单个 SQLite 库，WAL） |
| `sessions/<session-id>/outputs/` | 外置的大工具输出（`recall` 用 `out:<session>:<hex>` 读） |
| `memory/user/` | 用户级记忆（files 后端） |
| `projects/<key>/memory/` | 项目记忆（files 后端，非仓库模式） |
| `trash/` | `file_remove` 的回收区（保留 7 天，`origin.json` 记录来源） |
| `run/` | chat 进程登记与锁（`suga ps`、`suga send` 用） |
| `skills/` | 释放的内置 skill 与用户自己的 skill |
| `transcripts/` | chat 会话记录（`transcripts: false` 关闭） |
| `AGENTS.md` | 用户级项目规则（每级规则文件之外额外读取） |
| `output-rules.yaml` | 用户级输出整理规则 |

## 命令与常用 flag

```text
suga                      # 等价 suga chat（交互式对话）
suga chat [--resume <id>|--session-id <id>] [--model <p/m>] [--thinking <level>]
suga run --prompt "..."   # 单轮；[--allow-write] [--read-only] [--model] [--thinking]
                          # [--session-id <id>] [--resume <id>] [--from <id>]
                          # [--output-format text|stream-json] [--mcp-config <file>]
                          # [--append-system-prompt <text>] [--clear-append-system-prompt]
                          # [--no-project-rules] [--injected] [--max-steps <N>]
suga acp                  # 以 ACP stdio 服务（供 gofer 等客户端接入）
suga send --session <id|前缀> --text <文本>   # 给正在运行的 chat 送话（--wait 等真正开始）
suga ps                   # 正在运行的 chat 进程
suga sessions [关键词] [-n 10] [--since 7d] [--cwd .] [--agents] [--json]
suga trace <session> [--turn <id>] [--json] [--analyze] [--web]
suga stats [--session <id>] [--since 7d] [--json]
suga export <session> [--redact] [-o <file>]
suga gc --older-than 30d [--apply]
suga manifest [--json] [--model <p/m>] [--thinking <level>]
suga version
```

chat 里的命令：`:session`（会话 ID）、`:compact`（开启新段）、`:quit` / `:exit`（退出）。退出码：会话不存在 `4`、被中断 `130`、其它 `1`。
