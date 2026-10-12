# 在 Gofer 中运行 Suag

Suag 提供两个 Gofer agent：`suag` 执行一次性 CLI 任务，`suag-acp` 提供常驻 ACP 会话。两者共用个人会话数据，默认位于启动 Suag 的用户主目录 `~/.suag/db/sessions.db`（单个 SQLite 库，WAL）。Gofer job ID 是执行记录，Suag session ID 是对话记录。

## 准备二进制与配置

在 Suag 仓库执行 `make install`。它将二进制安装到 `$(go env GOPATH)/bin`；先确认该目录位于终端和 Gofer server 进程的 PATH 中，再在新终端执行 `suag run --help`。主机 Gofer 配置的 `agents` 下增加以下两项：

```yaml
agents:
  suag:
    type: cli-agent
    command: suag
    args: [run, --output-format, stream-json, --append-system-prompt, "{{system_prompt}}", --prompt, "{{prompt}}"]
    interactive_args: [chat]
    session_inject: [--session-id, "{{session_id}}"]
    session_resume: [run, --output-format, stream-json, --resume, "{{session_id}}", --prompt, "{{prompt}}"]
    session_resume_interactive: [chat, --resume, "{{session_id}}"]
    read_only_args: [--read-only]
    output_format: ndjson
    ndjson_keep: [session, turn_start, turn_end, result, error, input_required]
    ndjson_stdout: final_text
    ndjson_stdout_path: result
    transcript_dialect: generic
    inject_process: [suag]
    session_family: suag
    deliver_command: [send, --session, "{{session_id}}"]
    deliver_stdin: true
    detect:
      command: suag
      args: [version]
  suag-acp:
    type: acp-agent
    command: suag
    args: [acp]
    session_family: suag
    acp:
      modes: {read_only: read_only}
      permission_policy: ask
      load_session: true
    detect:
      command: suag
      args: [version]
```

用 `gofer config validate server` 检查文件，再按当前 Gofer 服务的配置管理流程 reload，并用 `gofer agent list --runner server` 确认两项可用。`detect` 使用 `suag version`，其输出为单行 `suag <version> (<commit>)`。

Suag 创建会话时从外层到内层读取项目规则；每一级按顺序读取 `AGENTS.md`、`CLAUDE.md`（若存在），并在最前加入 `~/.suag/AGENTS.md`。展开后的真实文件路径会去重，因此两个规则文件共同引入的文件只读取一次。规则里的独占行 `@相对文件` 展开一层；规则和 `--append-system-prompt` 在会话创建时保存，续接时只在规则文件变化后向下一轮补充更新消息。`SUAG_APPEND_SYSTEM_PROMPT` 可提供默认追加提示，命令行参数优先；不传或传空串都沿用会话已保存的追加提示（Gofer 模板在 job 没有系统提示时会渲染出空串），用 `--clear-append-system-prompt` 清空；`--no-project-rules` 可在调试时跳过项目文件。Suag 不再因 `GOFER_JOB_ID` 自动追加任何提示；Gofer job 需要的汇报提示（`gofer work report` / `gofer issue`）由 Gofer 经 `SUAG_APPEND_SYSTEM_PROMPT` 或 `--append-system-prompt` 提供（见「升级迁移」）。

## 本机 chat 会话送话

`suag chat` 会在 `SUAG_HOME/run` 登记当前进程并监听仅本机用户可访问的 socket。可用 `suag ps --json` 查看活跃会话，用 `suag send --session <session-id|前缀|名称> --text <文本>` 送话；省略 `--text` 时从 stdin 读取（上限 1 MiB）。没有匹配进程时退出码为 3，多个匹配项为 2，stdin 超限为 4，成功送达或排队为 0，其他错误为 1。加 `--wait` 时，排队的消息要等到真正开始一轮才返回 0；若 chat 在此之前退出，消息被丢弃并返回 3。不加时排队即返回，chat 退出时会在其终端提示丢弃的条数。

