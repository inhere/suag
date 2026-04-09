# Runtime Policies

`agentcore` 当前已经把三类最容易从 runtime 主路径中“长进 hooks 里”的语义，提升成了正式 policy contract：

- `ResumePolicy`
- `ToolExecutionPolicy`
- `ErrorPolicy`

它们的目标不是把 runtime 变成一个大而全 policy framework，而是把已经稳定、并且外部宿主确实会想显式定制的主路径决策，从隐式 hook 协议里抽出来。

## 1. 为什么需要 Runtime Policies

仅靠 `Hook` 也能做很多事，但如果某类逻辑同时满足下面几个条件，就更适合提升成正式 policy：

- 它会改变 runtime 主路径，而不只是观察或轻量注入
- 它会被 embedding host 反复定制
- 如果只靠 hook 表达，会让语义分散到多个阶段
- 一旦写错，就容易影响 transcript、resume 或错误处理的一致性

当前最先被提升出来的，就是这三类决策：

- run 恢复时怎么处理
- tool loop 怎么处理
- recoverable error 怎么处理

## 2. Policy 边界

### 2.1 ResumePolicy

`ResumePolicy` 负责恢复同一次 run 时的主路径决策：

- 这个 run 现在能不能继续
- 是否追加新的 user input
- 恢复时如何合并 metadata

它适合表达的策略包括：

- terminal run 是否允许特殊 resume
- 恢复时总是忽略新的人工输入
- 恢复时对 metadata 做更明确的 overlay 规则

它不适合承担：

- prompt 生成
- context 裁剪
- tool 调用治理

### 2.2 ToolExecutionPolicy

`ToolExecutionPolicy` 负责 tool loop 中两类主路径决策：

- 当前这批 tool calls 能不能执行
- tool 执行失败后，是继续写入 transcript，还是直接 fail

它适合表达的策略包括：

- 放宽或收紧每步 tool call 数量限制
- 将某些 tool error 映射成稳定 fallback `ToolResult`
- 将某些 tool error 提升成 terminal failure

它不适合承担：

- tool schema 暴露
- 实际 tool 业务实现
- tool 输出如何进入 prompt

这些仍然分别属于 `ToolRegistry`、具体 `Tool` 实现和 `ContextBuilder`。

### 2.3 ErrorPolicy

`ErrorPolicy` 当前只负责 recoverable error path。

它适合表达的策略包括：

- 某类 recoverable error 先 yield 给 host
- 某类 recoverable error 返回稳定 fallback `RunResult`
- 某类 recoverable error 必须保持 terminal，不能再被 hook 恢复

它当前不接管：

- `persist_tool_output` 这类 authority transcript 风险路径
- 观察型错误上报
- 错误分类本身

这意味着：

- `ErrorPolicy` 是 recoverable runtime decision layer
- 它不是一个全局异常系统

## 3. Policy 与 Hook 的关系

推荐这样理解：

- `Policy`
  负责主路径决策
- `Hook`
  负责观察、审计、注入和局部拦截

一个简单判断标准是：

- 如果你在决定“runtime 接下来应该怎么走”，优先考虑 policy
- 如果你在观察“runtime 刚刚发生了什么”，优先考虑 hook

例如：

- “恢复时不要追加新的 instruction”
  更适合 `ResumePolicy`
- “tool 执行失败时先转成稳定 fallback result”
  更适合 `ToolExecutionPolicy`
- “provider error 时先 yield，等待宿主稍后重试”
  更适合 `ErrorPolicy`
- “把所有 model request 打到日志里”
  更适合 `Hook`

## 4. 当前调用顺序

当前可以粗略理解成下面的顺序：

1. runtime 准备或恢复 `RunState`
2. `ResumePolicy` 决定恢复主路径
3. runtime 进入 model / tool loop
4. `ToolExecutionPolicy` 决定 tool loop 主路径
5. 如果遇到 recoverable error，`ErrorPolicy` 先决定是否转成显式结果
6. 如果 policy 没接管，再进入现有 `OnError` hook 控制语义

这里最重要的一点是：

- policy 是更正式、更前置的主路径 contract
- hook 仍然存在，但不应该继续承担越来越多的默认 runtime 语义

## 5. 推荐用法

当前更推荐的方式不是一上来就实现全部 policy，而是：

1. 先使用默认实现
2. 只在确实有宿主需求时，覆盖单个 policy
3. 每次只改一个策略域

例如：

- 只需要调整恢复语义 -> 只自定义 `ResumePolicy`
- 只需要调整 tool error 策略 -> 只自定义 `ToolExecutionPolicy`
- 只需要把 provider error 变成 yield -> 只自定义 `ErrorPolicy`

这样更容易保证：

- transcript authority 不被意外破坏
- 默认 runtime 语义仍然清晰
- 文档和测试可以保持同步

## 6. 一个最小组合示例

如果你想直接看三类 policy 如何组合使用，继续看：

- [../examples/policies/main.go](../examples/policies/main.go)

如果你还想看它们各自所在的更大上下文，继续看：

- [api.md](./api.md)
- [architecture.md](./architecture.md)
- [roadmap.md](./roadmap.md)
