# LLM Config v0

当前 v0 阶段的 LLM 配置推荐以协议类型为主，而不是直接把顶层配置绑死到某个厂商品牌。

## 推荐配置结构

```yaml
llm:
  api_type: openai
  provider_name: deepseek
  model: deepseek-chat
  api_key: "${DEEPSEEK_API_KEY}"
  base_url: "https://api.deepseek.com/v1"
```

或：

```yaml
llm:
  api_type: anthropic
  provider_name: claude
  model: claude-sonnet-4-6
  api_key: "${ANTHROPIC_API_KEY}"
```

## 字段语义

- `llm.api_type`
  决定协议实现。当前支持：
  - `openai`
  - `anthropic`
- `llm.provider_name`
  决定厂商标签、默认行为和兼容分支。当前已覆盖：
  - `openai`
  - `deepseek`
  - `claude`
- `llm.model`
  始终显式填写，不依赖运行时猜默认模型。
- `llm.api_key`
  优先使用配置值；为空时会回退到协议或厂商对应环境变量。
- `llm.base_url`
  用于覆盖协议默认 endpoint，适合 OpenAI-compatible 网关或自建代理。
- `llm.headers`
  可选，给每个模型请求附加的 HTTP 头（键为头名，值支持 `${ENV}`）。值中的 `{{session_id}}` 会替换为当前 suag 会话 ID，同一会话固定不变；没有会话 ID 时（不经过会话的调用）这类头不发送。默认 `User-Agent: suag`，也可在此覆盖。切换到另一个 provider（`--provider` / `SUAG_PROVIDER`）时与 `api_key`、`base_url` 一起丢弃。

## 网关示例：OpenCode Go

OpenCode Go 要求每个对话带固定的 `x-opencode-session`，并只接受部分模型走 `/chat/completions`（模型 ID 见其官方文档的接口表）：

```yaml
llm:
  api_type: openai
  provider_name: opencode-go
  model: deepseek-v4-flash
  api_key: "${OPENCODE_API_KEY}"   # 写在 $SUAG_HOME/.env
  base_url: "https://opencode.ai/zen/go/v1"
  headers:
    x-opencode-session: "{{session_id}}"
```

## 说明

- 统一使用 `anthropic` 拼写，不使用 `authropic`。
- `provider_name` 主要是兼容和标识维度，不替代 `api_type` 的协议选择职责。
- v0 当前只认 `api_type + provider_name`，不再推荐也不再维护旧 `llm.provider` 配置写法。
- 后续如果新增 `openrouter`、自建 OpenAI-compatible gateway 等，优先扩展 `provider_name`，而不是重新引入顶层 `llm.provider` 分支。
