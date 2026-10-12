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
| `providers.<id>` | `type`（openai/anthropic）、`base_url`、`api_key`、`headers`、`models.<model>.context_window` |
| `providers.<id>.models.<model>.price` | 每百万 token 的美元价格（`input`、`output`、`cache_read`、`cache_write`）；provider 不返回成本时，`suag stats` 据此计算 |
| `providers.<id>.thinking`、`providers.<id>.models.<model>.thinking` | 思考档位 `off\|minimal\|low\|medium\|high\|xhigh\|max\|auto`，`openai` 类型作为 `reasoning_effort` 发送（不设或 `auto` 不发送）；`run`/`chat --thinking` 可覆盖主模型的档位 |
| `providers.<id>.context_window` | 该 provider 所有模型的默认上下文窗口（模型级 `models.<model>.context_window` 优先；都没有时按模型名推断） |
| `providers.<id>.models.<model>.model_id` | 请求里发送的模型 id（与键不同时），如键 `deepseek-v4.1-flash`、`model_id: deepseek/deepseek-v4.1-flash`；其余地方都用键 |
| `compact_model`、`fallback_models` | 生成摘要用的便宜模型；主模型失败时依次尝试的备用模型 |
| `context.fold_every`、`context.fold_keep` | 历史折叠（默认开启，`10` / `5`；`fold_every: 0` 关闭）：较早步骤发送时去掉思考内容，过长的字符串参数换成一句说明。只改发给模型的请求，存储的历史保持完整 |
| `context.idle_after`、`context.idle_min_ratio` | 仅 chat：安静超过此时长（默认 `15m`，`0` 关闭），且上下文已达分段阈值的此比例（默认 `0.5`）时，在后台开启新段 |
| `providers.<id>.prompt_cache` | 仅 Claude（`anthropic` 类型），默认关：加 `cache_control` 断点（system 变为块数组）；部分网关不接受 |
| `tools.allow_write` | 仅 chat：启用写文件和 shell 工具（默认 false） |
| `tools.approval` | 仅 chat：`ask`（默认）执行写/shell 工具前询问，`auto` 不询问 |
| `tools.max_output_bytes`、`tools.max_spill_bytes` | 内联阈值：单次工具结果完整写入会话的上限（默认 8000，超出时原文存 `$SUAG_HOME/sessions/<id>/outputs/`，会话只留 ref 与头尾片段）；单次工具原始输出上限（默认 1 MiB） |
| `tools.shell`、`tools.shell_timeout_max` | 指定 shell（默认 Windows 用 pwsh/powershell，其他系统用 sh）；单条命令最长秒数（600） |
| `tools.dependency_roots`、`tools.read_roots` | 工作区外的只读目录，`file_read`/`file_list`/`file_grep`/`file_find` 可用绝对路径读取：自动识别的 Go 模块缓存、Go 标准库源码与 Rust crate 源码（默认 `true`，`false` 关闭识别），以及额外目录列表（支持 `~`）。写入仍只限工作区；这些目录会列在提示词的环境一节 |
| `tools.web_fetch.proxy`、`tools.web_fetch.allow_private` | `web_fetch` 使用的代理；允许访问内网地址 |
| `tools.auto_allow`、`tools.read_only_allow` | MCP 放行名单，条目形如 `server/工具glob` |
| `tools.deferred` | MCP 工具 schema 是否延迟加载：`auto`（默认）、`always`、`never` |
| `mcp.servers`、`mcp.call_timeout_sec` | 默认 MCP 服务；单次 MCP 调用超时（120） |
| `skills.list_max`、`skills.always_load` | 超过此数量（15）后只列工作区 skills、其余靠检索；预加载正文的 skills |
| `hooks.*` | chat 的 hook 命令（SessionStart、UserPromptSubmit、PostToolUse、Stop 等） |
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

