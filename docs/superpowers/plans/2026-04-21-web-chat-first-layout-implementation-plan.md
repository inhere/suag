# Web Chat-First Layout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 Axon Web 默认界面从偏运行观察的工作台收敛为 `Chat-first` 视图，同时保留 HITL、memory candidates 和紧凑 run context 的可见性与可操作性。

**Architecture:** 继续保留左中右三栏，但重构壳层职责为 `Sessions / Chat / Context`。中栏由 `ConversationPane` 升级为默认主视图，新增薄状态条与必要的内联状态卡；右栏把现有大型 `RunInspector` 降级为紧凑上下文块，并保持 `HITL`、`Memory Candidates` 的独立职责。

**Tech Stack:** React 19, TypeScript, Vite, Vitest, Testing Library, 现有 `web/app` 组件与 `App.tsx` 会话状态装配

---

## Scope Check

这份计划只覆盖 `web/app` 默认 `Chat` 视图重构，不包含：

- `Workbench` 模式实现
- 多 agent 专业任务时间线
- 新的后端 API / websocket contract
- tools / mcp / skills 独立页面
- 大型视觉系统重做

本计划完成后，应满足：

- 左栏只承担会话切换与轻量关注状态
- 中栏成为默认聊天主视图，结构固定为 `Header -> Thin summary strip -> Message stream -> Composer`
- 右栏收敛为 `Pending HITL / Session summary / Memory Candidates / Run details (compact)`
- 默认视图不再把 `RecentTurnsStrip`、大型 `RunInspector`、external activity 流作为主视觉重点
- `App.test.tsx` 与组件级测试能够稳定覆盖新的主交互

## File Map

### Existing files to modify

- `web/app/src/app/App.tsx`
  - 调整传给壳层和聊天区的数据形态，补齐 chat-first 所需的 summary/context props
- `web/app/src/app/App.test.tsx`
  - 更新 `App` 级行为测试，覆盖新的默认布局与上下文展示
- `web/app/src/app/app-shell.tsx`
  - 重构三栏职责，移除默认 `Run Inspector` 标题和旧的工作台导向装配
- `web/app/src/features/conversation/conversation-pane.tsx`
  - 从“消息 + turn summary + recent turns”升级为聊天主视图容器
- `web/app/src/features/conversation/conversation-pane.test.tsx`
  - 更新为 chat-first 中栏结构测试
- `web/app/src/features/conversation/composer.tsx`
  - 对齐新的布局层级与提交反馈位置
- `web/app/src/features/session-list/session-list.tsx`
  - 降低默认控制台感，保持紧凑列表与轻量过滤
- `web/app/src/features/session-list/session-list-item.tsx`
  - 收紧单项信息密度，对齐轻量状态点
- `web/app/src/features/run-inspector/run-inspector.tsx`
  - 降级为默认聊天页可复用的紧凑 run context 组合，或拆分后仅保留薄包装
- `web/app/src/features/run-inspector/run-inspector.test.tsx`
  - 更新为 compact context 预期
- `web/app/src/styles.css`
  - 重做默认布局、聊天区、summary strip、context panels 的样式
- `AGENTS.md`
  - 更新当前正在进行的工作，指向新的实现计划

### New files to create

- `web/app/src/app/app-shell.test.tsx`
  - 锁定 chat-first 三栏壳层装配
- `web/app/src/features/conversation/chat-header.tsx`
  - 中栏顶部轻量 header，显示 session 标题与连接态
- `web/app/src/features/conversation/summary-strip.tsx`
  - 当前 `turn / run` 的薄状态条
- `web/app/src/features/conversation/inline-status-card.tsx`
  - `waiting / approval / failed / completed-without-text` 等必要内联状态卡
- `web/app/src/features/conversation/chat-header.test.tsx`
  - 锁定 header 展示
- `web/app/src/features/conversation/summary-strip.test.tsx`
  - 锁定 strip 状态文案
- `web/app/src/features/conversation/inline-status-card.test.tsx`
  - 锁定允许进入消息流的状态卡
- `web/app/src/features/context/session-summary-panel.tsx`
  - 右栏 session summary 块
