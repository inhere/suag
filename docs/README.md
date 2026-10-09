# Suag 文档

Suag 当前是个人 Agent，通过 CLI 或 ACP 运行；Gofer 负责作业、会话入口与审批。功能和命令以仓库根目录 [README](../README.md) 与实际源码为准。

## 当前资料

- [配置分层与 chat 体验设计](design/2026-10-09-config-layering-chat-design.md)与[实施计划](plans/2026-10-09-config-layering-chat-plan.md)（[复盘](plans/2026-10-09-config-layering-chat-retrospective.md)）：providers/model 配置、包分层、hooks 通用化、chat 渲染（当前实施依据）。
- [核心能力演进设计](design/2026-10-08-core-capabilities-design.md)：单库存储、压缩、计划、`--from` 续接、`tool_search`、观测与工具层审批。
- [核心能力实施计划](plans/2026-10-08-core-capabilities-plan.md)与[复盘](plans/2026-10-08-core-capabilities-retrospective.md)：已完成。
- [个人 Agent 设计](design/2026-10-06-personal-agent-redesign-design.md)与[实施计划](plans/2026-10-06-personal-agent-redesign-plan.md)：产品范围与会话边界（历史基线）。
- [Gofer 接入](usage/gofer-personal-agent.md)：当前 `suag` CLI 与 `suag-acp` 的配置、续接和审批。
- [LLM 配置](usage/llm-config-v0.md)：`model` / `providers` 格式、内置预设、引用解析与旧配置迁移。
- [MockProvider 用法](usage/llm-mock-usage.md)：本地测试执行内核。
- [ACP 调研](research/acp-protocoal-research.md)：协议参考；实现合同以源码和依赖版本为准。
- [agentcore 资料](../pkg/agentcore/docs/README.md)：可复用执行内核的接口说明。

## 历史资料

[archive/](archive/README.md) 保存早期 Agent OS、Host/Web、协议网关方案、旧实施计划与草稿。这些资料不再定义当前命令或运行方式。
