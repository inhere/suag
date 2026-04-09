# Agentcore Release Readiness

这份文档面向一个更具体的问题：

- 如果把 `agentcore` 当成将来可独立发布的包，当前离“可放心对外依赖”还有多远
- 哪些 public surface 已经适合当成长期 contract
- 哪些能力仍然更像 first-party default，而不是稳定协议
- 每次准备对外发布或做高风险变更前，应该检查什么

它不是 roadmap，也不是 changelog。

它更像一份 package-local 的 release checklist 和 contract matrix。

---

## 1. 当前结论

`agentcore` 当前已经具备一个可工作的 `v0` kernel，可以承接真实单 agent runtime 主路径。

但如果从“独立发布 readiness”角度看，更准确的判断是：

- core runtime contracts 已经开始适合外部依赖
- first-party defaults 仍然处在持续打磨阶段
- docs / examples 已经可读，但还在继续收敛为长期主路径

也就是说：

- 它已经不是实验性质草稿
- 但还没有进入“所有 public surface 都高度保守”的阶段

---

## 2. Contract Matrix

下面这张矩阵的目标不是给出绝对承诺，而是帮助判断：

- 什么可以当成依赖点
- 什么应该当成实现参考
- 什么仍然需要谨慎使用

### 2.1 Core Runtime Contracts

状态：`High confidence in v0`

范围：

- `RunInput`
- `RunState`
- `RunResult`
- `RunID + ResumeMode`
- `Message`
- `ToolCall`
- `ToolResult`
- `GenerateRequest`
- `GenerateResult`

当前判断：

- 这些类型已经构成 `agentcore` 的核心语义骨架
- 后续仍可能继续收紧，但不应轻易做重命名级或职责翻转级改动

发布前检查：

- 是否存在仓库内部语义泄漏到 public fields
- 是否有字段仅为了当前内部 adapter 临时存在
- 是否已有测试覆盖核心恢复与 transcript 行为

### 2.2 Extension Interfaces

状态：`Stable direction, careful evolution`

范围：

- `Provider`
- `StreamingProvider`
- `Storage`
- `Memory`
- `ToolRegistry`
- `PromptBuilder`
- `ContextBuilder`
- `Hook`

当前判断：

- 这些接口的职责边界已经相对清晰
- 继续新增 optional capability 是合理的
- 不应轻易改变接口主职责

发布前检查：

- 新能力能否通过 optional interface 完成
- 是否把某个 default implementation 误做成了事实标准
- hook 是否开始承担本应进入正式 contract 的能力
- 是否已经存在 package-local conformance tests，帮助外部实现验证兼容性

### 2.3 Authority And Recovery Semantics

状态：`Must stay conservative`

范围：

- append-only transcript
- `assistant(tool_call)` / `tool(result)` 持久化语义
- final assistant completion 持久化时机
- terminal state resumability
- `ResumeFromState` / `ResumeWithInput` / `ResumeAuto`

当前判断：

- 这是最不应随意变化的一层
- 任何“看起来只是内部重构”的调整，都可能影响 embedding host

发布前检查：

- repeat resume 是否行为一致
- terminal state resume 是否有明确预期
- resume 时追加新输入的规则是否在 docs 和 tests 中一致

### 2.4 Error Model

状态：`Should keep tightening`

范围：

- `ProviderError`
- `ToolError`
- `HookError`
- `StorageError`
- `MemoryError`

当前判断：

- 这些错误类型已经不只是内部细节
- 一旦 host 基于它们做策略分流，行为就属于对外 contract

发布前检查：

- 错误包装是否稳定
- 是否保留足够 root cause
- docs 是否说明了分类语义，而不只是列出类型名

### 2.5 First-Party Defaults

状态：`Useful but still evolving`

范围：

- `files/`
- `memory/`
- `context/`
- `prompt/`
- `hooks/`
- `mock/`

当前判断：

- 这些内容已经适合作为推荐起步路径
- 但它们不应被误解为唯一正确接法

发布前检查：

- 默认实现是否仍严格服从 root package contract
- examples 是否把 default behavior 误写成唯一语义
- default package 命名与入口是否足够自解释

### 2.6 Conformance And Compatibility Tooling

状态：`Needed for independent package posture`

范围：

- `testing/` 或等价的 package-local conformance helpers
- `Storage` / `ToolRegistry` / `Provider` 的 contract verification utilities

当前判断：

