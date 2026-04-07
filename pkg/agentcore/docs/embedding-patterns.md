# Agentcore Embedding Patterns

这份文档关注一个更具体的问题：当你决定使用 `agentcore` 时，应该如何把它嵌入到自己的系统里。

当前最常见的其实只有两条主路径：

1. 直接把 `agentcore` 当作独立 runtime kernel 使用
2. 通过 adapter 把已有内部 runtime 组件桥接到 `agentcore`

## 1. Pattern A: 直接嵌入 Agentcore

这是最“纯”的使用方式。

你直接围绕公共 contract 组装这些组件：

- `Provider`
- `Storage`
- `Memory`
- `ToolRegistry`
- `PromptBuilder`
- `ContextBuilder`
- `Hooks`

典型形态：

```go
runtime := agentcore.New(agentcore.Config{
	Provider: provider,
	Storage:  storage,
	Memory:   memory,
	Tools:    tools,
	Hooks:    hooks,
})
```

### 适合什么场景

- 新系统，从零开始搭 runtime
- 希望最大化保持 provider / storage / memory 的可替换性
- 希望未来把 `agentcore` 单独发布或复用到别的仓库

### 优点

- 边界最清晰
- 依赖最干净
- 最符合 `agentcore` 未来独立发布的目标

### 注意点

- 一开始不要急着把所有扩展点都实现掉
- 推荐从 `mock + files + empty tools` 起步
- `Storage` 和 `Memory` 要严格区分 authority layer 与 context layer

如果你不是在比较大方向，而是已经准备开始写 host 接入代码，继续看：

- [recommended-runtime.md](./recommended-runtime.md)
- [host-integration.md](./host-integration.md)

## 2. Pattern B: 通过 Adapter 桥接已有系统

这是对已有 runtime 逐步迁移时更现实的方式。

思路不是重写全部组件，而是把已有内部实现桥接到 `agentcore` 的公共 contract。

在 Axon 里，目前对应的 adapter 包包括：

- `internal/agentcoreadapters/internalllm`
- `internal/agentcoreadapters/internalmemory`
- `internal/agentcoreadapters/internaltools`

典型形态：

```go
runtime := agentcore.New(agentcore.Config{
	Provider: internalllm.NewProvider(existingProvider),
	Storage:  storage,
	Memory:   internalmemory.New(existingMemoryStore),
	Tools:    internaltools.NewRegistry(existingRegistry),
})
```

### 适合什么场景

- 你已经有一套可工作的 provider / memory / tool 实现
- 想先统一 core loop，而不是立刻重写上层产品
- 想逐步把旧 runtime 的“事实标准”替换成公共 contract

### 优点

- 迁移成本低
- 能先稳定核心运行语义
- 可以边接入边收敛公共 contract

### 注意点

- adapter 应该依赖 `agentcore`，不要反向让 `agentcore` 依赖内部实现
- 桥接层负责语义映射，不应该偷偷改写 core contract
- 如果 adapter 逻辑越来越重，往往说明公共 contract 还需要升级

## 3. 如何选择

可以用一个简单判断：

- 如果你在做新系统，优先选 Pattern A
- 如果你在给已有系统收敛核心运行语义，优先选 Pattern B

更具体一点：

- 想要最干净的长期边界 -> Pattern A
- 想先把现有系统接进统一 core loop -> Pattern B

## 4. 推荐迁移顺序

如果你当前在已有系统里引入 `agentcore`，推荐按这个顺序：

1. 先桥接 `Provider`
2. 再桥接 `ToolRegistry`
3. 再桥接 `Storage`
4. 再桥接 `Memory`
5. 最后把外层 `Suspend/Resume` 语义对齐到 `RunID + ResumeMode`

这样做的原因是：

- provider / tools 更容易桥接
- transcript / resume 的 contract 收紧后，memory 桥接会更稳
- 一开始先保证 core loop 语义一致，比一开始追求 feature parity 更重要

## 5. 什么时候该从 Adapter 走向原生实现

如果你发现下面这些情况越来越明显，通常说明应该从桥接走向原生实现：

- adapter 里出现越来越多的语义补丁
- `agentcore` 的公共 contract 已经足够稳定
- 你希望这套 runtime 在仓库外也能复用
- 你希望减少 internal 依赖链

理想状态是：

- `agentcore` 提供长期稳定 contract
- internal adapter 只承担过渡和桥接作用
- 更高层系统逐步直接围绕 `agentcore` 构建

---

## 6. 下一步看哪里

- 如果你要直接嵌入 `agentcore`：看 [recommended-runtime.md](./recommended-runtime.md)
- 如果你要给聊天服务、worker、CLI 封装 host：看 [host-integration.md](./host-integration.md)
- 如果你要从已有 runtime / adapter 逐步迁移：看 [migration.md](./migration.md)
- 如果你要评估当前发布姿态：看 [release-readiness.md](./release-readiness.md)
