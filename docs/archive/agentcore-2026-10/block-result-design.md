# Agentcore Block Result Design

这份文档记录 `agentcore` 长期支持 mixed model output 的设计方向。

这里的 mixed output 指一次模型响应里同时包含：

- assistant text
- tool calls

典型例子是：

```text
我先看一下当前目录。
<tool_call name="file_list" args="{\"path\":\".\"}">
```

当前 v0 运行时仍保持 `GenerateResult.Content` 与 `GenerateResult.ToolCalls`
二选一的严格契约。这个限制对当前主线稳定性有价值，但长期会难以表达更真实的 provider 输出形态。

---

## 1. 背景

当前 `GenerateResult` 是一个扁平结构：

```go
type GenerateResult struct {
    Content      string
    ToolCalls    []ToolCall
    FinishReason FinishReason
    Usage        Usage
    Artifacts    []Artifact
    Metadata     map[string]any
}
```

`Validate()` 会拒绝 `Content` 和 `ToolCalls` 同时存在。

这个约束让 runtime 主路径非常清晰：

- 有 `Content` 就是 final assistant answer
- 有 `ToolCalls` 就进入 tool loop

但真实 provider 响应不总是这么干净。OpenAI-compatible、Claude-style 以及未来 streaming/tool-aware provider 都可能返回有序 block：

- text preamble
- tool call
- more text
- more tool call

如果长期继续在 provider adapter 里直接丢弃这些 text，会损失模型意图、UI 展示信息和调试线索。

---

## 2. 目标

长期目标是让 `agentcore` 能表达有序的模型输出块，同时不破坏当前 v0 主线。

具体目标：

- 支持一次 model result 中同时包含 text 与 tool calls
- 保留当前 `Content` / `ToolCalls` 的兼容投影视图
- 让 runtime 可以逐步迁移到 block-first 消费模型
- 让 host / AG-UI / web 后续可以选择展示 tool 前说明文本
- 不把 provider-specific response shape 泄漏进 runtime kernel

---

## 3. 非目标

这一阶段不做：

- 立即重写 runtime tool loop
- 立即改变 transcript authority 规则
- 立即把所有 streaming delta 写入 transcript
- 立即要求 host / web 完整消费 blocks
- 引入 provider-specific block 类型

这不是一次 UI 产品化设计，而是 kernel contract 的长期演进方向。

---

## 4. 推荐模型

推荐新增 block-based result model，而不是简单放宽 `Content + ToolCalls`。

方向示例：

```go
type OutputBlockKind string

const (
    OutputBlockText     OutputBlockKind = "text"
    OutputBlockToolCall OutputBlockKind = "tool_call"
)

type OutputBlock struct {
    Kind     OutputBlockKind
    Text     string
    ToolCall *ToolCall
    Metadata map[string]any
}

type GenerateResult struct {
    Blocks       []OutputBlock
    FinishReason FinishReason
    Usage        Usage
    Artifacts    []Artifact
    Metadata     map[string]any

    // Compatibility projection for existing consumers.
    Content   string
    ToolCalls []ToolCall
}
```

`Blocks` 是更完整的 authority completion 表达。

`Content` / `ToolCalls` 是兼容视图：

- 纯 text blocks 可投影为 `Content`
- 纯 tool call blocks 可投影为 `ToolCalls`
- mixed blocks 不应该被误判为非法，但旧视图需要明确降级规则

---

## 5. 为什么不直接允许 Content + ToolCalls

直接放宽现有字段看起来改动小，但语义会变模糊。

主要问题：

- `Content` 是 final answer，还是 tool 前过渡语？
- mixed `Content` 是否应该写入 transcript？
- tool 执行后，这段 text 是否参与下一轮 context？
- UI 是否要显示它？
- replay 时如何表达它与 tool call 的顺序？

block model 可以把这些问题显式化：

- text block 是 text block
- tool call block 是 tool call block
- 顺序由 blocks 保证
- 是否投影到旧字段由兼容层决定

---

## 6. Runtime 消费规则

建议分阶段迁移。

### Phase A: 引入 Blocks，但保持当前行为

- `GenerateResult` 新增 `Blocks`
- provider 可以填 `Blocks`
- runtime 仍通过兼容投影视图消费
- `Validate()` 允许 `Blocks` 存在，但仍要求旧字段形态保持可解释

这一阶段主要建立 contract，不改变主路径。

### Phase B: Runtime Block-First

runtime 改为优先看 `Blocks`：

