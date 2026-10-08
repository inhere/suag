# 在 Gofer 中运行 Suag

Suag 提供两个 Gofer agent：`suag` 执行一次性 CLI 任务，`suag-acp` 提供常驻 ACP 会话。两者共用个人会话数据，默认位于启动 Suag 的用户主目录 `~/.suag/db`。Gofer job ID 是执行记录，Suag session ID 是对话记录。

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

Suag 创建会话时从外层到内层读取项目规则；每一级按顺序读取 `AGENTS.md`、`CLAUDE.md`（若存在），并在最前加入 `~/.suag/AGENTS.md`。展开后的真实文件路径会去重，因此两个规则文件共同引入的文件只读取一次。规则里的独占行 `@相对文件` 展开一层；规则和 `--append-system-prompt` 在会话创建时保存，续接时只在规则文件变化后向下一轮补充更新消息。`SUAG_APPEND_SYSTEM_PROMPT` 可提供默认追加提示，命令行参数优先；不传或传空串都沿用会话已保存的追加提示（Gofer 模板在 job 没有系统提示时会渲染出空串），用 `--clear-append-system-prompt` 清空；`--no-project-rules` 可在调试时跳过项目文件。Gofer job 中还会附上 `gofer work report` / `gofer issue` 的汇报提示。

## 本机 chat 会话送话

`suag chat` 会在 `SUAG_HOME/run` 登记当前进程并监听仅本机用户可访问的 socket。可用 `suag ps --json` 查看活跃会话，用 `suag send --session <session-id|前缀|名称> --text <文本>` 送话；省略 `--text` 时从 stdin 读取（上限 1 MiB）。没有匹配进程时退出码为 3，多个匹配项为 2，stdin 超限为 4，成功送达或排队为 0，其他错误为 1。加 `--wait` 时，排队的消息要等到真正开始一轮才返回 0；若 chat 在此之前退出，消息被丢弃并返回 3。不加时排队即返回，chat 退出时会在其终端提示丢弃的条数。

Gofer agent 可配置 `deliver_command: [send, --session, "{{session_id}}"]` 和 `deliver_stdin: true`，由 Gofer 在会话所在 worker 上调用 Suag。Suag chat 忙时最多排队 8 条；空闲时送入下一轮。Gofer 送话会以 `[gofer web 回复]` 显示并作为注入输入记录。`hook generic` 的 `UserPromptSubmit` 事件对这类输入（含 Stop block 注入的 web 回复和 `[gofer job 完成]` 通知）带 `"injected": true`；终端手敲的输入不带该字段，其它事件也不带。`run` 和 ACP 续接同一会话时使用会话锁，避免与仍运行的 chat 同时写入；chat 退出后进程登记、socket 和锁会清理。

`interactive_args: [chat]` 允许 Gofer 在原进程退出后以交互终端启动 `suag chat --resume`；`session_resume_interactive` 给出续接参数。Windows 本机 socket 路径最多支持 100 个 UTF-16 code units；超长的 `SUAG_HOME/run/<pid>.sock` 会明确报错，需要将 `SUAG_HOME` 配置得更短。

模型地址与认证由启动 Suag 的用户配置。例如在该用户的 `~/.suag/config.yaml` 中设置：

```yaml
llm:
  api_type: openai
  provider_name: bailian
  model: glm-5
  api_key: "${BAILIAN_API_KEY}"
  base_url: "${BAILIAN_BASE_URL}"
```

将 `BAILIAN_API_KEY` 和兼容 OpenAI API 的 `BAILIAN_BASE_URL` 放在同一用户的 `~/.suag/.env`，例如：

```dotenv
BAILIAN_API_KEY=<你的密钥>
BAILIAN_BASE_URL=https://dashscope.aliyuncs.com/compatible-mode/v1
```

也可将这两个变量设为 Gofer server 进程可用的环境变量。仓库的 `config/config.yaml` 是字段样例；Suag 只加载 `~/.suag/config.yaml` 与 `~/.suag/.env`（或 `SUAG_HOME` 下的同名文件），不读取当前工作目录的配置和 `.env`。`SUAG_HOME` 替换 `~/.suag` 作为数据与配置根，`SUAG_CONFIG` 指定唯一配置文件。模型与 provider 的优先级是 `--model` / `--provider` > `SUAG_MODEL` / `SUAG_PROVIDER` > 配置文件；这些选项不会接收密钥。换用 OpenAI、DeepSeek 或 Anthropic 时，调整 `api_type`、`provider_name`、`model`、`api_key` 与 `base_url`；Anthropic 使用 `api_type: anthropic`。不要把真实密钥写入 agent argv、job `--env` 或任务文本：Gofer 会持久化 job 请求。需要读取旧仓库 `db/` 会话时，显式配置 `database_url` 和 `agentcore.run_root`；默认路径不会自动迁移旧数据。

## 批处理 CLI

