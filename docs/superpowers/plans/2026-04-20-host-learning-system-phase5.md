# Host Learning-System Phase 5 Plan

> **Goal:** 在不提前引入 FTS5 检索、自动技能提炼或复杂 review pipeline 的前提下，先把 Host 当前 learning-system 主线补到“可人工采纳、可落正式记忆文件、可持续刷新草稿”的最小可自用闭环。

**Architecture:** 继续沿用当前 `session.Service -> recap hooks -> memory_candidates -> draft files` 主线，不把 learning 逻辑下沉到 `pkg/agentcore`。本阶段只增强 Host 侧的人审后沉淀能力：围绕 `UpdateMemoryCandidate()`、memory file writer、gateway API 与文档收口 `draft/rendered -> applied/rejected -> MEMORY.md / USER.md` 的稳定行为。

**Tech Stack:** Go 1.24, SQLite, existing `internal/host/session`, `internal/host/gateway`, filesystem markdown memory files, `go test`

---

## Scope

本阶段只覆盖 Host 本地 learning-system 的第一刀，不包含：

- SQLite + FTS5 检索接入
- 会话启动时的 memory retrieval 注入
- 自动 review / adopt / iterate 后台流程
- skill 候选自动生成与发布
- 多 session / 全局用户画像合并策略

本阶段完成后，应满足：

- `memory candidate` 被 Accept 后，会真正写入正式记忆文件
- `memory candidate` 被 Reject 后，草稿和正式文件都保持一致刷新
- 当前 session 级 draft / formal memory 文件行为清晰、可手测
- 文档对 `draft`、`rendered`、`applied`、`rejected` 的边界解释与代码一致

---

## Current Gap

当前主线已经具备：

- terminal turn -> `recaps`
- `recaps` -> `memory_candidates`
- `memory_candidates` -> `MEMORY.draft.md` / `USER.draft.md`
- REST `accept/reject`

但还缺一个关键闭环：

- `accept` 之后并不会真正写入 `MEMORY.md` / `USER.md`

这意味着当前 learning-system 仍停在“候选与草稿”阶段，还不能算真正沉淀。

---

## Target Design

本阶段建议继续保持简单直接的 Host contract：

### 1. 文件分层

- `MEMORY.draft.md` / `USER.draft.md`
  - 继续作为 session 级候选草稿视图
  - 只展示 `draft` / `rendered`
- `MEMORY.md` / `USER.md`
  - 作为当前 host memory root 下的正式记忆文件
  - 只展示 `applied`

### 2. 状态语义

- `draft`
  - 新候选，尚未渲染草稿
- `rendered`
  - 已进入草稿文件
- `applied`
  - 已被人工采纳，应进入正式记忆文件
- `rejected`
  - 已被人工拒绝，不应再进入任何记忆视图

### 3. 操作行为

- Accept
  - 更新候选状态为 `applied`
  - 重新刷新 draft 文件
  - 重新刷新 formal memory 文件
- Reject
  - 更新候选状态为 `rejected`
  - 重新刷新 draft 文件
  - formal memory 文件不应包含该候选

### 4. 边界

- 继续保持 best-effort，不让 memory file 刷新影响主 turn 生命周期
- 当前 formal memory 仍按 session 级聚合，不做全局 merge 策略
- 当前 formal memory 仍按 `kind + content` 去重，不引入复杂编辑器或 patch 机制

---

## File Structure

- Modify: `internal/host/session/service.go`
  Responsibility: 在 `UpdateMemoryCandidate()` 后挂接正式记忆文件刷新
- Modify: `internal/host/session/recap_hooks.go`
  Responsibility: 提取可复用的 draft / formal memory 文件渲染逻辑
- Test: `internal/host/session/memory_draft_test.go`
  Responsibility: 覆盖 applied/rejected 后的 formal memory 文件刷新与去重
- Test: `internal/host/session/service_test.go`
  Responsibility: 覆盖 `UpdateMemoryCandidate()` 触发 memory file 刷新的行为
- Modify: `internal/app/server.go`
  Responsibility: 为 `session.Service` 注入 learning-system file writer 所需配置
- Modify: `docs/usage/host-recap-memory-hooks-v0.md`
  Responsibility: 更新当前 learning-system 落地边界
- Modify: `docs/usage/web-host-local-dev.md`
  Responsibility: 更新手测说明

---

## Task 1: Formal Memory File Writer

- [x] 提取共享 memory file writer，支持 `draft` 与 `formal` 两类文件刷新
- [x] 新增测试覆盖：
  - `applied` 候选会进入 `MEMORY.md` / `USER.md`
  - `rejected` 候选不会进入正式文件
  - 重复候选按 `kind + content` 去重

Run: `go test ./internal/host/session -run "MemoryDraft|FormalMemory" -v`

---

## Task 2: Wire Candidate Accept/Reject To Formal Memory Refresh

- [x] 在 `session.Service.UpdateMemoryCandidate()` 后挂接 memory file refresh
- [x] 保持 `accept/reject` 对外 API 不变
- [x] 失败策略保持 best-effort，但需要可观察日志
- [x] 覆盖 service / gateway 回归测试

Run: `go test ./internal/host/session ./internal/host/gateway`

---

## Task 3: Update Docs And Local Dev Flow

- [x] 更新 `docs/usage/host-recap-memory-hooks-v0.md`
- [x] 更新 `docs/usage/web-host-local-dev.md`
- [x] 补充最小手测：
  - 创建 session / turn
  - 生成 memory candidates
  - Accept 一个候选后确认 `MEMORY.md` 或 `USER.md` 已更新
  - Reject 一个候选后确认 draft / formal 文件都不再包含它

---

## Exit Criteria

- `memory candidate -> accept/reject -> 文件刷新` 主线完整可用
- `MEMORY.draft.md` / `USER.draft.md` 与 `MEMORY.md` / `USER.md` 的职责边界清晰
- `go test ./internal/host/session ./internal/host/gateway` 通过
- 文档与当前行为一致

---

## Notes

- 本阶段故意不碰 FTS5 检索
- 本阶段故意不做自动 adopt
- 本阶段故意不把 `planner_hint` / `next_action_hint` 直接变成自动学习策略
- 先把人工可控的 learning-system 闭环收口，再继续往 retrieval 与 review 流程扩展
