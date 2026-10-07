# Suag 文档

Suag 当前是个人 Agent，通过 CLI 或 ACP 运行；Gofer 负责作业、会话入口与审批。功能和命令以仓库根目录 [README](../README.md) 与实际源码为准。

## 当前资料

- [个人 Agent 设计](design/2026-10-06-personal-agent-redesign-design.md)：产品范围、会话与数据边界。
- [个人 Agent 实施计划](plans/2026-10-06-personal-agent-redesign-plan.md)：已完成阶段及验收记录。
- [Gofer 接入](usage/gofer-personal-agent.md)：当前 `suag` CLI 与 `suag-acp` 的配置、续接和审批。
- [LLM 配置](usage/llm-config-v0.md)：支持的协议、模型与凭据字段。
- [MockProvider 用法](usage/llm-mock-usage.md)：本地测试执行内核。
- [ACP 调研](research/acp-protocoal-research.md)：协议参考；实现合同以源码和依赖版本为准。
- [agentcore 资料](../pkg/agentcore/docs/README.md)：可复用执行内核的接口说明。

## 历史资料

[archive/](archive/README.md) 保存早期 Agent OS、Host/Web、协议网关方案、旧实施计划与草稿。这些资料不再定义当前命令或运行方式。
