# LLM 配置

模型由「模型引用」和「provider 定义」两部分描述：`model: <provider>/<model>` 选择模型，`providers.<id>` 定义协议、地址与凭据。旧的 `llm` 配置段已移除。

## 配置结构

```yaml
model: deepseek/deepseek-chat

providers:
  deepseek:                      # 内置预设，只需覆盖想改的字段
    api_key: "${DEEPSEEK_API_KEY}"
  bailian:                       # 自定义 provider 必须写 type
    type: openai                 # openai | anthropic
    base_url: "${BAILIAN_BASE_URL}"
    api_key: "${BAILIAN_API_KEY}"
    headers:
      x-extra: "value"
    models:
      glm-5:
        context_window: 128000

compact_model: bailian/glm-4-flash
fallback_models:
  - anthropic/claude-sonnet-4-6
```

## 字段

- `model`：主模型，`<provider>/<model>`。
- `providers.<id>.type`：协议，`openai` 或 `anthropic`。内置预设可省略；自定义 provider 必填。
- `providers.<id>.base_url`、`api_key`：地址与密钥，值支持 `${VAR}`。
- `providers.<id>.headers`：附加的 HTTP 请求头，值支持 `${VAR}`。
- `providers.<id>.models.<model>.context_window`：该模型的上下文窗口；未配置时按模型名推断（claude 200k、gpt-4.1 1M、gpt-5 400k、gpt-4o/deepseek 128k，未知为 128k）。
- `compact_model`：自动压缩与 `:compact` 使用的模型，引用规则同 `model`；裸模型名使用主模型的 provider；不设则用当前回合的模型。
- `fallback_models`：主模型失败后按顺序尝试的模型列表，引用规则同 `compact_model`。

### 内置预设

| id | type | 默认 base_url | 密钥环境变量 |
|---|---|---|---|
| `openai` | openai | `https://api.openai.com/v1` | `OPENAI_API_KEY` |
| `anthropic` | anthropic | `https://api.anthropic.com` | `ANTHROPIC_API_KEY` |
| `deepseek` | openai | `https://api.deepseek.com/v1` | `DEEPSEEK_API_KEY` |

同名的 `providers.<id>` 逐字段覆盖预设，空字段不覆盖；`headers` 与 `models` 整体替换。

## 模型引用解析

1. 只按第一个 `/` 切分，且前缀必须是已定义的 provider（预设或 `providers` 下的 id）。
2. 前缀不是已定义 provider 时，整串都是模型名，使用默认 provider。例如 `meta/llama-3` 是模型名，不是 provider `meta`。
3. 默认 provider：`--model` / `SUAG_MODEL` 与 `compact_model` / `fallback_models` 中的裸模型名，分别使用配置 `model` 的 provider 与主模型的 provider。

主模型优先级：`--model` > `SUAG_MODEL` > 配置 `model`。`--model gpt-5` 这类裸名使用配置 `model` 的 provider。provider 未定义、没有模型名或没有配置模型时启动报错。

## base_url 拼接

- `openai` 类型：在 `base_url` 后拼 `/chat/completions`。
- `anthropic` 类型：`base_url` 后拼 `/v1/messages`；`base_url` 已以 `/v1` 结尾时拼 `/messages`。Claude 使用流式（SSE）请求。

## headers

- 值中的 `{{session_id}}` 替换为当前 suag 会话 ID，同一会话固定不变；没有会话 ID 时（不经过会话的调用）这类头不发送。
- 默认带 `User-Agent: suag`，可在 `headers` 中覆盖。

## 环境变量

- `${VAR}` 在 `api_key`、`base_url`、`headers` 中展开，来源为进程环境或 `$SUAG_HOME/.env`（默认 `~/.suag/.env`）。
- 配置文件为 `$SUAG_HOME/config.yaml`，`SUAG_CONFIG` 可指定其它文件；不读取当前工作目录的配置与 `.env`。
- 真实密钥放在 `.env` 或环境变量，不写进命令行参数。

## 示例：OpenCode Go

OpenCode Go 要求每个对话带固定的 `x-opencode-session`。它有 OpenAI 兼容和 Anthropic 兼容两个端点（各模型走哪个端点见其官方文档）：

```yaml
model: opencode-go/deepseek-v4-flash

providers:
  opencode-go:                    # /chat/completions
    type: openai
    base_url: "https://opencode.ai/zen/go/v1"
    api_key: "${OPENCODE_API_KEY}"   # 写在 $SUAG_HOME/.env
    headers:
      x-opencode-session: "{{session_id}}"
  opencode-go-claude:             # 实际请求 https://opencode.ai/zen/go/v1/messages
    type: anthropic
    base_url: "https://opencode.ai/zen/go"
    api_key: "${OPENCODE_API_KEY}"
    headers:
      x-opencode-session: "{{session_id}}"
```

之后可用 `--model opencode-go-claude/<model>` 临时切换到 Anthropic 端点。

## 从旧配置迁移

旧的 `llm` 配置段、provider 选择环境变量与命令行选项已移除（见下表）；仍存在时启动报错并提示改用新写法。trace/stats 中的 provider 名现在是配置里的 id（原 `claude` 变为 `anthropic` 或自定义 id）。

旧写法（仅作对照，已不再支持）：

    llm:
      api_type: openai
      provider_name: bailian
      model: glm-5
      api_key: "${BAILIAN_API_KEY}"
      base_url: "${BAILIAN_BASE_URL}"
      compact_model: glm-4-flash
      context_window: 128000

新写法：

```yaml
model: bailian/glm-5
compact_model: bailian/glm-4-flash
providers:
  bailian:
    type: openai
    api_key: "${BAILIAN_API_KEY}"
    base_url: "${BAILIAN_BASE_URL}"
    models:
      glm-5:
        context_window: 128000
```

对照：

| 旧 | 新 |
|---|---|
| `llm.api_type` | `providers.<id>.type` |
| `llm.provider_name` + `llm.model` | `model: <id>/<model>` |
| `llm.api_key` / `llm.base_url` / `llm.headers` | `providers.<id>.api_key` / `base_url` / `headers` |
| `llm.compact_model` | 顶层 `compact_model: <provider>/<model>`（可用裸名沿用主 provider） |
| `llm.context_window` | `providers.<id>.models.<model>.context_window` |
| `SUAG_PROVIDER` / `--provider` | `SUAG_MODEL=<provider>/<model>` / `--model <provider>/<model>` |
