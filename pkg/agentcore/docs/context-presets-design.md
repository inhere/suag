# Agentcore Context Presets Design

这份设计文档描述 `pkg/agentcore/context` 下一轮默认组合能力的目标与边界。

这轮的目标不是设计一套新的 context framework，而是为 `agentcore` 提供几种第一方、可直接使用、边界清晰的默认 `ContextBuilder` 组合，让外部使用者不需要自己手搓最常见的 context 拼装方式。

---

## 1. 背景

当前 `agentcore` 已经具备：

- `PromptBuilder` / `ContextBuilder` 公共接口
- `pkg/agentcore/context.DefaultBuilder`
- `pkg/agentcore/memory.WorkingMemory`
- `RunID + ResumeMode` 驱动的恢复语义

但默认 context story 仍然偏底层：

- 外部使用者虽然可以自己实现 `ContextBuilder`
- 也可以直接依赖 `Memory.BuildContext(...)`
- 但缺少几种“第一方推荐组合”

这使得当前接入路径仍然存在一个问题：

- 核心 contract 已经稳定
- 默认实现也开始具备可用性
- 但“怎么把这些默认件拼成一条自然路径”还没有被公共化

因此，这一轮的目标是：

- 在 `pkg/agentcore/context` 中新增命名清晰的第一方预置组合
- 让“不用 memory”和“用默认 memory”两条路径都拥有可直接使用的 builder
- 不引入新的 runtime preset 层，不把 `Runtime.Config` 做胖

---

## 2. 目标

这一轮明确要达成的目标：

- 在 `pkg/agentcore/context` 提供第一方预置 `ContextBuilder`
- 至少覆盖两条核心路径
  - transcript window
  - memory-first
- 预留第三条增强路径
  - memory + recent transcript
- 保持 `context` 只负责 messages 组合，不读取或修改 storage authority
- 保持 `memory` 只负责派生上下文状态，不把组合逻辑塞回 `memory`
- 不改变现有 `agentcore.ContextBuilder` 公共接口

---

## 3. 非目标

这一轮明确不做：

- runtime 级 preset config
- 新增 `presets/` 子包
- token-aware context trimming
- tool pair 自动补全
- memory output 与 recent transcript 的自动去重
- metadata-driven 动态上下文切换
- provider-specific context shaping
- 复杂 layered prompt framework

换句话说，这一轮做的是第一方预置组合，而不是 context composition platform。

---

## 4. 设计原则

### 4.1 Keep Presets at Builder Layer

预置组合应放在 `context/` 与 `prompt/` 层，而不是放进 `Runtime` 根层。

这样做可以：

- 保持现有边界稳定
- 避免 `Runtime.Config` 膨胀
- 让默认件以“第一方 builder”的方式自然增长

### 4.2 Context Only Shapes Messages

`ContextBuilder` 只负责：

- 从已有状态和已有依赖生成 provider messages

它不负责：

- 修改 memory
- 持久化数据
- 推断 provider-specific 行为

### 4.3 Memory Is Explicit

涉及 memory 的 builder 应显式接收 `agentcore.Memory`，而不是偷偷从 runtime 或 state 里查找。

这样依赖关系更清楚，也不需要改 `ContextBuilder` 公共接口。

### 4.4 Small Presets, Strong Defaults

这轮预置组合应优先覆盖：

- 最容易理解的 baseline
- 最自然的默认 memory 使用路径

而不是追求“组合能力最强”。

---

## 5. 预置组合范围

### 5.1 `TranscriptWindow`

用途：

- 不依赖 memory
- 直接从 `RunState.Messages` 取最近窗口

适合：

- 最小 runtime
- transcript-only 场景
- 调试 provider 输入形状

它是所有默认组合的 baseline。

### 5.2 `MemoryFirst`

用途：

- 优先使用 `Memory.BuildContext(...)`
- memory 不存在时回退到 transcript window

适合：

- 默认 `WorkingMemory` 路径
- 希望 context 主要由 memory 决定的场景

这是这一轮最重要的一条默认 story。

### 5.3 `MemoryThenRecent`

用途：

- 先用 `Memory.BuildContext(...)`
- 再显式追加 recent transcript window

适合：

- memory 提供 summary / derived context
- 但调用方仍希望显式保留最近原始消息

这一项建议作为本轮后半步增强，而不是最先实现的内容。

---

## 6. API 草图

建议公开 API 保持直白：

```go
package context

type WindowConfig struct {
    MaxMessages int
}

func TranscriptWindow(cfg WindowConfig) agentcore.ContextBuilder

type MemoryFirstConfig struct {
    MaxMessages int
}

func MemoryFirst(mem agentcore.Memory, cfg MemoryFirstConfig) agentcore.ContextBuilder

type MemoryThenRecentConfig struct {
    MaxMessages int
}

func MemoryThenRecent(mem agentcore.Memory, cfg MemoryThenRecentConfig) agentcore.ContextBuilder
```

