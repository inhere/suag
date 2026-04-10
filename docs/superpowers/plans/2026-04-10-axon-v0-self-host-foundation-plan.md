# Axon v0 Self-Host Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the first backend foundation slice for `v0.self-host`: persistent `session / turn / run / event` host state, a session runtime skeleton, and a WebSocket-first gateway path that can drive and observe agentcore-backed runs.

**Architecture:** Keep `pkg/agentcore` unchanged as the runtime kernel and add a host-side `internal/sessionruntime` package responsible for product-layer lifecycle and persistence. Reuse the existing SQLite experience in `internal/agentcorestorage`, reuse `internal/gateway` as the transport shell, and refactor the current trace-oriented event bus into session-aware event streaming with room for later AG-UI mapping and ACP integration.

**Tech Stack:** Go 1.24, `modernc.org/sqlite`, existing `internal/gateway`, existing `internal/orchestrator`, existing `pkg/agentcore`, `go test`

---

## File Structure

### New package: host state and runtime

- Create: `internal/sessionruntime/doc.go`
  Responsibility: package overview and first-stage scope notes
- Create: `internal/sessionruntime/types.go`
  Responsibility: public internal models for `Session`, `Turn`, `Run`, `RunEvent`, `ExternalTask`, `Recap`, state enums, and ID helpers
- Create: `internal/sessionruntime/store.go`
  Responsibility: store interfaces for host state persistence and read/query helpers
- Create: `internal/sessionruntime/sqlite.go`
  Responsibility: SQLite-backed implementation, schema bootstrap, CRUD operations, append-only event persistence
- Create: `internal/sessionruntime/sqlite_test.go`
  Responsibility: schema and persistence contract tests
- Create: `internal/sessionruntime/service.go`
  Responsibility: session runtime service, create/continue session-turn-run flow, runtime state transitions, event publishing
- Create: `internal/sessionruntime/service_test.go`
  Responsibility: service orchestration tests with a fake runner and fake clock

### Gateway refactor for session-aware streaming

- Modify: `internal/gateway/eventbus.go`
  Responsibility: evolve the bus from trace-oriented pub/sub into session-aware typed event pub/sub while preserving wildcard support
- Create: `internal/gateway/ws.go`
  Responsibility: WebSocket connection handler for session subscriptions and command input
- Modify: `internal/gateway/server.go`
  Responsibility: register WebSocket endpoint and new session APIs while keeping current health endpoint
- Modify: `internal/gateway/handler.go`
  Responsibility: add session-oriented handlers and keep old task endpoint working or mark it transitional
- Create: `internal/gateway/ws_test.go`
  Responsibility: connection lifecycle, subscription, and basic command handling tests

### App wiring

- Modify: `cmd/axon/main.go`
  Responsibility: wire the new session runtime store/service into server mode without breaking existing chat mode
- Modify: `internal/orchestrator/orchestrator.go`
  Responsibility: expose a narrow entry point that the new session runtime can call, or add a small adapter if needed

### Documentation

- Modify: `docs/superpowers/specs/2026-04-10-axon-v0-self-host-design.md`
  Responsibility: link the implementation plan in the “下一步建议” section after the plan lands
- Modify: `README.md`
  Responsibility: mention the new `v0.self-host` backend foundation slice at a high level

---

### Task 1: Define Host State Models and SQLite Schema

**Files:**
- Create: `internal/sessionruntime/doc.go`
- Create: `internal/sessionruntime/types.go`
- Create: `internal/sessionruntime/store.go`
- Create: `internal/sessionruntime/sqlite.go`
- Test: `internal/sessionruntime/sqlite_test.go`

- [ ] **Step 1: Write the failing schema contract tests**

