# Agentcore 与 Platform Contract 对齐设计

> 状态：Draft for review
> 日期：2026-04-14
> 范围：`pkg/agentcore/api` 与 `internal/platform/{llm,tools,memory}` 的 contract 收口
> 相关文档：`docs/v0-self-host-design.md`、`docs/usage/agentcore-internal-integration.md`、`docs/superpowers/specs/2026-04-02-agentcore-design.md`

---

## 1. 目标

这份文档定义 Axon 在进入 Host internal `session runner` 实装之前，需要先完成的一轮 runtime contract 收口设计。

这轮工作的目标不是直接让 Host 跑内部模型链路，而是先回答一个更基础的问题：

**`pkg/agentcore/api` 是否已经是内部运行时能力的主 contract，如果是，`internal/platform/llm`、`internal/platform/tools`、`internal/platform/memory` 应该如何自然地围绕它组织，而不是继续通过 `internal/acadapters/internal*` 做长期桥接。**

如果这件事不先处理好，后面一旦开始构建 Host internal `session runner`，很可能出现下面的问题：

- 先用现有 adapter 把链路接通
- 再因为 contract 割裂回头调整 `llm / tools / memory`
- Host run 主链和 adapter 结构一起返工

因此，这份设计文档明确把“contract 对齐”放在“session runner 接线”之前。

---

## 2. 问题背景

当前仓库已经形成了 `pkg/agentcore` 独立内核，以及一组迁移期 bridge：

- `pkg/agentcore/api`
- `internal/platform/llm`
- `internal/platform/tools`
- `internal/platform/memory`
- `internal/acadapters/internalllm`
- `internal/acadapters/internaltools`
- `internal/acadapters/internalmemory`

这组结构在迁移初期是合理的，因为它让旧 `internal/*` 能力可以快速桥接到 `agentcore` runtime。

但在当前阶段，已经开始出现两个信号：

1. `agentcore` 已经不只是实验性内核，而是被 `internal/agent`、`internal/orchestrator`、`internal/team` 真实使用。
2. `platform/*` 与 `agentcore/api` 的 contract 已经足够接近，继续长期维持“两套主语义 + 中间 adapter”会让后续主链接线显得割裂。

当前最明显的重复点有三组：

- `llm`
  - `internal/platform/llm/provider.go`
  - `pkg/agentcore/api/provider.go`
- `tools`
  - `internal/platform/tools/registry.go`
  - `pkg/agentcore/api/tools.go`
- `memory`
  - `internal/platform/memory/store.go`
  - `pkg/agentcore/api/memory.go`

这些重复不是简单命名问题，而是会直接影响：

- Host internal run 的接线方式
- 后续 tools / mcp / skills 的承载方式
- LLM provider 配置如何抽象成协议族而不是厂商品牌

---

## 3. 设计结论

这轮采用下面的总体方案：

### 3.1 `pkg/agentcore/api` 是 runtime 主 contract

`pkg/agentcore/api` 继续承担唯一的 runtime-facing contract 源头职责。

它定义的不是“对外发布才用的 API”，而是仓库内部也应该统一依赖的运行时公共语言，包括：

- `Provider`
- `StreamingProvider`
- `Memory`
- `Tool`
- `ToolRegistry`
- `Storage`
- `RunState`
- `Message`
- `Artifact`

### 3.2 `internal/platform/*` 是第一方实现层

`internal/platform/llm`、`internal/platform/tools`、`internal/platform/memory` 的定位调整为：

- 围绕 `agentcore/api` 直接提供第一方实现
- 可以保留本模块自己的 richer capability
- 但不再与 `agentcore/api` 平行维护另一套 runtime 主 contract

换句话说，`platform/*` 不是“另一套运行时抽象”，而是“runtime contract 的第一方实现层”。

### 3.3 `internal/acadapters/internal*` 是迁移期结构

当前 `internalllm`、`internaltools`、`internalmemory` 三个 bridge 不作为长期架构层保留。

它们的职责是：

- 在 contract 收口期间帮助旧代码平滑迁移
- 为现有调用点提供短期兼容

它们不再是后续 Host internal run、orchestrator、team manager 的推荐依赖路径。

### 3.4 Host internal `session runner` 延后

Host Session internal runner 的实装排在 contract 收口之后。

原因很简单：

- 这条链路会同时触及 `llm / tools / memory / hitl / tracing / runtime event`
- 如果 contract 还没统一，Host 这条线接一次就会把 adapter 层继续固化下来

因此本设计明确规定：

**先收口 contract，再构建 Host internal `session runner`。**

---

## 4. 非目标

以下内容不属于这份设计的直接范围：

- 本轮直接实现 Host internal `session runner`
- 本轮删除 `ReactRunner`
- 本轮统一 tools / mcp / skills 的产品交互
- 本轮重写 `pkg/agentcore` runtime 主循环
- 本轮把所有 provider-specific 配置和能力一次性抽象到终局形态

这份设计只解决“runtime contract 谁是主语义层，以及平台能力如何围绕它收口”。

---

## 5. 三块能力的收口策略