CLI 可在 agent 的 `args` 中加入 `--mcp-config <file>` 接入本机 stdio MCP 服务；可重复指定多个 JSON 文件。文件格式为 Claude 的 `mcpServers`，例如 `{"mcpServers":{"gofer":{"command":"gofer","args":["mcp"]}}}`。Suag 的 YAML 配置也可在 `mcp.servers` 下列出默认服务。MCP 子进程继承启动环境（`GOFER_*` 等保留），但会剔除凭据变量：名称以 `_API_KEY`、`_AUTH_TOKEN` 结尾的变量及 suag 自己读取的 LLM 密钥（`ANTHROPIC_API_KEY`、`OPENAI_API_KEY`、`DEEPSEEK_API_KEY`），再叠加各服务的 `env`；服务 `env` 里显式配置的同名变量照常传入。MCP 工具名保持原名；若与内置工具或更早配置的 server 的工具重名，后者被丢弃并记录警告，不会覆盖。需要让 Gofer job 调用其工具时，须在可执行路径和运行环境中提供 `gofer`；不要把密钥放在参数或文档中。

`tools.auto_allow` 是 YAML 中的 glob 列表。条目形如 `server/工具glob`，默认只自动放行名为 `gofer` 的 MCP 服务的 `gofer/gofer_*_list`、`gofer/gofer_*_get`、`gofer/gofer_work_report`、`gofer/gofer_session_list`，其他服务即使暴露同名工具也不放行；不含 `/` 的旧式条目仍按工具名匹配任意服务（有冒充风险，建议改写为带服务名的形式）；若显式配置，列表替换默认值。其余 MCP 工具在 CLI 中被拒绝，即使带 `--allow-write`；拒绝信息作为工具错误交给模型，`run --output-format stream-json` 发出 `input_required` 与最终 `result` 后正常退出。需要逐次审批 MCP 写工具时使用 ACP。

```powershell
gofer job run --runner server --project my-tools-dev --cwd inhere-tools/suag --agent suag --read-only --prompt "检查当前项目"
gofer job show <job-id>
gofer job logs <job-id> --stream stdout --tail -n 20
gofer job resume <job-id> --prompt "继续上个问题"
```

首个 CLI job 的 `stdout` 是 Gofer 提取的最终答复；`session_id` 可从 `job show` 读取。Gofer 0.118.0 的 `job resume` 已验证继承源 job 的环境，并将最终答复投影到 `stdout`；原始 NDJSON 保留在 `stderr` 供排查。真实凭据仍应放在 Suag 私有配置或稳定的进程环境中。历史作业若使用已移除的 `suag-cli` agent 键，不能直接用当前的 `suag` 键续接。

## ACP 常驻与审批

```powershell
gofer job run --runner server --project my-tools-dev --cwd inhere-tools/suag --agent suag-acp --session --read-only --prompt "检查当前项目"
gofer job say <job-id> "继续分析"
gofer job end <job-id>
gofer job resume <job-id> --prompt "从上次会话继续"
```

`--read-only` 映射为 ACP `read_only` mode，写文件、命令与 MCP 工具不会加入可执行工具集合。默认 mode 中，写入/执行及所有 MCP 工具（含与内置只读工具同名者）在执行前调用 Gofer `session/request_permission`；可用 `gofer job interactions <job-id>` 查看待答复请求，再用 `gofer job answer <job-id> <interaction-id> reject|allow` 处理。拒绝时工具不执行；允许仅作用于当前一次请求。

2026-10-07 的 Gofer 实际作业已验证 CLI/ACP 续聊、ACP `job say/end/resume`、只读无写入及审批允许/拒绝；0.118.0 另以当前 `suag` agent 验证了 CLI 续接的环境继承与输出投影。模型响应由本机回环模拟服务提供；最终配置不含测试模型地址或测试 HOME，真实模型凭据路径须在运行环境中单独准备和验证。

### ACP 用量上报

每轮结束（含被取消的回合）发送 `usage_update`：`_meta` 的 `input_tokens`/`output_tokens`/`total_tokens`（及可选 `cache_read_tokens`/`cache_write_tokens`/`cost_usd`）和顶层 `cost.amount` 均为**会话累计值**；顶层 `used` 是最近一次模型请求的上下文 token 数，`size` 是模型上下文窗口。窗口取 `llm.context_window`（可选配置），否则按实际生效的模型（`--model`/`SUAG_MODEL` 覆盖后的值）名取保守默认（claude 200k、gpt-4.1 1M、gpt-5 400k、gpt-4o/deepseek 128k），未知模型为 128k。会话累计保存在会话 metadata（`sessions.metadata_json`），`session/load` 或重启后从已保存值继续累计。`input_tokens` 不含缓存，缓存另报 `cache_read_tokens`/`cache_write_tokens`，`total_tokens` = input + output + cache_read + cache_write（与 Gofer 口径一致）。
