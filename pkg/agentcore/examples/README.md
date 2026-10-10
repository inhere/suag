# Agentcore Examples

每个目录是一个可直接运行的小程序，用 `mock` 脚本化 provider，不访问网络。示例参与 `go vet ./...`，随代码一起保持可编译。

| 示例 | 演示内容 |
|---|---|
| `minimal` | 用 `mock + files` 跑通最小 runtime |
| `custom` | 自定义 tool 和 hook，展示常见扩展路径 |
| `authority` | 宿主通过 `LoadAuthorityBundle(...)` 读取 authoritative messages 与 artifacts |
| `policies` | `ResumePolicy`、`ToolExecutionPolicy`、`ErrorPolicy` 如何组合 |
| `policyhost` | 宿主把 policy 收拢进自己的 host wrapper |
| `recommended` | 推荐的组装路径：`llm`（设置 `OPENAI_API_KEY` 时）+ 只读内置工具 + skills + 可选 MCP + 审批层；默认用 mock 离线运行 |
| `host` | embedding host 封装 `Start/Continue` 入口并把 streaming 转发给上层 UI |
| `stream` | 最小 streaming contract：转发 text delta，final 结果仍为权威 |
| `resume` | `yield -> resume` 的最小恢复链路 |

运行（在仓库根）：

```bash
go run ./pkg/agentcore/examples/minimal
go run ./pkg/agentcore/examples/recommended
```

建议阅读顺序：`minimal` → `custom` → `authority` → `policies` → `policyhost` → `recommended` → `host` → `stream` → `resume`。

旧的 `memory`、`context`、`prompt` 与 `bridge` 示例已随对应子包删除，见 [删除记录](../../../docs/archive/removed-2026-10.md)。