```go
func TestSQLiteStore_CreateSessionTurnRunAndEvent(t *testing.T) {
    store := newTestStore(t)

    session := Session{SessionID: "sess_001", Status: SessionOpen}
    turn := Turn{TurnID: "turn_001", SessionID: session.SessionID, Status: TurnRunning}
    run := Run{RunID: "run_001", SessionID: session.SessionID, TurnID: turn.TurnID, State: RunQueued}
    event := RunEvent{EventID: "evt_001", SessionID: session.SessionID, TurnID: turn.TurnID, RunID: run.RunID, EventType: "run.queued", Sequence: 1}

    require.NoError(t, store.CreateSession(ctx, session))
    require.NoError(t, store.CreateTurn(ctx, turn))
    require.NoError(t, store.CreateRun(ctx, run))
    require.NoError(t, store.AppendEvent(ctx, event))

    gotSession, err := store.GetSession(ctx, session.SessionID)
    require.NoError(t, err)
    require.Equal(t, SessionOpen, gotSession.Status)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sessionruntime -run TestSQLiteStore_CreateSessionTurnRunAndEvent -v`
Expected: FAIL with missing package or missing store/type errors

- [ ] **Step 3: Define the core host-side models and enums**

```go
type SessionStatus string
type TurnStatus string
type RunState string

const (
    SessionOpen     SessionStatus = "OPEN"
    SessionRunning  SessionStatus = "RUNNING"
    SessionWaiting  SessionStatus = "WAITING"
    SessionArchived SessionStatus = "ARCHIVED"
)

type Session struct {
    SessionID  string
    Title      string
    Status     SessionStatus
    LastTurnID string
    CreatedAt  time.Time
    UpdatedAt  time.Time
    Metadata   map[string]any
}
```

- [ ] **Step 4: Implement store interfaces with explicit product-layer boundaries**

```go
type Store interface {
    CreateSession(ctx context.Context, s Session) error
    GetSession(ctx context.Context, sessionID string) (Session, error)
    CreateTurn(ctx context.Context, t Turn) error
    CreateRun(ctx context.Context, r Run) error
    UpdateRunState(ctx context.Context, runID string, state RunState, endedAt *time.Time) error
    AppendEvent(ctx context.Context, e RunEvent) error
    ListEventsBySession(ctx context.Context, sessionID string, afterSequence int64) ([]RunEvent, error)
}
```

- [ ] **Step 5: Implement SQLite schema bootstrap and CRUD methods**

```sql
CREATE TABLE IF NOT EXISTS sessions (
    session_id   TEXT PRIMARY KEY,
    title        TEXT NOT NULL,
    status       TEXT NOT NULL,
    last_turn_id TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    metadata_json BLOB NOT NULL
);

CREATE TABLE IF NOT EXISTS turns (
    turn_id        TEXT PRIMARY KEY,
    session_id     TEXT NOT NULL,
    initiator_type TEXT NOT NULL,
    status         TEXT NOT NULL,
    primary_run_id TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL,
    completed_at   TEXT NOT NULL DEFAULT '',
    metadata_json  BLOB NOT NULL
);

CREATE TABLE IF NOT EXISTS runs (
    run_id           TEXT PRIMARY KEY,
    session_id       TEXT NOT NULL,
    turn_id          TEXT NOT NULL,
    agent_id         TEXT NOT NULL,
    parent_run_id    TEXT NOT NULL DEFAULT '',
    state            TEXT NOT NULL,
    resume_from_run_id TEXT NOT NULL DEFAULT '',
    started_at       TEXT NOT NULL,
    ended_at         TEXT NOT NULL DEFAULT '',
    metadata_json    BLOB NOT NULL
);

CREATE TABLE IF NOT EXISTS run_events (
    event_id      TEXT PRIMARY KEY,
    session_id    TEXT NOT NULL,
    turn_id       TEXT NOT NULL,
    run_id        TEXT NOT NULL,
    agent_id      TEXT NOT NULL,
    event_type    TEXT NOT NULL,
    sequence      INTEGER NOT NULL,
    payload_json  BLOB NOT NULL,
    source        TEXT NOT NULL,
    created_at    TEXT NOT NULL
);
```

- [ ] **Step 6: Run the package tests**

Run: `go test ./internal/sessionruntime/...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/sessionruntime
git commit -m "feat: add self-host session runtime store foundation"
```

