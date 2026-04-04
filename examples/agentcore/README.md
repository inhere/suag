# Agentcore Examples

这组示例面向 `pkg/agentcore` 的外部使用者，覆盖三类最常见接法：

- `minimal`
  - 用 `mock + files` 跑通最小 runtime
- `custom`
  - 自定义 tool 和 hook，展示常见扩展路径
- `bridge`
  - 用 `internal/agentcoreadapters` 桥接内部实现

运行方式：

```bash
go run ./examples/agentcore/minimal
go run ./examples/agentcore/custom
go run ./examples/agentcore/bridge
```
