# MockProvider 使用指南

`internal/llm.MockProvider` 是一个零网络依赖的假 LLM，专为测试 Agent 执行流程设计。

支持两种互补的控制模式：

| 模式 | 适用场景 | 核心方法 |
|------|---------|---------|
| **脚本模式** | 固定流程的自动化测试 | `QueueText` / `QueueToolCall` / `QueueError` |
| **手动模式** | 步进调试、探索性测试 | `WaitForCall` + `reply()` |

两种模式可混用：队列耗尽后自动退回手动模式。

---

## 快速上手

```go
import "github.com/axon/axon/internal/llm"

m := llm.NewMockProvider("my-test")
```

---

## 模式一：脚本模式

### 构建响应队列

```go
m := llm.NewMockProvider("scripted").
    QueueToolCall("file_read", map[string]any{"path": "go.mod"}).  // 第1次调用
    QueueToolCall("shell_exec", map[string]any{"command": "go build ./..."}). // 第2次调用
    QueueText("构建成功，任务完成")                                             // 第3次调用
```

每次 `Complete()` 按 FIFO 顺序消耗一个响应。

### QueueResponse — 完整控制

```go
m.QueueResponse(llm.MockResponse{
    Content:      "部分回答",
    FinishReason: "stop",
})

// 同时包含文本和工具调用（模拟复杂 LLM 行为）
m.QueueResponse(llm.MockResponse{
    Content: "我需要先读一下文件",
    ToolCalls: []llm.ToolCall{{
        ID:        "call-001",
        Name:      "file_read",
        Arguments: map[string]any{"path": "README.md"},
    }},
    FinishReason: "tool_calls",
})
```

### 注入错误

```go
m.QueueError(errors.New("rate limited"))
// → 下一次 Complete() 返回该 error，可测试 Agent 的重试/HITL 逻辑
```

### 断言调用历史

```go
// 验证 Agent 确实发起了正确数量的 LLM 调用
assert.Equal(t, 3, m.CallCount())

// 检查第 1 次请求携带的工具 schema
calls := m.Calls()
assert.Equal(t, "file_read", calls[0].Tools[0].Name)

// 检查消息内容
assert.Contains(t, calls[2].Messages[0].Content, "go.mod")
```

---

## 模式二：手动模式

当队列为空时，`Complete()` 会阻塞，等待 `WaitForCall()` 接收并 `reply()`。

### 基本步进

```go
func TestManual(t *testing.T) {
    m := llm.NewMockProvider("manual")
    ctx := context.Background()

    // 在 goroutine 里跑 Agent（它会多次调用 Complete）
    results := make(chan *agent.TaskResult, 1)
    go func() {
        r, _ := runner.Run(ctx, task)
        results <- r
    }()

    // ── 第 1 步：LLM 决定调用工具 ───────────────────────────────
    req, reply, _ := m.WaitForCall(ctx)
    t.Logf("Agent 请求 #1，消息数: %d", len(req.Messages))

    reply(llm.MockResponse{
        ToolCalls: []llm.ToolCall{{
            ID:        "c1",
            Name:      "file_read",
            Arguments: map[string]any{"path": "README.md"},
        }},
    })

    // ── 第 2 步：工具结果回来，LLM 给出最终答案 ─────────────────
    req2, reply2, _ := m.WaitForCall(ctx)
    // 验证工具结果已注入到消息中
    lastMsg := req2.Messages[len(req2.Messages)-1]
    t.Logf("工具结果: %s", lastMsg.Content)

    reply2(llm.MockResponse{Content: "README 已阅读，任务完成"})

    result := <-results
    t.Logf("最终输出: %s", result.Output)
}
```

### 手动注入错误触发 HITL

```go
// 连续 3 次注入错误 → 触发 HITL 挂起
for range 3 {
    _, reply, _ := m.WaitForCall(ctx)
    reply(llm.MockResponse{
        ToolCalls: []llm.ToolCall{{Name: "shell_exec", Arguments: map[string]any{"command": "bad"}}},
    })
    // 注：工具本身执行失败由 ToolRegistry mock 控制，见下文
}
// 此时 HITL Manager 应收到 ticket
```

### 上下文取消

```go
ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
defer cancel()

_, _, err := m.WaitForCall(ctx)
// err == context.DeadlineExceeded  → 可测试超时行为
```

---

## 混合模式（脚本 + 手动）

```go
m := llm.NewMockProvider("hybrid").
    QueueText("先用脚本走完已知步骤")

// 第 1 次 Complete() 消耗队列
// 第 2 次 Complete() 队列已空，自动进入手动模式
_, reply, _ := m.WaitForCall(ctx)
reply(llm.MockResponse{Content: "手动控制的步骤"})
```

---

## 辅助 API 速查

```go
// 排队
m.QueueText("content")
m.QueueToolCall("name", args)
m.QueueError(err)
m.QueueResponse(llm.MockResponse{...})

// 手动模式
req, reply, err := m.WaitForCall(ctx)
reply(llm.MockResponse{...})  // 注入文本、工具调用或错误

// 检查
m.CallCount()        // 累计 Complete() 调用次数
m.QueueLen()         // 剩余脚本响应数
m.Calls()            // []CompletionRequest 历史副本

// 重置（子测试间隔离）
m.Reset()
```

---

## 与 ToolRegistry 配合

MockProvider 只控制 LLM 侧的响应。工具执行结果由 `tools.Registry` 决定。
测试中可用 `registry.MustRegister(mockTool)` 注入假工具：

```go
type echoTool struct{}
func (e *echoTool) Name() string      { return "echo" }
func (e *echoTool) Description() string { return "echo" }
func (e *echoTool) Kind() tools.Kind  { return tools.KindNative }
func (e *echoTool) Schema() llm.ToolSchema { return llm.ToolSchema{Name: "echo"} }
func (e *echoTool) Execute(_ context.Context, args map[string]any) (string, error) {
    return fmt.Sprint(args["msg"]), nil
}

reg := tools.NewRegistry()
reg.MustRegister(&echoTool{})

m.QueueToolCall("echo", map[string]any{"msg": "hello"}) // LLM 请求调用
m.QueueText("收到 echo 结果，完成")                       // LLM 读到工具结果后回复
```