Gofer agent 可配置 `deliver_command: [send, --session, "{{session_id}}"]` 和 `deliver_stdin: true`，由 Gofer 在会话所在 worker 上调用 Suag。Suag chat 忙时最多排队 8 条；空闲时送入下一轮。`send` 的 `--source <label>`（默认 `cli`）决定 chat 中的显示前缀 `[来自 <label>] `；文本首行已带 `[...] ` 标签时原样保留。送话作为注入输入记录。`UserPromptSubmit` 事件对这类输入（含 Stop block 注入的回复和 `[gofer job 完成]` 通知）带 `"injected": true`；终端手敲的输入不带该字段，其它事件也不带。`run` 和 ACP 续接同一会话时使用会话锁，避免与仍运行的 chat 同时写入；chat 退出后进程登记、socket 和锁会清理。

`interactive_args: [chat]` 允许 Gofer 在原进程退出后以交互终端启动 `suag chat --resume`；`session_resume_interactive` 给出续接参数。Windows 本机 socket 路径最多支持 100 个 UTF-16 code units；超长的 `SUAG_HOME/run/<pid>.sock` 会明确报错，需要将 `SUAG_HOME` 配置得更短。

模型地址与认证由启动 Suag 的用户配置。例如在该用户的 `~/.suag/config.yaml` 中设置：

```yaml
model: bailian/glm-5
providers:
  bailian:
    type: openai
    api_key: "${BAILIAN_API_KEY}"
    base_url: "${BAILIAN_BASE_URL}"
```

将 `BAILIAN_API_KEY` 和兼容 OpenAI API 的 `BAILIAN_BASE_URL` 放在同一用户的 `~/.suag/.env`，例如：

```dotenv
BAILIAN_API_KEY=<你的密钥>
BAILIAN_BASE_URL=https://dashscope.aliyuncs.com/compatible-mode/v1
```

也可将这两个变量设为 Gofer server 进程可用的环境变量。仓库的 `config/config.yaml` 是字段样例；Suag 只加载 `~/.suag/config.yaml` 与 `~/.suag/.env`（或 `SUAG_HOME` 下的同名文件），不读取当前工作目录的配置和 `.env`。`SUAG_HOME` 替换 `~/.suag` 作为数据与配置根，`SUAG_CONFIG` 指定唯一配置文件。模型优先级是 `--model` > `SUAG_MODEL` > 配置 `model`（`<provider>/<model>`）；这些选项不会接收密钥。OpenAI、Anthropic、DeepSeek 是内置预设，换用时改 `model` 并提供对应环境变量即可；完整字段见 [LLM 配置](llm-config-v0.md)。不要把真实密钥写入 agent argv、job `--env` 或任务文本：Gofer 会持久化 job 请求。会话库位置只由 `SUAG_HOME` 决定（`$SUAG_HOME/db/sessions.db`），`database_url` 与 `agentcore.run_root` 已移除。旧版本的库（`suag.db`、`agentcore/` 目录）不读取、不迁移、也不删除，仍留在磁盘上：旧会话 ID 无法用 `--resume`/`--session-id` 续接，Gofer 中登记的旧 Suag 会话（`session_family: suag`）无法再被唤醒或接管，需要新建会话。

## 批处理 CLI

CLI 可在 agent 的 `args` 中加入 `--mcp-config <file>` 接入本机 stdio MCP 服务；可重复指定多个 JSON 文件。文件格式为 Claude 的 `mcpServers`，例如 `{"mcpServers":{"gofer":{"command":"gofer","args":["mcp"]}}}`。Suag 的 YAML 配置也可在 `mcp.servers` 下列出默认服务。MCP 子进程继承启动环境（`GOFER_*` 等保留），但会剔除凭据变量：名称以 `_API_KEY`、`_AUTH_TOKEN` 结尾的变量及 suag 自己读取的 LLM 密钥（`ANTHROPIC_API_KEY`、`OPENAI_API_KEY`、`DEEPSEEK_API_KEY`），再叠加各服务的 `env`；服务 `env` 里显式配置的同名变量照常传入。MCP 工具名保持原名；若与内置工具或更早配置的 server 的工具重名，后者被丢弃并记录警告，不会覆盖。需要让 Gofer job 调用其工具时，须在可执行路径和运行环境中提供 `gofer`；不要把密钥放在参数或文档中。