- `web/app/src/features/context/run-details-compact-panel.tsx`
  - 右栏 compact run details 块
- `web/app/src/features/context/session-summary-panel.test.tsx`
  - 锁定 session summary 展示
- `web/app/src/features/context/run-details-compact-panel.test.tsx`
  - 锁定 compact run details 展示

## Implementation Notes

- 不修改后端 contract；所有 chat-first 信息都从现有 `snapshot`、`currentTurn`、`focusedRun`、`socketStatus` 派生
- `Thin summary strip` 只显示当前轮次的最短状态，不展示 run 列表和长文本
- 允许进入消息流的状态卡严格限制为：
  - `waiting for input`
  - `needs approval / HITL`
  - `failed`
  - `stopped / canceled`
  - `completed but no assistant text yet`
- `RecentTurnsStrip` 不再作为默认中栏主块；可以在实现中删除默认引用，或保留组件但从默认装配移除
- `RunInspector` 不再默认展示 `Active Runs` / `External Activities` 大块；必要时拆成紧凑右栏块，未来 `Workbench` 再复用重型版本
- 测试优先锁定：
  - 默认页面骨架
  - 中栏信息层级
  - 右栏块顺序
  - `App` 级最小主链不回退

### Task 1: Reshape The Shell Into Sessions / Chat / Context

**Files:**
- Create: `web/app/src/app/app-shell.test.tsx`
- Modify: `web/app/src/app/app-shell.tsx`
- Modify: `web/app/src/features/session-list/session-list.tsx`
- Modify: `web/app/src/features/session-list/session-list-item.tsx`
- Modify: `web/app/src/styles.css`

- [ ] **Step 1: 写壳层失败测试**

在 `web/app/src/app/app-shell.test.tsx` 新增一个 chat-first 装配测试，至少断言：

- 左栏标题为 `Sessions`
- 中栏标题不再是旧的 `Conversation` 大标题，而是由聊天主区内部自带 header
- 右栏标题不再是 `Run Inspector`
- 右栏按顺序出现 `Pending HITL`、`Session summary`、`Memory Candidates`、`Run details`

可用的最小测试骨架：

```tsx
it("renders the chat-first shell with sessions, chat, and context columns", () => {
  render(
    <AppShell
      sessions={[]}
      messages={[]}
      hitlTickets={[]}
      memoryCandidates={[]}
      currentTurn={null}
      runs={[]}
    />,
  );

  expect(screen.getByRole("complementary", { name: /Sessions panel/i })).toBeInTheDocument();
  expect(screen.getByRole("main", { name: /Chat panel/i })).toBeInTheDocument();
  expect(screen.getByRole("region", { name: /Context panel/i })).toBeInTheDocument();
  expect(screen.queryByText("Run Inspector")).not.toBeInTheDocument();
});
```

Run: `pnpm --dir web/app test -- app-shell.test.tsx`
Expected: FAIL，提示旧壳层结构仍然使用 `Conversation / Run Inspector`

- [ ] **Step 2: 重构 AppShell 三栏职责**

在 `web/app/src/app/app-shell.tsx` 中：

- 将中栏 `aria-label` 改为 `Chat panel`
- 将右栏 `aria-label` 改为 `Context panel`
- 移除默认的 `Conversation`、`Run Inspector` 大标题
- 为右栏准备 4 个上下文块插槽：
  - `Pending HITL`
  - `Session summary`
  - `Memory Candidates`
  - `Run details (compact)`

建议壳层结构收敛为：

```tsx
<div className="app-shell">
  <aside className="app-shell__sidebar" aria-label="Sessions panel">
    <SessionList ... />
  </aside>
  <main className="app-shell__main" aria-label="Chat panel">
    <ConversationPane ... />
  </main>
  <section className="app-shell__context" aria-label="Context panel">
    <div className="app-shell__stack">
      <HITLPanel ... />
      <SessionSummaryPanel ... />
      <MemoryCandidatesPanel ... />
      <RunDetailsCompactPanel ... />
    </div>
  </section>
</div>
```

- [ ] **Step 3: 收紧 SessionList 视觉职责**