这里刻意使用构造函数返回 `agentcore.ContextBuilder`，而不是强调很多 exported struct，原因是：

- 外部用起来更顺手
- 内部实现以后更容易调整
- 这轮目标是“默认组合”，不是开放一个 builder inheritance 层

内部可以使用私有 struct 实现：

- `transcriptWindowBuilder`
- `memoryFirstBuilder`
- `memoryThenRecentBuilder`

---

## 7. 精确定义

### 7.1 `TranscriptWindow`

行为：

- 从 `RunState.Messages` 读取消息
- 按 `MaxMessages` 保留最后 `N` 条
- `MaxMessages <= 0` 表示不截断
- 返回消息拷贝
- `state == nil` 时返回空

它不读取 `Memory`，也不注入任何额外消息。

### 7.2 `MemoryFirst`

行为：

1. 如果 `mem == nil`
   - 回退到 transcript window
2. 如果 `mem != nil`
   - 直接返回 `mem.BuildContext(...)`
3. 不对 memory 输出做二次裁剪
4. 不追加 recent transcript

重要约束：

- memory 返回空列表时，视为合法结果
- 不因为结果为空而偷偷 fallback
- 只有 `mem == nil` 才回退

### 7.3 `MemoryThenRecent`

行为：

1. 如果 `mem != nil`
   - 先取 `mem.BuildContext(...)`
2. 然后追加 recent transcript window
3. recent transcript 只对 appended 那一段应用 `MaxMessages`
4. 不对 memory 输出做二次裁剪
5. 不做去重

如果 `mem == nil`，则只输出 recent transcript window。

---

## 8. 配置语义

这轮建议统一配置语义：

- `MaxMessages <= 0`
  不截断
- `MaxMessages > 0`
  保留最后 `N` 条

但它在不同 preset 中作用不同：

- `TranscriptWindow`
  对 transcript window 生效
- `MemoryFirst`
  只对 fallback transcript window 生效
- `MemoryThenRecent`
  只对 appended recent transcript 生效

这个取舍的意义是：

- 不让 context preset 去“二次裁剪” memory 输出
- 保持 `Memory` 和 `ContextBuilder` 的职责边界清楚

---

## 9. 关于现有 `DefaultBuilder`

当前 `context.DefaultBuilder` 不建议这一轮直接删除。

建议策略：

- 保留 `DefaultBuilder`，避免破坏现有引用
- 新增预置组合构造函数作为更明确的推荐入口
- 文档和 examples 逐步迁移到新入口

这样可以：

- 保持兼容
- 让未来 public story 更清晰

---

## 10. 测试矩阵

### 10.1 `TranscriptWindow`

至少覆盖：

- `MaxMessages` 截断正确
- `<= 0` 时不截断
- 返回消息拷贝而不是原始 slice
- `state == nil` 返回空

### 10.2 `MemoryFirst`

至少覆盖：

- `mem != nil` 时直接使用 memory 输出
- `mem == nil` 时回退到 transcript window
- memory 输出为空时不偷偷 fallback
- memory 返回错误时错误透传

### 10.3 `MemoryThenRecent`

至少覆盖：

- 输出顺序为 `memory messages + recent transcript`
- appended recent transcript 按配置截断
- `mem == nil` 时只输出 recent transcript
- 不做去重

---

## 11. 实施优先级

建议顺序：

1. `TranscriptWindow`
2. `MemoryFirst`
3. `MemoryThenRecent`

原因：

- `TranscriptWindow` 是所有场景都能立即理解的 baseline
- `MemoryFirst` 是默认 `WorkingMemory` 的自然搭档
- `MemoryThenRecent` 是实用增强，但更容易引出下一层复杂问题

如果前两项已经形成完整对外 story，可以先提交一轮，再补第三项。

---

## 12. 文档与示例

这一轮如果落地实现，建议同步更新：

- [pkg/agentcore/README.md](../README.md)
- [pkg/agentcore/docs/getting-started.md](./getting-started.md)
- [pkg/agentcore/docs/api.md](./api.md)

必要时新增或更新 example，展示：

- transcript-only 路径
- default memory 路径

---

## 13. 完成定义

这一轮完成时应达到：

- `context/` 拥有命名清晰的第一方预置组合
- 默认 `WorkingMemory` 有自然搭档
- 外部使用者不需要自己手写最常见的 context 组合
- 文档能够清楚表达：
  - 不用 memory 怎么配
  - 用默认 memory 怎么配

如果达成这些目标，`agentcore` 会继续从“核心 contract 稳定”推进到“默认嵌入体验更顺手”，而不会把 kernel 推向过重的 preset framework。