MCP 工具默认一律不放行，由 YAML 中两个 glob 列表控制，条目形如 `server/工具glob`：`tools.auto_allow` 用于普通运行；`--read-only` 只认 `tools.read_only_allow`（条目必须带服务名，`auto_allow` 不会扩大它）。两者都为空时所有 MCP 调用被拒绝。Gofer 只读 job 需要的典型配置见「升级迁移」。其余 MCP 工具在 CLI 中被拒绝，即使带 `--allow-write`；拒绝信息作为工具错误交给模型，回合继续。拒绝语义见下文“工具拒绝语义”。需要逐次审批 MCP 写工具时使用 ACP。

### 内置工具与相关配置

内置工具来自同一份清单，run、chat、ACP 共用：

- 读工具（只读模式也有，chat 不询问）：`file_read`（按行分段，`offset`/`limit`，默认 2000 行，带行号）、`file_list`（`depth` ≤ 5、`max_entries` 500）、`file_grep`（RE2 正则，`path`/`glob`/`ignore_case`/`output: content|files`/`max_results` 默认 100 上限 1000）、`file_find`（`**` glob，按修改时间新到旧，默认 200 条）、`web_fetch`（http(s)，20 秒、下载上限 5MB、最多 5 次跳转，HTML 转为保留标题/链接/代码块的文本，默认返回 20000 字节）、`web_search`（配置 `serpapi_key` 时注册）、`skill`（见下）。`update_plan` 各模式都有。
- 写工具（`--allow-write` / `tools.allow_write` / ACP `default` mode）：`file_write`（整文件写）、`file_edit`（按唯一字符串替换，`replace_all` 替换全部，`edits` 一次多处且全成或全不改，保留 CRLF）、`shell_exec`。
- 文件类工具都经 `os.OpenRoot` 限制在工作区内，不跟随指向外部的 symlink；`file_list`/`file_grep`/`file_find` 不进入 `.git`、`node_modules`、`vendor`、`dist` 与根 `.gitignore` 的简单模式（不支持 `!` 与子目录 `.gitignore`），`file_grep` 跳过二进制与超过 1MB 的文件；搜索超过 10 秒返回已有结果并注明不完整。
- `web_fetch` 在拨号时检查实际连接的 IP，拒绝回环、私网、链路本地（含云元数据地址）、CGNAT 与组播地址，跳转与 DNS 重绑定同样受限；不读取环境代理（`HTTPS_PROXY` 等）。需要代理才能访问的站点，设 `tools.web_fetch.proxy`（`http://`、`https://` 或 `socks5://` 地址，如 `http://127.0.0.1:7890`）：此时连接只发往代理，改为在请求前与每次跳转时解析目标域名并校验其 IP，解析失败也拒绝（代理端若对同一域名解析出不同地址则无法察觉，保护弱于直连）；格式错误时启动报错。`tools.web_fetch.allow_private: true` 可放开内网限制。
- skills：启动时扫描一次 `<工作区>/.agents/skills`、`<工作区>/skills`、`~/.agents/skills` 下的 `SKILL.md`（frontmatter 含 `name`、`description`，可选 `allowed-tools` 仅展示），同名时工作区优先，无效文件记警告后跳过。段上下文消息按数量呈现 skills：总数不超过 `skills.list_max`（缺省 15）时全部列出“名字 — 描述”（描述截到 160 字符）；超过时只列工作区内的 skills，其余只给数量，模型用 `skill` 工具的 `query`（关键词，名字命中优先，中文按两字切分；`*` 按工作区/用户分组列出全部名字）检索后再按名字加载。模型用 `skill`（`name`）取正文与目录文件列表，`file` 参数只能读该 skill 目录内的文件。需要每次都生效的 skill 可预加载：在 SKILL.md frontmatter 写 `metadata: {always_load: true}`（或顶层 `always_load: true`），或在 `~/.suag/config.yaml` 设 `skills.always_load: [名字]`；其正文（每个上限 8000 字节）放进段上下文消息。

