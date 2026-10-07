# Host Data Boundary v0

> Status: v0 working agreement
> Date: 2026-04-30
> Scope: storage / repository / memory / markdown projection boundaries for Suag Host

## 1. Purpose

This document fixes the current v0 boundary between several concepts that used
to look similar in code:

- `agentcore.Storage`
- Host session repository
- runtime/backend memory
- memory markdown projection files
- future DB connection and migration infrastructure

The goal is not to introduce a large data-layer framework. The goal is to keep
new code from mixing runtime persistence, product state, memory recall, and
derived markdown files under vague names such as `store`, `storage`, or `files`.

## 2. Current Boundaries

### 2.1 `pkg/agentcore/storage/*`

`pkg/agentcore/storage/*` contains first-party implementations of
`agentcore.Storage`.

It is responsible for runtime authority persistence:

- run snapshot
- authoritative runtime transcript
- optional runtime artifacts when supported by a backend

It must not know about Suag Host concepts:

- session
- turn
- external task
- recap
- memory candidate
- workspace/project business state

Current implementations:

- `pkg/agentcore/storage/files`
- `pkg/agentcore/storage/sqlite`

Future implementations may be added as sibling packages:

- `pkg/agentcore/storage/postgres`
- `pkg/agentcore/storage/mysql`

Do not introduce a shared SQL abstraction before a second SQL backend exists.

### 2.2 `internal/host/session.Repository`

`internal/host/session.Repository` is the Host product-state authority.

It is responsible for:

- sessions
- turns
- runs
- run events
- external tasks
- recaps
- memory candidates

It is not an `agentcore.Storage` implementation. It owns Suag Host state, not
single-runtime snapshots.

Current implementation:

- `internal/host/session.SQLiteRepository`

Constructor behavior:

- repository startup may validate required tables
- repository startup should not own schema migrations
- server startup explicitly prepares the Host DB before repository creation
- migrations are handled by app/data startup using the miglite-format SQL files

### 2.3 `internal/platform/memory`

`internal/platform/memory` is the runtime/backend memory package.

It provides richer backend memory behavior and a runtime-facing adapter for
`agentcore/api.Memory`.

It is responsible for:

- short-term message memory
- backend memory fragments
- runtime context building
- future semantic/FTS recall backend behavior

It is not the Host session repository. It is also not the markdown memory file
projection layer.

This package remains in `internal/platform` for now because its API is still
closely tied to current runtime needs. It should not be promoted to `pkg/`
until the API is stable and useful outside Suag Host.

### 2.4 `internal/host/session.MemoryProjection*`

`MemoryProjection*` types render Host learning state into local markdown files.

They are responsible for derived projection files:

- `MEMORY.draft.md`
- `USER.draft.md`
- `MEMORY.md`
- `USER.md`

They read from `session.Repository` memory candidates and write markdown views.
The markdown files are not the source of truth for Host learning state.

The source of truth remains:

- `memory_candidates` in the Host repository
- accepted/rejected/rendered candidate status

The markdown projection is consumed by the Host internal runner as a pragmatic
v0 recall input.

## 3. Current `internal/data`

`internal/data` is a thin infrastructure layer.

It owns:

- DB open/close helpers
- driver-specific connection setup
- miglite-format SQL file loading/parsing
- idempotent migration bootstrap helpers for app startup, tests, and local setup
- the repository-level convention that local CLI migrations use `miglite.yaml`

It may later own:

- DSN normalization
- migration execution through the miglite library/CLI integration
- optional transaction helpers once needed

It must not own Host business repositories.

Do not move `session.Repository` into `internal/data`. Product-state queries and
commands should stay close to the owning domain.

Current shape:

```text
internal/
  data/
    sqlite.go
    migrations.go
    testutil/

  app/
    data.go

  host/
    session/
      repository.go
      sqlite.go
      sqlite_sessions.go
      sqlite_turns.go
      sqlite_runs.go
      sqlite_events.go
      sqlite_external_tasks.go
      sqlite_learning.go
```

If a second Host DB backend is added, prefer this progression:

1. Keep the `session.Repository` interface in `internal/host/session`.
2. Add a backend-specific implementation beside or below the domain package.
3. Extract shared SQL helpers only after duplicated behavior is real.

Potential future structure:

```text
internal/host/session/
  repository.go
  sqlite/
  postgres/
  mysql/
```

Do not create this nested backend structure before a real second backend lands.

## 4. Naming Rules

Use `Storage` only for runtime persistence contracts such as
`agentcore.Storage`.

Use `Repository` for Host product-state persistence.

Use `Memory` for runtime/backend memory and recall behavior.

Use `Projection` for derived views that can be rebuilt from repository state,
including markdown memory files.

Avoid adding new packages or types named only `store`, `storage`, or `files`
unless the owner and authority boundary is obvious from the package path.

## 5. Current Next Steps

Near-term cleanup:

- Keep migrations outside the repository constructor.
- Keep app startup responsible for preparing the Host DB before constructing repositories.
- Use `miglite.yaml` for local CLI migration checks, while v0 server bootstrap can apply idempotent UP SQL for first-run startup.

Deferred:

- Moving `internal/platform/memory`.
- Adding PostgreSQL/MySQL.
- Creating a shared SQL repository framework.
