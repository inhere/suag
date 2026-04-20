# Host Learning Retrieval Phase 6 Plan

> **Goal:** 在不引入 SQLite FTS5、向量检索或复杂 prompt framework 的前提下，把当前已经存在的 `MEMORY.md / USER.md` 正式记忆文件接入 Host internal runner，让后续会话至少能按最小规则消费已采纳记忆。

**Architecture:** 继续保持 `pkg/agentcore` 只负责 runtime kernel，不把 Host learning retrieval 直接塞进 core。Phase 6 只在 `internal/host/intrunner` 增加一个本地 file-based memory recall 层：从 `host.memory.root/memory/MEMORY.md`、`USER.md` 读取正式记忆，转成轻量 `system` memory message 注入到 session memory 上下文中。先解决“已采纳记忆能被用起来”，不解决“检索是否足够聪明”。

**Tech Stack:** Go 1.24, existing `internal/host/intrunner`, `internal/platform/memory`, filesystem markdown memory files, `go test`

---

## Scope

本阶段只覆盖 Host internal runner 的最小 retrieval，不包含：

- SQLite + FTS5 全文检索
- 向量召回
- 多 session / 全局 memory merge 策略
- ACP runner 的远端 retrieval 对齐
- planner_hint / next_action_hint 自动触发 retrieval

本阶段完成后，应满足：

- intrunner 在新 turn 启动时能读取正式 `MEMORY.md / USER.md`
- 正式记忆会作为轻量 `system` message 进入运行上下文
- 没有正式记忆文件时保持静默，不影响现有主链
- 文档清晰说明当前 retrieval 是 file-based baseline，不是最终 memory system

---

## Current Gap

当前 learning-system 已经完成：

- recap -> memory candidates
- candidate accept/reject
- `MEMORY.md / USER.md` 正式写入

但还缺一个关键闭环：

- formal memory 还没有真正进入后续会话运行上下文

也就是说，当前系统已经会“记住”，但还不会“使用已经记住的内容”。

---

## Target Design

### 1. Retrieval Source

只读取：

- `host.memory.root/memory/MEMORY.md`
- `host.memory.root/memory/USER.md`

只消费 formal memory，不读 draft 文件。

### 2. Retrieval Timing

只在 intrunner 新建 `sessionMemory` 时做一次加载：

- 每次 `Run()` 开始时
- 读取当前 formal memory 文件
- 转成前置 `system` memory messages

不做运行中的动态刷新。

### 3. Retrieval Shape

采用最简单可观察的注入方式：

- `MEMORY.md` -> system message
  - 例如前缀 `[Host MEMORY]`
- `USER.md` -> system message
  - 例如前缀 `[Host USER]`

要求：

- 保持可调试
- 明确区分 Agent 自己的经验与用户偏好
- 不改 agentcore prompt builder contract

### 4. Failure Policy

- 文件不存在：静默跳过
- 文件读取失败：warn 日志，但不打断 turn
- 内容为空：不注入

---

## File Structure

- Modify: `internal/host/intrunner/runner.go`
  Responsibility: 加本地 formal memory recall，并注入 session memory
- Test: `internal/host/intrunner/runner_test.go`
  Responsibility: 覆盖无记忆、有 MEMORY、有 USER、刷新后的 retrieval 行为
- Modify: `internal/app/server.go`
  Responsibility: 为 intrunner 传递 host memory root
- Modify: `internal/config/paths.go`
  Responsibility: 继续复用 `ResolveHostMemoryDraftRoot()` 或抽更清晰命名 helper
- Modify: `docs/usage/host-recap-memory-hooks-v0.md`
  Responsibility: 增补“formal memory 已进入 intrunner retrieval baseline”
- Add or Modify: `docs/usage/host-learning-retrieval-v0.md`
  Responsibility: 说明当前 retrieval 语义边界

---

## Task 1: File-Based Formal Memory Recall

- [ ] intrunner 在新 turn 启动时读取 `MEMORY.md` / `USER.md`
- [ ] 转成前置 `system` messages 注入 `sessionMemory`
- [ ] 不影响现有 transcript 追加顺序

Run: `go test ./internal/host/intrunner -run "Memory|Retrieval" -v`

---

## Task 2: Keep Contract Minimal And Observable

- [ ] 注入 message 前缀明确区分 `Host MEMORY` / `Host USER`
- [ ] 文件缺失或读取错误不打断主链
- [ ] 测试覆盖 mock provider 最终看到的 messages 顺序

Run: `go test ./internal/host/intrunner`

---

## Task 3: Update Docs

- [ ] 更新 `docs/usage/host-recap-memory-hooks-v0.md`
- [ ] 补一份 retrieval 说明文档
- [ ] 更新联调说明中“当前 retrieval 能力”的状态

---

## Exit Criteria

- intrunner 可消费 formal memory 文件
- 新会话回合的 provider request 中可看到 injected memory messages
- `go test ./internal/host/intrunner` 通过
- 文档与代码一致

---

## Notes

- 本阶段故意不碰 FTS5
- 本阶段故意不做 query-aware retrieval
- 本阶段故意不把记忆写进 system prompt builder，而是先走 memory context 注入
- 先验证“记忆可被使用”的主链，再继续优化“检索是否聪明”