`shell_exec` 在 Windows 用 PowerShell（优先 `pwsh`，否则 `powershell`），输出固定 UTF-8，返回原生命令的退出码；其他系统用 `sh -c`。每次调用可传 `timeout`（秒，默认 120），超时后整组进程（Unix 进程组 / Windows job object）被终止，已有输出与退出码照常返回；后台残留进程占住输出时最多再等 2 秒。命令以 `GIT_TERMINAL_PROMPT=0`、`GIT_PAGER=cat`、`PAGER=cat` 运行，没有标准输入。

配置项（`~/.suag/config.yaml`，均有缺省值）：

- `tools.max_output_bytes`（缺省 8000）：单次工具结果完整写入会话的上限；`file_read` 按行分页并提示续读的 `offset`，其他工具超出时原文（脱敏后）存入 `~/.suag/sessions/<id>/outputs/`，会话只保留 ref 与头尾各 2000 字节。
- `tools.max_spill_bytes`（缺省 1048576）：单次工具（含 MCP）原始输出上限，超出部分按 UTF-8 边界保留头尾截断。
- `tools.shell`：显式指定 shell（命令名或路径，如 `bash`）；不自动探测。
- `tools.shell_timeout_max`（缺省 600）：`shell_exec` 的 `timeout` 上限（秒）。
- `mcp.call_timeout_sec`（缺省 120）：单次 MCP 工具调用的超时。

另有三组工具，run、chat、ACP 均有：

- `session_search`（全文检索过往消息、工具结果与摘要）与 `recall`（按 ref 读命中附近的消息，或读已外置输出的带行号文本）：只读，但结果算外部内容，用过即把会话标为已污染。
- `suag_manifest`：以 JSON 返回能力清单，等同在主机上跑 `suag manifest --json`（模型、工具及元数据、MCP、skills、记忆后端及选择原因、输出上限）。
- `memory`：长期记忆，见下节。

### 输出治理

工具结果超过 `tools.max_output_bytes`（缺省 8000）时，脱敏后的原文存到 `$SUAG_HOME/sessions/<会话 id>/outputs/`，会话里只有头尾各 2000 字节与 ref（`out:<session>:<hex>`），模型用 `recall` 分页读、用 `session_search`（带 `ref`）在其中检索；`file_read` 自己分页，不外置；出错结果不外置。输出外置不影响 stream-json 与 ACP 的事件格式。`tools.max_spill_bytes`（缺省 1 MiB）是单次原始输出的上限。`suag stats --json` 的 `tools` 项给出 `raw_bytes`、`inline_bytes`、`spilled`；`suag trace` 的 `turn_context` 行记录每回合的模型、模式、工具集哈希与是否已污染。

### 记忆

- 范围与后端：用户记忆在 `$SUAG_HOME/memory/user`；项目记忆后端的选择顺序为 `memory.project.backend` > 仓库根 `MEMORY.md`（files，条目在 `.suag/memory/<id>.md`）> 仓库根有 `.gofer/tracker` 时选 gofer > `$SUAG_HOME/projects/<key>/memory`。`memory.user.backend` / `memory.project.backend` 可选 `files`、`exec`（项目另可选 `gofer`），`exec` 需配 `memory.<scope>.command`（可选 `args`、`timeout_sec`）。完整字段见 `config/config.yaml`。
- 仓库根有 `.gofer/tracker` 且没有 `MEMORY.md` 时，项目记忆自动选 gofer 后端：每条项目记忆写成带 `suag` 标签的 gofer memory（key 形如 `suag-<kind>-<id>`，来源 `suag:<session>`），**会进入该仓库所有 agent 的开场上下文**，所以应简短且对各 agent 都有用；suag 只管理 `suag` 标签条目，不读写、不删除其他 gofer memory。不想写进 gofer 时设 `memory.project.backend: files`。
- 索引在每段开始时冻结进段上下文消息，本段内写入下一段可见，开启新段时刷新。
- 污染与确认：会话读过外部内容后（`web_fetch`、`web_search`、MCP 工具、`session_search`、`recall`，以及 `memory.taint_tools`、`memory.taint_on_spill`（缺省 `[shell_exec]`）列出的工具）写记忆需要确认，由 `memory.write_tainted`（`ask` 缺省 / `deny` / `allow`）决定。ACP 下确认走 `session/request_permission`（标题“memory: 会话读过外部内容，确认写入记忆”，可用 `gofer job interactions` / `answer` 处理）；`suag run` 没有交互，直接拒绝；chat 在终端询问。`--read-only` 与 ACP `read_only` mode 下 `memory` 只允许 `get`/`search`；`run` 默认模式可写记忆（它是 suag 自身状态，不是工作区写入）。`memory.write: off` 让工具整体只读。

