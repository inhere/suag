# Capability Governance Phase C-D Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 收紧 skill resource hints 边界，并让 recall、skills、tool policy 开始共同影响 planner-lite 的 turn 级提示结果。

**Architecture:** 继续保持 `agentcore` 只消费解析后的 runtime context，不把 capability governance 下沉到 kernel。先把 `resolved_resource_hints` 从路径列表升级为带来源信息的结构化 hint，并在 `intrunner` 消费时做硬边界校验；再增强 `internal/capabilities/skills` 的 matcher/ranking；最后补一个最小 `PostTurnActionHook`，让 planner-lite 消费 recall + skills + tool subset 信号并产出更可解释的 hint。

**Tech Stack:** Go, `internal/capabilities/skills`, `internal/app`, `internal/host/intrunner`, `internal/host/session`, `gookit` test assertions

---

## File Structure

- Modify: `internal/capabilities/skills/types.go`
  Responsibility: 定义结构化 `ResourceHint` 与 richer skill resolution 输出。
- Modify: `internal/capabilities/skills/loader.go`
  Responsibility: 推断 resource hint 来源、类型、优先级与排序。
- Modify: `internal/capabilities/skills/resolver.go`
  Responsibility: richer matching / ranking，输出更稳定的 skill 选择结果。
- Modify: `internal/capabilities/skills/registry.go`
  Responsibility: 深拷贝结构化 resource hint。
- Modify: `internal/capabilities/skills/skills_test.go`
  Responsibility: 验证 parser / loader / resolver 的结构化输出与排序。
- Modify: `internal/app/server.go`
  Responsibility: 把 resolved capability signals 编码到 runtime context。
- Modify: `internal/app/server_test.go`
  Responsibility: 验证 runtime context builder 会输出结构化 resource hints 与 richer skill signals。
- Modify: `internal/host/intrunner/runtime_context_resources.go`
  Responsibility: 在 runtime 侧解码、校验、排序并注入 resource hints。
- Modify: `internal/host/intrunner/runner_test.go`
  Responsibility: 验证 runtime-only 注入、范围控制、扩展名白名单与来源标记。
- Modify: `internal/host/session/service.go`
  Responsibility: 挂接最小 capability-aware planner-lite hook。
- Modify: `internal/host/session/service_test.go`
  Responsibility: 验证 planner_hint / next_action_hint 消费 recall + skills + tools。
- Modify: `docs/superpowers/specs/2026-04-20-host-capability-governance-design.md`
  Responsibility: 同步 Phase C-D 实现边界。
- Modify: `docs/usage/agentcore-internal-integration.md`
  Responsibility: 记录结构化 resource hints 与 planner-lite capability 消费链路。
- Modify: `docs/usage/web-host-local-dev.md`
  Responsibility: 更新本地联调说明。
- Modify: `AGENTS.md`
  Responsibility: 更新当前进行中的主线进展。

## Task 1: Resource Hints Hard Boundary And Provenance

**Files:**
- Modify: `internal/capabilities/skills/types.go`
- Modify: `internal/capabilities/skills/loader.go`
- Modify: `internal/capabilities/skills/registry.go`
- Modify: `internal/capabilities/skills/skills_test.go`
- Modify: `internal/app/server.go`
- Modify: `internal/app/server_test.go`
- Modify: `internal/host/intrunner/runtime_context_resources.go`
- Modify: `internal/host/intrunner/runner_test.go`

- [x] **Step 1: 写 failing tests，锁定结构化 hint 与 runtime 校验边界**
- [x] **Step 2: 运行 `go test ./internal/capabilities/skills ./internal/app ./internal/host/intrunner`，确认按预期失败**
- [x] **Step 3: 实现 `ResourceHint` 结构，补充 skill 来源、kind、priority、source_path**
- [x] **Step 4: 在 loader 中实现稳定排序**
规则：
`references` 优先于 `assets`，名称包含 `guide` / `readme` / `prompt` 的优先，同权重下按路径稳定排序。
- [x] **Step 5: 在 intrunner 消费时增加硬校验**
规则：
只允许 skill 目录内、只允许 `references/` 或 `assets/` 子树、只允许白名单扩展名、超限继续截断。
- [x] **Step 6: 重新运行测试确认通过**
- [ ] **Step 7: 提交**