在 `web/app/src/features/session-list/session-list.tsx` 和 `web/app/src/features/session-list/session-list-item.tsx` 中：

- 保留 `+ New Session`
- 保留搜索和 filter，但降低视觉权重
- 每个 item 只展示：
  - `title`
  - `preview`
  - `updatedAt`
  - 轻量状态点

建议给 item 增加一个状态点容器，类似：

```tsx
<div className="session-item__meta">
  <span className={`session-item__dot session-item__dot--${tone}`} aria-hidden="true" />
  <span>{relativeUpdatedAt}</span>
</div>
```

如果当前数据模型还没有直接的 `tone` 字段，实现阶段先基于现有 `status` 做最小映射即可。

- [ ] **Step 4: 补壳层与左栏样式**

在 `web/app/src/styles.css` 中新增或重写：

- `.app-shell__sidebar`
- `.app-shell__main`
- `.app-shell__context`
- `.session-item__dot`
- `.session-item__dot--idle`
- `.session-item__dot--running`
- `.session-item__dot--waiting`
- `.session-item__dot--attention`

样式目标：

- 中栏明显比两侧宽
- 左栏更像会话导航，不像后台过滤面板
- 右栏更像辅助上下文列，而不是第二主舞台

- [ ] **Step 5: 运行壳层测试并提交**

Run: `pnpm --dir web/app test -- app-shell.test.tsx`
Expected: PASS

```bash
git add web/app/src/app/app-shell.tsx web/app/src/app/app-shell.test.tsx web/app/src/features/session-list/session-list.tsx web/app/src/features/session-list/session-list-item.tsx web/app/src/styles.css
git commit -m "feat: reshape web shell for chat-first layout"
```

### Task 2: Promote ConversationPane Into The Default Chat View

**Files:**
- Create: `web/app/src/features/conversation/chat-header.tsx`
- Create: `web/app/src/features/conversation/summary-strip.tsx`
- Create: `web/app/src/features/conversation/inline-status-card.tsx`
- Create: `web/app/src/features/conversation/chat-header.test.tsx`
- Create: `web/app/src/features/conversation/summary-strip.test.tsx`
- Create: `web/app/src/features/conversation/inline-status-card.test.tsx`
- Modify: `web/app/src/features/conversation/conversation-pane.tsx`
- Modify: `web/app/src/features/conversation/conversation-pane.test.tsx`
- Modify: `web/app/src/features/conversation/composer.tsx`
- Modify: `web/app/src/styles.css`

- [ ] **Step 1: 写中栏信息层级失败测试**

在 `web/app/src/features/conversation/conversation-pane.test.tsx` 中，把旧断言更新为 chat-first 预期：

- 会显示 `summary strip`
- 不再默认显示 `Recent Turns`
- 会在特定 waiting/approval 状态下显示内联状态卡
- `Composer` 仍在最底部，并保留现有提交反馈

可用的最小测试骨架：

```tsx
it("renders chat-first conversation hierarchy", () => {
  render(
    <ConversationPane
      messages={[{ id: "u1", role: "user", text: "hi" }]}
      currentTurn={{ ...turn, displayState: "input_required" }}
      waitingForInput
      onSubmit={vi.fn()}
    />,
  );

  expect(screen.getByText("Waiting for input")).toBeInTheDocument();
  expect(screen.queryByText("Recent Turns")).not.toBeInTheDocument();
  expect(screen.getByRole("textbox")).toBeInTheDocument();
});
```

Run: `pnpm --dir web/app test -- conversation-pane.test.tsx`
Expected: FAIL，提示旧的 `RecentTurnsStrip` 仍然存在，新的 strip/card 尚未实现

- [ ] **Step 2: 新增 chat header / summary strip / inline status 组件**

新增三个小组件：

`chat-header.tsx`

```tsx
type ChatHeaderProps = {
  title: string;
  connectionLabel: string;
  subtitle?: string;
};
```

`summary-strip.tsx`

```tsx
type SummaryStripProps = {
  label: string;
  tone?: "neutral" | "running" | "waiting" | "attention" | "failed";
};
```

`inline-status-card.tsx`