### 5.1 `llm`：直接收敛到 `agentcore/api.Provider`

`llm` 是这轮最优先收口的模块，因为它与后续 Host internal run 最直接相关。

当前问题：

- `internal/platform/llm` 自己维护一套 `Provider / CompletionRequest / CompletionResponse`
- `agentcore/api` 也维护了一套 `Provider / GenerateRequest / GenerateResult`
- 现有差异更多是命名与细节表达，而不是层次本质不同

这说明 `llm` 更适合作为 `agentcore/api.Provider` 的第一方实现，而不是长期保留平行 contract。

本轮建议：

- `internal/platform/llm` 直接围绕 `agentcore/api.Provider` 组织
- `OpenAIProvider`、`ClaudeProvider`、`DeepSeekProvider` 直接实现新的 contract
- `internalllm` bridge 在迁移完成后删除

#### 5.1.1 `llm` 侧对 `agentcore/api` 的缺口判断

当前 `agentcore/api.Provider` 只有生成能力，没有 embedding 扩展接口。

这会导致后续如果记忆检索、摘要或向量召回要复用同一 provider，就不得不重新引入 internal-only `llm.Provider`。

因此建议在 `agentcore/api` 中增加一个可选扩展接口，例如：

- `EmbeddingProvider`

这样可以保持：

- 基础 `Provider` 仍然克制
- richer capability 也有正式扩展位

### 5.2 `tools`：直接收敛到 `agentcore/api.ToolRegistry`

`tools` 的主要问题不是 schema 映射，而是内部执行结果表达比 runtime contract 更弱。

当前：

- internal tool 执行返回 `string`
- `agentcore/api.ToolResult` 已经支持 `Content / Metadata / Artifacts`

这说明如果继续让 internal tool 以“仅返回字符串”为中心，后面很容易再做一轮工具结果 contract 升级。

本轮建议：

- `internal/platform/tools.Tool` 直接兼容 `api.Tool`
- `Registry` 直接兼容 `api.ToolRegistry`
- 工具执行结果升级为 `ToolResult`
- 保留 internal 的扩展信息，例如 `Kind`

这样后面工具结果里的：

- artifact
- structured metadata
- future tool diagnostics

都不需要再回头修改基础 contract。

### 5.3 `memory`：模块内双层，不强行并成单层

`memory` 不适合像 `llm` 和 `tools` 一样直接整体并到一个薄接口上。

原因是 `internal/platform/memory.Store` 本来就承担 richer backend 职责，例如：

- transcript access
- summary
- fragment persistence
- retrieval

而 `agentcore/api.Memory` 的职责非常明确，只是 runtime-facing 的上下文层：

- `Hydrate`
- `ObserveMessages`
- `BuildContext`

因此这轮建议不是删除 `Store`，而是明确双层关系：

- `memory.Store`
  - richer memory backend
- `memory.RuntimeMemory`（或等价命名）
  - 运行时视图
  - 直接实现 `agentcore/api.Memory`

也就是说，把今天 `internalmemory` adapter 的能力回收到 `internal/platform/memory` 模块内部，而不是让 `acadapters` 长期承载这层关系。

---

## 6. `agentcore/api` 需要先补的点

在正式迁移 `platform/*` 之前，建议先补齐下面几类 contract 缺口。

### 6.1 Provider 扩展能力

建议新增可选扩展接口：

- `EmbeddingProvider`

并明确：

- 不是所有 provider 都必须支持 embedding
- runtime 主路径仍只依赖基础 `Provider`
- richer memory / retrieval 能力可以按需检测扩展接口

### 6.2 Tool schema 统一表达

当前 `ToolSchema` 在 internal 和 `agentcore` 之间存在表达差异。

本轮需要明确：

- 是否继续使用 `map[string]any` 作为 schema authority
- 或者引入一个共享的 schema node 定义

无论选哪种，目标都应是：

- internal 与 `agentcore` 之间不再长期做双向 schema 转换

### 6.3 Tool result 的 authority 表达

`api.ToolResult` 已经具备较好的 authority 形态。

本轮需要明确文档约定：

- `Content` 是人类和模型都可读的主要结果
- `Metadata` 是执行事实附属信息
- `Artifacts` 是结构化附属产物

这样 `platform/tools` 在实现时有清晰目标，不需要再围绕“只返回 string”设计。

### 6.4 Memory 分层说明

`agentcore/api.Memory` 需要在文档语义上进一步明确：

- 它不是长期记忆系统的全部 contract
- 它只是 runtime-facing context layer
- richer memory backend 可以建立在它之下或旁边，但不替代它的职责

---

## 7. LLM 配置演进设计

### 7.1 当前问题

当前配置仍然是：

- `llm.provider: openai | claude | deepseek`

这会把两类维度混在一起：

- 协议类型
- 厂商名字

但未来实际接入时会出现很多“同协议、不同品牌”的情况，例如：

- OpenAI
- DeepSeek
- OpenRouter
- 自建 OpenAI-compatible gateway

这些不应该都变成代码里的独立 provider type。

### 7.2 建议配置形态

