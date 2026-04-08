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
        )
    })
}
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