```tsx
type InlineStatusCardProps = {
  title: string;
  body?: string;
  tone?: "waiting" | "attention" | "failed" | "neutral";
};
```

要求：

- 三个组件都保持纯展示
- `summary-strip` 只接收一条短状态，不承担复杂格式化
- `inline-status-card` 只用于 spec 限定的几种状态

- [ ] **Step 3: 重构 ConversationPane**

在 `web/app/src/features/conversation/conversation-pane.tsx` 中：

- 移除默认 `RecentTurnsStrip` 装配
- 将 `TurnSummaryCard` 改为只有在需要时才以内联状态卡方式出现，或者直接替换为 `InlineStatusCard`
- 让结构固定为：

```tsx
<div className="conversation-pane">
  <ChatHeader ... />
  <SummaryStrip ... />
  <div className="conversation-pane__content">
    {messages.map(...)}
    {showInlineStatus ? <InlineStatusCard ... /> : null}
  </div>
  <Composer ... />
</div>
```

实现约束：

- `summary strip` 的文案在 pane 内做最小派生
- 只对 spec 白名单状态展示 `InlineStatusCard`
- 消息流仍然以 `MessageItem` 为主

- [ ] **Step 4: 调整 Composer 与中栏样式**

在 `web/app/src/features/conversation/composer.tsx` 和 `web/app/src/styles.css` 中：

- 确保 `Composer` 在 chat-first 布局里固定贴底
- `summary strip` 很薄，不像卡片堆栈
- 内联状态卡和消息气泡视觉层级清楚，但不喧宾夺主

建议新增样式类：

- `.chat-header`
- `.chat-header__title`
- `.chat-header__meta`
- `.summary-strip`
- `.summary-strip--running`
- `.summary-strip--waiting`
- `.summary-strip--attention`
- `.summary-strip--failed`
- `.inline-status-card`

- [ ] **Step 5: 运行中栏测试并提交**

Run: `pnpm --dir web/app test -- conversation-pane.test.tsx chat-header.test.tsx summary-strip.test.tsx inline-status-card.test.tsx`
Expected: PASS

```bash
git add web/app/src/features/conversation/conversation-pane.tsx web/app/src/features/conversation/conversation-pane.test.tsx web/app/src/features/conversation/chat-header.tsx web/app/src/features/conversation/chat-header.test.tsx web/app/src/features/conversation/summary-strip.tsx web/app/src/features/conversation/summary-strip.test.tsx web/app/src/features/conversation/inline-status-card.tsx web/app/src/features/conversation/inline-status-card.test.tsx web/app/src/features/conversation/composer.tsx web/app/src/styles.css
git commit -m "feat: promote conversation pane to chat-first view"
```

### Task 3: Replace The Default Run Inspector With Compact Context Panels

**Files:**
- Create: `web/app/src/features/context/session-summary-panel.tsx`
- Create: `web/app/src/features/context/run-details-compact-panel.tsx`
- Create: `web/app/src/features/context/session-summary-panel.test.tsx`
- Create: `web/app/src/features/context/run-details-compact-panel.test.tsx`
- Modify: `web/app/src/features/run-inspector/run-inspector.tsx`
- Modify: `web/app/src/features/run-inspector/run-inspector.test.tsx`
- Modify: `web/app/src/app/app-shell.tsx`
- Modify: `web/app/src/styles.css`

- [ ] **Step 1: 写右栏紧凑块失败测试**

新增两个组件测试：

`session-summary-panel.test.tsx`

- 显示当前状态、turnId、active run 数、最近更新时间

`run-details-compact-panel.test.tsx`

- 显示 `runId`、`agent`、`status`、`wait kind`
- completed 优先展示 `summary`
- failed 优先展示失败原因
- 不再要求展示 `External Activities` 和长消息流

Run: `pnpm --dir web/app test -- session-summary-panel.test.tsx run-details-compact-panel.test.tsx run-inspector.test.tsx`
Expected: FAIL，提示右栏仍是旧的重型 inspector

- [ ] **Step 2: 实现 Session Summary 与 Compact Run Details**

实现两个新组件：

`session-summary-panel.tsx`

