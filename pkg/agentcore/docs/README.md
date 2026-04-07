# Agentcore Docs

这里是 `agentcore` 随包携带的文档入口，目标是让它在未来独立发布时，也能拥有一套自解释的 package-local 文档。

## 阅读顺序

1. [../README.md](../README.md) 包首页，说明定位、边界、默认构件和稳定语义
2. [getting-started.md](./getting-started.md) 第一次接入时的最小上手路径
3. [architecture.md](./architecture.md) kernel 分层、数据流与边界说明
4. [api.md](./api.md) 公共接口职责与边界
5. [embedding-patterns.md](./embedding-patterns.md) 直接嵌入与 adapter 桥接两种常见接法
6. [compatibility.md](./compatibility.md) 当前兼容性预期、发布姿态与检查清单
7. [versioning.md](./versioning.md) 当前版本策略与变更分级建议
8. [release-readiness.md](./release-readiness.md) 独立发布前的 contract matrix 与检查清单
9. [recommended-runtime.md](./recommended-runtime.md) 当前更推荐的外嵌 starter path
10. [host-integration.md](./host-integration.md) embedding host 应该承担哪些职责、如何接 run/stream/yield
11. [migration.md](./migration.md) 从已有 runtime 或 adapter 逐步迁到更原生 agentcore 的顺序与风险点
12. [working-memory-design.md](./working-memory-design.md) 下一轮默认 memory kernel 演进设计
13. [context-presets-design.md](./context-presets-design.md) 下一轮默认 context 组合演进设计
14. [prompt-layered-design.md](./prompt-layered-design.md) 下一轮默认 prompt 组合演进设计
15. [streaming-design.md](./streaming-design.md) 下一轮 streaming contract 设计
16. [roadmap.md](./roadmap.md) 后续演进路线

## 这组文档回答什么问题

- `agentcore` 是什么，解决什么问题
- 哪些能力应该进入 core，哪些应该放在 core 之上
- 如何接入最小 runtime
- 如何把 `agentcore` 嵌进自己的系统
- 如何理解 transcript、resume、memory 等核心语义
- 如何围绕公共 contract 做扩展
- 当前哪些 public surface 已经适合依赖，哪些仍在演进
- 变更应该如何分级、如何避免无意破坏兼容性
- 独立发布前还需要检查哪些 contract、docs、examples 边界
- 当前更推荐从哪条 starter path 起步，以及为什么
- 外部宿主应该如何围绕 `RunInput -> RunResult` 组织集成代码
- 已有系统应该如何逐步迁移，而不是一次性重写
- 下一轮 kernel 演进准备做什么
- 默认 builder 组合下一步怎么长
- 默认 prompt 组合下一步怎么长
- streaming contract 应该如何进入 kernel

## 相关示例

如果你想直接看运行中的最小示例，继续看：

- `examples/minimal`
- `examples/memory`
- `examples/context`
- `examples/custom`
- `examples/prompt`
- `examples/resume`
- `../../examples/agentcore/bridge`
