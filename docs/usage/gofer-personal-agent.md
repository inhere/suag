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
    session_inject: [--session-id, "{{session_id}}"]
    session_resume: [run, --output-format, stream-json, --resume, "{{session_id}}", --prompt, "{{prompt}}"]
    read_only_args: [--read-only]
    output_format: ndjson
    ndjson_keep: [session, turn_start, turn_end, result, error, input_required]
    ndjson_stdout: final_text
    ndjson_stdout_path: result
    detect:
      command: suag
      args: [version]
  suag-acp:
    type: acp-agent
    command: suag
    args: [acp]
    acp:
      modes: {read_only: read_only}
      permission_policy: ask
      load_session: true
    detect:
      command: suag
      args: [version]
```

用 `gofer config validate server` 检查文件，再按当前 Gofer 服务的配置管理流程 reload，并用 `gofer agent list --runner server` 确认两项可用。`detect` 使用 `suag version`，其输出为单行 `suag <version> (<commit>)`。

Suag 创建会话时按外层到内层读取项目 `AGENTS.md`（没有时读取 `CLAUDE.md`），并在最前加入 `~/.suag/AGENTS.md`。`SUAG_HOME` 可改变用户级规则目录。规则里的独占行 `@相对文件` 展开一层；规则和 `--append-system-prompt` 在会话创建时保存，续接时只在规则文件变化后向下一轮补充更新消息。`SUAG_APPEND_SYSTEM_PROMPT` 可提供默认追加提示，命令行参数优先；`--no-project-rules` 可在调试时跳过项目文件。Gofer job 中还会附上 `gofer work report` / `gofer issue` 的汇报提示。

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

也可将这两个变量设为 Gofer server 进程可用的环境变量。`config/config.yaml` 提供了字段样例；Suag 启动时依次加载当前目录和用户目录的配置。`SUAG_HOME` 替换 `~/.suag` 作为数据与配置根，`SUAG_CONFIG` 指定唯一配置文件。模型与 provider 的优先级是 `--model` / `--provider` > `SUAG_MODEL` / `SUAG_PROVIDER` > 配置文件；这些选项不会接收密钥。换用 OpenAI、DeepSeek 或 Anthropic 时，调整 `api_type`、`provider_name`、`model`、`api_key` 与 `base_url`；Anthropic 使用 `api_type: anthropic`。不要把真实密钥写入 agent argv、job `--env` 或任务文本：Gofer 会持久化 job 请求。需要读取旧仓库 `db/` 会话时，显式配置 `database_url` 和 `agentcore.run_root`；默认路径不会自动迁移旧数据。

## 批处理 CLI

CLI 可在 agent 的 `args` 中加入 `--mcp-config <file>` 接入本机 stdio MCP 服务；可重复指定多个 JSON 文件。文件格式为 Claude 的 `mcpServers`，例如 `{"mcpServers":{"gofer":{"command":"gofer","args":["mcp"]}}}`。Suag 的 YAML 配置也可在 `mcp.servers` 下列出默认服务。MCP 子进程继承启动环境，并叠加各服务的 `env`。需要让 Gofer job 调用其工具时，须在可执行路径和运行环境中提供 `gofer`；不要把密钥放在参数或文档中。

`tools.auto_allow` 是 YAML 中的 glob 列表。默认自动放行 `gofer_*_list`、`gofer_*_get`、`gofer_work_report`、`gofer_session_list`；若显式配置，列表替换默认值。其余 MCP 工具在 CLI 中被拒绝，即使带 `--allow-write`；拒绝信息作为工具错误交给模型，`run --output-format stream-json` 发出 `input_required` 与最终 `result` 后正常退出。需要逐次审批 MCP 写工具时使用 ACP。

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

`--read-only` 映射为 ACP `read_only` mode，写文件、命令与 MCP 工具不会加入可执行工具集合。默认 mode 中，写入/执行及未知 MCP 工具在执行前调用 Gofer `session/request_permission`；可用 `gofer job interactions <job-id>` 查看待答复请求，再用 `gofer job answer <job-id> <interaction-id> reject|allow` 处理。拒绝时工具不执行；允许仅作用于当前一次请求。

2026-10-07 的 Gofer 实际作业已验证 CLI/ACP 续聊、ACP `job say/end/resume`、只读无写入及审批允许/拒绝；0.118.0 另以当前 `suag` agent 验证了 CLI 续接的环境继承与输出投影。模型响应由本机回环模拟服务提供；最终配置不含测试模型地址或测试 HOME，真实模型凭据路径须在运行环境中单独准备和验证。