```tsx
type SessionSummaryPanelProps = {
  sessionId: string | null;
  currentTurnId: string | null;
  currentStatus: string | null;
  activeRunCount: number;
  updatedAt?: string | null;
};
```

`run-details-compact-panel.tsx`

```tsx
type RunDetailsCompactPanelProps = {
  run: StateSnapshotRun | null;
  detailStatus?: "idle" | "loading" | "loaded" | "error";
  onRetryDetail?: () => void;
};
```

要求：

- 两个组件都保持“概览块”定位
- `run-details-compact-panel` 只展示必要字段，不复刻旧 `FocusedRunPanel`

- [ ] **Step 3: 降级 run-inspector 默认职责**

在 `web/app/src/features/run-inspector/run-inspector.tsx` 中二选一：

- 要么直接改成只导出 chat-first 默认需要的 compact wrapper
- 要么保留文件，但把内部默认装配改为新紧凑块，不再拼 `CurrentTurnPanel / ActiveRunsPanel / ExternalTaskPanel / FocusedRunPanel`

建议默认结果接近：

```tsx
export function RunInspector(props: RunInspectorProps) {
  return (
    <div className="run-inspector run-inspector--compact">
      <RunDetailsCompactPanel ... />
    </div>
  );
}
```

注意：

- 不删除 future `Workbench` 可能复用的旧子组件文件
- 但默认壳层不再渲染它们

- [ ] **Step 4: 调整 AppShell 右栏顺序与样式**

在 `web/app/src/app/app-shell.tsx` 和 `web/app/src/styles.css` 中确认右栏最终顺序：

1. `HITLPanel`
2. `SessionSummaryPanel`
3. `MemoryCandidatesPanel`
4. `RunDetailsCompactPanel`

新增或重写样式：

- `.context-panel`
- `.context-panel__header`
- `.context-panel__meta`
- `.run-inspector--compact`

- [ ] **Step 5: 运行右栏测试并提交**

Run: `pnpm --dir web/app test -- run-inspector.test.tsx session-summary-panel.test.tsx run-details-compact-panel.test.tsx`
Expected: PASS

```bash
git add web/app/src/features/context/session-summary-panel.tsx web/app/src/features/context/session-summary-panel.test.tsx web/app/src/features/context/run-details-compact-panel.tsx web/app/src/features/context/run-details-compact-panel.test.tsx web/app/src/features/run-inspector/run-inspector.tsx web/app/src/features/run-inspector/run-inspector.test.tsx web/app/src/app/app-shell.tsx web/app/src/styles.css
git commit -m "feat: add compact context panels for chat-first view"
```

### Task 4: Wire App State Into The New Chat-First Props And Verify The Flow

**Files:**
- Modify: `web/app/src/app/App.tsx`
- Modify: `web/app/src/app/App.test.tsx`
- Modify: `AGENTS.md`
- Optional Modify: `docs/usage/web-host-local-dev.md`

- [ ] **Step 1: 写 App 级失败用例**

在 `web/app/src/app/App.test.tsx` 中补或更新断言：

- 默认渲染后，中栏展示 chat-first 结构而不是旧 `Conversation / Run Inspector` 标题
- `STATE_SNAPSHOT` 进入后，summary strip 能展示当前状态
- waiting / approval / failed 状态能在中栏看到必要内联状态
- 右栏可以同时看到 `Pending HITL`、`Session summary`、`Memory Candidates`、`Run details`

补一个最小断言块：

```tsx
expect(screen.getByText(/Socket connected|Socket connecting/)).toBeInTheDocument();
expect(screen.getByText(/Running|Waiting for input|Needs approval|Failed/)).toBeInTheDocument();
expect(screen.queryByText("Run Inspector")).not.toBeInTheDocument();
```

Run: `pnpm --dir web/app test -- App.test.tsx`
Expected: FAIL，提示 `App.tsx` 还没有给新聊天组件和右栏块提供正确 props

- [ ] **Step 2: 在 App.tsx 派生 chat-first 所需 props**

在 `web/app/src/app/App.tsx` 中新增最小派生逻辑：

