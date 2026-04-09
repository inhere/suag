# Kernel Stability

这份文档专门回答一个更偏发布姿态的问题：

- 把 `agentcore` 看成一个可独立发布的 runtime kernel 时
- 当前哪些 surface 已经值得外部依赖
- 哪些 surface 仍然应该视为可继续演进

它不是 changelog，也不是 roadmap。

它更像一份“当前已经稳定到什么程度”的说明。

## 1. 当前判断标准

如果一个能力同时满足下面几个条件，就更接近 kernel-stable surface：

- 已经进入主路径
- embedding host 很可能直接依赖它
- 变更会影响 transcript、resume、tool loop 或错误处理
- 已经有明确测试和文档约束

当前 `agentcore` 最接近这个标准的，是下面几组内容。

## 2. 已形成的 Kernel Surface

### 2.1 Runtime Core Contracts

这组类型已经是当前最值得外部依赖的核心 contract：

- `RunInput`
- `RunState`
- `RunResult`
- `Message`
- `ToolCall`
- `ToolResult`
- `GenerateRequest`
- `GenerateResult`

这些类型定义了：

- host 如何发起一次 run
- runtime 如何持久化事实层状态
- provider / tool / storage / memory 如何围绕同一组数据模型协作

### 2.2 Authority Semantics

当前已经形成稳定预期的 authority 语义包括：

- `Storage` 是 authority layer
- `Memory` 是 derived context layer
- transcript 采用 append-only 语义
- tool loop 持久化 `assistant(tool_call)` 与 `tool(result)` 成对消息
- streaming 仍然不替代 final completion authority

这组语义是 `agentcore` 作为 kernel 的真正地基。

### 2.3 Runtime Policy Contracts

当前已经提升为正式 contract 的 runtime policy 有三类：

- `ResumePolicy`
- `ToolExecutionPolicy`
- `ErrorPolicy`

它们都满足同一个设计原则：

- 只承接一个小的策略域
- 默认实现保持当前语义不变
- 用正式接口替代越来越隐式的 hook 协议

如果未来还要继续提升 policy，也应该沿着这个原则走，而不是引入统一大 policy framework。

### 2.4 Error Categories

下面这些错误分类已经不再只是内部实现细节：

- `ProviderError`
- `ToolError`
- `HookError`
- `StorageError`
- `MemoryError`

一旦 embedding host 开始围绕这些错误做分流，它们就已经成为公共 runtime contract 的一部分。

## 3. 当前更像 First-Party Defaults 的 Surface

下面这些内容已经可用，但更适合被理解成“第一方默认构件”，而不是完全收紧的 kernel contract：

- `memory/`
- `context/`
- `prompt/`
- `mock/`
- `files/` 的非 contract 细节

它们仍然可以继续增强，例如：

- 更好的配置项
- 更多默认组合
- 更贴近长期主路径的 helper
- 更清晰的 examples

只要不破坏 kernel surface，这类增强仍然是合理的。

## 4. 当前更偏可演进的区域

下面这些区域仍然应该允许继续快速演进：

- package-local docs 的组织方式
- examples 的命名和阅读顺序
- adapter story
- 更多 optional capability

这不代表它们不重要，而是它们还处在“对外可用性持续打磨”的阶段。

## 5. 对外使用时的建议

如果你准备在外部系统中直接依赖 `agentcore`，当前更推荐这样分层：

- 强依赖：
  - runtime core contracts
  - authority semantics
  - error categories
  - 三类 runtime policy contract
- 谨慎依赖：
  - 第一方默认实现的具体细节
  - docs / examples 的组织形态
  - 仍在扩展中的 optional capability

一个简单判断方式是：

- 你是在依赖 runtime kernel 语义
  这通常是合理的
- 你是在依赖某个第一方默认实现的偶然细节
  这通常要更谨慎

## 6. 当前对外姿态总结

可以把今天的 `agentcore` 理解成：

- 一个已经成型的单-agent runtime kernel
- 一组正在被明确化、测试化、文档化的稳定 contract
- 一批仍然允许继续打磨的第一方默认构件

换句话说：

- 它已经不只是概念验证
- 也还没有走到“全部 public surface 完全冻结”

当前最合理的使用方式，是把它当成一个 `v0` 但已经具备稳定核心语义的 kernel。

## 7. 搭配阅读

如果你想继续看：

- [compatibility.md](./compatibility.md)
- [versioning.md](./versioning.md)
- [runtime-policies.md](./runtime-policies.md)
- [host-integration.md](./host-integration.md)
