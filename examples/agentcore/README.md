# Agentcore Examples

这组示例面向 `pkg/agentcore` 的外部使用者，覆盖几条最常见接法：

- `minimal`
  - 用 `mock + files` 跑通最小 runtime
- `custom`
  - 自定义 tool 和 hook，展示常见扩展路径
- `resume`
  - 演示 `yield -> resume` 的最小恢复链路
- `bridge`
  - 用 `internal/agentcoreadapters` 桥接内部实现

运行方式：

```bash
go run ./examples/agentcore/minimal
go run ./examples/agentcore/custom
go run ./examples/agentcore/resume
go run ./examples/agentcore/bridge
```

其中 `resume` 示例重点展示两件事：

- 第一次 run 用稳定 `RunID` 执行并进入 `RunYielded`
- 第二次 run 使用相同 `RunID` 和显式 `ResumeMode` 继续，而不是重新开始
