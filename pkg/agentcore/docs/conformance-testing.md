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
   - SaveRun / LoadRun / AppendMessages / LoadMessages 的最小错误传播 contract
2. `ToolRegistry`
   - `ListTools` 的基本 schema contract
   - `ExecuteTool` 的结果 shape 与错误传播
3. `Provider`
   - 最小 final/tool-calls result contract
   - 可选 streaming capability 的基本验证 helper
   - streaming success / provider error / host callback error 的最小 contract

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

对于 `Storage`，当前可以把 conformance 分成两层：

- `RunStorageSuite(...)`
  验证最小 happy path
- `RunStorageFixtureSuite(...)`
  在需要时补上错误传播 contract

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

如果你想先从“一页结构化总览”判断当前该验证哪组 surface，再进入 suites，也可以先看：

- [../testing/README.md](../testing/README.md) 里的 `ContractChecklist()`
- [../testing/README.md](../testing/README.md) 里的 `OnboardingChecklist()`
- [contract-matrix.md](./contract-matrix.md)

如果你想直接看 package-local 入口与自测样例，可以继续看：

- [../testing/README.md](../testing/README.md)
- [../testing/storage_test.go](../testing/storage_test.go)
- [../testing/tools_test.go](../testing/tools_test.go)
- [../testing/provider_test.go](../testing/provider_test.go)
- [../testing/external_examples_test.go](../testing/external_examples_test.go)

其中 storage 侧现在建议这样理解：

- happy-path 优先从 [../testing/storage_test.go](../testing/storage_test.go) 里的 `RunStorageSuite(...)` 用法开始
- 需要覆盖错误传播时，再看同一文件里的 `RunStorageFixtureSuite(...)`

## 5. Streaming contract tests 目前怎么理解

对 `Provider` 来说，streaming conformance 现在推荐至少分成三类情况：

1. streaming success
   - stream events 按预期发出
   - 最终返回合法 `GenerateResult`
2. stream provider error
   - provider 在 stream 过程中返回错误
   - 已发出的 events 仍然可以被断言
3. stream handler error
   - host 侧 callback 返回错误
   - provider 应把这个错误向上传播，而不是吞掉

`agentcore/testing` 当前对应的最小表达方式是：

- `WithStreamingProvider(...)`
  用于把 streaming 脚本源与普通 `Generate(...)` 脚本源分开
- `StreamingCase.WantErr`
  表示 `Stream(...)` 最终应该返回什么错误
- `StreamingCase.EmitErr`
  表示 host callback 主动返回错误

这样做的好处是：

- 外部适配器可以分别验证 generate 与 stream contract
- 脚本化 mock provider 不会因为共用一步步消费的状态而让测试相互污染
- suite 可以覆盖最值钱的 streaming error-path，而不必一开始就做成重量级测试框架

## 6. Storage contract tests 目前怎么理解

对 `Storage` 来说，当前 suite 建议至少区分两类目标：

1. authority happy path
   - run snapshot 能 round-trip
   - append-only transcript 顺序稳定
   - resume 能重放 authoritative transcript
2. storage error propagation
   - `SaveRun` 错误不会被吞掉
   - `LoadRun` / `LoadMessages` 错误不会被吞掉
   - `AppendMessages` 错误不会被吞掉
   - 明显无效输入，例如缺失 `RunID`，可以被断言

`agentcore/testing` 当前对应的最小表达方式是：

- `RunStorageSuite(...)`
  最小 happy path
- `RunStorageFixtureSuite(...)`
  happy path + error path 的扩展入口
- `StorageFixture`
  用于为不同子场景提供不同 storage，实现“正常路径”和“错误 stub”分离

这样做的好处是：

- 不会因为一组 error stub 把所有 happy-path 子测一起打红
- 外部实现者可以先验证最小 contract，再逐步补错误路径
- storage / tool / provider 三条 suite 逐渐形成更对称的 contract story

## 7. 与其他文档的关系

- [api.md](./api.md)
  解释 public contract 本身
- [release-readiness.md](./release-readiness.md)
  解释为什么 conformance/testing 属于独立发布姿态的一部分
- [roadmap.md](./roadmap.md)
  解释它在下一阶段演进中的优先级

一句话总结：

> 如果 `agentcore` 想成为真正可嵌入、可发布、可被别人独立实现适配器的 kernel，那么 package-local conformance/testing 不是锦上添花，而是 contract story 的必要组成部分。
