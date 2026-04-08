# agentcore/testing

`agentcore/testing` 提供面向外部实现者的 package-local conformance helpers，用来验证自定义 `Storage`、`ToolRegistry`、`Provider` 是否满足 `agentcore` 的公共 contract。

它的定位不是：

- 再造一套复杂测试框架
- 暴露 `agentcore` 仓库内部测试细节
- 替代 API 文档或 examples

它的定位是：

- 给外部实现一个可复用的 contract test 入口
- 让独立仓库里的适配器可以自证兼容
- 把“文档说明的预期”变成“可以直接跑的验证”

## 当前提供的 suites

- `RunStorageSuite`
  验证 run snapshot、append-only transcript、resume 后 authority transcript 可恢复
- `RunStorageFixtureSuite`
  在保留 happy path 的同时，验证 `SaveRun` / `LoadRun` / `AppendMessages` / `LoadMessages` 的错误传播
- `RunToolRegistrySuite`
  验证 `ListTools` schema 与 `ExecuteTool` 的基本结果 contract
- `RunProviderSuite`
  验证 `GenerateResult` 的最小合法性，以及可选 streaming provider 的基本行为

## 最小使用方式

### Storage

```go
package mystorage_test

import (
    "testing"

    "github.com/inhere/axon/pkg/agentcore"
    agentcoretesting "github.com/inhere/axon/pkg/agentcore/testing"
)

func TestStorageContract(t *testing.T) {
    agentcoretesting.RunStorageSuite(t, agentcoretesting.NewStorageFactoryFromPath(
        func(t *testing.T, root string) agentcore.Storage {
            t.Helper()
            return mystorage.New(root)
        },
    ))
}
```

如果你还想验证 storage 的错误传播 contract，可以使用 `RunStorageFixtureSuite(...)`：

```go
fixture := agentcoretesting.NewStorageFixture(erroringStorage).
    WithSaveRunError(errSaveBoom).
    WithLoadRunError(errLoadBoom).
    WithAppendMessagesError(errAppendBoom).
    WithLoadMessagesError(errLoadMessagesBoom)

agentcoretesting.RunStorageFixtureSuite(t, func(t *testing.T) agentcoretesting.StorageFixture {
    t.Helper()
    return fixture
})
```

当你既想保留 round-trip / resume 这类 happy path，又想单独验证错误路径时，可以给 fixture 分开提供：

- `RoundTripStorage`
- `MessagesStorage`
- `ResumeStorage`

这样 happy-path 子测可以继续跑在正常 storage 上，错误传播子测则跑在错误 stub 上。

### ToolRegistry

```go
package mytools_test

import (
    "testing"

    "github.com/inhere/axon/pkg/agentcore"
    agentcoretesting "github.com/inhere/axon/pkg/agentcore/testing"
)

func TestRegistryContract(t *testing.T) {
    agentcoretesting.RunToolRegistrySuite(t, func(t *testing.T) agentcoretesting.ToolRegistryFixture {
        t.Helper()

        registry := mytools.NewRegistry()
        registry.Register(...)

        return agentcoretesting.NewToolRegistryFixture(
            registry,
            agentcore.ToolSchema{
                Name: "echo",
            },
            agentcore.ToolCall{
                CallID: "call-1",
                Name:   "echo",
            },
            &agentcore.ToolResult{
                CallID: "call-1",
                Name:   "echo",
            },
        ).WithExecuteError(
            "tool_failure",
            agentcore.ToolCall{
                CallID: "call-2",
                Name:   "fail",
            },
            errToolBoom,
        )
    })
}
```

### Provider

```go
package myprovider_test

import (
    "testing"

    "github.com/inhere/axon/pkg/agentcore"
    agentcoretesting "github.com/inhere/axon/pkg/agentcore/testing"
)

func TestProviderContract(t *testing.T) {
    agentcoretesting.RunProviderSuite(t, func(t *testing.T) agentcoretesting.ProviderFixture {
        t.Helper()

        provider := myprovider.New(...)
        streamProvider := myprovider.NewStreaming(...)

        return agentcoretesting.NewProviderFixture(
            provider,
            agentcoretesting.NewGenerateCase(
                "final",
                &agentcore.GenerateRequest{},
                &agentcore.GenerateResult{
                    Content:      "done",
                    FinishReason: agentcore.FinishFinal,
                },
            ),
        ).WithStreamingProvider(streamProvider).WithStreaming(agentcoretesting.StreamingCase{
            Name:    "stream_success",
            Request: &agentcore.GenerateRequest{},
            WantEvents: []agentcore.StreamEvent{
                {Kind: agentcore.StreamEventTextDelta, Delta: "hel"},
                {Kind: agentcore.StreamEventTextDelta, Delta: "lo"},
                {Kind: agentcore.StreamEventMessageStop},
            },
            WantResult: &agentcore.GenerateResult{
                Content:      "hello",
                FinishReason: agentcore.FinishFinal,
            },
        })
    })
}
```

