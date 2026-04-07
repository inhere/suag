# Agentcore Docs

这里是 `pkg/agentcore` 随包携带的文档入口，目标是让 `agentcore` 在未来独立发布时，也能拥有一套自解释的 package-local 文档。

## 阅读顺序

1. `../README.md`
   包首页，说明定位、边界、默认构件和稳定语义
2. `getting-started.md`
   第一次接入时的最小上手路径
3. `api.md`
   公共接口职责与边界
4. `embedding-patterns.md`
   直接嵌入与 adapter 桥接两种常见接法
5. `working-memory-design.md`
   下一轮默认 memory kernel 演进设计
6. `context-presets-design.md`
   下一轮默认 context 组合演进设计
7. `prompt-layered-design.md`
   下一轮默认 prompt 组合演进设计
8. `roadmap.md`
   后续演进路线

## 这组文档回答什么问题

- `agentcore` 是什么，解决什么问题
- 哪些能力应该进入 core，哪些应该放在 core 之上
- 如何接入最小 runtime
- 如何把 `agentcore` 嵌进自己的系统
- 如何理解 transcript、resume、memory 等核心语义
- 如何围绕公共 contract 做扩展
- 下一轮 kernel 演进准备做什么
- 默认 builder 组合下一步怎么长
- 默认 prompt 组合下一步怎么长

## 相关示例

如果你想直接看运行中的最小示例，继续看：

- `examples/agentcore/minimal`
- `examples/agentcore/custom`
- `examples/agentcore/resume`
- `examples/agentcore/bridge`