建议把配置收敛成：

- `llm.api_type: openai | anthropic`
- `llm.provider_name: openai | deepseek | openrouter | claude | custom`
- `llm.base_url`
- `llm.api_key`
- `llm.model`
- `llm.embed_model`

语义约定：

- `api_type`
  - 决定协议实现
- `provider_name`
  - 主要用于默认值、日志标签、厂商识别
- `base_url`
  - 明确覆盖默认 endpoint
- `model`
  - 总是显式配置，不再把默认模型散落在多处分支

### 7.3 配置示例

OpenAI-compatible:

```yaml
llm:
  api_type: openai
  provider_name: deepseek
  base_url: https://api.deepseek.com/v1
  api_key: ${DEEPSEEK_API_KEY}
  model: deepseek-chat
  embed_model: text-embedding-3-small
```

Anthropic:

```yaml
llm:
  api_type: anthropic
  provider_name: claude
  api_key: ${ANTHROPIC_API_KEY}
  model: claude-sonnet-4-6
```

这里统一采用 `anthropic` 拼写，不引入 `authropic` 这一类拼写变体。

### 7.4 `buildLLMProvider()` 的目标形态

后续 `cmd/axon/main.go` 中的 `buildLLMProvider()` 需要改成：

1. 先按 `api_type` 选择协议实现
2. 再按 `provider_name` 决定默认标签、默认 base URL 或兼容分支
3. `base_url` 优先级高于 provider 默认值

这样做的好处是：

- 以后支持更多厂商时，不需要不断新增顶层 `switch provider`
- internal `llm` 能更自然地围绕协议族组织

---

## 8. 实施顺序

建议实施顺序固定为下面五步：

1. 先补 `agentcore/api` 缺口
2. 先收 `llm`
3. 再收 `tools`
4. 再收 `memory`
5. 最后再开始 Host internal `session runner`

这个顺序不建议打乱。

特别是：

- 不建议先做 Host internal `session runner`
- 不建议在 `llm/tools/memory` 尚未统一前，把 adapter 继续固化到 Host 主链

---

## 9. 风险与取舍

### 9.1 风险：前期看起来比直接接 runner 更慢

这是这轮最主要的 tradeoff。

如果只看眼前，“先把 internal runner 接到 Host”会更快见到结果。

但长期看，这会让：

- Host
- adapter
- platform
- agentcore

四层一起背负重复 contract，后面更难拆。

### 9.2 风险：`memory` 过度收敛

`memory` 的 richer backend 不能被 runtime-facing `api.Memory` 完全替代。

因此本设计明确：

- `memory` 采用双层同模块策略
- 不追求为了统一而把 richer backend 削成薄接口

### 9.3 风险：`agentcore/api` 被拉得过大

补 `EmbeddingProvider` 或统一 schema 表达时，需要坚持 `agentcore` kernel 边界：

- 只补 contract 缺口
- 不把平台侧策略、厂商特定逻辑、产品配置塞回 `agentcore`

---

## 10. 完成状态的判断标准

如果达到下面这些条件，就说明这轮 contract 收口已经成功，可以开始规划 Host internal run 接线：

- `internal/platform/llm` 直接实现 `agentcore/api.Provider`
- `internal/platform/tools` 直接实现 `agentcore/api.ToolRegistry`
- `internal/platform/memory` 模块内直接提供 `api.Memory` 实现
- `internal/acadapters/internalllm`、`internaltools`、`internalmemory` 不再是主路径依赖
- `buildLLMProvider()` 已切到 `api_type + provider_name` 的配置模型
- `internal/agent`、`internal/orchestrator`、`internal/team` 不再需要依赖迁移期 internal adapter 才能使用 `agentcore`

---

## 11. 下一步

这份设计确认后，下一步不直接开发 Host internal `session runner`，而是先进入一份专门的 implementation plan，计划顺序应为：

1. `agentcore/api` contract patch
2. `llm` 收口与配置调整
3. `tools` 收口
4. `memory` 收口
5. `acadapters` 清理
6. Host internal `session runner` 设计与接线

这样可以最大限度避免“先接主链，再回头拆 contract”的返工。

### 11.1 Phase 1 计划约束补充

当前已补齐第一阶段实施计划：

- `docs/superpowers/plans/2026-04-14-agentcore-llm-contract-alignment-phase1.md`

在进入真实编码前，需要明确一个实现约束：

- `internal/platform/llm` 的 contract 收口不能被当作一次纯粹的包内重命名
- 现有 `ReactRunner`、`cron.Scheduler`、`llm.Pool` 仍直接依赖 `Complete / CompletionRequest / CompletionResponse / Embed`
- 因此 Phase 1 应优先让 `agentcore` 主路径直接使用公共 contract，同时保留一个最小兼容面给 legacy caller

也就是说，这轮更适合采用：

- `agentcore` 主路径先切到公共 contract
- 旧调用点维持 compile-safe compatibility
- legacy `llm` facade 与 `acadapters` 清理放到后续 phase

而不是一次性把所有 `internal/platform/llm` 调用点一起重写。
