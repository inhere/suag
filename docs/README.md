# Docs Guide

> 状态：Current
> 更新时间：2026-04-10

本文档用于说明当前 `docs/` 目录的使用方式，避免旧 `v1` 方案文档干扰新的 `v0` 开发。

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

当前新的 `v0` 开发，以以下文档作为主要依据：

- `docs/superpowers/specs/2026-04-10-axon-v0-self-host-design.md`
  - 当前 `v0.self-host` 总纲
- `docs/superpowers/plans/2026-04-10-axon-v0-self-host-foundation-plan.md`
  - 当前第一阶段基础骨架实施计划
- `docs/superpowers/specs/2026-04-02-agentcore-design.md`
  - `pkg/agentcore` 的独立设计说明
- `docs/superpowers/plans/2026-04-02-agentcore-implementation-plan.md`
  - `agentcore` 的基础实现计划
- `docs/superpowers/plans/2026-04-06-agentcore-working-memory-phase1.md`
- `docs/superpowers/plans/2026-04-06-agentcore-context-presets-phase1.md`
- `docs/superpowers/plans/2026-04-07-agentcore-prompt-layered-phase1.md`
- `docs/superpowers/plans/2026-04-07-agentcore-streaming-phase1.md`

如果这些文档与旧 `PRD / SPEC / docs/archive/v1-old/*` 有冲突，以当前主线文档为准。

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

## 4. 草稿与想法池

以下文档保留为想法沉淀和方向记录，不应直接视为当前实现任务单：

- `docs/axon-prd.draft.md`
- `docs/code-refer.v0.1.draft.md`

这类文档的价值主要在于：

- 保留思路演进过程
- 收纳尚未收敛的想法
- 为后续正式设计文档提供素材

---

## 5. 历史 v1 参考

以下文档基于旧的 `v1 foundation` 假设撰写，当前仅作为历史参考：

- `docs/PRD.md`
- `docs/SPEC.md`
- `docs/axon-v1-process.md`
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
