# Contract Matrix

这份文档把 `agentcore` 当前最重要的 public surface，按“能否依赖、已经验证到哪一层、还有哪些边界未完全收紧”整理成一张更容易扫描的矩阵。

它不是完整 API 文档，也不是发布 checklist。

它更适合在下面几种场景里快速使用：

- 判断某个 surface 现在能不能被外部依赖
- 判断某个能力已经有 runtime-level 验证，还是已经有 host-level 验证
- 判断某个区域属于 kernel contract，还是 first-party default

## 1. 如何读这张矩阵

矩阵里的四个关键维度分别是：

- `Surface`
  你实际依赖的对象是什么
- `Current Status`
  当前对它的稳定性判断
- `Evidence`
  目前已经验证到了哪一层
- `Notes`
  还需要特别留意的边界或未完全统一的地方

这里的 `Evidence` 建议这样理解：

- `Runtime`
  主要由 runtime 自己的 tests / docs 支撑
- `Runtime + Host`
  不只 runtime 自己成立，也已经在 first-party host wrapper 上被验证承接
- `Default Path`
  主要是第一方推荐实现和示例已经比较清楚，但仍不宜过度依赖具体细节

## 2. Matrix

| Surface | Current Status | Evidence | Notes |
| --- | --- | --- | --- |
| `RunInput` / `RunState` / `RunResult` | `High confidence in v0` | `Runtime` | 已经是最核心的 runtime 数据 contract，不应轻易做职责翻转级变更 |
| `RunID + ResumeMode` 语义 | `Must stay conservative` | `Runtime` | 恢复主路径最容易被 embedding host 直接依赖 |
| `Message` / `ToolCall` / `ToolResult` | `High confidence in v0` | `Runtime` | transcript/replay/recovery 的核心骨架 |
| append-only transcript authority | `Must stay conservative` | `Runtime` | 影响 replay、resume、审计可信性 |
| `assistant(tool_call)` + `tool(result)` 成对持久化 | `Must stay conservative` | `Runtime` | tool loop authority 语义已经高度敏感 |
| `Provider` / `Storage` / `Memory` / `ToolRegistry` | `Stable direction, careful evolution` | `Runtime` | 接口职责已清晰，但仍应优先 additive 演进 |
| `PromptBuilder` / `ContextBuilder` | `Stable direction, careful evolution` | `Runtime` | 适合继续增强默认组合，但不应混入 authority 语义 |
| `Hook` | `Stable direction, careful evolution` | `Runtime` | 适合观察、注入、局部拦截；不应重新承接核心默认语义 |
| `ResumePolicy` | `Kernel-stable candidate` | `Runtime + Host` | 已经在 `AgentcoreRunner` 与 legacy `ReactRunner` 上验证承接 |
| `ToolExecutionPolicy` | `Kernel-stable candidate` | `Runtime + Host` | 已经覆盖 tool failure 升级与 host-facing parity |
| `ErrorPolicy` | `Kernel-stable candidate` | `Runtime + Host` | 已经覆盖 recoverable error -> yield 的 host-facing parity |
| `ProviderError` / `ToolError` / `HookError` / `StorageError` / `MemoryError` | `Should keep tightening` | `Runtime` | 一旦 host 用它们做策略分流，就已经进入对外 contract |
| first-party `files/` | `Useful but still evolving` | `Default Path` | 适合默认起步，但不要把实现细节当公共协议 |
| first-party `memory/` / `context/` / `prompt/` | `Useful but still evolving` | `Default Path` | 推荐路径已开始成型，但仍在继续收紧 |
| first-party `mock/` | `Useful but still evolving` | `Default Path` | 更像测试/示例辅助，而不是长期标准实现 |
| `testing/` conformance helpers | `Needed for independent package posture` | `Runtime` | 应持续保持只依赖 public contract |
| first-party host wrapper conformance | `Important before calling contracts host-ready` | `Runtime + Host` | 当前重点覆盖了三类 policy，但 lifecycle parity 仍未完全统一 |
| package-local `docs/` / `examples/` | `Important release surface` | `Default Path` | 对独立包来说属于产品表面，不只是附属材料 |

## 3. 当前最值得外部直接依赖的区域

如果今天要把 `agentcore` 嵌进外部系统，当前最值得直接依赖的是：

- 核心 runtime 数据 contract
- authority / transcript / resume 语义
- 三类 runtime policy contract
- 错误分类模型

这些是最接近 kernel surface 的区域。

## 4. 当前更适合“参考使用”的区域

下面这些区域今天已经可用，但更适合被理解成 first-party defaults：

- `files/`
- `memory/`
- `context/`
- `prompt/`
- `mock/`
- examples 的具体组织方式

依赖它们是合理的，但更应该依赖它们“服从的 contract”，而不是具体实现细节。

## 5. 当前还没完全统一的地方

最值得明确说出来的，不是某个 contract 不成立，而是仍有几处“contract 已对齐、lifecycle 还未完全统一”的区域：

- `AgentcoreRunner` 与 legacy `ReactRunner` 的 resume 生命周期仍不同
- 两条 first-party host wrapper 的 loop ownership 仍不同
- legacy path 仍保留一层默认行为兼容层

这意味着：

- contract 已开始收紧
- 实现统一仍是后续工作

## 6. 搭配阅读

如果你想继续看：

- [kernel-stability.md](./kernel-stability.md)
- [runtime-policies.md](./runtime-policies.md)
- [host-conformance.md](./host-conformance.md)
- [release-readiness.md](./release-readiness.md)