## Streaming cases

`RunProviderSuite` 现在不仅能验证 streaming success path，也能验证两类常见错误路径：

- stream provider 自己返回错误
- host 侧 stream callback 返回错误

这几个字段分别表示：

- `WithStreamingProvider(...)`
  当你的 `Generate(...)` 和 `Stream(...)` 不适合共用同一个脚本化 provider，或者你想把 generate/stream contract 分开验证时使用
- `StreamingCase.WantErr`
  表示这次 `Stream(...)` 调用最终应该返回的错误
- `StreamingCase.EmitErr`
  表示 host 侧 stream callback 主动返回错误，用来验证 provider 是否正确把 callback 失败向上传播

一个最小 streaming error 示例：

```go
fixture := agentcoretesting.NewProviderFixture(
    generateProvider,
    agentcoretesting.NewGenerateCase(
        "final",
        &agentcore.GenerateRequest{},
        &agentcore.GenerateResult{
            Content:      "done",
            FinishReason: agentcore.FinishFinal,
        },
    ),
).WithStreamingProvider(streamProvider).WithStreamingError(agentcoretesting.StreamingCase{
    Name:    "stream_provider_error",
    Request: &agentcore.GenerateRequest{},
    WantErr: errStreamBoom,
}).WithStreamingHandlerError(agentcoretesting.StreamingCase{
    Name:    "stream_handler_error",
    Request: &agentcore.GenerateRequest{},
    WantErr: errHandlerBoom,
    EmitErr: errHandlerBoom,
})
```

## Storage error cases

`RunStorageFixtureSuite` 当前主要用于验证 storage 的错误传播，而不替代最简单的 `RunStorageSuite`。

推荐把它理解成两层：

- `RunStorageSuite(...)`
  只关心最小 happy path
- `RunStorageFixtureSuite(...)`
  在 happy path 之外，额外验证错误路径 contract

当前支持的最小错误路径包括：

- `SaveRun` 返回错误
- `LoadRun` 返回错误
- `AppendMessages` 返回错误
- `LoadMessages` 返回错误
- `SaveRun` 在缺失 `RunID` 时返回错误

如果你的错误 stub 会破坏 happy path，推荐像下面这样把正常 storage 和错误 storage 分开：

```go
agentcoretesting.RunStorageFixtureSuite(t, func(t *testing.T) agentcoretesting.StorageFixture {
    t.Helper()

    return agentcoretesting.NewStorageFixture(erroringStorage).
        WithSaveRunError(errSaveBoom).
        WithLoadRunError(errLoadBoom).
        WithAppendMessagesError(errAppendBoom).
        WithLoadMessagesError(errLoadMessagesBoom)
})
```

如果还需要同时保留 happy path：

```go
agentcoretesting.RunStorageFixtureSuite(t, func(t *testing.T) agentcoretesting.StorageFixture {
    t.Helper()

    return agentcoretesting.StorageFixture{
        Storage:          erroringStorage,
        RoundTripStorage: healthyStorageA,
        MessagesStorage:  healthyStorageB,
        ResumeStorage:    healthyStorageC,
    }.WithSaveRunError(errSaveBoom)
})
```

## 设计原则

- 只依赖 public `agentcore` contract
- 失败信息尽量指向 contract 级问题，而不是内部实现细节
- 优先补最容易被独立实现的接口
- 保持 additive 演进，不轻易把第一版 suite 做成重量级框架

## 什么时候用它

推荐在下面这些场景里使用：

- 你在独立仓库里实现了自己的 `Storage`
- 你在桥接已有工具系统到 `ToolRegistry`
- 你在封装自己的 provider / streaming provider
- 你准备把适配器对外发布，希望先验证自己没有偏离 `agentcore` contract

如果你想先理解 contract 本身，再来使用这些 suites，推荐先看：

- [../docs/api.md](../docs/api.md)
- [../docs/conformance-testing.md](../docs/conformance-testing.md)
- [../docs/release-readiness.md](../docs/release-readiness.md)

如果你想直接看“外部适配器作者”的最小示例测试，继续看：

- [external_examples_test.go](./external_examples_test.go)
