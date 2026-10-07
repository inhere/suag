# Docs Guide

> 状态：Current
> 更新时间：2026-10-07

本文档区分当前个人 Agent 主线与早期 Host/Web 资料；早期设计保留作历史参考。

---

## 1. 当前文档分层

`docs/` 目前建议按 4 类理解：

1. 当前主线
2. 研究资料
3. 草稿与想法池
4. 历史 `v1` 参考

默认阅读顺序应该始终是：

`当前主线 > 研究资料 > 草稿与想法池 > 历史 v1 参考`

---

## 2. 当前主线（Source of Truth）

当前个人 Agent 改造以以下文档为依据：

- `docs/design/2026-10-06-personal-agent-redesign-design.md`：产品与数据边界。
- `docs/plans/2026-10-06-personal-agent-redesign-plan.md`：已批准实施阶段与 Gate。
- `docs/usage/gofer-personal-agent.md`：当前 CLI/ACP 接入与操作。
- `pkg/agentcore/docs/README.md`：保留的执行内核资料。

早期 `docs/v0-self-host-design.md` 和 Web 工作台文档记录历史设计，不再描述当前运行入口。

---

## 3. 研究资料

以下文档属于协议和外部方案调研，可继续作为技术输入使用，但不直接等于当前实现规范：

- `docs/research/ag-ui-protocol-research.md`
- `docs/research/a2a-protocol-research.md`
- `docs/research/a2ui-protocol.md`
- `docs/research/a2a-agui-a2ui-how-to-cooperate.md`

使用方式建议是：

- 先看当前主线文档定义的产品边界
- 再用研究资料辅助协议选型和接口设计

---

## 3.1 当前使用约定

部分已经落地到代码的当前使用约定，放在 `docs/usage/` 下，供开发和客户端对接时直接参考，例如：

- `docs/usage/gofer-personal-agent.md`
  - 当前 Gofer CLI/ACP 用户路径
- `docs/usage/agentcore-internal-integration.md`
  - `internal/*` 接入 `pkg/agentcore` 的既有说明，按当前源码核对

---

## 4. 草稿与想法池

以下文档保留为想法沉淀和方向记录，不应直接视为当前实现任务单：

- `docs/suag-prd.draft.md`
- `docs/code-refer.v0.1.draft.md`

这类文档的价值主要在于：

- 保留思路演进过程
- 收纳尚未收敛的想法
- 为后续正式设计文档提供素材

---

## 5. 历史 v1 参考

以下文档基于旧的 `v1 foundation` 假设撰写，当前仅作为历史参考：

- `docs/archive/v1-old/legacy-prd-v1-foundation.md`
- `docs/archive/v1-old/legacy-spec-v1-foundation.md`
- `docs/suag-v1-process.md`
- `docs/archive/v1-old/00-overview.md`
- `docs/archive/v1-old/01-agent-core.md`
- `docs/archive/v1-old/02-orchestrator.md`
- `docs/archive/v1-old/03-team.md`
- `docs/archive/v1-old/04-tools-skills.md`
- `docs/archive/v1-old/05-memory-tracing.md`
- `docs/archive/v1-old/06-gateway-api.md`
- `docs/archive/v1-old/07-implementation-plan.md`

这些文档可以用于：

- 回看旧方案如何思考问题
- 参考某些模块拆分方式
- 提取仍然有价值的局部设计

这些文档不应用于：

- 直接指导新的 `v0` 开发
- 约束 `session / turn / run` 新模型
- 证明 `internal/*` 当前实现一定正确

---

## 6. 关于 `internal/*` 代码的阅读原则

当前 `internal/*` 下相当一部分代码是基于旧 `v1` 方案开发的，因此它们应被视为：

**候选资产库，而不是当前架构真相。**

更具体的模块分层与可信度说明，见：

- `docs/internal-code-status.md`

阅读和复用时建议遵循下面的顺序：

1. 先看当前 `v0` 主线文档是否已定义新的边界
2. 再判断旧 `internal/*` 是否只是可复用基础设施
3. 如果旧实现和当前 `v0` 模型冲突，以新的 `v0` 设计为准

因此：

- 旧代码不再定义新架构
- 旧文档不再约束新实现
- 只有和当前 `v0` 模型对齐的旧模块，才应该被按需吸纳

---

## 7. 后续整理原则

后续继续整理文档时，建议遵循以下原则：

- 不急着删除旧文档，优先做分层和状态标记
- 新的 `v0` 设计和计划尽量进入 `docs/superpowers/specs/` 与 `docs/superpowers/plans/`
- 每份文档开头都明确标注 `Current / Draft / Legacy`
- 当某份草稿已经被正式设计吸收后，再决定是否归档或保留

这样可以最大限度减少历史信息丢失，同时降低旧文档对当前开发的干扰。
