# Agentcore Conformance Testing

这份文档说明为什么 `agentcore` 需要 package-local 的 conformance/testing story，以及第一阶段建议先覆盖哪些公共接口。

## 1. 为什么需要这层能力

如果 `agentcore` 只是仓库内部模块，README、API guide、examples 往往已经够用。

但如果它的目标是未来可以独立发布，情况就不一样了：

- 外部实现者会自己写 `Provider`
- 外部实现者会自己写 `Storage`
- 外部实现者会自己写 `ToolRegistry`

这时仅靠“看文档理解 contract”通常不够。

更理想的状态是：

- 文档解释 contract
- examples 展示推荐接法
- conformance tests 验证实现是否真的符合 contract

也就是说，conformance/testing 不是附属工具，而是独立包姿态的一部分。

## 2. 第一阶段目标

第一阶段不追求完整测试框架，而是先做一层最小、稳定、只依赖 public contract 的测试入口。

推荐优先覆盖：

1. `Storage`
   - append-only transcript 预期
   - snapshot 与 messages 的基本读写一致性
   - tool transcript 在存储层的可恢复性
2. `ToolRegistry`
   - `ListTools` 的基本 schema contract
   - `ExecuteTool` 的结果 shape 与错误传播
3. `Provider`
   - 最小 final/tool-calls result contract
   - 可选 streaming capability 的基本验证 helper

原因很直接：

- 这三类接口最容易被外部单独实现
- 它们直接影响 runtime 的核心兼容性
- 它们适合通过 public contract 做 package-local 验证

## 3. 不应该做成什么样

这层能力不应该演进成：

- 一整套复杂测试 DSL
- 对仓库内部实现细节的暴露
- 只能在 Axon 仓库里跑通的 helper
- 和 runtime 私有测试工具深度耦合的外部 API

更好的方向是：

- 小函数
- 清晰约定
- 明确需要调用方提供哪些实现
- 失败时给出 contract 级别的错误信息

## 4. 建议入口形态

第一阶段更推荐的对外使用方式是：

```go
func TestMyStorageContract(t *testing.T) {
    agentcoretesting.RunStorageSuite(t, func(t *testing.T) agentcore.Storage {
        return mystorage.New(...)
    })
}
```

以及：

```go
func TestMyRegistryContract(t *testing.T) {
    agentcoretesting.RunToolRegistrySuite(t, func(t *testing.T) agentcore.ToolRegistry {
        return mytools.NewRegistry(...)
    })
}
```

这类 API 的特点是：

- 不要求调用方理解 `agentcore` 自己的内部测试布局
- 直接围绕 public contract 工作
- 可以自然复用到独立仓库

如果你想直接看 package-local 入口与自测样例，可以继续看：

- [../testing/README.md](../testing/README.md)
- [../testing/storage_test.go](../testing/storage_test.go)
- [../testing/tools_test.go](../testing/tools_test.go)
- [../testing/provider_test.go](../testing/provider_test.go)
- [../testing/external_examples_test.go](../testing/external_examples_test.go)

## 5. 与其他文档的关系

- [api.md](./api.md)
  解释 public contract 本身
- [release-readiness.md](./release-readiness.md)
  解释为什么 conformance/testing 属于独立发布姿态的一部分
- [roadmap.md](./roadmap.md)
  解释它在下一阶段演进中的优先级

一句话总结：

> 如果 `agentcore` 想成为真正可嵌入、可发布、可被别人独立实现适配器的 kernel，那么 package-local conformance/testing 不是锦上添花，而是 contract story 的必要组成部分。