---

### Task 2: Implement Session Runtime Service Skeleton

**Files:**
- Create: `internal/sessionruntime/service.go`
- Test: `internal/sessionruntime/service_test.go`
- Modify: `internal/orchestrator/orchestrator.go`

- [ ] **Step 1: Write the failing service orchestration test**

```go
func TestService_StartTurnCreatesQueuedRunAndPublishesEvent(t *testing.T) {
    store := newTestStore(t)
    bus := newFakePublisher()
    runner := newFakeRunner("run-output", nil)
    svc := NewService(store, bus, runner, Config{})

    result, err := svc.StartTurn(ctx, StartTurnInput{
        SessionID:    "sess_001",
        AgentID:      "default",
        UserMessage:  "implement feature x",
        InitiatorType: InitiatorUser,
    })

    require.NoError(t, err)
    require.Equal(t, "sess_001", result.SessionID)
    require.NotEmpty(t, result.TurnID)
    require.NotEmpty(t, result.RunID)
    require.Contains(t, bus.EventTypes(), "run.queued")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sessionruntime -run TestService_StartTurnCreatesQueuedRunAndPublishesEvent -v`
Expected: FAIL with missing service types or methods

- [ ] **Step 3: Implement service inputs, outputs, and narrow runtime dependency**

```go
type Runner interface {
    Run(ctx context.Context, in RuntimeRequest) (RuntimeResult, error)
}

type Service struct {
    store  Store
    pub    Publisher
    runner Runner
    now    func() time.Time
}
```

- [ ] **Step 4: Implement `StartTurn` with explicit lifecycle transitions**

```go
func (s *Service) StartTurn(ctx context.Context, in StartTurnInput) (StartTurnResult, error) {
    // 1. ensure session exists
    // 2. create turn in RUNNING
    // 3. create run in QUEUED
    // 4. append run.queued event
    // 5. execute runner
    // 6. update run/session/turn state based on result
    // 7. append final state event
}
```

- [ ] **Step 5: Add a narrow orchestrator adapter instead of leaking product-layer logic into `pkg/agentcore`**

```go
type RuntimeRequest struct {
    SessionID   string
    TurnID      string
    RunID       string
    AgentID     string
    Instruction string
}
```

- [ ] **Step 6: Run the service tests**

Run: `go test ./internal/sessionruntime/... ./internal/orchestrator/...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/sessionruntime internal/orchestrator/orchestrator.go
git commit -m "feat: add self-host session runtime service skeleton"
```

---

### Task 3: Refactor Event Bus for Session-Aware Streaming

**Files:**
- Modify: `internal/gateway/eventbus.go`
- Test: `internal/gateway/ws_test.go`

- [ ] **Step 1: Write the failing bus behavior tests**

```go
func TestEventBus_SubscribeSessionReceivesMatchingEventsOnly(t *testing.T) {
    bus := NewEventBus()
    ch := bus.Subscribe(Subscription{SessionID: "sess_001"})
    defer bus.Unsubscribe(ch)

    bus.Publish(Event{SessionID: "sess_002", Type: "run.queued"})
    bus.Publish(Event{SessionID: "sess_001", Type: "run.started"})

    ev := <-ch
    require.Equal(t, "run.started", ev.Type)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gateway -run TestEventBus_SubscribeSessionReceivesMatchingEventsOnly -v`
Expected: FAIL because current bus only keys on `TraceID`

- [ ] **Step 3: Introduce session-aware event identity while keeping wildcard subscribers**

```go
type Event struct {
    Type      string `json:"type"`
    SessionID string `json:"session_id,omitempty"`
    TurnID    string `json:"turn_id,omitempty"`
    RunID     string `json:"run_id,omitempty"`
    AgentID   string `json:"agent_id,omitempty"`
    Sequence  int64  `json:"sequence,omitempty"`
    Payload   any    `json:"payload,omitempty"`
}
```

- [ ] **Step 4: Implement typed subscriptions**

```go
type Subscription struct {
    SessionID string
    Wildcard  bool
}
```

