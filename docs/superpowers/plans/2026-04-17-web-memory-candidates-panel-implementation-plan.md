# Web Memory Candidates Panel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在现有 Axon Web 三栏工作台右侧接入 `Memory Candidates` 最小人工处理面板，打通 session 级候选查看、采纳和拒绝链路。

**Architecture:** 保持 `memory candidates` 作为独立于 websocket snapshot 的右侧辅助面板数据，不混入现有 session reducer。前端通过 typed REST client 拉取和更新候选列表，`App.tsx` 负责局部状态与重新拉取，`AppShell.tsx` 只做右侧列插槽传递，新增纯展示型 `memory-candidates-panel` 负责分组和动作渲染。

**Tech Stack:** React 19, TypeScript, Vite, Vitest, Testing Library, REST client in `web/app/src/lib/session-client.ts`

---

## Scope

本计划只覆盖 Web 第一版 `Memory Candidates` 接入，不包含：

- `applied / rejected` 历史视图
- draft 文件正文展示
- websocket 实时同步
- candidate 编辑、批量操作、搜索筛选
- 独立记忆中心页面

本计划完成后，应满足：

- 切换 session 时会自动加载当前 session 的待处理 `memory candidates`
- 右侧工作台出现独立 `Memory Candidates` 卡片，位置在 `Run Inspector` 后、`HITL` 前
- 候选按 `MEMORY / USER` 两组展示
- `Accept / Reject` 成功后会重新拉取列表，并从待处理视图移除
- 加载失败、动作失败、动作 pending 都有明确状态反馈

## File Map

### Existing files to modify

- `web/app/src/lib/host-types.ts`
  - 新增 `MemoryCandidate` 类型与候选状态/目标字段
- `web/app/src/lib/session-client.ts`
  - 新增 memory candidate REST 类型映射和 client 方法
- `web/app/src/lib/session-client.test.ts`
  - 补 memory candidate REST mapping 测试
- `web/app/src/app/App.tsx`
  - 接 session 级 memory candidate 列表加载、重试、accept/reject 动作和局部状态
- `web/app/src/app/app-shell.tsx`
  - 为右侧列增加 `MemoryCandidatesPanel` 插槽和 props 透传
- `web/app/src/app/App.test.tsx`
  - 补 `App` 级端到端交互测试
- `web/app/src/styles.css`
  - 增补 memory panel 的右侧卡片样式
- `AGENTS.md`
  - 更新正在进行的工作，反映计划进入实现阶段

### New files to create

- `web/app/src/features/memory-candidates/memory-candidates-panel.tsx`
  - 纯展示型右侧卡片，负责分组、空状态、错误态、条目动作按钮
- `web/app/src/features/memory-candidates/memory-candidates-panel.test.tsx`
  - 可选但建议补一个轻量展示测试，锁定分组和条目禁用状态

## Implementation Notes

- 只展示 `draft` 和 `rendered` 状态；过滤逻辑优先放在 `App.tsx`，面板接收已经整理好的候选数组
- `target` 作为分组依据，预期值至少包含 `MEMORY`、`USER`
- 动作成功后直接重新调用 `listMemoryCandidates(currentSessionId)`，不在前端本地猜状态
- `memoryCandidateActionPendingId` 只允许单条 pending；第一版不做并行多条动作
- 面板错误反馈分两类：
  - 列表级错误：`Failed to load memory candidates.`
  - 动作级错误：`Failed to update candidate.`
- 组件保持“纯 props + callback”边界，避免把 REST 或状态机塞进展示组件

### Task 1: Add Memory Candidate Types And Typed Client

**Files:**
- Modify: `web/app/src/lib/host-types.ts`
- Modify: `web/app/src/lib/session-client.ts`
- Test: `web/app/src/lib/session-client.test.ts`

- [ ] **Step 1: 写 memory candidate client 失败测试**

在 `web/app/src/lib/session-client.test.ts` 中新增三组测试：

- `listMemoryCandidates("sess_001")` 请求 `GET /api/sessions/sess_001/memory-candidates`
- `acceptMemoryCandidate("cand_001")` 请求 `POST /api/memory-candidates/cand_001/accept`
- `rejectMemoryCandidate("cand_001")` 请求 `POST /api/memory-candidates/cand_001/reject`

测试返回值至少覆盖：

- `candidateId`
- `sessionId`
- `turnId`
- `runId`
- `target`
- `kind`
- `content`
- `score`
- `status`
- `createdAt`
- `metadata`

Run: `pnpm --dir web/app test -- session-client.test.ts`
Expected: FAIL，提示 client 方法或类型尚未实现

- [ ] **Step 2: 在 host-types 中补齐 MemoryCandidate 类型**

在 `web/app/src/lib/host-types.ts` 中新增：