`file_grep`、`file_find` 与递归的 `file_list` 跳过 `.git`、`node_modules`、`vendor`、`dist` 和 `.gitignore` 匹配的路径（各级目录的 `.gitignore`，支持 `!` 与 `**`）；结果末尾一行注明跳过了什么，传 `no_ignore: true` 可包含它们（`.git` 除外）。
| `web_fetch` | 读 | 抓取网页正文（保留标题、链接、代码块）；拒绝内网和本机地址 |
| `web_search` | 读 | SerpAPI 搜索（需 `serpapi_key`） |
| `skill` | 读 | 加载 skill、读取其文件，或检索 skills（`query`；`"*"` 列出全部名字） |
| `session_search` | 读 | 全文检索过往消息、工具结果与摘要（`scope`：workspace、current、all）；带 `ref=out:...` 时在那份已保存的输出内检索 |
| `recall` | 读 | 按 ref 读取命中附近内容：`<session>/<seq>` 起的消息，或已保存输出 `out:<session>:<hex>` 的带行号文本 |
| `suag_manifest` | 读 | 以 JSON 返回本实例的能力清单（与 `suag manifest` 相同） |
| `memory` | 状态 | 对长期记忆 `add`、`replace`、`remove`、`get`、`search`（`scope` 为 project 或 user），见[记忆](#记忆) |
| `file_write` | 写 | 整文件写入 |
| `file_edit` | 写 | 替换一处唯一的字符串（`replace_all` 替换全部）；保留 CRLF |
| `shell_exec` | 写 | 执行命令（Windows 用 PowerShell，其他系统用 `sh`），带超时；返回输出与退出码 |

- **默认只读**：写工具只在 `run --allow-write`、chat 设置 `tools.allow_write: true`、或 ACP 会话处于 `default` 模式时才有；`--read-only` 永远优先。模型调用了未开启的工具时，会被告知开启方法，并计入拒绝次数。
- **chat 先询问**：`tools.approval: ask` 时，每次写文件或执行命令前都会询问，并显示完整命令、路径或改动前后的文本。回答 `y` 允许一次，`a` 在本进程内对该工具始终允许，其他任何回答都是拒绝。一轮里被拒 3 次就结束这一轮。
- **大输出**：工具结果超过 `tools.max_output_bytes` 时，脱敏后的原文存到 `$SUAG_HOME/sessions/<id>/outputs/`，上下文只留头尾（各 2000 字节）和 ref（形如 `out:<session>:<hex>`）；用 `recall` 读取，用 `session_search` 检索。`file_read` 自己分页，不会被外置。
- **边界**：文件工具经 `os.OpenRoot` 访问，不会顺着 symlink 跑出工作区；`shell_exec` 超时会终止整个进程树；`web_fetch` 检查实际连接的 IP，跳转也一样受限；所有工具结果都有上限。
- **MCP**：用 `--mcp-config <file>`（Claude 风格的 `mcpServers` JSON）或配置里的 `mcp.servers` 接入。MCP 调用默认一律拒绝，需在 `tools.auto_allow`（`--read-only` 时用 `tools.read_only_allow`）中放行。工具很多时会延迟加载，模型通过 `tool_search` 取用。

完整语义见 [Gofer 接入与工具说明](docs/usage/gofer-personal-agent.md#内置工具与相关配置)。

## Skills

启动时扫描一次 `<工作区>/.agents/skills`、`<工作区>/skills` 和 `~/.agents/skills`，同名时工作区的优先。skills 数量不超过 `skills.list_max` 时全部列在系统提示词里；超过时只列工作区的，其余由模型用 `skill(query=…)` 检索。要让某个 skill 的正文每次都放进提示词，可在其 frontmatter 写 `metadata: {always_load: true}`，或把名字加进 `skills.always_load`。

## 记忆

跨会话共享的长期记忆，由模型通过 `memory` 工具写入。

- **四类条目**：`user`（用户是谁、偏好）、`feedback`（纠正与确认过的做法）、`project`（代码里看不出的目标、决定、进展）、`reference`（外部资源在哪）。不记：能从代码或 git 推出的内容、规则文件已有的内容、本次会话的临时细节、密钥。流程类内容应写成 skill。
- **范围与后端**：用户记忆在 `$SUAG_HOME/memory/user`（files）。项目记忆按顺序选后端：`memory.project.backend` 的配置；仓库根有 `MEMORY.md`（files，仓库模式）；仓库根有 `.gofer/tracker`（gofer）；否则用 `$SUAG_HOME/projects/<key>/memory` 下的 files。仓库模式下索引就是 `MEMORY.md`，每条记忆放在 `.suag/memory/<id>.md`，可随项目提交；根目录的 `MEMORY.md` 不会由 suag 创建。gofer 后端中每条项目记忆是带 `suag` 标签的 gofer memory，只管理这些条目。`suag manifest` 会显示选了哪个后端及原因。
- **冻结的索引**：记忆索引（仅标题）放在段上下文消息里，本段内不变；本会话写入的内容下一段才可见，开启新段时刷新。正文用 `memory get` 读取。
- **污染与确认**：会话一旦读过外部内容（`web_fetch`、`web_search`、MCP 工具、`session_search`、`recall`，以及 `memory.taint_tools` / `memory.taint_on_spill` 中的工具），就永久标记为已污染，此后写记忆需要确认（`memory.write_tainted`）：chat 在终端询问（`y`、`n`，`a` 表示本会话不再问），ACP 发权限请求，`run` 直接拒绝；`deny` 永不写入，`allow` 不询问。`--read-only`（及 ACP `read_only`）下该工具只读。普通 `run` 可以写记忆：它是 suag 自身的状态，不属于工作区写入。

## 会话

- **续接与交接**：`--session-id` 创建或复用指定 ID 的会话；`--resume` 要求会话已存在，不存在时以退出码 4 退出。`run --from <session>` 新建一个会话，继承来源会话的摘要、计划和已加载的工具。
- **分段**：会话是一串段。每段以一条段起始记录开头（原因：开始、阈值、`:compact`、空闲、`--from`），记录里有上下文消息；第一段之后还有交接包：面向当前任务的摘要、最近的用户请求原文和仍相关的 ref。上下文接近模型窗口或执行 `:compact` 时开启新段，原样保留的只有最后一组完整的工具调用/结果。旧段仍在库里，用 `session_search` 和 `recall` 取回。
- **历史折叠**：每次请求都会重发此前的对话，其中大部分是模型自己的思考内容和整文件写入。`context.fold_every`（默认 10）与 `context.fold_keep`（默认 5）开启后，最近 5 步之前的步骤发送时去掉思考内容，超过 1000 字符的字符串参数（整文件写入、长编辑）换成一句说明，提示需要时重读文件。折叠点每 10 步才前移一次，期间缓存前缀保持有效。存储的会话不变；折叠点前移时 `suag trace` 显示一条 `fold`。T9 对比中每次请求的上下文减半、prompt 总量下降 59%（见 docs/research/2026-10-11-suag-vs-omp-loop-analysis.md）。`context.fold_every: 0` 关闭。
- **空闲整理（chat）**：超过 `context.idle_after`（默认 15m）没有输入，且上下文已达阈值的 `context.idle_min_ratio`（默认 0.5）时，chat 在后台开启新段。回合进行中不会触发，开始输入即取消。
- **计划**：`update_plan` 工具记录模型的计划。stream-json 中输出为 `plan` 事件，ACP 中转为计划更新。
- **系统提示词与段上下文**：系统提示词只含稳定内容（身份与工作约定），同一模式下跨会话、跨工作区字节相同。环境（系统、shell、工作区）、项目规则（`AGENTS.md`、`CLAUDE.md`、`~/.suag/AGENTS.md`）、`--append-system-prompt`、记忆索引、skills 列表和延迟工具列表放在每段开头的上下文消息里。段内发生变化（如规则文件被改）时，追加一条“上下文更新”消息，不改写已有内容。`--no-project-rules` 跳过项目规则。
- **投递与 hooks**：`suag send --session <s> --text <t>` 把文本送进正在运行的 chat；`hooks.command` 以 stdin 上的 JSON 接收 chat 的生命周期事件。

## 配合 Gofer 使用

Gofer 拉起 `suag run`（CLI 作业），或 `suag acp`（常驻会话，通过 `permission_policy` 逐次审批）。agent 配置、只读与可写两种 run、stream-json 事件和拒绝语义见 [docs/usage/gofer-personal-agent.md](docs/usage/gofer-personal-agent.md)。

## 观测

```text
suag trace <session> [--turn <id>] [--json]       # 时间线：模型调用、工具、拒绝、计划
suag stats [--session <id>] [--since 7d] [--json] # 调用次数、耗时、token、缓存命中
suag export <session> [--redact] [-o <file>]      # 以 jsonl 导出会话内容与事件
suag gc --older-than 30d [--apply]                # 列出（默认）或删除长期未活动的会话
suag ps                                           # 正在运行的 chat 进程
suag manifest [--json] [--model <p/m>]            # 模型、工具、MCP、skills、记忆后端、输出上限
```

`suag trace` 每回合还会显示一行 `turn_context`（模型、模式、工具集哈希、规则与记忆索引哈希、是否已污染），每次分段显示一行（`segment=#N reason=... tokens=前->后`），以及 `schedule` 事件（chat 空闲整理：fired、done、cancelled、skipped）。`suag stats` 显示各模型成本（provider 返回，或按配置的 `price` 计算）；加 `--json` 还有工具输出字节（`raw_bytes`、`inline_bytes`、`spilled`）。

`run --output-format stream-json` 在每次模型调用后输出一行 `usage`，为本次 run 的累计值：`input_tokens`（不含缓存）、`output_tokens`、`cache_read_tokens`、`cache_write_tokens`、`total_tokens`、`cost_usd`、`tool_calls`。`turn_end` 与 `result` 带同名 `usage`，调用方可在运行中途做预算控制。ACP 在同一时机发 `usage_update`（[说明](docs/usage/gofer-personal-agent.md#acp-用量上报)）。

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
