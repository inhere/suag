# Axon Web Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the first Axon Web foundation slice: a React + Vite single-page workbench that can create and switch sessions, render `STATE_SNAPSHOT v0`, stream updates over WebSocket, send new turns, and continue existing turns.

**Architecture:** Start from an empty `web/app` and build a thin frontend around the current Host APIs instead of inventing new frontend-side state semantics. Keep the UI as a three-column workbench: left `Session List`, middle `Conversation`, right `Run Inspector`, with data flowing from HTTP bootstrap + `STATE_SNAPSHOT v0` + incremental WS events.

**Tech Stack:** React, Vite, TypeScript, Vitest, Testing Library, `github.com/coder/websocket`-compatible browser WebSocket, existing Host HTTP/WS APIs, `docs/usage/host-state-snapshot-v0.md`

---

## File Structure

### Frontend scaffold

- Create: `web/app/package.json`
  Responsibility: app scripts and frontend dependencies
- Create: `web/app/tsconfig.json`
  Responsibility: TypeScript compiler settings for app and tests
- Create: `web/app/tsconfig.node.json`
  Responsibility: Vite/node-side TypeScript config
- Create: `web/app/vite.config.ts`
  Responsibility: Vite dev/build/test config
- Create: `web/app/index.html`
  Responsibility: app entry HTML
- Create: `web/app/src/main.tsx`
  Responsibility: bootstraps the React app
- Create: `web/app/src/styles.css`
  Responsibility: base layout tokens and app-wide styling

### App shell and layout

- Create: `web/app/src/app/App.tsx`
  Responsibility: top-level app shell wiring
- Create: `web/app/src/app/app-shell.tsx`
  Responsibility: three-column layout and global view composition
- Create: `web/app/src/app/app-shell.test.tsx`
  Responsibility: layout-level smoke tests

### Shared types and API client

- Create: `web/app/src/lib/host-types.ts`
  Responsibility: `session`, `turn`, `run`, `STATE_SNAPSHOT v0`, and event type definitions
- Create: `web/app/src/lib/session-client.ts`
  Responsibility: HTTP client for sessions and turns
- Create: `web/app/src/lib/session-client.test.ts`
  Responsibility: REST client contract tests
- Create: `web/app/src/lib/session-ws.ts`
  Responsibility: WebSocket subscription lifecycle and message parsing
- Create: `web/app/src/lib/session-ws.test.ts`
  Responsibility: WS client unit tests with mocked browser WebSocket

### Session state and selectors

- Create: `web/app/src/features/session/session-store.ts`
  Responsibility: frontend state container for current session, snapshot, focus run, filters, and loading states
- Create: `web/app/src/features/session/session-store.test.ts`
  Responsibility: snapshot hydration and focus-selection tests
- Create: `web/app/src/features/session/session-selectors.ts`
  Responsibility: pure selectors for current turn, active runs, focused run, session previews
- Create: `web/app/src/features/session/session-selectors.test.ts`
  Responsibility: selector contract tests against `STATE_SNAPSHOT v0`

### Session list UI

- Create: `web/app/src/features/session-list/session-list.tsx`
  Responsibility: left-column session list container
- Create: `web/app/src/features/session-list/session-list-item.tsx`
  Responsibility: compact session list item rendering
- Create: `web/app/src/features/session-list/session-list.test.tsx`
  Responsibility: list render, filter, and selection tests

### Conversation UI

- Create: `web/app/src/features/conversation/conversation-pane.tsx`
  Responsibility: middle-column message flow and turn summary rendering
- Create: `web/app/src/features/conversation/message-item.tsx`
  Responsibility: user / assistant message block rendering
- Create: `web/app/src/features/conversation/turn-summary-card.tsx`
  Responsibility: thin turn summary card component
- Create: `web/app/src/features/conversation/composer.tsx`
  Responsibility: new turn / continue input composer
- Create: `web/app/src/features/conversation/conversation-pane.test.tsx`
  Responsibility: conversation rendering and composer intent tests

### Run inspector UI