- `chatTitle`
- `chatSubtitle`
- `summaryStripLabel`
- `summaryStripTone`
- `inlineStatusCard`
- `sessionSummaryData`

实现约束：

- 尽量基于已有 `currentTurn`、`focusedRun`、`socketStatus` 和 `snapshot` 派生
- 不新增复杂全局 reducer
- 不破坏现有 `memory candidates`、`HITL`、`run detail retry` 等行为

建议把字符串派生收成小函数，例如：

```ts
function buildSummaryStrip(turn: StateSnapshotTurn | null, run: StateSnapshotRun | null) {
  // return { label, tone }
}

function buildInlineStatus(turn: StateSnapshotTurn | null, run: StateSnapshotRun | null) {
  // return null or { title, body, tone }
}
```

- [ ] **Step 3: 跑 App 测试并补回归验证**

Run: `pnpm --dir web/app test -- App.test.tsx`
Expected: PASS

然后跑：

Run: `pnpm --dir web/app test`
Expected: PASS

如果有时间，再跑：

Run: `pnpm --dir web/app build`
Expected: PASS

- [ ] **Step 4: 更新 AGENTS 与可选联调说明**

更新 `AGENTS.md`，把“Web 默认界面当前开始从偏工作台收敛为 Chat-first”推进到“已进入实现 / 已完成第一阶段重构”的准确状态。

如果联调步骤有新增注意点，再更新 `docs/usage/web-host-local-dev.md`，例如：

- 默认中栏应看到 summary strip
- 右栏不再是旧的大型 run inspector

- [ ] **Step 5: 提交收尾**

```bash
git add AGENTS.md docs/usage/web-host-local-dev.md web/app/src/app/App.tsx web/app/src/app/App.test.tsx
git commit -m "feat: wire app into chat-first default layout"
```

## Verification Checklist

- `pnpm --dir web/app test -- app-shell.test.tsx`
- `pnpm --dir web/app test -- conversation-pane.test.tsx chat-header.test.tsx summary-strip.test.tsx inline-status-card.test.tsx`
- `pnpm --dir web/app test -- run-inspector.test.tsx session-summary-panel.test.tsx run-details-compact-panel.test.tsx`
- `pnpm --dir web/app test -- App.test.tsx`
- `pnpm --dir web/app test`
- `pnpm --dir web/app build`

## Risks

- `App.test.tsx` 已经很大，继续堆场景会让维护成本变高；实现阶段如果测试明显继续膨胀，可以顺手抽出 `snapshot` fixture 和 `renderApp` helper
- 右栏紧凑化后，`RunInspector` 旧测试大概率会大面积失效；实现阶段应接受“默认行为变更”，不要为了保老测试反向保留重型 UI
- `ConversationPane` 去掉 `RecentTurnsStrip` 后，若产品仍想保留历史切换入口，后续应放进 `Workbench` 或折叠块，而不是重新塞回默认聊天主区
- `styles.css` 继续集中修改可能会变得难维护；如果 chat-first 样式明显扩张，实施时可以顺手按 `app-shell / conversation / context` 小段重排，但不要做与本任务无关的大规模 CSS 模块化

## Spec Self-Review

- **Spec coverage:** 已覆盖三栏职责重定义、中栏 `Header -> Thin summary strip -> Message stream -> Composer`、右栏四块、弱化旧 `Run Inspector`、保留 `HITL / Memory Candidates`、为未来 `Workbench` 留扩展位
- **Placeholder scan:** 无 `TODO / TBD / later` 占位；每个任务都有明确文件、测试命令与提交点
- **Type consistency:** 计划中新增的 `summary-strip`、`inline-status-card`、`session-summary-panel`、`run-details-compact-panel` 都以纯 props 组件定义，与现有 `App.tsx` 装配方式兼容

## Handoff

Plan complete and saved to `docs/superpowers/plans/2026-04-21-web-chat-first-layout-implementation-plan.md`. Two execution options:

**1. Subagent-Driven (recommended)** - 我分任务派发新 agent 逐步实现、每步 review 后再推进

**2. Inline Execution** - 我在当前会话里按这个计划连续执行，并在阶段点提交

Which approach?
