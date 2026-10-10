# Agentcore Working Memory Design

这份设计文档描述 `memory/` 的第一版目标与边界。

目标不是做完整的记忆系统，而是为 `agentcore` 提供一个最小、稳定、可扩展的默认 `Memory` 实现，让外部使用者可以在不自己从零实现 `agentcore.Memory` 的前提下，跑通一个真正可用的 context-aware runtime。

---

## 1. 背景

当前 `agentcore` 已经具备：

- 稳定的 runtime loop
- `RunID + ResumeMode` 恢复语义
- append-only transcript 与 run snapshot
- `Memory` 公共接口
- internal memory adapter

但它仍然缺少一个默认可用的 `Memory` 实现。

这带来两个问题：

1. 外部使用者虽然可以扩展 `Memory`，但第一次接入成本仍然偏高
2. `Memory` 公共 contract 虽然已经存在，但缺少一个 package-owned 默认实现来帮助收紧行为边界

因此，这一轮演进的目标是：

- 为 `agentcore` 增加一个内置 `memory/` 子包
- 提供一个最小但稳定的 `WorkingMemory` 默认实现
- 用这个默认实现把 `Memory` 的核心行为进一步测实、文档化、公共化

---

## 2. 目标

这一轮明确要达成的目标：

- 新增 `memory/`
- 提供默认 `WorkingMemory`，直接实现 `agentcore.Memory`
- 支持两个最小但真实有用的 context 组成部分
  - summary slot
  - recent transcript window
- 明确 `Hydrate / ObserveMessages / BuildContext` 三段生命周期语义
- 保持与 `Storage` 边界清晰
- 保持对未来 summary compaction / recall / artifact-aware context 的增长空间

---

## 3. 非目标

这一轮明确不做：

- 自动 LLM summary 生成
- retrieval / recall index
- 向量库或长期记忆后端
- token-budget 精确裁剪
- artifact-aware memory
- 新的 memory 持久化公共接口
- 复杂的 provider-specific context shaping

换句话说，这一轮做的是一个默认 working memory v0，而不是完整 memory framework。

---

## 4. 设计原则

### 4.1 Storage Is Authority, Memory Is Context

继续坚持 `agentcore` 当前最重要的边界：

- `Storage`
  authoritative transcript 与 run snapshot
- `Memory`
  model-facing context 的派生层

默认 memory 实现不能越界去承担 authority log 语义。

### 4.2 Rebuild Must Be Safe

`Hydrate` 必须是幂等、可重建、可作为保底对齐手段的。

### 4.3 Incremental Path Must Match Rebuild Path

`ObserveMessages` 是增量更新路径，但它的最终效果必须与“把完整 transcript 重新 hydrate”保持语义一致。

### 4.4 Small Default, Strong Growth

默认实现先解决：

- 好理解
- 好测试
- 好嵌入

不提前引入过多聪明行为。

---

## 5. 包结构

第一版建议新增：

- `memory/`

初始文件建议：

- `working.go`
  - `Config`
  - `WorkingMemory`
  - `New(...)`
  - `Hydrate / ObserveMessages / BuildContext`
- `summary.go`
  - `SummaryInjector`
  - `DefaultSummaryInjector`
- `working_test.go`
  - 行为测试矩阵

第一版不建议一开始拆成更多子目录或子模块。

---

## 6. Public API 草图

```go
package memory

type Config struct {
    MaxRecentMessages   int
    IncludeSystemMessages bool
    IncludeToolMessages bool
    SummaryInjector     SummaryInjector
}

type SummaryInjector interface {
    InjectSummary(summary string, state *agentcore.RunState) []agentcore.Message
}

type WorkingMemory struct {
    // internal derived state
}

func New(config Config) *WorkingMemory

func (m *WorkingMemory) Hydrate(ctx context.Context, state *agentcore.RunState) error
func (m *WorkingMemory) ObserveMessages(ctx context.Context, state *agentcore.RunState, messages ...agentcore.Message) error
func (m *WorkingMemory) BuildContext(ctx context.Context, state *agentcore.RunState) ([]agentcore.Message, error)

func (m *WorkingMemory) SetSummary(summary string)
func (m *WorkingMemory) Summary() string
```

关键取舍：

- `Summary` 能力不进入 `agentcore.Memory` 公共接口
- `SetSummary` 只属于默认 `WorkingMemory`
- `SummaryInjector` 是一个小扩展点，而不是更大的 context pipeline

---

## 7. 内部状态模型

`WorkingMemory` 第一版只维护两块派生状态：

- `summary`
  - 一段可选文本
  - 由 embedding host 显式设置
- `recent window`
  - 从 transcript 过滤并截断得到的最近消息列表

它不维护：

- retrieval index
- token ledger
- artifact cache
- provider-specific prompt fragments

---

## 8. 生命周期语义

### 8.1 Hydrate