- 如果 `agentcore` 要长期对外发布，单靠文档和 mock 示例还不够
- 外部实现需要一个“如何验证自己满足 contract”的正式入口
- 这层能力本身也属于 package surface，需要保持最小且稳定

发布前检查：

- conformance helpers 是否只依赖 public contract
- 是否优先覆盖 authority 风险最高的接口
- 是否避免把内部测试工具直接暴露成外部 API

### 2.6.1 Storage Optional Capabilities

状态：`Next kernel growth point`

范围：

- artifact store / replay
- checkpoint / listing / transactional capability
- 不膨胀主 `Storage` 接口的可选能力层

当前判断：

- `Storage` 主接口现在是合适的最小 authority contract
- 但 artifact 已经进入 `RunState` 与 completion authority，说明这条线需要更完整的 persistence story
- 更合理的方向不是直接把主接口做大，而是引入 additive optional capability
- 当前第一方 `files` 实现已经开始提供 artifact persistence / replay 的 capability 示例

发布前检查：

- optional capability 是否没有反向污染主 `Storage` contract
- runtime 在 capability 缺失时是否仍保持当前最小语义
- files 等第一方实现是否能作为清晰示范实现

### 2.7 First-Party Host Conformance

状态：`Important before calling contracts host-ready`

范围：

- first-party host wrapper 是否能自然承接 kernel contract
- `AgentcoreRunner`
- legacy `ReactRunner`
- host-facing policy parity

当前判断：

- 如果某个 contract 只能在 runtime 内部自洽，但 first-party host wrapper 无法自然承接，它还不适合被描述成真正 host-ready 的 kernel surface
- 当前已经有明确行为级验证的重点区域，是：
  - `ResumePolicy`
  - `ToolExecutionPolicy`
  - `ErrorPolicy`
- 这层验证让这些 contract 更接近可独立发布时的真实依赖点，而不只是 runtime 内部抽象

发布前检查：

- first-party host wrapper 是否已经覆盖核心 policy contract
- parity tests 是否区分了“contract parity”和“lifecycle parity”
- host wrapper 是否仍然依赖分散 hook 协议去承载默认主路径语义
- 文档是否已经清楚说明 host 间仍保留的实现差异

### 2.8 Package Docs And Examples

状态：`Important release surface`

范围：

- `README.md`
- `docs/`
- `examples/`

当前判断：

- 对独立包来说，这些内容属于产品表面，而不是附属材料
- 如果文档和示例不自洽，外部就无法真正依赖内核

发布前检查：

- package-local docs 是否仍依赖大量仓库上下文
- examples 是否不依赖 `internal/*`
- docs 的阅读顺序是否清楚

---

## 3. 发布前最小检查清单

如果要把某个阶段视为“可以对外推荐试用”，建议至少完成下面几类检查。

### 3.1 Contract Check

- `RunInput` / `RunState` / `RunResult` 是否存在明显仓库私有字段含义
- transcript model 是否仍然统一
- `Provider` / `Storage` / `Memory` / `Hook` 的职责边界是否仍清晰

### 3.2 Behavior Check

- tool loop 是否仍然保持成对 transcript 持久化
- final completion 是否仍然是 authority output
- streaming delta 是否没有误写入 authority transcript
- resume 行为是否在多次恢复下保持一致

### 3.3 Boundary Check

- `Storage = authority`、`Memory = derived context` 边界是否仍成立
- `ContextBuilder` 是否没有偷偷承担 authority 职责
- hooks 是否没有变成 parallel runtime

### 3.4 Documentation Check

- `README` 是否能独立解释包定位
- `README` 是否已经能快速回答它适合谁、当前能做什么、从哪条采用路径开始
- `README` 是否已经提供足够直接的 starter matrix，帮助新使用者快速选主路径
- `getting-started.md` 是否能带新使用者跑通最小链路
- `api.md` 是否能解释公共扩展面
- `compatibility.md` / `versioning.md` / `release-readiness.md` 是否相互衔接

### 3.5 Example Check

- `examples/minimal` 是否代表最小主路径
- `examples/resume` 是否代表恢复主路径
- `examples/stream` 是否代表当前 streaming 能力边界
- 根目录 `examples/agentcore/bridge` 是否仍明确属于 bridge example，而不是 package-local main path

### 3.6 Host Conformance Check

