# Agentcore Compatibility

这份文档描述 `agentcore` 当前阶段的兼容性预期、发布姿态和后续收敛方向。

它不是版本发布公告，也不是 changelog。

它回答的是：

- 现在哪些内容可以被视为相对稳定的公共 contract
- 哪些内容仍然处于 `v0` 快速演进阶段
- 如果要把 `agentcore` 当成可独立发布的包，发布前该检查什么

---

## 1. 当前发布姿态

`agentcore` 目前仍然处于 `v0` 阶段。

这意味着两件事同时成立：

- 它已经足够完整，可以承载真实单 agent runtime kernel
- 它还没有承诺“所有公共 surface 都完全冻结”

当前更准确的表述是：

- 运行语义正在收紧
- contract 正在稳定
- 默认实现仍允许谨慎演进

也就是说，`agentcore` 已经不只是实验品，但还没到“完全保守演进”的阶段。

---

## 2. 相对稳定的 Public Contract

如果当前要把 `agentcore` 嵌入到外部系统，下面这些内容可以视为优先稳定的公共面：

### 2.1 Runtime Input / Output

- `RunInput`
- `RunState`
- `RunResult`
- `RunID + ResumeMode` 语义

这些类型和语义决定：

- run 如何开始
- run 如何恢复
- embedding host 如何理解结果

这部分应尽量避免频繁破坏式调整。

### 2.2 Transcript Model

- `Message`
- `ToolCall`
- `ToolResult`
- `assistant(tool_call) + tool(result)` 成对持久化语义

这部分是恢复、replay、审计和跨 provider / adapter 互通的基础。

如果这部分频繁变化，外部接入成本会显著上升。

### 2.3 Core Extension Interfaces

- `Provider`
- `Storage`
- `Memory`
- `ToolRegistry`
- `PromptBuilder`
- `ContextBuilder`
- `Hook`

这些接口本身仍然可以继续微调，但不应该轻易做“重命名级别”或“职责重写级别”的大改。

### 2.4 Error Categories

- `ProviderError`
- `ToolError`
- `HookError`
- `StorageError`
- `MemoryError`

这部分已经开始承担 embedding host 的策略分流职责，所以更应该稳定。

---

## 3. 仍然允许演进的区域

下面这些区域当前仍然应该被视为“可以继续快速演进”。

### 3.1 First-Party Default Implementations

- `memory/`
- `context/`
- `prompt/`
- `mock/`
- `files/` 的非 contract 细节

以及新引入的最小 streaming surface：

- `text_delta`
- `message_stop`
- host-facing stream callback 形状

原因不是它们不重要，而是它们仍然处在“默认体验打磨期”。

只要不破坏核心 contract，这些默认实现还可以继续增强：

- builder 新入口
- 新示例
- 更多默认组合
- 更清晰的配置项

### 3.2 Package-Local Documentation Shape

- 文档结构
- 阅读顺序
- package-local examples 组织方式

这些都是可以继续优化的，因为它们本身就是独立发布准备的一部分。

### 3.3 Adapter Story

internal adapter 如何组织、如何放在仓库级位置，目前也仍然可以继续收敛。

关键原则只有一个：

- adapter 依赖 `agentcore`
- `agentcore` 不反向依赖 internal

---

## 4. 当前不应轻易破坏的语义

如果下一轮继续演进，下面这些语义最好被视为“变更成本很高”的区域。

### 4.1 Resume Semantics

- stable `RunID`
- explicit `ResumeMode`
- resume 时是否追加新 user input 的规则

这是外部最容易直接依赖的行为之一。

### 4.2 Storage / Memory Boundary

- `Storage` = authority
- `Memory` = derived context

这条边界一旦模糊，后续几乎所有 context / replay / recall 设计都会变乱。

### 4.3 Transcript Persistence Rules

特别是 tool loop：

1. `assistant(tool_call)`
2. `tool(result)`

这是 replay 与恢复可信性的关键。

### 4.4 Hook Control Expectations

哪些阶段允许真正控制流退出，哪些阶段只适合观察，这部分现在已经开始形成稳定预期。

后续如果继续调整，也应该通过：

- 更强测试
- 明确文档
- 尽量渐进

来推进，而不是静默改行为。

### 4.5 Streaming Authority Rules

当前 streaming phase 1 已经形成的关键预期是：

- stream delta 用于 host 渐进观测
- final `GenerateResult` 仍然是 authority completion
- authority transcript 仍然只从最终 completion 写入

后续如果继续增强 streaming，也不应轻易破坏这组规则。

---

## 5. 推荐的兼容性策略

在正式独立发布前，推荐按下面的策略看待兼容性。

### 5.1 `v0` 阶段的策略

允许：

- 默认实现增强
- 文档结构优化
- 新增配置项
- 新增 first-party builders / helpers

谨慎：

- 修改已有 runtime 语义
- 修改 transcript 形状
- 修改 resume 规则
- 修改核心接口职责

如果确实必须改，至少应满足：

- 测试覆盖先到位
- 文档同步说明
- 示例同步更新

### 5.2 到 `v1` 前应该达到的状态

在考虑更正式版本承诺前，至少应满足：

- 核心 contract 有明确边界测试矩阵
- examples 足够代表真实主路径
- package-local docs 能独立解释主要使用方式
- 默认实现与 contract 的职责边界稳定

---

## 6. 发布前检查清单

如果下一阶段开始把 `agentcore` 当成“可独立发布”的候选包，建议至少检查这些事项。

### 6.1 Contract Check

- `RunInput` / `RunState` / `RunResult` 是否有不必要的仓库私有语义
- 扩展接口职责是否清晰
- 错误分类是否足够稳定

### 6.2 Boundary Check

- 是否仍然坚持 `Storage` / `Memory` 分层
- 是否有默认实现偷偷承担了 authority 责任
- 是否有 hook 行为开始替代正式 contract

### 6.3 Documentation Check

- `README` 是否能独立解释包定位
- `docs/getting-started.md` 是否足以让新用户跑通
- `docs/api.md` 是否足以解释公共扩展接口
- package-local docs 是否仍然依赖大量仓库语境

### 6.4 Examples Check

- examples 是否覆盖最小主路径
- examples 是否与文档一致
- package-local examples 是否不依赖 internal

### 6.5 Verification Check

至少保证：

```bash
go test ./pkg/agentcore/... ./examples/agentcore/bridge
```

在仓库当前状态下持续通过。

---

## 7. 下一阶段最值得推进什么

如果以兼容性和发布姿态为优先级，接下来推荐顺序是：

1. 继续收紧 contract tests
2. 明确 versioning / change policy
3. 把 examples 尽量做成长期可信的主路径示例
4. 再进入 streaming contract 设计

也就是说：

> 先让别人敢依赖，再让内核变得更强。

---

## 8. 如何使用这份文档

这份文档适合在下面几种场景中使用：

- 评估某个改动是否会破坏已形成的公共 contract
- 判断一个新能力应该先放在 default implementation 还是直接进入核心接口
- 评估 `agentcore` 离独立发布还有哪些差距

如果未来引入正式版本策略，这份文档应继续收敛成：

- compatibility policy
- release checklist
- migration guidance

而不是长期停留在“宽泛姿态说明”。

进一步的版本与变更分级建议，继续看：

- [versioning.md](./versioning.md)

如果想把当前状态当成独立包发布候选来评估，再继续看：

- [release-readiness.md](./release-readiness.md)