职责：

- 根据 `RunState.Messages` 重建 `WorkingMemory` 的派生视图

要求：

- 幂等
- 允许重复调用
- 只重建可由 transcript 推导出的部分

默认行为：

- recent window 从 `state.Messages` 重新过滤和截断
- 已有 summary 保持不变

`Hydrate` 不应：

- 清空 summary
- 追加新消息
- 修改 `RunState`

### 8.2 ObserveMessages

职责：

- 在 runtime 正式提交新消息后，增量更新 working memory

默认行为：

- 将新增消息按顺序合并到 recent window
- 再执行过滤与截断
- 不修改 summary

约束：

- 保序
- 只做增量维护
- 最终效果应与“完整 transcript 再 hydrate 一次”语义兼容

### 8.3 BuildContext

职责：

- 读取当前派生状态，生成最终喂给模型的上下文消息

默认输出顺序：

1. summary messages
2. filtered recent window

约束：

- 只读
- 不应修改内部状态
- 多次调用输出应稳定

---

## 9. Summary 注入策略

第一版默认 summary 注入策略建议如下：

- 如果 summary 为空，不注入 summary message
- 如果 summary 非空，注入一条 `system` 消息
- message 内容使用稳定前缀，便于调试与测试

默认形态：

```go
agentcore.Message{
    Role:    "system",
    Content: "[Memory summary]\n" + summary,
}
```

存在 `SummaryInjector` 的目的是允许未来自定义：

- role
- 内容模板
- 是否完全禁用 summary message

但第一版默认只需要一个 `DefaultSummaryInjector` 即可。

---

## 10. Window 过滤与截断规则

### 10.1 过滤规则

基于两类配置：

- `IncludeSystemMessages`
- `IncludeToolMessages`

默认建议：

- `IncludeSystemMessages = false`
- `IncludeToolMessages = true`

具体行为：

- 普通 `user` 与普通 `assistant` 消息始终保留
- `system` 消息在 `IncludeSystemMessages=false` 时过滤掉
- `assistant(tool_call)` 与 `tool(result)` 在 `IncludeToolMessages=false` 时过滤掉

### 10.2 截断规则

`MaxRecentMessages` 针对过滤后的消息列表生效：

- `MaxRecentMessages <= 0`
  表示不截断
- `MaxRecentMessages > 0`
  保留最后 `N` 条

第一版明确不做：

- token-aware 裁剪
- tool_call / tool_result 成对补全
- role-sensitive 多层窗口

这些能力可以在后续版本里演进。

---

## 11. BuildContext 输出形状

默认输出形状固定为：

```text
summary message (0 or 1)
+ recent filtered messages
```

例如：

```text
system("[Memory summary]\nRepo has three packages.")
user("analyze this repo")
assistant(tool_call=search)
tool(result="found 3 packages")
assistant("I summarized the structure")
```

这个固定顺序的价值是：

- 可预测
- 易测试
- 对 embedding host 足够透明

---

## 12. 测试矩阵

第一版至少覆盖以下测试：

### 12.1 Hydrate

- 相同 transcript 多次 hydrate 输出一致
- hydrate 后 recent window 重建正确
- hydrate 不会覆盖已有 summary

### 12.2 ObserveMessages

- 新消息顺序保持不变
- 增量追加后按配置过滤和截断
- observe 后的输出与完整 hydrate 语义兼容

### 12.3 BuildContext

- 无 summary 时只输出 window
- 有 summary 时输出 `summary + window`
- 默认 summary injector 输出稳定 `system` message
- 多次 build 不产生状态漂移

### 12.4 Config

- `MaxRecentMessages <= 0` 表示不截断
- `IncludeSystemMessages=false` 生效
- `IncludeToolMessages=false` 生效
- 自定义 `SummaryInjector` 生效

---

## 13. 分阶段实现顺序

### Phase 1

- `Config`
- `WorkingMemory`
- `Hydrate`
- `BuildContext`
- `DefaultSummaryInjector`
- 基础测试

目标：

- 先跑通“summary + recent window”静态构建

### Phase 2

- `ObserveMessages`
- 增量更新测试
- observe / hydrate 一致性测试

目标：

- 让默认 memory 真正可挂进 runtime 正常工作

### Phase 3

- package-local 文档补充
- 使用示例补充
- 必要时新增 `examples/memory`

目标：

- 让默认 memory 能被外部使用者直接发现并使用

---

## 14. 完成定义

这一轮完成的标准不是“做出完整记忆系统”，而是：

- `agentcore` 拥有一个默认可用的 `Memory` 实现
- 行为简单、稳定、可预测
- 与 `Storage` 边界清晰
- 为后续更高级 memory 能力留出增长空间

如果达成这些目标，`agentcore` 会从“公共 contract 已稳定”继续前进一步，变成“默认就能跑出一个带基础上下文能力的 kernel”。
