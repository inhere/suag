# Host Internal Self-Loop Phase 4 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在不引入重型 orchestration 的前提下，把 Host internal self-loop 补到“单 turn 可持续推进、等待后可稳态恢复、终态后可触发标准后续动作”的下一阶段基线。

**Architecture:** 继续保持 `pkg/agentcore` 作为最小 runtime kernel，不把 planner 或 Host 生命周期判断下沉进 core。新增能力优先放在 `internal/host/session` 与 `internal/host/intrunner` 的 host-facing contract 中：先补结构化 loop metadata 与 resume contract，再补 planner-lite 挂点和终态 follow-up hooks，最后把这些语义同步到 gateway / AG-UI / 文档。

**Tech Stack:** Go 1.24, SQLite, WebSocket, `pkg/agentcore`, `internal/host/session`, `internal/host/intrunner`, existing AG-UI mapper, `go test`

---

## Scope

本阶段只覆盖 Host internal runner 主线，不包含：

- 真正的多 agent planner / DAG 调度
- ACP 外部 runner 的新一轮能力扩展
- Web 新产品交互
- Skills / MCP / review pipeline 产品化

本阶段完成后，应满足：

- Host 能区分一次 run 是“首轮执行”、“等待后恢复”还是“终态后 follow-up”
- internal self-loop 的 waiting / resume contract 带有稳定的结构化 metadata
- planner-lite 只作为挂点存在，不把重型规划逻辑硬编码进 runtime
- completed / failed / canceled 后有统一 post-turn action hook，便于后续接 recap、review、skill 沉淀、自继续策略

## File Structure

- Modify: `internal/host/session/types.go`
  Responsibility: 扩展 Host loop metadata、post-turn action、resume source 等稳定类型
- Modify: `internal/host/session/service.go`
  Responsibility: 收口 turn/run 层的 loop metadata 持久化、resume lineage、post-turn action hooks
- Modify: `internal/host/session/service_test.go`
  Responsibility: 覆盖 resume source、loop metadata、post-turn action 的服务层回归
- Modify: `internal/host/intrunner/runner.go`
  Responsibility: internal runner 透出 planner-lite / loop metadata，明确恢复语义
- Modify: `internal/host/intrunner/runner_test.go`
  Responsibility: 覆盖首轮执行、恢复执行、等待再次挂起、多次恢复后的 metadata 约束
- Modify: `internal/host/agui/mapper.go`
  Responsibility: 把新的 loop metadata / post-turn action 映射到 snapshot 与增量事件
- Modify: `internal/host/agui/mapper_test.go`
  Responsibility: 校验 Focused Run / run activity 对新字段的映射稳定性
- Modify: `internal/host/gateway/ws_test.go`
  Responsibility: 补 continue / stop / refresh 后对 loop metadata 的端到端断言
- Modify: `docs/usage/host-state-snapshot-v0.md`
  Responsibility: 补充 loop metadata 与 post-turn action 的 host-facing 约定
- Add: `docs/usage/host-internal-self-loop-v0.md`
  Responsibility: 记录 internal self-loop 的语义边界、defer 项和手测建议

### Task 1: Stabilize Host Self-Loop Metadata Contract

**Files:**
- Modify: `internal/host/session/types.go`
- Modify: `internal/host/session/service.go`
- Test: `internal/host/session/service_test.go`
- Modify: `docs/usage/host-state-snapshot-v0.md`

- [ ] **Step 1: 写出 loop metadata 缺口测试**

补充测试覆盖：

- `StartTurn` 首轮执行时 run metadata 会标记 `loop_phase=initial`
- `ContinueTurn` 成功恢复等待中的 run 时会标记 `loop_phase=resumed`
- 同一 turn 下的新 run 会稳定记录 `resume_from_run_id` 与 `resume_source`
- turn 终态后 snapshot 中仍能看到最后一次 loop metadata

Run: `go test ./internal/host/session -run "Loop|ResumeSource" -v`
Expected: 至少一组新增断言先失败

- [ ] **Step 2: 在 service/types 收口稳定字段**

