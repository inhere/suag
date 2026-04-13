# 开发进度记录

> 状态：Legacy reference
> 说明：本文档记录的是旧 `v1` 路线下的开发进度，当前仅作为历史参考。新的 `v0` 主线请先看 `docs/README.md`。

> 项目开始时间：2026-03-14 22:00

- 原始需求文档 [axon-prd.md](/docs/axon-prd.md)

## Phase 1 MVP Complete ✓

> 完成时间：2026-03-16 9:30

Build & tests: `go build ./... and go test ./...` both pass cleanly (pure-Go, no CGO required).

What was implemented

```txt
D:/work/aidev/axon/
├── cmd/axon/main.go          # CLI: chat mode + HTTP server mode
├── internal/
│   ├── agent/                  # Agent struct, ReAct runner, state machine, SOUL.md parser
│   ├── llm/                    # OpenAI, Claude (Anthropic), DeepSeek, Pool w/ failover
│   ├── memory/                 # InMemStore (sliding window + summarize), SQLiteStore
│   ├── tools/
│   │   ├── registry.go         # Unified tool registry
│   │   ├── native/             # file_read, file_write, file_list, shell_exec, web_search
│   │   ├── mcp/                # JSON-RPC 2.0 stdio MCP client
│   │   └── skills/             # SKILL.md parser + shell/python runner
│   ├── tracing/                # TraceID/SpanID, in-memory tracer, SSE-notifiable
│   ├── hitl/                   # Channel-based suspend/resume, ticket lifecycle
│   ├── team/                   # DAG-based multi-agent team execution
│   ├── orchestrator/           # Top-level task router (single agent or team)
│   ├── cron/                   # Agentic cron: sense → decide → act loop
│   └── gateway/                # chi HTTP server, SSE stream, REST API, EventBus
├── config/
│   ├── config.yaml             # Viper config template
│   └── souls/                  # architect.md, coder.md, reviewer.md
└── Makefile
```

Quick start

```bash
# Set your LLM key
export OPENAI_API_KEY=sk-...
# or ANTHROPIC_API_KEY / DEEPSEEK_API_KEY + set llm.provider in config.yaml

# Chat mode (Phase 1 entry point)
go run ./cmd/axon chat

# HTTP server mode (Phase 4+ UI)
go run ./cmd/axon server   # listens on :8080
# GET  /events?trace_id=*    → SSE stream
# POST /api/tasks            → {"description": "..."}
# GET  /api/hitl             → pending HITL tickets
# POST /api/hitl/{id}/respond
```

Architecture decisions

- Pure-Go SQLite (modernc.org/sqlite) — no CGO, cross-platform binary
- Interface-driven — swap LLM/Memory/VectorStore without changing agent logic
- HITL via channels — agents block on chan *Response; operators unblock via REST or CLI
- Tracing via context propagation — TraceID/SpanID thread through every call; pushed to SSE on span completion
