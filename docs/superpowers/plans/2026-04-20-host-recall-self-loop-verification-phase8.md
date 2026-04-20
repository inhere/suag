# Host Recall Self-Loop Verification Phase 8 Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 验证 Host intrunner 当前的 `query-aware lite` formal memory recall，是否已经在 self-loop 主链里被稳定消费，而不只是停留在可观测 metadata。

**Architecture:** 本阶段不扩展 planner / orchestration，也不引入 FTS5、向量召回或新 memory scope。重点围绕 `internal/host/intrunner` 和 `internal/host/session` 的现有 contract，补齐“命中是否合理”“继续回合时是否仍会重新 recall”“recalled memory 是否影响 run 结果/等待态”的证据链，并把验证基线同步到 usage 文档。

**Tech Stack:** Go 1.24, `internal/host/intrunner`, `internal/host/session`, existing mock/test provider, `go test`

---

## Scope

本阶段只做验证闭环，不做新的能力扩张：

- 补 recall 命中质量测试
- 补 self-loop / continue 场景下的 recall 消费测试
- 补 usage 文档中的验证说明

本阶段明确不做：

- planner hint 主动消费 `memory_recall`
- post-turn learning 自动调节 recall 策略
- SQLite + FTS5 / 向量检索
- ACP runner retrieval 对齐

完成标准：

- 可以明确区分“recall 被注入了”和“recall 真正影响了本轮行为”
- 可以覆盖首轮 run 与 `continue_turn` / resumed run 两类路径
- 文档能说明当前验证到了哪里、还没承诺什么

---

## Files Involved

- Modify: `internal/host/intrunner/runner_test.go`
  - 补 recall 与 self-loop 的行为验证测试
- Modify: `internal/host/session/service_test.go`
  - 仅在确实需要 Host service 级 contract 断言时补一条最小测试
- Modify: `docs/usage/host-learning-retrieval-v0.md`
  - 增加“当前已验证到的行为边界”
- Modify: `docs/usage/host-internal-self-loop-v0.md`
  - 增加 self-loop 与 recall 的关系说明
- Modify: `AGENTS.md`
  - 更新当前进行中主线

---

## Task 1: Recall Consumption Test Baseline

**Files:**
- Modify: `internal/host/intrunner/runner_test.go`

- [ ] **Step 1: 先补一个“memory 影响最终输出”的失败测试**

目标：

- 不再只断言 provider 看到了 recalled memory
- 要断言 recalled memory 进入上下文后，最终 summary / output 会体现该 memory

建议做法：

- 在测试文件中增加一个 test-only provider，按输入 messages 内容决定返回值
- 当 context 中包含某条 recalled memory 时，返回对应 summary
- 否则返回 baseline summary

建议新增测试：

- `TestRunner_RunRecallCanAffectFinalSummary`

- [ ] **Step 2: 运行单测确认新增断言先失败**

Run: `go test ./internal/host/intrunner -run TestRunner_RunRecallCanAffectFinalSummary -v`

Expected:

- 先看到断言失败，证明当前测试确实覆盖了“消费效果”，而不是重复已有 metadata 测试

- [ ] **Step 3: 补最小测试支撑代码**

实现方向：

- 尽量把 test-only provider 留在 `runner_test.go`
- 不把测试专用逻辑下沉到正式代码
- 保持断言关注：
  - recalled memory 是否进入上下文
  - 最终返回是否受其影响

- [ ] **Step 4: 重新运行单测确认通过**

Run: `go test ./internal/host/intrunner -run TestRunner_RunRecallCanAffectFinalSummary -v`

Expected:

- PASS

- [ ] **Step 5: 提交**

```bash
git add internal/host/intrunner/runner_test.go
git commit -m "test: verify host recall affects final summary"
```

---

## Task 2: Resumed Self-Loop Recall Verification

**Files:**
- Modify: `internal/host/intrunner/runner_test.go`

- [ ] **Step 1: 补一个“continue / resumed run 仍会重新 recall”的失败测试**

目标：

- 覆盖 suspended runtime 恢复路径
- 证明 resumed run 不只是复用旧状态，也会基于新的 instruction 再做一次 recall 选择

建议新增测试：

- `TestRunner_RunReappliesRecallForResumedTurn`

建议断言：

- 首轮 waiting run 命中第一条 memory
- resumed run 使用新 instruction 后，provider context 改为命中另一条 memory
- recalled memory 仍不会被写进 session history transcript

- [ ] **Step 2: 运行单测确认失败点合理**

Run: `go test ./internal/host/intrunner -run TestRunner_RunReappliesRecallForResumedTurn -v`

Expected:

- FAIL，且失败点落在 resumed run 的 recall 或 history 边界断言

- [ ] **Step 3: 补最小实现或测试辅助逻辑**

实现方向：

- 优先只改测试或 intrunner 内极小范围逻辑
- 如果发现 resumed path 没有重新按 instruction recall，需要只在 `internal/host/intrunner` 内修正

- [ ] **Step 4: 运行定向测试确认通过**

Run: `go test ./internal/host/intrunner -run "TestRunner_RunReusesSuspendedRunnerOnContinue|TestRunner_RunReappliesRecallForResumedTurn" -v`

Expected:

- PASS

- [ ] **Step 5: 提交**

```bash
git add internal/host/intrunner/runner_test.go internal/host/intrunner/runner.go
git commit -m "test: verify resumed runs reapply host recall"
```

---

## Task 3: Host Contract Verification And Docs

**Files:**
- Modify: `internal/host/session/service_test.go`
- Modify: `docs/usage/host-learning-retrieval-v0.md`
- Modify: `docs/usage/host-internal-self-loop-v0.md`
- Modify: `AGENTS.md`

- [ ] **Step 1: 判断是否需要补 service 级测试**

判定标准：

- 如果 intrunner tests 已足够覆盖“首轮 + resumed + history 不回写”的 contract，则 `service_test.go` 可以不改
- 如果 resume / continue 在 Host service 级别还有 contract 空洞，再补一条最小测试

候选测试：

- `TestService_ContinueTurnKeepsRecallRuntimeOnly`

- [ ] **Step 2: 同步 usage 文档**

文档至少说明：

- 当前已验证 recall 会影响 provider 可见上下文
- 当前已验证 recall 在 resumed turn 会重新按 instruction 选择
- 当前仍未验证“planner / post-turn hooks 主动消费 recall metadata”

- [ ] **Step 3: 跑本阶段回归测试**

Run:

- `go test ./internal/host/intrunner -v`
- `go test ./internal/host/session -run "ContinueTurn|PostTurn|Recap" -v`
- `go test ./internal/host/agui ./internal/host/gateway`

Expected:

- 全部 PASS

- [ ] **Step 4: 更新进度并提交**

```bash
git add AGENTS.md docs/usage/host-learning-retrieval-v0.md docs/usage/host-internal-self-loop-v0.md internal/host/session/service_test.go
git commit -m "docs: record host recall self-loop verification baseline"
```

---

## Exit Criteria

- 至少有一条测试证明 recalled memory 会影响最终 summary / output
- 至少有一条测试证明 resumed run 会重新按 instruction recall
- 仍保持 recalled memory 不回写 session history 的边界
- 文档明确写出“当前已验证行为”与“尚未承诺行为”