新增或明确下面这类 Host-facing 字段：

- `loop_phase`
- `resume_source`
- `resume_from_run_id`
- `continue_mode`
- `turn_outcome`

要求：

- 字段名保持 host/AG-UI 友好，不直接暴露 runtime 私有细节
- 关键字段在注释中说明“谁负责写、谁负责读”

- [ ] **Step 3: 在 service 层统一写入与透传 loop metadata**

在 `internal/host/session/service.go` 中把下面逻辑收口到 helper：

- 首轮 run 的 metadata 初始化
- continue / hitl resume 的 metadata 补齐
- terminal run 的 `turn_outcome` 与最后一次 loop metadata 合并
- snapshot / event payload 一律复用同一份稳定 metadata

- [ ] **Step 4: 更新状态快照文档**

在 `docs/usage/host-state-snapshot-v0.md` 中补充：

- `waitMetadata` 与 `loop metadata` 的边界
- 哪些字段在 waiting 与 terminal 都应保留
- 哪些字段只是 best-effort，不应被前端拿来当强约束

- [ ] **Step 5: 跑测试并提交**

Run: `go test ./internal/host/session`
Expected: PASS

```bash
git add internal/host/session/types.go internal/host/session/service.go internal/host/session/service_test.go docs/usage/host-state-snapshot-v0.md
git commit -m "feat: stabilize host self-loop metadata contract"
```

### Task 2: Harden Internal Runner Resume-After-Wait Semantics

**Files:**
- Modify: `internal/host/intrunner/runner.go`
- Test: `internal/host/intrunner/runner_test.go`
- Test: `internal/host/gateway/ws_test.go`

- [x] **Step 1: 写出 internal runner 恢复语义测试**

补充测试覆盖：

- 首轮执行 yield 后再次 `Run()` 会透出 `loop_phase=resumed`
- 恢复后再次 yield 时 metadata 会增加稳定的循环序号或恢复次数
- `ResumeFromState` 与 `ResumeWithInput` 两类恢复在 Host metadata 上可区分
- 丢失 suspended runtime 时错误继续保持明确，不退化成新任务

Run: `go test ./internal/host/intrunner ./internal/host/gateway -run "Resume|WaitMetadata|LoopPhase" -v`
Expected: 至少一组新增断言先失败

- [x] **Step 2: 在 intrunner 明确恢复来源与循环计数**

实现目标：

- internal runner 对每次执行补齐 `loop_phase`
- waiting 后恢复时记录 `resume_source` 和 `resume_count`
- 同一 session 的 suspended runtime 恢复后不丢失上轮 wait metadata 的必要字段

要求：

- 关键分支前补简短注释
- 继续保持 React runtime 仅 explicit legacy fallback

- [x] **Step 3: 校准 gateway 继续命令的 metadata 断言**

在 `internal/host/gateway/ws_test.go` 中补充端到端断言：

- `continue_turn` 后返回的新 run detail 带 `loop_phase=resumed`
- refresh 后获取 run detail / session snapshot 仍能看到恢复 metadata
- `AUTH_REQUIRED` 的 HITL 恢复也能保留同样的结构化 contract

- [x] **Step 4: 跑测试并提交**

Run: `go test ./internal/host/intrunner ./internal/host/gateway`
Expected: PASS

```bash
git add internal/host/intrunner/runner.go internal/host/intrunner/runner_test.go internal/host/gateway/ws_test.go
git commit -m "feat: harden internal runner resume metadata"
```

### Task 3: Add Planner-Lite And Post-Turn Action Hooks

**Files:**
- Modify: `internal/host/session/types.go`
- Modify: `internal/host/session/service.go`
- Test: `internal/host/session/service_test.go`
- Add: `docs/usage/host-internal-self-loop-v0.md`

- [ ] **Step 1: 写出 post-turn action hook 测试**

补充测试覆盖：

- completed / failed / canceled 后会统一触发 `PostTurnActionHook`
- waiting 态不会误触发 post-turn action
- hook 失败仍是 best-effort，不破坏主链
- hook 输入能拿到 loop metadata、turn outcome、summary/failure