系统提示词只含稳定内容（身份、使用约定），同一模式下逐字节不变，利于提供方做前缀缓存。环境（系统与 shell、工作区、只读或可写及开启写工具的方法）、项目规则、记忆索引、skills 列表在每段开头的上下文消息里；段内规则变化以“上下文更新”消息追加，并随会话回放。

Gofer 中两种 `suag run` 用法：

- 只读（模板缺省）：模型可读、列、搜索文件，抓取网页，使用 skills；要改文件或跑命令时会说明需要 `--allow-write`。
- 可写：在 `suag` agent 的 `args` / `session_resume` 中加入 `--allow-write`，模型可写文件、执行 shell（run 没有逐次确认，适合可信的本机任务）。需要逐次审批时用 `suag-acp`：审批请求的等待时长与超时处理由 Gofer 的 `permission_policy` 决定。

### chat 的写权限与终端确认

仅作用于 `suag chat`（`run` 与 ACP 行为不变）：

- `tools.allow_write`（布尔，缺省 `false`）：为 `true` 时 chat 启用写文件与命令工具，等同 `--allow-write`。`run` 仍需 `--allow-write`；`--read-only` 优先；ACP 不读取该项；非布尔值使 chat 启动失败。
- `tools.approval`（`ask` 缺省 / `auto`）：`ask` 时，只读名单（`file_read`、`file_list`、`file_grep`、`file_find`、`web_fetch`、`web_search`、`skill`、`update_plan`、`tool_search`）之外的所有非 MCP 工具（目前是 `file_write`、`file_edit`、`shell_exec`）执行前在终端询问；`file_edit` 的询问显示路径与原文/改为各前 20 行执行前在终端询问，显示完整命令 / 路径与字节数 / 查询（已脱敏、不截断）。回答 `y`/`yes` 允许一次，`a`/`always` 在本次 chat 进程内放行后续同名工具，其他（含回车）拒绝，回合继续。询问中 Ctrl-C / Ctrl-D 只取消本回合并显示“已取消”，不退出 chat；回合中提前敲的输入不会被当作回答。提示符处按一次 Ctrl-C 只清掉已输入内容并提示，3 秒内再按一次才退出 chat；Ctrl-D 直接退出。`auto` 不询问。
- 标准输入不是终端时，需确认的工具一律拒绝，并提示设置 `tools.approval: auto`。
- 同一回合 3 次拒绝结束回合；回合末显示“本轮被拒绝的工具：…”。
- MCP 工具不询问，仍按 `tools.auto_allow` / `tools.read_only_allow`。

```powershell
gofer job run --runner server --project my-tools-dev --cwd inhere-tools/suag --agent suag --read-only --prompt "检查当前项目"
gofer job show <job-id>
gofer job logs <job-id> --stream stdout --tail -n 20
gofer job resume <job-id> --prompt "继续上个问题"
```

首个 CLI job 的 `stdout` 是 Gofer 提取的最终答复；`session_id` 可从 `job show` 读取。Gofer 0.118.0 的 `job resume` 已验证继承源 job 的环境，并将最终答复投影到 `stdout`；原始 NDJSON 保留在 `stderr` 供排查。真实凭据仍应放在 Suag 私有配置或稳定的进程环境中。历史作业若使用已移除的 `suag-cli` agent 键，不能直接用当前的 `suag` 键续接。