```bash
git add internal/capabilities/skills/types.go internal/capabilities/skills/loader.go internal/capabilities/skills/registry.go internal/capabilities/skills/skills_test.go internal/app/server.go internal/app/server_test.go internal/host/intrunner/runtime_context_resources.go internal/host/intrunner/runner_test.go
git commit -m "feat: harden skill resource hint consumption"
```

## Task 2: Richer Skill Matching And Ranking

**Files:**
- Modify: `internal/capabilities/skills/types.go`
- Modify: `internal/capabilities/skills/resolver.go`
- Modify: `internal/capabilities/skills/skills_test.go`
- Modify: `internal/app/server.go`
- Modify: `internal/app/server_test.go`

- [x] **Step 1: 写 failing tests，覆盖 message / recall / name / description 多源命中**
- [x] **Step 2: 运行 `go test ./internal/capabilities/skills ./internal/app`，确认失败原因正确**
- [x] **Step 3: 实现 richer match 输出**
最小输出：
`match_source`、`match_terms`、`score`
- [x] **Step 4: 在 runtime context builder 中透出最小 skill resolution metadata**
- [x] **Step 5: 重新运行测试确认通过**
- [ ] **Step 6: 提交**

```bash
git add internal/capabilities/skills/types.go internal/capabilities/skills/resolver.go internal/capabilities/skills/skills_test.go internal/app/server.go internal/app/server_test.go
git commit -m "feat: add richer skill matching signals"
```

## Task 3: Planner-Lite Consumption Of Capability Signals

**Files:**
- Modify: `internal/host/session/service.go`
- Modify: `internal/host/session/service_test.go`
- Modify: `internal/host/session/types.go`
- Modify: `internal/app/server.go`

- [x] **Step 1: 写 failing tests，覆盖 planner hint 会消费 recall + skills + tool mode / subset**
- [x] **Step 2: 运行 `go test ./internal/host/session -run "Planner|PostTurn" -v`，确认失败**
- [x] **Step 3: 实现最小 capability-aware planner-lite hook**
输出要求：
`planner_hint.metadata` 至少包含 `resolved_skill_names`、`resolved_tool_mode`、`resource_hint_count`、`recall_hit`
- [x] **Step 4: 保持 best-effort，不影响主链 terminal flow**
- [x] **Step 5: 重新运行测试确认通过**
- [ ] **Step 6: 提交**

```bash
git add internal/host/session/service.go internal/host/session/service_test.go internal/host/session/types.go internal/app/server.go
git commit -m "feat: feed capability signals into planner hints"
```

## Task 4: Docs And Verification

**Files:**
- Modify: `docs/superpowers/specs/2026-04-20-host-capability-governance-design.md`
- Modify: `docs/usage/agentcore-internal-integration.md`
- Modify: `docs/usage/web-host-local-dev.md`
- Modify: `AGENTS.md`

- [ ] **Step 1: 更新设计与使用文档，明确新 contract 与边界**
- [ ] **Step 2: 运行相关回归测试**

```bash
go test ./internal/capabilities/skills ./internal/app ./internal/host/intrunner ./internal/host/session
```

- [ ] **Step 3: 提交**

```bash
git add docs/superpowers/specs/2026-04-20-host-capability-governance-design.md docs/usage/agentcore-internal-integration.md docs/usage/web-host-local-dev.md AGENTS.md
git commit -m "docs: sync capability governance phase c-d"
```

## Notes

- 不引入 workflow engine、marketplace、重型 planner DAG。
- `agentcore` 不感知 skill provenance 细节，只消费上层解析后的 runtime context。
- `resolved_resource_hints` 优先走结构化对象；必要时 runtime 可兼容旧字符串列表，但不再扩展旧格式。
- planner-lite 仍然只产出 hint，不控制执行图。