- first-party host wrapper 是否已经验证核心 policy contract 的行为承接
- `AgentcoreRunner` 是否仍然是当前推荐主路径承接者
- legacy host wrapper 是否已经至少进入 contract 对照体系
- docs 是否已经明确说明 host 间仍然存在的 lifecycle / 实现差异

当前已整理的说明，继续看：

- [host-conformance.md](./host-conformance.md)

### 3.7 Verification Check

至少保证下面命令持续通过：

```bash
go test ./pkg/agentcore/... ./examples/agentcore/bridge
```

如果这条命令不稳定，就还不适合把当前状态描述成“release-ready candidate”。

如果已经引入 package-local conformance helpers，还应补充：

```bash
go test ./pkg/agentcore/testing/...
```

---

## 4. 当前最接近发布阻塞点的事项

如果只从 release readiness 角度排序，当前更值得持续补齐的是这些点。

### 4.1 Resume And Transcript Edge Coverage

原因：

- 这是最核心的 authority 语义
- 一旦外部开始依赖，回退成本最高

优先补：

- repeat resume
- resume after yield again
- terminal resume policy
- empty input resume policy

### 4.2 Default Path Confidence

原因：

- `memory/`、`context/`、`prompt/` 已经开始形成推荐主路径
- 但还需要更多 examples 与 docs 来减少歧义

优先补：

- 更明确的 default path narrative
- default builder 组合示例
- example 与 docs 的互相跳转

当前对应入口：

- [recommended-runtime.md](./recommended-runtime.md)
- [host-integration.md](./host-integration.md)
- [../examples/recommended/main.go](../examples/recommended/main.go)
- [../examples/host/main.go](../examples/host/main.go)

### 4.3 Streaming Contract Growth Discipline

原因：

- streaming 已经进入正式 contract
- 但现在还只是最小 phase 1

优先原则：

- 继续 additive
- 继续 optional capability
- 不破坏 final completion authority

### 4.4 Independent Package Posture

原因：

- `agentcore` 的目标是未来可以独立发布
- 那么 package-local docs、examples、naming、versioning posture 都需要继续收敛

优先补：

- release-readiness 文档
- versioning/change policy
- package-local examples consistency
- package-local conformance/testing story

### 4.5 Host-Ready Contract Confidence

原因：

- 对外发布时，外部使用者依赖的不只是 runtime 本身
- 他们通常也会写自己的 host wrapper、runner wrapper 或 orchestration layer

优先补：

- 更系统的 host conformance matrix
- 更明确的 host parity 边界说明
- 持续减少 first-party host wrapper 中与 kernel 默认语义重复的分散逻辑

### 4.6 Policy Surface Discipline

原因：

- 当前 kernel 已经有一些清晰但仍隐含在 runtime 流程里的策略语义
- 如果长期只允许通过 hook 间接表达，核心边界会逐渐变模糊

优先补：

- 把高频 hook 策略使用模式整理出来
- 判断哪些应该升级成正式 policy contract
- 避免形成“hooks 才是真正 runtime”的长期形态

---

## 5. 建议如何使用这份文档

推荐在三类场景里使用它：

### 5.1 新能力准备进 Core 时

先用这份文档判断：

- 它是 core contract 增长，还是 default implementation 增长
- 它会不会碰 authority / resume / transcript
- 它是否更适合先做成 optional interface

### 5.2 准备提交高风险变更时

先对照这份文档检查：

- 是否触碰高风险 contract
- 是否需要同步补 tests
- 是否需要同步更新 compatibility / versioning 说明

### 5.3 准备独立发布整理时

把它当成一个 package-level checklist：

- 看 contract 是否足够清晰
- 看 docs/examples 是否足够自解释
- 看默认实现是否仍然没有越界

---

## 6. 与其他文档的关系

推荐搭配方式：

- [../README.md](../README.md)
  看包定位与主入口
- [compatibility.md](./compatibility.md)
  看当前兼容姿态
- [host-conformance.md](./host-conformance.md)
  看 first-party host wrapper 已经验证到哪些 kernel contract
- [versioning.md](./versioning.md)
  看变更分级与版本策略
- [roadmap.md](./roadmap.md)
  看后续能力演进顺序

可以把它们理解成：

- `README` 解释它是什么
- `compatibility` 解释现在能依赖到什么程度
- `host-conformance` 解释 first-party host 已经验证到什么程度
- `versioning` 解释以后怎么改
- `release-readiness` 解释发布前还要核对什么
- `roadmap` 解释接下来往哪里长
