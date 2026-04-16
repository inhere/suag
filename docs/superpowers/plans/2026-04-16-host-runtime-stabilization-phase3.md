# Host Runtime Stabilization Phase 3 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 Host v0 主链收敛到“可稳定继续开发和自用联调”的状态，优先补齐 wait/resume/HITL、runner contract 和旧 runtime 收口。

**Architecture:** 继续以 `internal/host/session` 作为 Host 生命周期编排中心，以 `session.Runner` 作为 internal runner / ACP runner 的统一边界。先补齐非终态与恢复态合同，再统一事件/快照消费语义，最后收窄旧 `ReactRunner` 的存在范围，避免后续能力接入时再次返工。

**Tech Stack:** Go, SQLite, WebSocket, AG-UI event mapping, `pkg/agentcore`, `gookit` config stack

---

## Scope

本阶段只覆盖 Host Go 主线，不包含：

- Web 新交互设计扩展
- MCP / skills / memory 检索完整产品化
- 多 agent 调度面板
- IM channel 接入

本阶段完成后，应满足：

- `StartTurn / ContinueTurn / StopTurn / HITL 响应` 的状态机一致
- internal runner 与 ACP runner 对 Host 的外显 contract 更一致
- AG-UI 快照和增量事件可稳定表达 wait/resume 流程
- `ReactRunner` 不再影响 Host 主链判断

### Task 1: Harden Wait-State And Resume Contract

**Files:**
- Modify: `internal/host/session/service.go`
- Modify: `internal/host/session/types.go`
- Modify: `internal/host/intrunner/runner.go`
- Modify: `internal/host/gateway/ws.go`
- Test: `internal/host/session/service_test.go`
- Test: `internal/host/intrunner/runner_test.go`
- Test: `internal/host/gateway/ws_test.go`

- [ ] **Step 1: 写出等待态与恢复态缺口测试**

补充下面几类测试：

- `continue_turn` 只能继续最新等待中的 turn/run
- `AUTH_REQUIRED` 时只能走 HITL 响应，不允许继续 `continue_turn`
- `stop_turn` 后 session/turn/run 状态与事件流一致
- suspended internal runtime 恢复后不会重复发 terminal event

Run: `go test ./internal/host/session ./internal/host/intrunner ./internal/host/gateway`
Expected: 至少一组新增用例先失败，暴露当前 contract 缺口

- [ ] **Step 2: 收口 service 层状态判断**

在 `internal/host/session/service.go` 中把 wait/resume 的判断集中到清晰的辅助方法里：

- latest active/waiting run 判定
- continue 前置校验
- stop/cancel 后的生命周期回写
- waiting 与 terminal 状态分流

要求：

- 注释说明为什么 `INPUT_REQUIRED` 和 `AUTH_REQUIRED` 不能走同一路径
- 避免把更多状态机判断继续散落在 gateway 或 runner 中

- [ ] **Step 3: 收口 intrunner 的 waiting 语义**

在 `internal/host/intrunner/runner.go` 中明确 internal runtime 对 Host 暴露的 wait-state contract：

- 哪些 yield reason 归类到 `AUTH_REQUIRED`
- 哪些 yield reason 归类到 `INPUT_REQUIRED`
- 恢复 suspended runner 时 summary / waitState 的返回约束

要求：

- 保持 Host 看到的是粗粒度稳定状态，不泄漏过多 runtime 内部细节
- 关键归类逻辑前加简短注释

- [ ] **Step 4: 校准 WS 命令入口**

在 `internal/host/gateway/ws.go` 中保证：

- `continue_turn` / `stop_turn` 的 command ack 与真实执行结果一致
- 错误清晰返回给前端
- 不在 gateway 层重复实现状态机规则

- [ ] **Step 5: 跑测试并提交**

Run: `go test ./internal/host/session ./internal/host/intrunner ./internal/host/gateway`
Expected: PASS

