# Agentcore Recommended Runtime Path

这份文档描述 `agentcore` 当前更推荐的 `v0` 起步路径。

它不是在说“只有这一种接法是正确的”，而是在回答：

- 如果你准备把 `agentcore` 嵌入到自己的 host 里，当前最稳妥的起点是什么
- 哪些组件值得优先使用第一方默认实现
- 哪些地方应该优先围绕公共 contract 留扩展点

---

## 1. 推荐组合

当前更推荐的 starter path 是：

- `files.NewStorage(...)`
- `memory.New(...)`
- `context.MemoryFirst(...)` 或 `context.MemoryThenRecent(...)`
- `prompt.Layered(...)`
- 显式 `RunID + ResumeMode`
- 有需要时再接 `RunInput.Stream`

可以把它理解成：

- `files/` 提供 authority persistence 起点
- `memory/` 提供最小但够用的 derived context 起点
- `context/` 决定模型最终看到的消息组合方式
- `prompt/` 决定 system prompt 的稳定组合方式
- `ResumeMode` 负责把 pause / continue 做成显式行为
- streaming 是 optional capability，不应该改写 authority rules

---

## 2. 为什么推荐这条路径

### 2.1 它先把 authority 跑稳

无论以后 memory、context、prompt 怎样继续增强，最先要跑稳的仍然是：

- `Storage`
- transcript
- resume

所以推荐从 `files/` 开始，而不是一上来就为外部数据库或复杂 memory 策略设计过多抽象。

### 2.2 它让上下文工程留在正确边界里

推荐边界是：

- `Storage` 保存 authority transcript
- `Memory` 生成 derived context
- `ContextBuilder` 负责排布给模型的 messages
- `PromptBuilder` 负责 system prompt

这能避免常见混乱：

- 把 summary 塞进 storage 事实层
- 把 message assembly 藏进 prompt builder
- 把 authority 语义偷偷放进 context layer

### 2.3 它允许渐进增强而不是一次做大

这条路径天然支持按顺序加能力：

1. 先跑通最小 runtime
2. 再加 memory
3. 再加 context preset
4. 再加 layered prompt
5. 再跑通 yield / resume
6. 最后在需要时打开 streaming

这样更符合 `agentcore` 当前的 `v0` 姿态：

- kernel 要先稳
- default 要够用
- 高级能力走 additive，而不是推翻主路径

---

## 3. 每一层推荐怎么选

### 3.1 Storage

推荐起点：

- `files.NewStorage(...)`

适合原因：

- 行为简单
- 最容易观察 transcript 与 snapshot
- 对 examples、tests、外部嵌入都友好

什么时候再换：

- 需要共享存储
- 需要数据库事务
- 需要更强审计或多实例恢复

### 3.2 Memory

推荐起点：

- `memory.New(memory.Config{...})`

适合原因：

- 已提供 summary slot
- 已提供 recent transcript window
- 已支持 system/tool 消息过滤

什么时候再自定义：

- 需要 retrieval
- 需要 compaction policy
- 需要 artifact-aware context

### 3.3 Context

推荐顺序：

- 没有 memory 时：`context.TranscriptWindow(...)`
- 有 memory 且想直接用派生上下文时：`context.MemoryFirst(...)`
- 想保留 memory 结果同时显式补最近 transcript 时：`context.MemoryThenRecent(...)`

推荐理解：

- `MemoryFirst` 更像“derived context 是主路径”
- `MemoryThenRecent` 更像“derived context + recent ground truth”

### 3.4 Prompt

推荐顺序：

- 只要固定 prompt：`prompt.StaticBuilder`
- 需要稳定组合 base prompt、metadata、固定段落：`prompt.Layered(...)`

`Layered(...)` 适合作为当前第一方默认入口，因为它：

- 组合规则稳定
- metadata 注入显式
- 容易被 host 理解和调试

### 3.5 Resume

推荐始终显式使用：

- 稳定 `RunID`
- 明确 `ResumeMode`

不要把恢复理解成“再调一次 Run，大概会接着往下走”。

更推荐把它理解成：

- host 明确选择继续哪一次 run
- host 明确选择是否追加新输入
- runtime 明确按照该规则恢复

### 3.6 Streaming

推荐理解成：

- host-facing gradual delivery
- optional provider capability
- authority transcript 之外的观测层

当前不推荐的理解是：

- “streaming 出来的 delta 就是事实层答案”

当前 authority 仍然是最终 `GenerateResult`。

---

## 4. 推荐主路径示例

当前 package-local example 入口：

- [../examples/recommended/main.go](../examples/recommended/main.go)

这个示例故意把几件最容易一起出现的事情放到同一个 host 场景里：

- 第一轮 run 使用 tool loop
- host 通过 hook 让 run 显式 yield
- 第二轮 run 使用相同 `RunID` 恢复
- host 使用 `ResumeWithInput` 追加新输入
- provider 通过 streaming 把最终答案增量发回 host
- final `RunResult` 仍然保留 authority completion

它想表达的不是“所有项目都应该这样写”，而是：

- 这是当前更接近真实 embedding host 的 starter composition

---

## 5. 与其他示例如何配合阅读

推荐阅读顺序：

1. `examples/minimal`
2. `examples/memory`
3. `examples/context`
4. `examples/prompt`
5. `examples/recommended`
6. `examples/stream`
7. `examples/resume`

这样读的原因是：

- 先理解单点能力
- 再看推荐组合
- 最后再看更聚焦的 stream / resume 单项示例

---

## 6. 什么时候不该用这条路径

下面这些情况可以偏离推荐路径：

- 你已经有成熟的自定义 storage
- 你已经有明确的 memory architecture
- 你只想把 `agentcore` 当成极薄 runtime shell
- 你当前根本不需要 resume / persistence

即便如此，也建议先保留这些核心边界：

- `Storage = authority`
- `Memory = derived context`
- streaming 不改 authority
- resume 规则显式

---

## 7. 如何把这条路径视为 v0 产品起点

如果把 `agentcore` 看成一个 `v0` 产品，这条推荐路径的意义是：

- 它让新使用者不用自己发明默认组合
- 它让后续文档和 examples 有一条长期主路径
- 它也让后续能力演进有更稳定的参照系

后续如果这条路径继续成熟，下一步最值得补的是：

- 更多围绕 `recommended` example 的 host integration 文档
- 更清晰的 release checklist
- 更多 resume / transcript edge coverage