### 会话交接、观测与 deferred 工具

- `--from <session>`：`suag run --from <旧会话> --prompt ...` 新建会话并继承来源的摘要、计划与已加载工具（来源只读，不能与 `--resume` 同用）。可同时传 `--session-id` 指定新会话 ID；交接失败（如生成摘要失败）时不会创建会话，同一 ID 可直接重试。Gofer 目前没有对应的模板变量（Gofer 侧计划以 `--from-session` 参数支持），在此之前需要在 job 的 agent args 中手工写入 `--from <session-id>`。
- 会话不存在：`run`/`chat` 的 `--resume` 目标或 `--from` 来源不存在时，退出码为 4，stderr 输出 `session not found: <id>`；被 Ctrl-C / SIGTERM 中断时退出码为 130，stderr 输出 `已中断`；其他失败为 1。旧版本的会话 ID 在新库中都属于这种情况，可据此改用 `--from` 或新建会话。
- `suag trace <session> [--turn id] [--json]`、`suag stats [--session id] [--since 7d] [--json]`、`suag export <session> [--redact] [-o file]` 读取同一个会话库，在运行 Suag 的主机上执行（Gofer worker 所在用户的 `SUAG_HOME`）；`export --redact` 去掉工具参数，适合外发。
- `run` 的 stream-json 另有两种新增行：`tool_call`（`name`、`args` 摘要）与 `tool_result`（`name`、`is_error`、脱敏并截断的 `summary`）；已有行不变。未配置 `ndjson_keep` 的调用方会收到它们，上面的示例 `ndjson_keep` 不含它们，需要在 Gofer 中显示工具调用时再加入。`suag trace` 会显示 plan 行（文本 `plan ✔/▶/○ 步骤`，`--json` 为 `{"kind":"plan",...}`）。
- `run` 的 stream-json 在 `plan` 事件上输出完整计划；`ndjson_keep` 示例未保留它，需要在 Gofer 中显示计划时把 `plan` 加入 `ndjson_keep`。
- `tools.deferred`（`auto`/`always`/`never`，默认 `auto`）在 `~/.suag/config.yaml` 中配置。MCP 工具多时被延迟加载，模型通过内置 `tool_search` 取回；Gofer 不需要额外配置。
- `compact_model` 指定分段交接摘要（自动、`:compact`、空闲整理）使用的模型；会话按段组织，旧段可用 `session_search`/`recall` 取回；`providers.<id>.models.<model>.context_window` 覆盖上下文窗口。

### run 的 stream-json 输出与 chat hooks

- `run --output-format stream-json` 先输出 `session` 事件，再输出过程事件，最后输出一行 `result`。`result`、`input_required`、`error` 都带 `usage`（本回合）和 `session_usage`（会话累计，持久化在会话元数据中）。两者均含 `input_tokens`（不含缓存）、`output_tokens`、可选的 `cache_read_tokens`/`cache_write_tokens`，以及 `total_tokens`（输入、输出与缓存之和）。
- 每次模型调用结束后，`run` 还会输出一行 `usage`：`{"type":"usage","usage":{input_tokens,output_tokens,cache_read_tokens,cache_write_tokens,total_tokens,cost_usd,tool_calls}}`，为**本次 run 的累计值**（run 口径，含 compact 调用，`input_tokens` 不含缓存；`tool_calls` 为本次 run 的工具调用次数，含被拒的）。`turn_end` 与 `result` 带同名 `usage`，`result` 的是最终值；`usage` 逐行单调递增，因此调用方可以在运行中途读取。Gofer 已读取该行并显示 `tools N`，`--max-tokens` 在运行中途就能生效，不必等到 `result`。
- chat hooks：`hooks.command`（argv 列表，不经 shell）在 SessionStart、UserPromptSubmit、PostToolUse、Stop、SessionEnd、Interrupt 事件时被调用，事件以一行 JSON 写入其 stdin。未设 `hooks.command` 时不启用，`SUAG_HOOKS=off` 可整体关闭。`hooks.timeout_ms` 限制普通事件；`hooks.stop_args` 只追加到 Stop 调用，且必须同时设 `hooks.stop_wait_sec`（Stop 最多等 `stop_wait_sec+2` 秒，否则按 `timeout_ms`）。PostToolUse 只对匹配 `hooks.post_tool_use.tools` glob 的工具发送，设了 `hooks.post_tool_use.name` 时改用该名称。
- `suag run --injected` 把本轮输入标记为非人工输入，transcript 和 hook 事件中都会体现。