- Create: `web/app/src/features/run-inspector/run-inspector.tsx`
  Responsibility: right-column inspector container
- Create: `web/app/src/features/run-inspector/current-turn-panel.tsx`
  Responsibility: current turn summary block
- Create: `web/app/src/features/run-inspector/active-runs-panel.tsx`
  Responsibility: active run cards list
- Create: `web/app/src/features/run-inspector/focused-run-panel.tsx`
  Responsibility: focused run detail block with `summary`-first rendering
- Create: `web/app/src/features/run-inspector/external-task-panel.tsx`
  Responsibility: external task summary and action buttons
- Create: `web/app/src/features/run-inspector/run-inspector.test.tsx`
  Responsibility: inspector rendering and focus behavior tests

### Debug surface

- Create: `web/app/src/features/debug/debug-drawer.tsx`
  Responsibility: optional events/external detail drawer
- Create: `web/app/src/features/debug/debug-drawer.test.tsx`
  Responsibility: drawer visibility and raw event rendering tests

### Documentation

- Modify: `docs/README.md`
  Responsibility: point readers to the new frontend foundation plan/spec pair
- Modify: `docs/superpowers/specs/2026-04-12-axon-web-foundation-design.md`
  Responsibility: link the implementation plan after it lands

---

### Task 1: Scaffold the React + Vite App

**Files:**
- Create: `web/app/package.json`
- Create: `web/app/tsconfig.json`
- Create: `web/app/tsconfig.node.json`
- Create: `web/app/vite.config.ts`
- Create: `web/app/index.html`
- Create: `web/app/src/main.tsx`
- Create: `web/app/src/styles.css`
- Test: `web/app/src/app/app-shell.test.tsx`

- [ ] **Step 1: Write the failing app-shell smoke test**

```tsx
import { render, screen } from "@testing-library/react";
import { AppShell } from "./app-shell";

it("renders the three-column workbench shell", () => {
  render(<AppShell />);

  expect(screen.getByText("Sessions")).toBeInTheDocument();
  expect(screen.getByText("Conversation")).toBeInTheDocument();
  expect(screen.getByText("Run Inspector")).toBeInTheDocument();
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web/app && npm test -- app-shell.test.tsx`
Expected: FAIL with missing package or missing component/module errors

- [ ] **Step 3: Create the Vite + React + TypeScript scaffold**

```json
{
  "name": "axon-web",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "test": "vitest run",
    "test:watch": "vitest"
  }
}
```

- [ ] **Step 4: Add the minimal app shell and base styles**

```tsx
export function AppShell() {
  return (
    <div className="app-shell">
      <aside>Sessions</aside>
      <main>Conversation</main>
      <section>Run Inspector</section>
    </div>
  );
}
```

- [ ] **Step 5: Run the focused test to verify it passes**

Run: `cd web/app && npm test -- app-shell.test.tsx`
Expected: PASS

- [ ] **Step 6: Run the frontend build**

Run: `cd web/app && npm run build`
Expected: PASS and `web/app/dist` generated

- [ ] **Step 7: Commit**

```bash
git add web/app
git commit -m "feat: scaffold axon web foundation app"
```

---

### Task 2: Define Host Snapshot Types and REST / WS Clients

**Files:**
- Create: `web/app/src/lib/host-types.ts`
- Create: `web/app/src/lib/session-client.ts`
- Create: `web/app/src/lib/session-client.test.ts`
- Create: `web/app/src/lib/session-ws.ts`
- Create: `web/app/src/lib/session-ws.test.ts`

- [ ] **Step 1: Write the failing REST client test**

```ts
import { describe, expect, it, vi } from "vitest";
import { createSession } from "./session-client";

describe("createSession", () => {
  it("posts to /api/sessions and returns parsed session data", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ session_id: "sess_001", title: "demo" })
    }));

    const result = await createSession({ title: "demo" });

    expect(result.sessionId).toBe("sess_001");
  });
});
```

- [ ] **Step 2: Write the failing WebSocket client test**

