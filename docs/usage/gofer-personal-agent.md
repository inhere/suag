# 在 Gofer 中运行 Suag

Suag 提供两个入口：`suag-cli` 执行一次性任务，`suag-acp` 提供常驻 ACP 会话。两者共用个人会话数据，默认位于启动 Suag 的用户主目录 `~/.suag/db`。Gofer job ID 是执行记录，Suag session ID 是对话记录。

## 准备二进制与配置

在 Suag 仓库执行 `make install`。它将二进制安装到 `$(go env GOPATH)/bin`；先确认该目录位于终端和 Gofer server 进程的 PATH 中，再在新终端执行 `suag run --help`。主机 Gofer 配置的 `agents` 下增加以下两项：

```yaml
agents:
  suag-cli:
    type: cli-agent
    command: suag
    args: [run, --output-format, stream-json, --prompt, "{{prompt}}"]
    session_inject: [--session-id, "{{session_id}}"]
    session_resume: [run, --output-format, stream-json, --resume, "{{session_id}}", --prompt, "{{prompt}}"]
    read_only_args: [--read-only]
    output_format: ndjson
    ndjson_keep: [session, turn_start, turn_end, result, error, input_required]
    ndjson_stdout: final_text
    ndjson_stdout_path: result
    detect:
      command: suag
      args: [run, --help]
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
      args: [run, --help]
```

用 `gofer config validate server` 检查文件，再按当前 Gofer 服务的配置管理流程 reload，并用 `gofer agent list --runner server` 确认两项可用。`detect` 使用 `run --help`，避免默认 `--version` 探测走入旧运行入口。

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

也可将这两个变量设为 Gofer server 进程可用的环境变量。`config/config.yaml` 提供了字段样例；Suag 启动时依次加载当前目录和用户目录的配置。换用 OpenAI、DeepSeek 或 Anthropic 时，调整 `api_type`、`provider_name`、`model`、`api_key` 与 `base_url`；Anthropic 使用 `api_type: anthropic`。不要把真实密钥写入 agent argv、job `--env` 或任务文本：Gofer 会持久化 job 请求。需要读取旧仓库 `db/` 会话时，显式配置 `database_url` 和 `agentcore.run_root`；默认路径不会自动迁移旧数据。

## 批处理 CLI

```powershell
gofer job run --runner server --project my-tools-dev --cwd inhere-tools/suag --agent suag-cli --read-only --prompt "检查当前项目"
gofer job show <job-id>
gofer job logs <job-id> --stream stdout --tail -n 20
gofer job resume <job-id> --prompt "继续上个问题"
```

首个 CLI job 的 `stdout` 是 Gofer 提取的最终答复；`session_id` 可从 `job show` 读取。当前 Gofer 的 CLI resume 通过 `exec` 载体启动新进程，续接 job 的日志可能是原始 NDJSON 行，可在其中查看 `result` 事件。该载体不会继承原 job 临时传入的 `--env`，所以跨 job 必需的模型与数据配置应来自稳定的进程环境或 Suag 私有配置。

## ACP 常驻与审批

```powershell
gofer job run --runner server --project my-tools-dev --cwd inhere-tools/suag --agent suag-acp --session --read-only --prompt "检查当前项目"
gofer job say <job-id> "继续分析"
gofer job end <job-id>
gofer job resume <job-id> --prompt "从上次会话继续"
```

`--read-only` 映射为 ACP `read_only` mode，写文件、命令与 MCP 工具不会加入可执行工具集合。默认 mode 中，写入/执行及未知 MCP 工具在执行前调用 Gofer `session/request_permission`；可用 `gofer job interactions <job-id>` 查看待答复请求，再用 `gofer job answer <job-id> <interaction-id> reject|allow` 处理。拒绝时工具不执行；允许仅作用于当前一次请求。

2026-10-07 的 Gofer 实际作业已验证 CLI/ACP 续聊、ACP `job say/end/resume`、只读无写入及审批允许/拒绝。模型响应由本机回环模拟服务提供；最终配置不含测试模型地址或测试 HOME，真实模型凭据路径须在运行环境中单独准备和验证。
