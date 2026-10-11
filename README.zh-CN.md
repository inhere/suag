# Suag

[![Unit-Tests](https://github.com/inhere/suag/actions/workflows/go.yml/badge.svg)](https://github.com/inhere/suag/actions/workflows/go.yml)

[English](README.md) | 简体中文

Suag 是用 Go 写的个人编码 Agent。同一个持久会话引擎提供两种入口：终端 CLI（交互式 `chat` 和单轮 `run`）和 ACP stdio 服务（`acp`）。[Gofer](https://github.com/inhere/gofer) 可以拉起这两种入口，并在其上提供作业、worker、日志和交互式审批。

- **开发工具**：按行分段读文件、列目录、搜索内容与文件名、按字符串精确修改、执行 shell 命令（Windows 上用 PowerShell）、抓取网页、网页搜索。
- **默认安全**：不开启就是只读；chat 写文件、执行命令前会先询问；文件工具限制在工作区内；网页抓取拒绝内网地址。
- **Skills**：读取工作区和 `~/.agents/skills` 下标准的 `SKILL.md`，按需列出或检索。
- **持久会话**：单个 SQLite 库，支持续接、自动压缩、计划、会话交接，以及用于 trace 和统计的事件记录。
- **MCP**：接入 stdio MCP 服务，支持放行名单，工具多时延迟加载。

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

chat 中：`:session` 显示会话 ID，`:compact` 压缩历史，`:quit` 退出。在提示符处按一次 Ctrl+C 清掉已输入的内容，3 秒内再按一次才退出；Ctrl+D 直接退出。退出时会显示 token 用量和续接本会话的命令。

## 配置

Suag 只读取 `$SUAG_HOME/config.yaml` 和 `$SUAG_HOME/.env`（默认 `~/.suag`）。工作目录下的配置和 `.env` 属于你的项目，不会被读取。`SUAG_CONFIG` 可以指定唯一的配置文件。

| 配置项 | 含义 |
|---|---|
| `model` | 主模型，`<provider>/<model>`；可被 `SUAG_MODEL` 覆盖，再被 `--model` 覆盖 |
| `providers.<id>` | `type`（openai/anthropic）、`base_url`、`api_key`、`headers`、`models.<model>.context_window` |
| `compact_model`、`fallback_models` | 生成摘要用的便宜模型；主模型失败时依次尝试的备用模型 |
| `tools.allow_write` | 仅 chat：启用写文件和 shell 工具（默认 false） |
| `tools.approval` | 仅 chat：`ask`（默认）执行写/shell 工具前询问，`auto` 不询问 |
| `tools.max_output_bytes`、`tools.max_spill_bytes` | 单次工具结果完整写入会话的上限（默认 8000，超出时原文存 `sessions/<id>/outputs/`，会话只留 ref 与头尾片段）；单次工具原始输出上限（默认 1 MiB） |
| `tools.shell`、`tools.shell_timeout_max` | 指定 shell（默认 Windows 用 pwsh/powershell，其他系统用 sh）；单条命令最长秒数（600） |
| `tools.web_fetch.proxy`、`tools.web_fetch.allow_private` | `web_fetch` 使用的代理；允许访问内网地址 |
| `tools.auto_allow`、`tools.read_only_allow` | MCP 放行名单，条目形如 `server/工具glob` |
| `tools.deferred` | MCP 工具 schema 是否延迟加载：`auto`（默认）、`always`、`never` |
| `mcp.servers`、`mcp.call_timeout_sec` | 默认 MCP 服务；单次 MCP 调用超时（120） |
| `skills.list_max`、`skills.always_load` | 超过此数量（15）后只列工作区 skills、其余靠检索；预加载正文的 skills |
| `hooks.*` | chat 的 hook 命令（SessionStart、UserPromptSubmit、PostToolUse、Stop 等） |
| `serpapi_key` | 配置后启用 `web_search` |

所有数据都在 `$SUAG_HOME/db/sessions.db`。模型与 provider 的细节见 [LLM 配置](docs/usage/llm-config-v0.md)。

## 工具与权限

| 工具 | 类型 | 作用 |
|---|---|---|
| `file_read` | 读 | 带行号分段读取（`offset`/`limit`，默认 2000 行）；二进制文件只报告不输出 |
| `file_list` | 读 | 列目录，`depth`（≤ 5）可递归 |
| `file_grep` | 读 | RE2 正则搜索，支持 `glob`、`ignore_case`、`output: content\|files` |
| `file_find` | 读 | 支持 `**` 的 glob 查找，按修改时间从新到旧 |
| `web_fetch` | 读 | 抓取网页正文（保留标题、链接、代码块）；拒绝内网和本机地址 |
| `web_search` | 读 | SerpAPI 搜索（需 `serpapi_key`） |
| `skill` | 读 | 加载 skill、读取其文件，或检索 skills（`query`；`"*"` 列出全部名字） |
| `file_write` | 写 | 整文件写入 |
| `file_edit` | 写 | 替换一处唯一的字符串（`replace_all` 替换全部）；保留 CRLF |
| `shell_exec` | 写 | 执行命令（Windows 用 PowerShell，其他系统用 `sh`），带超时；返回输出与退出码 |

- **默认只读**：写工具只在 `run --allow-write`、chat 设置 `tools.allow_write: true`、或 ACP 会话处于 `default` 模式时才有；`--read-only` 永远优先。模型调用了未开启的工具时，会被告知开启方法，并计入拒绝次数。
- **chat 先询问**：`tools.approval: ask` 时，每次写文件或执行命令前都会询问，并显示完整命令、路径或改动前后的文本。回答 `y` 允许一次，`a` 在本进程内对该工具始终允许，其他任何回答都是拒绝。一轮里被拒 3 次就结束这一轮。
- **边界**：文件工具经 `os.OpenRoot` 访问，不会顺着 symlink 跑出工作区；`shell_exec` 超时会终止整个进程树；`web_fetch` 检查实际连接的 IP，跳转也一样受限；所有工具结果都有上限。
- **MCP**：用 `--mcp-config <file>`（Claude 风格的 `mcpServers` JSON）或配置里的 `mcp.servers` 接入。MCP 调用默认一律拒绝，需在 `tools.auto_allow`（`--read-only` 时用 `tools.read_only_allow`）中放行。工具很多时会延迟加载，模型通过 `tool_search` 取用。

完整语义见 [Gofer 接入与工具说明](docs/usage/gofer-personal-agent.md#内置工具与相关配置)。

## Skills

启动时扫描一次 `<工作区>/.agents/skills`、`<工作区>/skills` 和 `~/.agents/skills`，同名时工作区的优先。skills 数量不超过 `skills.list_max` 时全部列在系统提示词里；超过时只列工作区的，其余由模型用 `skill(query=…)` 检索。要让某个 skill 的正文每次都放进提示词，可在其 frontmatter 写 `metadata: {always_load: true}`，或把名字加进 `skills.always_load`。

## 会话

- **续接与交接**：`--session-id` 创建或复用指定 ID 的会话；`--resume` 要求会话已存在，不存在时以退出码 4 退出。`run --from <session>` 新建一个会话，继承来源会话的摘要、计划和已加载的工具。
- **压缩**：上下文接近模型窗口时，较早的历史会自动生成摘要；chat 中 `:compact` 可手动触发。
- **计划**：`update_plan` 工具记录模型的计划。stream-json 中输出为 `plan` 事件，ACP 中转为计划更新。
- **系统提示词**：每个进程生成一次，依次包含系统、shell、工作区、读写模式和使用约定，然后是项目规则（`AGENTS.md`、`CLAUDE.md`、`~/.suag/AGENTS.md`）和 `--append-system-prompt`。`--no-project-rules` 跳过项目规则。
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
```

## 开发

```bash
go test -race ./...     # 单元测试
make smoke-chat         # 伪终端端到端 chat 测试（Linux/macOS，需 Python 3）
make build-all          # 交叉编译到 dist/
```

- `cmd/suag`：程序入口；`internal/cli`：每个命令一个文件。
- `internal/wiring`：为 CLI 与 ACP 组装 provider、工具、skills、系统提示词和 runner。
- `internal/personal`：回合、压缩、交接和事件；`internal/personal/store`：SQLite 存储。
- `internal/chat`：终端对话；`internal/acpserver`：ACP 服务。
- `pkg/agentcore`：应用层之下可复用的各层，不依赖 `internal/*`（[README](pkg/agentcore/README.md)）：内核（模型/工具循环、run 状态、transcript），以及 `llm`（各 provider）、`tools`（注册表、审批、延迟加载）、`tools/native`（内置工具）、`tools/mcp`（MCP 客户端）与 `skills`。

版本变更见 [CHANGELOG.md](CHANGELOG.md)；设计与计划文档在 [docs/](docs/README.md)。CI 在 Ubuntu 和 Windows 上用 Go 1.26 和 stable 跑测试。旧的控制面（team、cron、orchestrator）保留在 `v0.1.0` 标签。