```ts
it("subscribes to a session websocket and forwards parsed messages", () => {
  const onMessage = vi.fn();
  const ws = new FakeWebSocket();

  connectSessionSocket({
    sessionId: "sess_001",
    createWebSocket: () => ws as unknown as WebSocket,
    onMessage
  });

  ws.emitMessage(JSON.stringify({ type: "STATE_SNAPSHOT", snapshot: {} }));

  expect(onMessage).toHaveBeenCalledWith(
    expect.objectContaining({ type: "STATE_SNAPSHOT" })
  );
});
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd web/app && npm test -- session-client.test.ts session-ws.test.ts`
Expected: FAIL with missing modules or missing exported functions

- [ ] **Step 4: Define exact frontend types from `STATE_SNAPSHOT v0`**

```ts
export type StateSnapshotV0 = {
  snapshotVersion: "v0";
  snapshotSchema: "axon.host.state.v0";
  generatedAt: string;
  sessionId: string;
  turns: TurnSnapshot[];
  runs: RunSnapshot[];
  externalTasks: ExternalTaskSnapshot[];
  externalMessages: ExternalMessageSnapshot[];
  events: HostEvent[];
};
```

- [ ] **Step 5: Implement the thin REST client**

```ts
export async function createSession(input: { title: string }) {
  const response = await fetch("/api/sessions", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(input),
  });
  return mapSessionResponse(await response.json());
}
```

- [ ] **Step 6: Implement the thin WS client**

```ts
export function connectSessionSocket(opts: ConnectSessionSocketOptions) {
  const ws = opts.createWebSocket?.() ?? new WebSocket(`/ws?session_id=${opts.sessionId}`);
  ws.onmessage = (event) => opts.onMessage(JSON.parse(event.data));
  return () => ws.close();
}
```

- [ ] **Step 7: Run the focused tests**