- 如果 blocks 全是 text，作为 final answer
- 如果 blocks 包含 tool call，进入 tool loop
- mixed text + tool call 时，tool call 决定 step 进入 tool loop
- mixed text 暂时不写成 final assistant answer，可放入 metadata 或 model event

这一阶段让 provider 不再需要丢弃 mixed text，但仍不改变 transcript authority。

### Phase C: Host / UI 消费 Mixed Text

host 可以选择把 tool 前 text 作为 observation event 转发给 UI。

例如：

- AG-UI 显示 `我先看一下当前目录`
- 然后显示 tool call activity
- tool result 回来后继续下一轮模型输出

这一阶段属于产品展示增强，不应该阻塞 Phase A/B。

---

## 7. Transcript 规则

短期建议：

- final text 才写入 assistant final message
- tool call 仍写入 assistant tool-call message
- tool result 仍写入 tool result message
- mixed preamble text 默认不作为 authority transcript 写入

原因：

- preamble 通常是过渡语，不是最终回答
- 过早把它写入 transcript 会污染下一轮 context
- UI 展示需求可以先通过 stream/model event 解决

长期可以再引入更细粒度 transcript message block，但这应该是单独设计项。

---

## 8. Streaming 关系

block result 与 streaming contract 是互补关系。

当前 streaming design 的原则是：

- stream event 用于渐进观测
- final `GenerateResult` 仍是 authority completion

block result 可以成为 final completion 的更精确表达：

- streaming 阶段发 `text_delta`
- streaming 阶段发 `tool_call`
- 最终收敛成 `GenerateResult.Blocks`

这样不会让 stream delta 直接污染 transcript，也能保持 replay 的稳定性。

---

## 9. Provider 适配规则

provider adapter 应负责把厂商响应映射成统一 blocks。

OpenAI-compatible：

- `message.content` 映射为 text block
- `message.tool_calls[]` 映射为 tool_call blocks
- `function.arguments` 继续兼容 JSON string 与 JSON object 两种形态

Claude-style：

- `content[].type == "text"` 映射为 text block
- `content[].type == "tool_use"` 映射为 tool_call block

provider adapter 不应该把 provider-specific block 直接透出到 runtime。

---

## 10. 兼容策略

为了避免破坏已有使用者，建议：

- 保留 `Content`
- 保留 `ToolCalls`
- 新增 `Blocks`
- 提供 normalize helper，统一补齐 blocks 与旧字段
- conformance tests 同时覆盖旧字段与 blocks

旧字段投影规则建议：

- 如果 `Blocks` 为空，沿用旧字段
- 如果 blocks 只有 text，合并为 `Content`
- 如果 blocks 只有 tool calls，填充 `ToolCalls`
- 如果 blocks mixed，`ToolCalls` 可作为执行视图，text blocks 保留在 `Blocks`

是否允许 mixed blocks 同时投影出 `Content`，需要谨慎。默认不建议，因为旧字段消费者会把 `Content` 理解为 final answer。

---

## 11. 影响面

需要改动的核心区域：

- `pkg/agentcore/api`
  定义 `OutputBlock`、扩展 `GenerateResult`、调整 `Validate()`
- `pkg/agentcore/runtime`
  从旧字段分支逐步迁移到 block-first 分支
- `pkg/agentcore/mock` 与 `pkg/agentcore/testing`
  增加 block result 构造和 conformance 覆盖
- provider adapter
  OpenAI / Claude 等映射到 blocks
- hooks / clone helpers
  保证 `ModelEvent.Result` 里的 blocks 被安全复制

可以延后处理的区域：

- host AG-UI mapper
- web 展示
- transcript block persistence
- streaming checkpoint

---

## 12. 推荐推进顺序

推荐顺序：

1. 继续保持 v0 provider 层归一化，避免当前主线失败
2. 在 `agentcore` 文档中明确 block result 是长期方向
3. 后续单独开一轮 Phase A 实现，只加 contract 和测试
4. 再推进 runtime block-first
5. 最后让 host / UI 消费 mixed text

这样可以避免为了一个 provider 兼容问题仓促扩大 kernel，同时为长期 mixed output / streaming / AG-UI 留出正确结构。

---

## 13. 当前决策

当前 v0 决策：

- 不立即放宽 `Content + ToolCalls`
- 不立即让 mixed content 进入 transcript
- provider adapter 继续把 mixed result 归一成 runtime 当前可消费形态
- 长期设计方向保留为 block-based `GenerateResult`

这让 v0 可以继续稳定推进，同时避免把错误的二义性写进 `agentcore` 公共契约。