## ACP 常驻与审批

```powershell
gofer job run --runner server --project my-tools-dev --cwd inhere-tools/suag --agent suag-acp --session --read-only --prompt "检查当前项目"
gofer job say <job-id> "继续分析"
gofer job end <job-id>
gofer job resume <job-id> --prompt "从上次会话继续"
```

`--read-only` 映射为 ACP `read_only` mode，写文件、命令与 MCP 工具不会加入可执行工具集合。默认 mode 中，写入/执行及所有 MCP 工具（含与内置只读工具同名者）在执行前调用 Gofer `session/request_permission`；可用 `gofer job interactions <job-id>` 查看待答复请求，再用 `gofer job answer <job-id> <interaction-id> reject|allow` 处理。拒绝时工具不执行；允许仅作用于当前一次请求。

### 工具拒绝语义

审批在工具注册表的包装层完成（从外到内：observe → visibility → approval → 工具），CLI 与 ACP 共用同一套语义：

- 被拒绝的调用（CLI 未授权的 MCP 工具；ACP 用户选择 reject、`session/request_permission` 请求失败、`read_only` mode 下调用写入/执行类工具）不执行，拒绝信息作为工具错误交给模型，回合继续。每次拒绝在 `suag trace`/`stats` 中记为一条 denial 事件。
- 同一回合累计被拒 3 次后（`read_only` 的拒绝同样计数），下一次模型调用前结束回合，最终答复说明拒绝次数与工具名。计数按回合重置。
- 审批被取消（ACP 返回 `cancelled` 结果或 `session/cancel`）时中止回合，不把取消当作工具结果交给模型。
- 调用清单中但本次未启用的内置工具（只读模式下的写工具、缺 `serpapi_key` 的 `web_search`）按拒绝处理并计数，拒绝信息按入口给出开启方法（run：`--allow-write`；chat：`tools.allow_write: true`；`--read-only`：去掉该参数；ACP：会话 mode 改为 `default`）。完全未知的工具名返回“not found”及可用工具列表，不计入拒绝。
- 尚未加载的 deferred MCP 工具被直接调用时，由 visibility 层返回“尚未加载”并自动加载，不会触发审批请求。

对外契约：

- stream-json：本回合只要发生过拒绝，回合结束时都会在 `result` 之前发出一行 `input_required`，并带 `denied_tools`（去重后的被拒工具名数组）；`result` 的 `status` 仍为 `COMPLETED`，进程正常退出。旧版仅在首次拒绝时发出且不带 `denied_tools`，并会在模型重复请求被拒工具时直接结束回合；该短路已移除，改为上面的 3 次上限。
- ACP（语义变化）：拒绝后模型若正常完成，`stopReason` 由原来的 `refusal` 改为 `end_turn`；只有累计 3 次拒绝时返回 `refusal`（结束前发送说明文本）；审批取消返回 `cancelled`。被拒工具的 `tool_call` 状态为 `failed`（不再跟 `completed`）；允许路径的状态序列不变（`pending` → `in_progress` → `completed`）；未经审批就失败的调用（如未加载的 deferred 工具）以 `failed` 结束。Gofer 的 ACP 客户端两种 stopReason 都能处理。

2026-10-07 的 Gofer 实际作业已验证 CLI/ACP 续聊、ACP `job say/end/resume`、只读无写入及审批允许/拒绝；0.118.0 另以当前 `suag` agent 验证了 CLI 续接的环境继承与输出投影。模型响应由本机回环模拟服务提供；最终配置不含测试模型地址或测试 HOME，真实模型凭据路径须在运行环境中单独准备和验证。

