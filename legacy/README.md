# Legacy runtime references

This nested Go module preserves the former multi-agent and Host control-plane code for reading and comparison. It is outside the root module's `go list ./...`, tests, and `cmd/suag` build. Its historical imports are intentionally unchanged; this directory is not maintained as a runnable module.

The active personal Agent uses `internal/personal`, `internal/acpserver`, and the CLI/ACP app wiring. The old `agent`, `team`, `orchestrator`, `ops/cron`, outbound `host/acpbridge`, and `host/intrunner` packages, plus bridge-specific session tests, are archived here. `internal/ops/hitl` remains in the root module because the retained `internal/host/session` service package still imports it.

For the original runnable baseline, use the local `v0.1.0` tag in the Suag Git history. Existing session databases are not part of this directory.