```bash
git add internal/host/session/service.go internal/host/session/types.go internal/host/intrunner/runner.go internal/host/gateway/ws.go internal/host/session/service_test.go internal/host/intrunner/runner_test.go internal/host/gateway/ws_test.go
git commit -m "feat: harden host wait-state contract"
```

### Task 2: Align Internal Runner And ACP Runner Host Contract

**Files:**
- Modify: `internal/host/session/service.go`
- Modify: `internal/host/acpbridge/runner.go`
- Modify: `internal/host/acpbridge/types.go`
- Modify: `internal/host/agui/mapper.go`
- Test: `internal/host/session/service_external_task_test.go`
- Test: `internal/host/session/service_acpbridge_integration_test.go`
- Test: `internal/host/acpbridge/bridge_test.go`
- Test: `internal/host/agui/mapper_test.go`

- [ ] **Step 1: 写出 runner contract 差异测试**

补充测试覆盖：

- internal / ACP runner 的 `RuntimeResult` 在 waiting / completed / failed 下的 Host 行为一致
- external task 持久化顺序与 finished event 顺序一致
- AG-UI 映射不依赖 runner 类型做特殊分支

Run: `go test ./internal/host/session ./internal/host/acpbridge ./internal/host/agui`
Expected: 新增 contract 用例先失败或暴露当前不一致

- [ ] **Step 2: 收口 RuntimeResult 与外部任务落库语义**

在 `internal/host/session/service.go` 中明确：

- `ExternalTask` 什么时候写库
- waiting 与 terminal event 的发布顺序
- summary / latestMessage / external stream 对 snapshot 的影响边界

- [ ] **Step 3: 收口 ACP bridge 到 Host 的状态表达**

在 `internal/host/acpbridge/runner.go` 和 `internal/host/acpbridge/types.go` 中整理：

- dispatching / external_running / completed / failed 的最小状态集合
- remote session resume 所需字段
- 返回给 Host 的 metadata 最小闭包

- [ ] **Step 4: 校准 AG-UI 映射**

在 `internal/host/agui/mapper.go` 中保证：

- run activity snapshot 优先基于 Host state，而不是 runner 私有语义
- `turnId`、`runId`、`externalTaskId` 这些关联键稳定存在
- wait-state 在 snapshot 与增量事件上的展示语义一致

- [ ] **Step 5: 跑测试并提交**

Run: `go test ./internal/host/session ./internal/host/acpbridge ./internal/host/agui`
Expected: PASS

```bash
git add internal/host/session/service.go internal/host/acpbridge/runner.go internal/host/acpbridge/types.go internal/host/agui/mapper.go internal/host/session/service_external_task_test.go internal/host/session/service_acpbridge_integration_test.go internal/host/acpbridge/bridge_test.go internal/host/agui/mapper_test.go
git commit -m "refactor: align host runner contracts"
```

### Task 3: Narrow Legacy React Runner Usage

**Files:**
- Modify: `internal/agent/runner_factory.go`
- Modify: `internal/host/intrunner/runner.go`
- Modify: `internal/team/manager.go`
- Modify: `internal/orchestrator/orchestrator.go`
- Test: `internal/agent/runner_factory_test.go`
- Test: `internal/agent/runner_parity_test.go`

- [ ] **Step 1: 先补清楚保留边界测试**

补测试明确：

- Host 默认 internal runner 走 `agentcore`
- `ReactRunner` 只在显式配置时保留
- parity test 继续作为遗留对照，不作为 Host 主链行为来源

Run: `go test ./internal/agent ./internal/team ./internal/orchestrator`
Expected: 新的默认行为断言先失败或暴露歧义点

- [ ] **Step 2: 收窄 ReactRunner 选择逻辑**

实现目标：

- `runner_factory` 明确默认是 `agentcore`
- Host internal runner 不再隐式受 `ReactRunner` 影响
- team/orchestrator 对遗留 runtime 的引用有清晰注释