- [ ] **Step 5: Verify bus tests and existing gateway tests**

Run: `go test ./internal/gateway/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/gateway/eventbus.go internal/gateway/ws_test.go
git commit -m "refactor: make gateway event bus session-aware"
```

---

### Task 4: Add WebSocket Session Endpoint and Session APIs

**Files:**
- Create: `internal/gateway/ws.go`
- Modify: `internal/gateway/server.go`
- Modify: `internal/gateway/handler.go`
- Test: `internal/gateway/ws_test.go`
- Modify: `cmd/axon/main.go`

- [ ] **Step 1: Write failing gateway tests for session creation and WebSocket subscription**

```go
func TestServer_CreateSessionAndStreamEventsOverWebSocket(t *testing.T) {
    // start httptest server
    // POST /api/sessions
    // connect /ws?session_id=<id>
    // publish a session event
    // assert the websocket client receives JSON event payload
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gateway -run TestServer_CreateSessionAndStreamEventsOverWebSocket -v`
Expected: FAIL with missing routes or missing websocket handler

- [ ] **Step 3: Add transitional REST endpoints for the new host model**

```go
POST /api/sessions
POST /api/sessions/{id}/turns
GET  /api/sessions/{id}
GET  /api/sessions/{id}/events
GET  /ws?session_id=<id>
```

- [ ] **Step 4: Implement a minimal WebSocket protocol**

```json
{ "type": "subscribe", "session_id": "sess_001" }
{ "type": "continue_turn", "session_id": "sess_001", "message": "please continue" }
```

- [ ] **Step 5: Wire the new service into server mode**

```go
sessionStore, err := sessionruntime.NewSQLiteStore(...)
sessionSvc := sessionruntime.NewService(sessionStore, bus, runtimeAdapter, cfg)
handler := gateway.NewHandler(a.orch, sessionSvc, bus, a.hitlMgr)
```

- [ ] **Step 6: Run gateway and server tests**

Run: `go test ./internal/gateway/... ./cmd/axon/...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/gateway cmd/axon/main.go
git commit -m "feat: add websocket session gateway foundation"
```

---

### Task 5: Add Documentation and Verification Notes

**Files:**
- Modify: `docs/superpowers/specs/2026-04-10-axon-v0-self-host-design.md`
- Modify: `README.md`

- [ ] **Step 1: Update the design spec with the plan link and scope note**

```md
后续实施计划见：
`docs/superpowers/plans/2026-04-10-axon-v0-self-host-foundation-plan.md`
```

- [ ] **Step 2: Add a short README note for the new foundation slice**

```md
- `internal/sessionruntime` provides the first host-side runtime foundation for `v0.self-host`
```

- [ ] **Step 3: Run the focused verification suite**

Run: `go test ./internal/sessionruntime/... ./internal/gateway/... ./internal/orchestrator/...`
Expected: PASS

- [ ] **Step 4: Run a full repository smoke test**

Run: `go test ./...`
Expected: PASS, or capture known unrelated failures before landing

- [ ] **Step 5: Commit**

```bash
git add README.md docs/superpowers/specs/2026-04-10-axon-v0-self-host-design.md
git commit -m "docs: document self-host foundation plan"
```

---

## Notes for Execution

- Do not move ACP bridge implementation into `pkg/agentcore`; keep it for a follow-up plan on top of this foundation.
- Keep `turnId` as a first-class field from day one. A single turn can fan out into multiple agent runs later.
- Treat `run_events` as append-only. Corrections should be represented by later events, not in-place mutation.
- Prefer SQLite for host state now, but keep interfaces narrow so `state.db` and future memory retrieval can evolve independently.
- Keep WebSocket transport and AG-UI event mapping separated. This plan only lays the session-aware event foundation; a later plan should define exact AG-UI mapping.

## Suggested Follow-Up Plans

After this plan lands, the next plan should cover:

1. `ACP bridge` contract and adapter implementation
2. `AG-UI` event mapping and A2UI payload envelope
3. minimal Web client session timeline UI