```ts
export type MemoryCandidate = {
  candidateId: string;
  sessionId: string;
  turnId: string;
  runId: string;
  sourceRecapId: string;
  target: string;
  kind: string;
  content: string;
  score: number;
  status: string;
  createdAt: string;
  metadata: Record<string, unknown>;
};
```

要求：

- 字段命名与现有前端 camelCase 风格一致
- `metadata` 默认走 `Record<string, unknown>`

- [ ] **Step 3: 在 session-client 中补齐 REST mapping**

在 `web/app/src/lib/session-client.ts` 中新增：

- `type MemoryCandidateResponse`
- `mapMemoryCandidateResponse(input: MemoryCandidateResponse): MemoryCandidate`
- `listMemoryCandidates(sessionId, fetchImpl?)`
- `acceptMemoryCandidate(candidateId, fetchImpl?)`
- `rejectMemoryCandidate(candidateId, fetchImpl?)`

实现约束：

- 继续复用 `parseJSON`
- 请求路径与 spec 完全一致
- `accept/reject` 如果服务端只返回简单状态对象，则 client 返回 `Promise<void>`

- [ ] **Step 4: 跑 client 测试并确认通过**

Run: `pnpm --dir web/app test -- session-client.test.ts`
Expected: PASS

- [ ] **Step 5: 提交类型与 client 阶段**

```bash
git add web/app/src/lib/host-types.ts web/app/src/lib/session-client.ts web/app/src/lib/session-client.test.ts
git commit -m "feat: add memory candidate web client"
```

### Task 2: Build The Right-Side Memory Candidates Panel

**Files:**
- Create: `web/app/src/features/memory-candidates/memory-candidates-panel.tsx`
- Test: `web/app/src/features/memory-candidates/memory-candidates-panel.test.tsx`
- Modify: `web/app/src/styles.css`

- [ ] **Step 1: 写面板展示测试**

在 `web/app/src/features/memory-candidates/memory-candidates-panel.test.tsx` 中覆盖下面行为：

- 有 `MEMORY` 和 `USER` 两组时，分组标题都会显示
- 空列表时显示 `No pending memory candidates.`
- `loading` 时显示 `Loading memory candidates...`
- `errorMessage` 时显示 `Failed to load memory candidates.` 与 `Retry`
- 某条 `pendingCandidateId` 命中时，仅该条 `Accept / Reject` 被禁用

Run: `pnpm --dir web/app test -- memory-candidates-panel.test.tsx`
Expected: FAIL，提示组件尚未实现

- [ ] **Step 2: 实现纯展示型面板组件**

在 `web/app/src/features/memory-candidates/memory-candidates-panel.tsx` 中实现：

- 顶部标题和待处理数量
- `MEMORY` 组列表
- `USER` 组列表
- 列表级 loading / error / empty
- 每条候选展示 `kind`、`content`、`status`
- 每条候选提供 `Accept` / `Reject`

建议 props：

```ts
type MemoryCandidatesPanelProps = {
  candidates: MemoryCandidate[];
  loading?: boolean;
  errorMessage?: string | null;
  actionError?: string | null;
  pendingCandidateId?: string | null;
  onRetry?: () => void;
  onAccept?: (candidateId: string) => void;
  onReject?: (candidateId: string) => void;
};
```

要求：

- 保持纯展示，不直接引入 client
- 分组顺序固定为 `MEMORY` 然后 `USER`
- 对未知 `target` 可暂时忽略或放在末尾，但不要影响 `MEMORY / USER` 主路径

- [ ] **Step 3: 补样式**

在 `web/app/src/styles.css` 增加：

- `.memory-candidates-panel`
- `.memory-candidates-panel__header`
- `.memory-candidates-panel__group`
- `.memory-candidate-card`
- `.memory-candidate-card__meta`
- `.memory-candidate-card__actions`

样式要求：

- 视觉上延续右侧现有 `run-panel` / `hitl-panel` 风格
- 内容支持 `white-space: pre-wrap`
- 错误/说明文案用现有 `.inline-feedback` 调性

- [ ] **Step 4: 跑组件测试**

Run: `pnpm --dir web/app test -- memory-candidates-panel.test.tsx`
Expected: PASS

- [ ] **Step 5: 提交面板组件阶段**

```bash
git add web/app/src/features/memory-candidates/memory-candidates-panel.tsx web/app/src/features/memory-candidates/memory-candidates-panel.test.tsx web/app/src/styles.css
git commit -m "feat: add memory candidates panel"
```

### Task 3: Wire Session-Level State Into App And AppShell

**Files:**
- Modify: `web/app/src/app/App.tsx`
- Modify: `web/app/src/app/app-shell.tsx`
- Modify: `web/app/src/features/memory-candidates/memory-candidates-panel.tsx`

- [ ] **Step 1: 在 App.test.tsx 中加失败用例**

在 `web/app/src/app/App.test.tsx` 中补 mock 与行为测试：