Run: `go test ./internal/host/session -run "PostTurnAction|PlannerLite" -v`
Expected: 至少一组新增断言先失败

- [ ] **Step 2: 加 planner-lite 挂点，但不做重型 planner**

实现目标：

- 定义最小 `planner_hint` / `next_action_hint` 输入输出结构
- 只允许 hook 或 runner 写入 hint，不在 service 内硬编码复杂规划流程
- hint 主要用于后续 review、skill 沉淀、自继续策略，不直接驱动多 agent

- [ ] **Step 3: 加 post-turn action hooks 骨架**

实现目标：

- `session.Service` 在终态节点统一调用 `PostTurnActionHook`
- 默认先不接复杂行为，只提供标准输入
- recap hooks 与 post-turn action hooks 语义分离：
  - recap 负责总结沉淀
  - post-turn action 负责“终态后下一步该干什么”的挂点

- [ ] **Step 4: 补文档说明边界**

在 `docs/usage/host-internal-self-loop-v0.md` 中说明：

- planner-lite 为什么只做 hint，不做 DAG
- post-turn action 与 recap hook 的区别
- 哪些能力仍明确 defer：
  - async background tools
  - subagents
  - external orchestration

- [ ] **Step 5: 跑测试并提交**

Run: `go test ./internal/host/session`
Expected: PASS

```bash
git add internal/host/session/types.go internal/host/session/service.go internal/host/session/service_test.go docs/usage/host-internal-self-loop-v0.md
git commit -m "feat: add host post-turn action hooks"
```

### Task 4: Surface Self-Loop State Through AG-UI Mapping

**Files:**
- Modify: `internal/host/agui/mapper.go`
- Test: `internal/host/agui/mapper_test.go`
- Modify: `docs/usage/host-state-snapshot-v0.md`

- [ ] **Step 1: 写出 AG-UI 映射测试**

补充测试覆盖：

- waiting run snapshot 暴露 `loop_phase` / `resume_source`
- terminal run snapshot 暴露 `turn_outcome` / `next_action_hint`
- raw run detail 与 activity item 的字段语义一致

Run: `go test ./internal/host/agui -run "LoopPhase|PostTurn|NextAction" -v`
Expected: 至少一组新增断言先失败

- [ ] **Step 2: 在 mapper 中统一映射新字段**

实现目标：

- snapshot 和增量事件都优先基于 Host run metadata，而不是 intrunner 私有推断
- `waitMetadata`、`loop metadata`、`post-turn hints` 的层次保持清晰
- 不额外引入前端临时兼容字段

- [ ] **Step 3: 更新快照文档**

补充说明：

- 前端哪些字段可直接展示
- 哪些字段只用于调试 / 诊断
- 当前 v0 仍不承诺完整 planner UI

- [ ] **Step 4: 跑测试并提交**

Run: `go test ./internal/host/agui`
Expected: PASS

```bash
git add internal/host/agui/mapper.go internal/host/agui/mapper_test.go docs/usage/host-state-snapshot-v0.md
git commit -m "feat: surface host self-loop state in ag-ui"
```

## Exit Criteria

- `go test ./internal/host/session ./internal/host/intrunner ./internal/host/gateway ./internal/host/agui` 通过
- Host run detail / snapshot 能稳定表达：
  - 首轮执行
  - waiting
  - resumed
  - terminal outcome
  - post-turn next action hint
- `docs/usage/host-state-snapshot-v0.md` 与 `docs/usage/host-internal-self-loop-v0.md` 与代码现状一致
- 不引入新的重型 planner / orchestration 抽象，仍保持 v0 可演进

## Notes

- `tool loop` 已在 `pkg/agentcore` 内完成基本闭环；本阶段重点是 Host 如何理解并表达 loop，不是重写 core tool loop
- planner-lite 只做 hint / hook，不做任务图、队列、子 agent 调度
- 如果后续发现 `resume_count`、`next_action_hint` 已经稳定，再考虑单独展开 Web 工作台的可视化交互
