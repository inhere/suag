# Suag

Suag is a personal Go agent with one persisted conversation engine and two interfaces: a terminal CLI and an ACP stdio server. Gofer can launch either interface and owns jobs, workers, logs, and interactive approval.

## Build and run

Requires Go 1.25. Build the command before running it:

```powershell
go build -o local/bin/suag.exe ./cmd/suag
./local/bin/suag.exe run --prompt "Summarize this project" --output-format stream-json --read-only
./local/bin/suag.exe chat
```

On a host with `make` and `upx`, `make install` builds and installs `suag` to `$(go env GOPATH)/bin`. Ensure that directory is on the PATH of both your shell and any Gofer server process, then check `suag run --help` from a new shell.

`run` executes one turn and emits a `session` event, progress events, then one `result` event. The `result`, `input_required` and `error` events carry `usage` (this turn) and `session_usage` (session cumulative, persisted in session metadata), each with `input_tokens` (excluding cache), `output_tokens`, optional `cache_read_tokens`/`cache_write_tokens`, and `total_tokens` (input + output + cache). `chat` keeps a terminal conversation open; enter `:session` to see its ID and `:quit` to leave. Use `--session-id <id>` to create or reuse a named session. Use `--resume <id>` when an existing session is required; an unknown ID fails instead of creating a new one. `suag version` prints `suag <version> (<commit>)`.

`suag acp` speaks ACP over stdin/stdout. Launch it through an ACP client such as Gofer; stdout is reserved for protocol frames. See [Gofer setup and commands](docs/usage/gofer-personal-agent.md) for the tested `suag` and `suag-acp` configurations.

## Data and tool boundaries

Personal sessions and run records default to `~/.suag/db`. `SUAG_HOME` replaces `~/.suag` as the data and configuration root; `SUAG_CONFIG` selects a specific YAML configuration file. Both are read from the process environment only. Suag loads only `$SUAG_HOME/config.yaml` and `$SUAG_HOME/.env` (default under `~/.suag`); the `config.yaml`, `config/config.yaml`, `.env` and `.env.local` of the working directory belong to the user's project and are never read, and `SUAG_HOME` / `SUAG_CONFIG` set inside `$SUAG_HOME/.env` are ignored. For `run` and `chat`, `--model` and `--provider` override `SUAG_MODEL` and `SUAG_PROVIDER`, which override `llm.model` and `llm.provider_name` in the configuration file. Credentials stay in the configuration file or provider environment variables. Tests use isolated temporary homes and databases.

The CLI starts with read and list tools. `--read-only` makes that intent explicit and also restricts MCP tools to the default query allowlist (list/get tools of the MCP server named `gofer`) plus its `gofer_work_report`; extra `tools.auto_allow` globs are ignored and denied through the normal denial path. `--allow-write` opts into workspace write and shell tools for a trusted local invocation; the CLI has no mid-turn approval prompt. `run` and `chat` accept repeatable `--mcp-config <file>` flags for Claude-style `mcpServers` JSON with stdio servers. YAML `mcp.servers` supplies default servers. MCP tools require `tools.auto_allow` glob permission; entries are `server/toolglob` (a bare `toolglob` matches that tool from any server, so prefer the qualified form), and the default allows only the `gofer` server's list/get tools and `gofer_work_report`, so another server cannot gain that permission by reusing a tool name. A tool name already taken by a built-in tool or an earlier server (configuration order) is not overridden: the later MCP tool is dropped with a warning. MCP subprocesses inherit the environment except credentials (`*_API_KEY`, `*_AUTH_TOKEN`, LLM keys) unless the server's own `env` sets them. `--allow-write` never grants other MCP tools. A denied MCP call returns a tool error to the model; `run --output-format stream-json` also emits `input_required` before its final result and exits normally. For Gofer tasks that require approval before a tool runs, use `suag-acp` with `permission_policy: ask`. Its `read_only` mode excludes mutating tools, and permission refusal stops a pending write or command before execution.

## Structure and checks

- `pkg/agentcore` runs the model/tool loop; [its API docs](pkg/agentcore/docs/README.md) cover embedding and extension contracts.
- `internal/personal` owns persistent session turns. `internal/app` assembles providers and tools for CLI and ACP; `internal/acpserver` maps ACP messages and permissions to that same runner.
- `legacy/` is a separate Go module containing the former team, cron, orchestrator, and outbound ACP bridge code for reference. It is excluded from the root module build.

Run `go test ./...`, `go list ./...`, and `go build ./cmd/suag` from the Suag root. The local `v0.1.0` tag records the code before the personal-agent redesign.
