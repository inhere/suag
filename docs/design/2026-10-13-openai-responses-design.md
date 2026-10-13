# OpenAI Responses API provider 与网关适配层（2026-10-13）

关联：issue suag-uesr，plan-20261013-002736-46d99988（R1/R2）。参考：oh-my-pi `packages/ai/src/providers/openai-responses.ts`、`openai-shared.ts`、`catalog/src/compat/rules/providers/commandcode.kdl`。

## 目标

1. 新增 provider 类型 `openai-responses`（`POST {base_url}/responses`），可写在 provider 或单个模型上（与模型级 `type` 覆盖一致）。现有 `openai`（chat/completions）不变。
2. 网关、模型家族的特殊处理放进独立的适配文件（`pkg/agentcore/llm/compat_*.go`），主流程不出现网关名；新增网关只加一个文件。
3. 与 chat/completions 在同一模型（command-code `deepseek/deepseek-v4.1-flash`）上 A/B：步数、输出量、缓存。

## 适配层

`Compat` 描述一次请求要遵守的差异，由已注册的适配器按 base_url 的域名与模型 id 合成（`ResolveCompat`），也可用 `providers.<id>.compat: [名字...]` 显式追加。

| 字段 | 含义 |
|---|---|
| `ReasoningSummary` | Responses：`reasoning.summary: "auto"` |
| `EncryptedReasoning` | Responses：`include: ["reasoning.encrypted_content"]`，回放时原样带回 |
| `ReasoningEveryTurn` | 思考模式下每个助手回合都要回放一条带 `reasoning_text`（或加密内容）的 reasoning 项，缺失时用文本或占位补齐（DeepSeek：否则 400 “reasoning_text … must be passed back”） |
| `NoToolChoice` | 不发送 `tool_choice` |
| `MaxOutputTokens` | 请求未设 max_tokens 时的上限（0 = 不发送） |
| `CacheHitField` | 用量里缓存命中的补充字段（DeepSeek 原生 `prompt_cache_hit_tokens`） |

适配文件：

- `compat_openai.go`：官方 OpenAI（`api.openai.com`）——摘要与加密推理。
- `compat_deepseek.go`：DeepSeek 模型（模型 id 含 `deepseek`，或 `api.deepseek.com`）——`ReasoningEveryTurn`、`CacheHitField`；chat/completions 的 `prompt_cache_hit_tokens` 回退也移到这里。
- `compat_commandcode.go`：`api.commandcode.ai`——摘要、加密推理、不发 `tool_choice`、`MaxOutputTokens` 64000。

未知网关：发送摘要与加密推理请求（Responses 的通用做法），其余按标准。

## Responses 请求

- `model`、`instructions`（req.System）、`input`、`tools`（`{type: function, name, description, parameters}`，无 `strict`）、`stream`、`store: false`。
- `reasoning`：设了思考档位时 `{effort, summary?}`；`off` 发送 `none`，`auto`/未设不发送。
- `include`：`EncryptedReasoning` 且为思考请求（设了档位或 `ReasoningEveryTurn`）时。
- `prompt_cache_key`：会话 id（ctx 中的 session id，≤64 字符）。
- 始终发送完整历史，不用 `previous_response_id`（第三方网关常不支持；与会话分段、折叠也冲突）。

历史 → `input`（先经 `groupToolBatches` 还原并行调用）：

| 消息 | input 项 |
|---|---|
| user / 中途的 system | `{role: user, content: [{type: input_text, text}]}`（command-code 不支持 developer 角色） |
| 助手文本 | `{type: message, role: assistant, content: [{type: output_text, text, annotations: []}]}` |
| 助手工具调用 | `{type: function_call, call_id, name, arguments}` |
| 工具结果 | `{type: function_call_output, call_id, output}` |
| 推理 | 该回合首条助手消息 Metadata 中保存的 reasoning 项（去掉 `id`、`status`），放在该回合最前；折叠过的不回放 |

每个“回合”= 连续的助手项（文本与调用）。

## 响应

- 非流式：`output` 中 `reasoning`（摘要文本或 `reasoning_text` 作为 Reasoning，原始项存入 Metadata `suag.responses_reasoning`）、`message`（`output_text` 拼成 Content）、`function_call`（→ ToolCall，参数解析失败沿用 ArgumentsError）。
- `status: incomplete` 且 `incomplete_details.reason = max_output_tokens`：截断，调用不执行（`markTruncated`）；`content_filter`：报错。
- 流式（SSE）：`response.output_text.delta` 实时发出；其余以终止事件 `response.completed` / `response.incomplete` 中的完整 `response` 为准，没有 `output` 时退到收集的 `response.output_item.done` 项；`error`、`response.failed` 报错；无终止事件即断流，走现有的重试。
- 用量：`input_tokens`（含缓存）→ InputTokens，`input_tokens_details.cached_tokens`（无则 `CacheHitField`）→ CacheReadTokens，`output_tokens` → OutputTokens。

## 配置

`providers.<id>.type` 或 `models.<key>.type` 取 `openai-responses`；`providers.<id>.compat` 可选（字符串列表）。HTTP 部分（重试、超时、请求头、流式断线重试）复用 OpenAI provider。

## 验证

单测覆盖请求体、历史映射（并行调用、推理回放与补齐、折叠）、非流式与流式解析、截断、用量；真实验证用 command-code `deepseek/deepseek-v4.1-flash`（多步工具任务），再与 chat/completions 做 T9 同模型 A/B。