- [ ] **Step 3: 清理注释与命名歧义**

重点处理：

- 让 `ReactRunner` 的遗留性质更明显
- 避免新代码误把它当作 Host v0 推荐路径

- [ ] **Step 4: 跑测试并提交**

Run: `go test ./internal/agent ./internal/team ./internal/orchestrator`
Expected: PASS

```bash
git add internal/agent/runner_factory.go internal/host/intrunner/runner.go internal/team/manager.go internal/orchestrator/orchestrator.go internal/agent/runner_factory_test.go internal/agent/runner_parity_test.go
git commit -m "refactor: narrow legacy react runtime usage"
```

### Task 4: Add Post-Turn Recap And Memory Hooks Skeleton

**Files:**
- Modify: `internal/host/session/service.go`
- Modify: `internal/platform/memory/*` (only the focused files needed)
- Modify: `internal/app/app.go`
- Modify: `docs/usage/host-state-snapshot-v0.md`
- Add: `docs/usage/host-recap-memory-hooks-v0.md`
- Test: `internal/host/session/service_test.go`

- [ ] **Step 1: 先补 hook 行为测试**

补充测试覆盖：

- turn completed / failed / canceled 后会触发统一 recap hook
- waiting 态不会误触发终态 recap
- hook 失败不会破坏主链执行结果

Run: `go test ./internal/host/session`
Expected: 新增 hook 行为用例先失败

- [ ] **Step 2: 加入 recap hook 骨架**

实现目标：

- `session.Service` 在终态节点留标准 recap 触发点
- 暂不引入完整记忆系统，只定义最小输入输出
- 后续可接 `MEMORY.md` / `USER.md` / SQLite FTS，不需要再改主状态机

- [ ] **Step 3: 更新文档**

补充说明：

- Host snapshot 与 recap hook 的关系
- 当前只提供挂点，不承诺完整记忆产品语义

- [ ] **Step 4: 跑测试并提交**

Run: `go test ./internal/host/session`
Expected: PASS

```bash
git add internal/host/session/service.go internal/app/app.go docs/usage/host-state-snapshot-v0.md docs/usage/host-recap-memory-hooks-v0.md internal/host/session/service_test.go
git commit -m "feat: add host recap memory hooks"
```

### Task 5: Manual Verification And Local Dev Tightening

**Files:**
- Modify: `docs/usage/web-host-local-dev.md`
- Modify: `internal/app/server_test.go`
- Modify: `internal/host/gateway/ws_test.go`

- [ ] **Step 1: 补联调与恢复验证项**

把下面场景写入文档和测试：

- create session
- start turn
- wait for `INPUT_REQUIRED`
- continue turn
- trigger `AUTH_REQUIRED`
- respond via HITL
- refresh page and recover focused state
- stop active turn

- [ ] **Step 2: 补最小 server 级集成测试**

确保：

- server 默认 wiring 可跑通
- 默认路径与配置假设不会再漂移

- [ ] **Step 3: 跑回归并提交**

Run: `go test ./internal/app ./internal/host/gateway`
Expected: PASS

```bash
git add docs/usage/web-host-local-dev.md internal/app/server_test.go internal/host/gateway/ws_test.go
git commit -m "test: tighten host local verification flow"
```

## Exit Criteria

- `go test ./internal/host/... ./internal/app ./internal/agent/...` 关键包通过
- `cmd/axon server` 本地默认配置可启动并完成一轮会话
- Web 联调可稳定覆盖 `start / continue / stop / auth-required + hitl`
- 文档与代码现状一致，不再依赖历史 v1 语义解释当前行为

## Notes

- 优先修改现有文件，不为了“好看”做无关重构
- 单个 Go 文件如果接近 1000 行，优先在本阶段内顺手拆辅助文件
- 每个任务结束都更新 `AGENTS.md` 的进行中事项，并提交 Git