- 当前 session 载入后会调用 `listMemoryCandidates(sessionId)`
- `Memory Candidates` 面板显示 `MEMORY / USER` 分组
- `Accept` 成功后会再次调用 `listMemoryCandidates(sessionId)`，并更新列表
- `Reject` 成功后会再次调用 `listMemoryCandidates(sessionId)`，并更新列表
- 列表加载失败显示 `Failed to load memory candidates.`
- 点击 `Retry` 后重新拉取
- 单条动作 pending 时，只禁用对应候选按钮
- 动作失败显示 `Failed to update candidate.`

Run: `pnpm --dir web/app test -- App.test.tsx`
Expected: FAIL，提示 `App` / `AppShell` 尚未接入 memory candidates

- [ ] **Step 2: 在 App.tsx 接入局部状态与加载函数**

在 `web/app/src/app/App.tsx` 中新增：

- `memoryCandidates`
- `memoryCandidatesLoading`
- `memoryCandidatesError`
- `memoryCandidateActionPendingId`
- `memoryCandidateActionError`

同时实现：

- `refreshMemoryCandidates(sessionId: string)`
- 当前 `sessionId` 变化时自动加载
- `handleAcceptMemoryCandidate(candidateId)`
- `handleRejectMemoryCandidate(candidateId)`

实现约束：

- session 切换时要清掉旧 session 的 memory candidate 错误和 pending
- 动作成功后重新拉当前 session 列表
- 如果 `currentSessionId` 为空，不主动请求

- [ ] **Step 3: 在 AppShell.tsx 接入右侧面板**

在 `web/app/src/app/app-shell.tsx` 中：

- 引入 `MemoryCandidatesPanel`
- 扩展 `AppShellProps`
- 将面板放在右侧栈中，位置是 `RunInspector` 后、`HITLPanel` 前

要求：

- 保持 `RunInspector` 和 `HITLPanel` 职责不变
- 右侧列标题仍保持 `Run Inspector`

- [ ] **Step 4: 跑 App 测试并修正交互细节**

Run: `pnpm --dir web/app test -- App.test.tsx`
Expected: PASS

重点确认：

- session 切换后不会残留上一个 session 的候选
- 列表刷新失败和动作失败互不覆盖错误文案
- `Accept / Reject` 后 UI 不会卡在 pending

- [ ] **Step 5: 提交接线阶段**

```bash
git add web/app/src/app/App.tsx web/app/src/app/app-shell.tsx web/app/src/app/App.test.tsx web/app/src/features/memory-candidates/memory-candidates-panel.tsx
git commit -m "feat: wire memory candidates into workbench"
```

### Task 4: Final Verification And Doc Sync

**Files:**
- Modify: `AGENTS.md`
- Optional Modify: `docs/usage/web-host-local-dev.md`

- [ ] **Step 1: 更新 AGENTS 进行中事项**

把当前进行中事项从“设计草案”推进到“Web 面板实现进行中/已接入完成”的准确状态。

- [ ] **Step 2: 运行前端相关测试**

Run: `pnpm --dir web/app test`
Expected: PASS

如果有时间，再跑：

Run: `pnpm --dir web/app build`
Expected: PASS

- [ ] **Step 3: 手工联调检查**

按 `docs/usage/web-host-local-dev.md` 的本地链路验证：

- 打开已有 session
- 触发产生 memory candidate 的 turn
- 在右侧面板看到候选
- 点击 `Accept` 或 `Reject`
- 列表自动刷新

如果联调时发现需要补一句使用说明，再更新 `docs/usage/web-host-local-dev.md`

- [ ] **Step 4: 提交收尾**

```bash
git add AGENTS.md docs/usage/web-host-local-dev.md web/app/src
git commit -m "feat: add memory candidates workbench flow"
```

## Verification Checklist

- `pnpm --dir web/app test -- session-client.test.ts`
- `pnpm --dir web/app test -- memory-candidates-panel.test.tsx`
- `pnpm --dir web/app test -- App.test.tsx`
- `pnpm --dir web/app test`
- `pnpm --dir web/app build`

## Risks

- `App.test.tsx` 当前体量已经较大，新增用例时要注意复用辅助数据，避免继续失控膨胀；如果明显继续变重，可以在实现阶段顺手抽 `memory candidate` fixture/helper
- 右侧列空间有限，候选内容可能较长；第一版样式要优先保证可滚动和不把 HITL 面板完全挤出可用区域
- `listMemoryCandidates` 与 session bootstrap 是两条独立请求，测试里需要允许它们并行，不要写成强时序耦合断言

## Handoff

Plan complete and saved to `docs/superpowers/plans/2026-04-17-web-memory-candidates-panel-implementation-plan.md`. Two execution options:

**1. Subagent-Driven (recommended)** - 我分任务派发新 agent 逐步实现、每步 review 后再推进

**2. Inline Execution** - 我在当前会话里按这个计划连续执行，并在阶段点提交

Which approach?
