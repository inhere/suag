# Agentcore Versioning

这份文档描述 `agentcore` 当前推荐的版本与变更策略。

它关注的是：

- `v0` 阶段应该如何理解兼容性
- 哪些类型的变更应该被视为高风险
- 新能力和破坏式调整分别应该怎么推进

这份文档不要求当前就进入严格 semver 承诺，但它为后续独立发布提供一个清晰的 change policy 起点。

---

## 1. 当前版本姿态

`agentcore` 当前仍然处于 `v0` 阶段。

在这个阶段，更准确的目标不是“完全冻结”，而是：

- 核心 contract 尽量稳定
- 默认实现允许渐进增强
- 破坏式修改必须越来越少、越来越显式

换句话说：

- `v0` 不是“可以随便改”
- `v0` 也不是“已经完全冻结”

它应该被理解成：

- core semantics 正在收敛
- first-party defaults 正在打磨
- package-local docs 和 examples 正在对外化

---

## 2. 变更级别

为了避免所有改动都混成一类，推荐把 `agentcore` 的变更分成 4 级。

### 2.1 Patch-Like Changes

这类改动原则上应尽量做到低风险：

- 测试补强
- 文档修正
- 示例修正
- bug fix
- 默认实现内部修复

理想情况下，这类改动不应改变公共 contract 预期。

### 2.2 Minor Additive Changes

这类改动允许在 `v0` 阶段继续推进：

- 新增可选配置项
- 新增 first-party helper / builder
- 新增 optional provider capability
- 新增 examples / docs

例如：

- `prompt.Layered(...)`
- `StreamingProvider`
- `ResumePolicy`
- `ToolExecutionPolicy`
- `ErrorPolicy`

这类变更的原则是：

- 默认不破坏现有主路径
- 新能力应尽量 optional

### 2.3 Contract-Tightening Changes

这类改动会影响外部对行为的理解，但不一定改接口签名：

- resume 规则收紧
- transcript 持久化语义收紧
- hook control 语义更明确
- 错误包装更稳定

这类改动在 `v0` 阶段仍然可能发生，但必须满足：

- 测试先行
- 文档明确说明
- example 与默认路径同步更新

### 2.4 Breaking Changes

这类改动应被视为最高风险：

- 删除 public type / field / method
- 重命名核心接口
- 改变 authority transcript 形状
- 改变 `RunID + ResumeMode` 核心恢复语义
- 把已形成稳定预期的 default behavior 反向改写

这类变更在 `v0` 阶段并不是绝对禁止，但必须被当成显式决策，而不是顺手改掉。

---

## 3. 哪些区域应该更保守

### 3.1 Runtime Contracts

下面这些区域应该优先收紧，而不是继续频繁变动：

- `RunInput`
- `RunState`
- `RunResult`
- `GenerateRequest`
- `GenerateResult`
- `Message`
- `ToolCall`
- `ToolResult`

### 3.2 Recovery Semantics

下面这些语义一旦变化，外部 embedding host 很容易被影响：

- `RunID`
- `ResumeMode`
- terminal run resumability
- transcript persistence timing

所以这部分应非常谨慎。

### 3.3 Error Categories

一旦上层开始围绕这些错误做策略分流：

- `ProviderError`
- `ToolError`
- `HookError`
- `StorageError`
- `MemoryError`

它们就不再只是内部细节，而是对外 contract 的一部分。

### 3.4 Runtime Policy Contracts

下面这些 policy 当前也应该开始按公共 contract 对待：

- `ResumePolicy`
- `ToolExecutionPolicy`
- `ErrorPolicy`

它们虽然仍处在 `v0` 阶段，但已经承接 runtime 主路径语义，不应被视为可以随意改写的辅助接口。

---

## 4. 哪些区域可以继续快一点

### 4.1 First-Party Defaults

只要不破坏核心 contract，下面这些区域还可以继续快速增强：

- `memory/`
- `context/`
- `prompt/`
- `mock/`
- examples

### 4.2 Package-Local Docs

文档结构、阅读顺序、示例组织方式，还可以继续优化。

这类变化不应该被误解成 contract 不稳定，而应该被视为对外可用性提升。

### 4.3 Optional Capabilities

像 streaming 这种新增能力，推荐优先走：

- optional interface
- additive event model
- non-breaking default path

而不是直接改写已有核心接口。

---

## 5. 推荐的 Change Policy

如果后续继续沿独立包方向演进，建议团队按下面的规则看待改动。

### 5.1 先问四个问题

每个改动先判断：

1. 它是 core contract 变更，还是 default implementation 变更
2. 它会不会改变 transcript / resume / authority 语义
3. 它能不能通过新增 optional surface 完成，而不是破坏已有 surface
4. 它会不会改变 policy 和 hook 之间已经形成的边界

### 5.2 默认优先 additive

如果新能力可以通过下面方式完成，优先选它们：

- 新增接口
- 新增可选字段
- 新增 helper / builder
- 新增 example / docs

而不是：

- 改名
- 改含义
- 改已有默认路径

### 5.3 破坏式修改必须显式化

如果真的要做 breaking change，建议至少同步做：

- 边界测试
- 文档说明
- compatibility 文档更新
- roadmap / migration 说明

不要让破坏式变化只体现在代码 diff 里。

---

## 6. 面向未来版本的收敛方向

如果未来进入更正式的独立发布节奏，推荐逐步收敛到下面的形态：

### 6.1 `v0.x`

特点：

- 允许 contract 收紧
- 默认实现继续增强
- 文档和 examples 快速演进

要求：

- 高风险变更必须有说明
- tests 与 docs 先跟上

### 6.2 `v1.0`

进入 `v1` 前，至少应满足：

- core runtime semantics 基本稳定
- compatibility policy 足够清晰
- examples 能代表长期主路径
- 默认实现与核心 contract 边界足够成熟

### 6.3 `v1.x`

进入 `v1` 后再更严格地承诺：

- public type / interface 更稳定
- breaking change 进入极少数场景
- migration guidance 成为标准配套

---

## 7. 这份文档如何配合其他文档使用

推荐搭配关系：

- `README`
  看包定位与主入口
- `compatibility.md`
  看当前稳定姿态与发布前检查
- `versioning.md`
  看变更策略与风险级别
- `roadmap.md`
  看后续能力推进顺序

这四者的关系可以理解成：

- `README` 解释它是什么
- `compatibility` 解释现在能依赖到什么程度
- `versioning` 解释以后怎么改
- `roadmap` 解释接下来往哪里长
