# Agentcore Streaming Design

这份文档描述 `agentcore` 下一轮 streaming contract 的最小设计方向。

它关注的是：

- streaming 应不应该成为正式 contract
- 如果要成为正式 contract，最小 public surface 应该长什么样
- streaming 与 transcript、resume、tool loop、hooks 的关系怎么定义

这份文档不直接引入实现，也不要求当前 runtime 立刻全面改造。

---

## 1. 为什么要做 Streaming

当前 `agentcore` 的模型调用 contract 是：

- runtime 构造 `GenerateRequest`
- provider 返回一个完整的 `GenerateResult`

这个 contract 很稳定，也足够支持：

- final answer
- tool calls
- transcript persistence
- yield / resume

但它还不够支持一些真实使用场景：

- UI 需要逐步显示模型输出
- host 需要边生成边观测
- 外部系统希望在模型完成前获取 token / text 增量
- 更复杂的 tool-call 前置体验

所以 streaming 值得进入 kernel，不是因为“更酷”，而是因为它已经开始成为 runtime 级能力，而不是 provider 私有特性。

---

## 2. 目标

这一轮的目标是：

- 为 `agentcore` 定义一个正式 streaming contract 方向
- 保持与现有 `GenerateRequest` / `GenerateResult` 模型兼容
- 不推翻现有 runtime loop
- 不把 streaming 先做成 provider-specific feature

更具体一点：

- 先定义 event model
- 先定义最小 provider streaming interface
- 先定义 runtime 应如何消费 stream
- 先定义 stream 与 transcript / resume 的边界

---

## 3. 非目标

这一轮明确不做：

- token-level provider 细节统一
- 多种 stream transport 适配
- UI protocol
- streaming tool execution pipeline
- partial transcript persistence 策略落地
- cancellation / backpressure 的完整工程化方案

换句话说，这一轮做的是 kernel contract 设计，不是完整 streaming 产品化。

---

## 4. 设计原则

### 4.1 Streaming Is A First-Class Contract

stream 不应只是 hook 里偷偷观察 provider 内部 token。

更推荐的方向是：

- streaming 是正式 public contract
- hook 只负责观察 streaming 事件，而不是替代 streaming contract 本身

### 4.2 Final Result Remains Authoritative

即使引入 stream，最终仍然要有一个完整、可验证的终态结果。

也就是说：

- streaming 事件用于渐进观测
- `GenerateResult` 仍然是本次模型调用的 authority completion

### 4.3 Transcript Must Stay Coherent

streaming 不能破坏当前 transcript 语义。

特别是：

- final assistant message 的落盘仍应稳定
- tool loop transcript 仍应保持 `assistant(tool_call) + tool(result)` 语义

不应该因为引入 stream，就把 transcript 变成一堆不稳定碎片。

### 4.4 Resume Must Stay Explicit

stream 中断不能变成“半隐式恢复”。

即使未来支持 streaming 中断，恢复点的 authority 仍然应该落在：

- stable `RunID`
- explicit `ResumeMode`
- persisted snapshot / transcript

---

## 5. 最小 Event Model

建议 `agentcore` 先引入一个最小 streaming event model。

例如：

```go
type StreamEventKind string

const (
    StreamEventTextDelta    StreamEventKind = "text_delta"
    StreamEventToolCall     StreamEventKind = "tool_call"
    StreamEventMessageStop  StreamEventKind = "message_stop"
)

type StreamEvent struct {
    Kind     StreamEventKind
    Delta    string
    ToolCall *ToolCall
    Metadata map[string]any
}
```

这里刻意保持很小：

- `text_delta` 渐进文本输出
- `tool_call` 流式生成过程中已经形成的工具调用
- `message_stop` 一次模型消息流结束

这一轮先不做：

- token logprobs
- reasoning trace
- provider-specific event kinds

---

## 6. Provider Contract 方向

当前 `Provider` 仍然保持：

```go
type Provider interface {
    Name() string
    Generate(ctx context.Context, req *GenerateRequest) (*GenerateResult, error)
}
```

推荐下一轮不是直接破坏它，而是增加一个可选 streaming capability：

```go
type StreamingProvider interface {
    Provider
    Stream(ctx context.Context, req *GenerateRequest, emit func(StreamEvent) error) (*GenerateResult, error)
}
```

这样做的好处是：