Run: `cd web/app && npm test -- session-client.test.ts session-ws.test.ts`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add web/app/src/lib
git commit -m "feat: add host session api clients"
```

---

### Task 3: Build Session Store and Snapshot Selectors

**Files:**
- Create: `web/app/src/features/session/session-store.ts`
- Create: `web/app/src/features/session/session-store.test.ts`
- Create: `web/app/src/features/session/session-selectors.ts`
- Create: `web/app/src/features/session/session-selectors.test.ts`

- [ ] **Step 1: Write the failing selector tests**

```ts
it("selects the current active run from the snapshot", () => {
  const snapshot = makeSnapshot({
    runs: [
      { runId: "run_001", displayState: "completed" },
      { runId: "run_002", displayState: "external_running" }
    ],
    currentRunId: "run_002"
  });

  expect(selectFocusedRun(snapshot, null)?.runId).toBe("run_002");
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web/app && npm test -- session-selectors.test.ts session-store.test.ts`
Expected: FAIL with missing selectors or missing store

- [ ] **Step 3: Implement a thin session store**

```ts
export type SessionViewState = {
  sessions: SessionListItem[];
  currentSessionId: string | null;
  snapshot: StateSnapshotV0 | null;
  focusedRunId: string | null;
  filter: "all" | "running" | "waiting";
};
```

- [ ] **Step 4: Implement pure selectors instead of UI-side ad-hoc logic**

```ts
export function selectFocusedRun(snapshot: StateSnapshotV0 | null, focusedRunId: string | null) {
  if (!snapshot) return null;
  if (focusedRunId) return snapshot.runs.find((run) => run.runId === focusedRunId) ?? null;
  return snapshot.runs.find((run) => run.runId === snapshot.currentRunId)
    ?? snapshot.runs.find((run) => run.displayState === "external_running" || run.displayState === "running")
    ?? snapshot.runs[0]
    ?? null;
}
```

- [ ] **Step 5: Add selectors for current turn, active runs, and session preview text**

```ts
export function selectCurrentTurn(snapshot: StateSnapshotV0 | null) { /* ... */ }
export function selectActiveRuns(snapshot: StateSnapshotV0 | null) { /* ... */ }
export function selectSessionPreview(snapshot: StateSnapshotV0 | null) { /* ... */ }
```

- [ ] **Step 6: Run the focused tests**

Run: `cd web/app && npm test -- session-selectors.test.ts session-store.test.ts`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add web/app/src/features/session
git commit -m "feat: add session snapshot view state"
```

---

### Task 4: Implement the Left Session List

**Files:**
- Create: `web/app/src/features/session-list/session-list.tsx`
- Create: `web/app/src/features/session-list/session-list-item.tsx`
- Create: `web/app/src/features/session-list/session-list.test.tsx`
- Modify: `web/app/src/app/app-shell.tsx`

- [ ] **Step 1: Write the failing session list render test**

```tsx
it("renders compact session items with title, status, and preview", () => {
  render(
    <SessionList
      sessions={[
        { sessionId: "sess_001", title: "host runtime", status: "RUNNING", preview: "正在整理页面结构", updatedAt: "2m" }
      ]}
      currentSessionId="sess_001"
      onSelect={() => {}}
    />
  );

  expect(screen.getByText("host runtime")).toBeInTheDocument();
  expect(screen.getByText(/正在整理页面结构/)).toBeInTheDocument();
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web/app && npm test -- session-list.test.tsx`
Expected: FAIL with missing component errors

- [ ] **Step 3: Implement the compact session list UI**

```tsx
export function SessionListItem({ item, selected, onSelect }: Props) {
  return (
    <button className={selected ? "session-item selected" : "session-item"} onClick={onSelect}>
      <div className="session-item-title">{item.title}</div>
      <div className="session-item-meta">{item.statusLabel} · {item.updatedLabel}</div>
      <div className="session-item-preview">{item.preview}</div>
    </button>
  );
}
```

- [ ] **Step 4: Add search box and status filter controls**

```tsx
<input placeholder="Search sessions" />
<button>All</button>
<button>Running</button>
<button>Waiting</button>
```

- [ ] **Step 5: Wire the left column into `AppShell`**

```tsx
<aside className="left-column">
  <SessionList {...sessionListProps} />
</aside>
```

- [ ] **Step 6: Run the focused tests**

Run: `cd web/app && npm test -- session-list.test.tsx app-shell.test.tsx`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add web/app/src/features/session-list web/app/src/app/app-shell.tsx
git commit -m "feat: add compact session list"
```

---

### Task 5: Implement the Conversation Pane and Composer

**Files:**
- Create: `web/app/src/features/conversation/conversation-pane.tsx`
- Create: `web/app/src/features/conversation/message-item.tsx`
- Create: `web/app/src/features/conversation/turn-summary-card.tsx`
- Create: `web/app/src/features/conversation/composer.tsx`
- Create: `web/app/src/features/conversation/conversation-pane.test.tsx`
- Modify: `web/app/src/app/app-shell.tsx`

- [ ] **Step 1: Write the failing conversation test**

```tsx
it("renders user messages, assistant summaries, and turn summary cards", () => {
  render(<ConversationPane messages={makeMessages()} currentTurn={makeTurn()} onSubmit={vi.fn()} />);

  expect(screen.getByText("Turn turn_012")).toBeInTheDocument();
  expect(screen.getByText(/external_running/)).toBeInTheDocument();
  expect(screen.getByRole("textbox")).toBeInTheDocument();
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web/app && npm test -- conversation-pane.test.tsx`
Expected: FAIL with missing component errors

- [ ] **Step 3: Implement the message list with only the allowed block types**

```tsx
type ConversationBlock =
  | { kind: "user"; text: string }
  | { kind: "assistant"; text: string }
  | { kind: "turn-summary"; turn: TurnSnapshot };
```

- [ ] **Step 4: Implement the thin turn summary card**

```tsx
export function TurnSummaryCard({ turn }: { turn: TurnSnapshot }) {
  return (
    <section>
      <strong>{`Turn ${turn.turnId}`}</strong>
      <p>{`${turn.displayState} · ${turn.activeRunCount} active runs`}</p>
      <p>{turn.latestMessagePreview}</p>
    </section>
  );
}
```

- [ ] **Step 5: Implement the composer with new-turn and continue intent**

```tsx
export function Composer({ waitingForInput, onSubmit }: Props) {
  return (
    <form onSubmit={/* ... */}>
      <textarea placeholder={waitingForInput ? "当前任务在等待你的补充说明..." : "给 Axon 发送下一条任务说明..."} />
      <button type="submit">{waitingForInput ? "Continue" : "Send"}</button>
    </form>
  );
}
```

- [ ] **Step 6: Wire the conversation pane into the middle column**

```tsx
<main className="center-column">
  <ConversationPane {...conversationProps} />
</main>
```

- [ ] **Step 7: Run the focused tests**

Run: `cd web/app && npm test -- conversation-pane.test.tsx app-shell.test.tsx`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add web/app/src/features/conversation web/app/src/app/app-shell.tsx
git commit -m "feat: add conversation workbench pane"
```

---

### Task 6: Implement the Right Run Inspector

**Files:**
- Create: `web/app/src/features/run-inspector/run-inspector.tsx`
- Create: `web/app/src/features/run-inspector/current-turn-panel.tsx`
- Create: `web/app/src/features/run-inspector/active-runs-panel.tsx`
- Create: `web/app/src/features/run-inspector/focused-run-panel.tsx`
- Create: `web/app/src/features/run-inspector/external-task-panel.tsx`
- Create: `web/app/src/features/run-inspector/run-inspector.test.tsx`
- Modify: `web/app/src/app/app-shell.tsx`

- [ ] **Step 1: Write the failing inspector test**

```tsx
it("shows summary first for a completed focused run", () => {
  render(
    <RunInspector
      currentTurn={makeTurn()}
      runs={[makeRun({ summary: "已完成基础方案", latestMessageText: "older text" })]}
      externalTasks={[]}
      focusedRunId="run_001"
      onFocusRun={vi.fn()}
    />
  );

  expect(screen.getByText("已完成基础方案")).toBeInTheDocument();
  expect(screen.queryByText("older text")).not.toBeInTheDocument();
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web/app && npm test -- run-inspector.test.tsx`
Expected: FAIL with missing component errors

- [ ] **Step 3: Implement the four fixed inspector regions**

```tsx
<CurrentTurnPanel />
<ActiveRunsPanel />
<FocusedRunPanel />
<ExternalTaskPanel />
```

- [ ] **Step 4: Keep the focused run rendering rule strict**

```tsx
const content = focusedRun.summary || focusedRun.latestMessageText || "Waiting for output...";
```

- [ ] **Step 5: Add run focus switching and external task rendering**

```tsx
<button onClick={() => onFocusRun(run.runId)}>{run.agentId}</button>
```

- [ ] **Step 6: Wire the inspector into the right column**

```tsx
<section className="right-column">
  <RunInspector {...inspectorProps} />
</section>
```

- [ ] **Step 7: Run the focused tests**

Run: `cd web/app && npm test -- run-inspector.test.tsx app-shell.test.tsx`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add web/app/src/features/run-inspector web/app/src/app/app-shell.tsx
git commit -m "feat: add run inspector workbench column"
```

---

### Task 7: Wire HTTP Bootstrap, WS Snapshot Hydration, and Incremental Updates

**Files:**
- Modify: `web/app/src/app/App.tsx`
- Modify: `web/app/src/features/session/session-store.ts`
- Modify: `web/app/src/lib/session-client.ts`
- Modify: `web/app/src/lib/session-ws.ts`
- Test: `web/app/src/features/session/session-store.test.ts`
- Test: `web/app/src/app/app-shell.test.tsx`

- [ ] **Step 1: Write the failing hydration test**

```ts
it("hydrates the workbench from STATE_SNAPSHOT v0 and keeps current active run focused", () => {
  const state = reduceSessionMessage(emptyState, {
    type: "STATE_SNAPSHOT",
    snapshot: makeSnapshot({ currentRunId: "run_002" })
  });

  expect(state.snapshot?.currentRunId).toBe("run_002");
  expect(state.focusedRunId).toBe("run_002");
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web/app && npm test -- session-store.test.ts`
Expected: FAIL with missing reducer or wrong focus behavior

- [ ] **Step 3: Add initial load flow**

```tsx
useEffect(() => {
  void loadSessions();
}, []);
```

- [ ] **Step 4: Load session details and connect the session WebSocket**

```tsx
useEffect(() => {
  if (!currentSessionId) return;
  void loadSession(currentSessionId);
  return connectSessionSocket({ sessionId: currentSessionId, onMessage: handleSocketMessage });
}, [currentSessionId]);
```

- [ ] **Step 5: Reduce `STATE_SNAPSHOT` and incrementals into the same store**

```ts
switch (message.type) {
  case "STATE_SNAPSHOT":
    return hydrateFromSnapshot(state, message.snapshot);
  default:
    return reduceIncrementalEvent(state, message);
}
```

- [ ] **Step 6: Run integration-level frontend tests**

Run: `cd web/app && npm test -- session-store.test.ts app-shell.test.tsx`
Expected: PASS

- [ ] **Step 7: Run the frontend build**

Run: `cd web/app && npm run build`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add web/app/src/app web/app/src/features/session web/app/src/lib
git commit -m "feat: wire session snapshot hydration and websocket updates"
```

---

### Task 8: Add Debug Drawer and Minimum Documentation Links

**Files:**
- Create: `web/app/src/features/debug/debug-drawer.tsx`
- Create: `web/app/src/features/debug/debug-drawer.test.tsx`
- Modify: `docs/README.md`
- Modify: `docs/superpowers/specs/2026-04-12-axon-web-foundation-design.md`

- [ ] **Step 1: Write the failing debug drawer test**

```tsx
it("renders raw events only when the drawer is open", () => {
  render(<DebugDrawer open events={[{ type: "run.started" } as any]} onClose={vi.fn()} />);
  expect(screen.getByText("run.started")).toBeInTheDocument();
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web/app && npm test -- debug-drawer.test.tsx`
Expected: FAIL with missing component errors

- [ ] **Step 3: Implement the drawer as the only raw event surface**

```tsx
export function DebugDrawer({ open, events, onClose }: Props) {
  if (!open) return null;
  return <aside>{events.map((event) => <pre key={event.sequence}>{event.type}</pre>)}</aside>;
}
```

- [ ] **Step 4: Link the design doc to this plan**

```md
实现计划见：
`docs/superpowers/plans/2026-04-12-axon-web-foundation-implementation-plan.md`
```

- [ ] **Step 5: Link the plan from `docs/README.md` usage/current-doc guidance**

```md
- `docs/superpowers/plans/2026-04-12-axon-web-foundation-implementation-plan.md`
  - Web 基础工作台第一阶段实现计划
```

- [ ] **Step 6: Run focused frontend tests and build**

Run: `cd web/app && npm test -- debug-drawer.test.tsx`
Expected: PASS

Run: `cd web/app && npm run build`
Expected: PASS

- [ ] **Step 7: Run the repository verification**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add web/app/src/features/debug docs/README.md docs/superpowers/specs/2026-04-12-axon-web-foundation-design.md
git commit -m "docs: link web foundation implementation plan"
```

---

## Notes for Execution

- Keep the frontend state model thin. Prefer selectors over derived state scattered across components.
- Do not build frontend-side project/workspace modeling beyond the current `workspace` top context.
- Keep the conversation pane human-readable. Raw events belong in the debug drawer only.
- Keep `summary`-first behavior consistent in `turns`, `runs`, and any focused-run detail surface.
- `tools`, `skills`, and `mcp` should remain secondary surfaces in this phase. Do not create standalone product pages for them yet.

## Suggested Follow-Up Plans

After this plan lands, the next plan should cover one of:

1. Visual design pass and responsive polish for the workbench
2. Frontend support for richer tool / skill / MCP capability surfaces
3. End-to-end browser QA against a running Host backend