### ACP 用量上报

每次模型调用结束后以及每轮结束（含被取消的回合）都发送 `usage_update`：`_meta` 顶层的扁平键 `input_tokens`/`output_tokens`/`cache_read_tokens`/`cache_write_tokens`/`total_tokens`/`cost_usd`/`tool_calls` 为**本次 run 的累计值**（run 口径，Gofer 只读这些扁平键，所以预算按 run 计）；另附嵌套对象 `_meta.run`（同一 run 值）和 `_meta.session`（会话累计，含本次 run；无 `tool_calls`）；顶层 `cost.amount` 仍为**会话累计值**；顶层 `used` 是最近一次模型请求的上下文 token 数，`size` 是模型上下文窗口。窗口取 `providers.<id>.models.<model>.context_window`（可选配置），否则按实际生效的模型（`--model`/`SUAG_MODEL` 覆盖后的值）名取保守默认（claude 200k、gpt-4.1 1M、gpt-5 400k、gpt-4o/deepseek 128k），未知模型为 128k。会话累计（`_meta.session`）保存在会话 metadata（`sessions.metadata_json`），`session/load` 或重启后从已保存值继续累计。`input_tokens` 不含缓存，缓存另报 `cache_read_tokens`/`cache_write_tokens`，`total_tokens` = input + output + cache_read + cache_write（与 Gofer 口径一致）。

## 升级迁移

本批改动（providers/model 配置、hooks 通用化、来源前缀）带来下列不兼容变化；按两侧分别处理。

### 用户 suag 配置（`~/.suag/config.yaml`）

- `llm:` 段改写为 `model: <provider>/<model>` 加 `providers:`，旧写法启动即报错；对照见 [LLM 配置](llm-config-v0.md#从旧配置迁移)。
- 补 `tools.read_only_allow`。只读 job（`--read-only`）不再有内置放行表，不配置则所有 MCP 工具被拒绝：

  ```yaml
  tools:
    read_only_allow:
      - gofer/gofer_*_list
      - gofer/gofer_*_get
      - gofer/gofer_work_report
      - gofer/gofer_session_list
  ```

- 补 `hooks.command` 等。原先 `gofer.hooks` 的 auto 模式（PATH 中有 `gofer` 即启用）已取消，不配置就不启用 hooks。等价于原行为的配置：

  ```yaml
  hooks:
    command: [gofer, hook, generic, --agent, suag]
    stop_args: [--wait, "7140"]
    stop_wait_sec: 7140
    post_tool_use:
      tools: ["*shell*", "*exec*"]
      name: shell
  ```

  只配 `stop_args` 不配 `stop_wait_sec` 会让 Stop 在 `timeout_ms`（默认 3000）后被结束，两者必须同时配置。`post_tool_use.tools` 不配置则不发送 PostToolUse。

- provider 名变化：trace/stats 中的 provider 名是配置里的 id（原 `claude` 变为 `anthropic` 或自定义 id）。

### Gofer 侧（由 Gofer 会话在 Gofer 仓库实施）

- job 环境设 `SUAG_HOOKS=off`，替代 suag 原先对 `GOFER_JOB_ID` 的判断：job 内的 suag 不应再启用 hooks。
- job 环境设 `SUAG_APPEND_SYSTEM_PROMPT`，携带原“运行在 Gofer job 中，可用 `gofer work report` / `gofer issue` 汇报”的提示。chat、resume 与 ACP 路径不带 `--append-system-prompt`。
- 经 `run` 续接投递 web 回复时加 `--injected`，suag 不再根据文本前缀 `[gofer web 回复] ` 推断注入。
- socket 投递（`suag send`）若要保留“[gofer web 回复]”措辞，需由 Gofer 自己加在文本前；suag 不再特判，`--source gofer`（或来源为 gofer）时显示为 `[来自 gofer] `。
- agent 配置中删除 `--provider` 用法（若有）；改用 `--model <provider>/<model>`。