- 兼容现有 `Provider`
- 支持 runtime 根据 capability 选择 stream/non-stream 路径
- 仍然以最终 `GenerateResult` 作为 authority completion

其中：

- `emit(...)` 用于逐步对外发送 stream event
- `GenerateResult` 仍作为最终收敛结果返回

---

## 7. Runtime Consumption Model

runtime 的最小支持方式建议是：

1. 检查 provider 是否实现 `StreamingProvider`
2. 如果 host 请求 streaming，则走 `Stream(...)`
3. runtime 逐步接收 stream events
4. runtime 最后仍拿到完整 `GenerateResult`
5. 后续逻辑仍沿用现有 final/tool-call 分支

这样做的关键好处是：

- tool loop 不需要整套重写
- transcript persistence 规则基本不变
- 现有 non-streaming provider 完全不受影响

---

## 8. Host Surface 方向

runtime 层也需要一个最小 host-facing stream surface。

建议方向是：

```go
type StreamHandler interface {
    OnStreamEvent(ctx context.Context, event StreamEvent) error
}
```

或者更轻量：

```go
type StreamFunc func(ctx context.Context, event StreamEvent) error
```

配置上可以通过 `RunInput` 或 `Config` 逐步引入，但这一轮不急着定死最终入口。

关键是先明确：

- stream 是 host-facing contract
- hook 不是唯一出口

---

## 9. 与 Transcript 的关系

这是最需要小心的地方。

推荐策略：

- streaming 增量默认不直接写入 authority transcript
- authority transcript 仍然以最终完成态消息为准

也就是说：

- text delta 可以被 host 看到
- final assistant message 才进入 transcript

对于 tool calls：

- provider streaming 过程中如果形成 tool call，可以发 `tool_call` event
- 真正进入 transcript 时，仍按现有规则落：
  1. `assistant(tool_call)`
  2. `tool(result)`

这样可以避免 authority transcript 变成 provider 细节日志。

---

## 10. 与 Resume 的关系

streaming 不能改变当前 resume 语义。

推荐先坚持：

- 只有已经持久化的 snapshot / transcript 才是恢复依据
- 未完成的 stream delta 不应被当作可恢复 authority

这意味着：

- stream 中断后，如果没有形成最终 completion，就不应假装存在一个可恢复的半消息
- 未来如果真的支持 stream checkpoint，也应是显式 contract，而不是隐式行为

---

## 11. 与 Hooks 的关系

hooks 仍然有价值，但定位应是：

- 观察 streaming 事件
- 注入轻量 metadata
- 做审计和观测

而不是：

- 通过 hook 替代 streaming contract

如果后续需要 hook 感知 stream，更自然的方式是新增类似：

- `OnStreamEvent`

而不是让 `BeforeModel` / `AfterModel` 承担整个 stream 生命周期。

---

## 12. 分阶段推进建议

### Phase 1: Design Only

先完成：

- event model
- optional `StreamingProvider`
- runtime consumption strategy
- transcript / resume boundary 文档

### Phase 2: Minimal Runtime Support

先支持：

- `text_delta`
- `message_stop`
- final `GenerateResult`

先不碰 tool streaming。

### Phase 3: Tool-Aware Streaming

再支持：

- `tool_call` event
- tool-call-ready 到 transcript persistence 的衔接

### Phase 4: Advanced Topics

再考虑：

- cancellation
- backpressure
- stream-aware hooks
- stream checkpoint / partial persistence

---

## 13. 完成定义

这一轮设计完成时，应达到：

- streaming 被明确定位为正式 contract，而不是 hook hack
- 现有 non-streaming runtime loop 不被推翻
- `GenerateResult` 仍然是 authority completion
- transcript / resume 语义不因 streaming 变模糊

如果达到这些目标，下一轮就可以安全进入 streaming 的最小实现阶段，而不会把 `agentcore` 从 kernel 方向拉偏。

---

## 14. Phase 1 Scope Lock

当前 Phase 1 实现范围进一步收窄为：

- `text_delta`
- `message_stop`
- optional `StreamingProvider`
- `RunInput.Stream`

这一轮刻意不包含：

- `tool_call` stream event 的 runtime 消费
- stream-aware transcript persistence
- stream-specific hook lifecycle

这样做是为了先把最小 host-facing streaming contract 稳住，再继续向 tool-aware streaming 推进。
