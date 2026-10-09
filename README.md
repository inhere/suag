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

All personal state lives in one SQLite database, `$SUAG_HOME/db/sessions.db` (default `~/.suag/db/sessions.db`, WAL mode), holding sessions, entries, turns and observability events. Its location is decided only by `SUAG_HOME`; `database_url` no longer exists. The databases of earlier versions (`suag.db`, the `agentcore/` run directory) are neither read, migrated nor deleted: old session IDs cannot be resumed, and old sessions registered in Gofer cannot be woken or taken over. `SUAG_HOME` replaces `~/.suag` as the data and configuration root; `SUAG_CONFIG` selects a specific YAML configuration file. Both are read from the process environment only. Suag loads only `$SUAG_HOME/config.yaml` and `$SUAG_HOME/.env` (default under `~/.suag`); the `config.yaml`, `config/config.yaml`, `.env` and `.env.local` of the working directory belong to the user's project and are never read, and `SUAG_HOME` / `SUAG_CONFIG` set inside `$SUAG_HOME/.env` are ignored. Models are configured as `model: <provider>/<model>` plus `providers.<id>` (`type` openai|anthropic, `base_url`, `api_key`, `headers`, `models.<model>.context_window`); `openai`, `anthropic` and `deepseek` are built-in presets. For `run` and `chat`, `--model` overrides `SUAG_MODEL`, which overrides `model` in the configuration file; a value is split at the first `/` only when the prefix is a defined provider, and a bare model id uses the configured model's provider. An `openai` provider posts to `<base_url>/chat/completions`; an `anthropic` provider streams (SSE) from `<base_url>/v1/messages` (`/messages` when `base_url` already ends in `/v1`). The previous `llm` section and the provider-selection flag and environment variable are removed and fail at startup. See [LLM config](docs/usage/llm-config-v0.md). Credentials stay in the configuration file or provider environment variables. Tests use isolated temporary homes and databases.

The CLI starts with read and list tools. `--read-only` makes that intent explicit and restricts MCP tools to `tools.read_only_allow` (every entry must be `server/toolglob`); `tools.auto_allow` never widens it, and other MCP calls are denied through the normal denial path. `--allow-write` opts into workspace write and shell tools for a trusted local invocation; in `chat`, `tools.allow_write: true` (boolean, default false) does the same without the flag. `run` still needs `--allow-write`, `--read-only` always wins, and ACP never reads the setting. In `chat`, `tools.approval` (`ask` default, `auto`) decides what happens next: with `ask`, every non-MCP tool outside the read-only list (`file_read`, `file_list`, `update_plan`, `tool_search`) is confirmed on the terminal before it runs (today `file_write`, `shell_exec`, and `web_search` when `serpapi_key` is set). The question shows the full, redacted command, path with byte count, or query, never truncated. `y`/`yes` allows once, `a`/`always` allows later calls of the same tool for the rest of this chat process, anything else (Enter included) refuses and the turn continues. Ctrl-C or Ctrl-D during the question only cancels that turn (it prints `已取消`) and does not leave `chat`; input typed ahead during a turn is never taken as the answer. When stdin is not a terminal, every tool that needs confirmation is refused with a hint to set `tools.approval: auto`. Three refusals in one turn end the turn, and the end of a turn lists `本轮被拒绝的工具：…`. MCP tools are not asked about: they keep following `tools.auto_allow` / `tools.read_only_allow`. `run` and `chat` accept repeatable `--mcp-config <file>` flags for Claude-style `mcpServers` JSON with stdio servers. YAML `mcp.servers` supplies default servers. MCP tools require `tools.auto_allow` glob permission; entries are `server/toolglob` (a bare `toolglob` matches that tool from any server, so prefer the qualified form). There is no built-in allowlist: with `tools.auto_allow` and `tools.read_only_allow` both empty every MCP call is refused. A tool name already taken by a built-in tool or an earlier server (configuration order) is not overridden: the later MCP tool is dropped with a warning. MCP subprocesses inherit the environment except credentials (`*_API_KEY`, `*_AUTH_TOKEN`, LLM keys) unless the server's own `env` sets them. `--allow-write` never grants other MCP tools. A denied MCP call returns a tool error to the model; `run --output-format stream-json` also emits `input_required` before its final result and exits normally. `run --output-format stream-json` additionally emits `tool_call` (`name`, `args` summary) and `tool_result` (`name`, `is_error`, redacted and truncated `summary`) lines; existing lines are unchanged, callers without an `ndjson_keep` filter receive them, and the sample Gofer `ndjson_keep` leaves them out. For Gofer tasks that require approval before a tool runs, use `suag-acp` with `permission_policy: ask`. Its `read_only` mode excludes mutating tools, and permission refusal stops a pending write or command before execution.

## Session features

- Automatic compaction: when the estimated context exceeds the model window minus a reserve, older history is summarized and the summary is persisted as a session entry. In `chat`, `:compact` compacts on demand. `compact_model` (`<provider>/<model>` or a bare id on the main provider) selects a cheaper model for summaries; unset means the turn's own model. `providers.<id>.models.<model>.context_window` overrides the window otherwise derived from the model name. `fallback_models` lists models tried in order when the main model fails.
- Plans: the `update_plan` tool records the model's plan in the session. `run --output-format stream-json` emits a `plan` event (`explanation`, `entries` of `step`/`status`, always the full list), and ACP maps it to a plan update.
- Handoff: `suag run --from <session> ...` starts a new session that inherits the source's summary, plan and loaded tools; the source is only read. It cannot be combined with `--resume`; a failed handoff creates no session, so the same `--session-id` can be retried.
- Chat hooks: `chat` calls the program in `hooks.command` (argv list, no shell) for SessionStart, UserPromptSubmit, PostToolUse, Stop, SessionEnd and Interrupt events, the event as one JSON line on stdin. Hooks are off unless `hooks.command` is set; `SUAG_HOOKS=off` disables them. `hooks.timeout_ms` bounds ordinary events, `hooks.stop_args` is appended to the Stop call only, and `hooks.stop_wait_sec` must be set with it (Stop waits `stop_wait_sec+2` seconds, otherwise `timeout_ms`). PostToolUse is sent only for tools matching the `hooks.post_tool_use.tools` globs, renamed to `hooks.post_tool_use.name` when set.
- Delivery: `suag send --session <s> --text <t> [--source <label>]` hands text to a running `chat`, shown as `[来自 <label>] <text>` (default label `cli`; text that already starts with a `[...]` tag is kept). `suag run --injected` marks the prompt as not typed by a person in the transcript and hook events.
- Missing session: when the `--resume` target or `--from` source does not exist, `run`/`chat` exit with code 4 and print `session not found: <id>` to stderr.

## Tools

`tools.deferred` (`auto` default, `always`, `never`) controls whether MCP tool schemas are deferred. When tools are deferred, the model sees only their names and loads them with the built-in `tool_search` tool; loaded tools persist in the session. `auto` defers once the MCP schemas exceed about 10% of the context window.

## Observability

Model calls, tool calls, compactions and denials are recorded as events in the session database:

```text
suag trace <session> [--turn <turn-id>] [--json]   # timeline of one session, including update_plan records (`plan ✔/▶/○ step`; `--json` rows have `"kind":"plan"`)
suag stats [--session <id>] [--since 12h|7d] [--json]  # calls, latency, tokens, cache hits
suag export <session> [--redact] [-o <file>]       # entries and events as jsonl (--redact drops tool arguments)
suag gc --older-than 30d [--apply]                 # list (default) or delete sessions idle that long, with their events; live sessions are kept
```

## Tool denial

A denied tool call returns a tool error to the model and the turn continues; three denials in one turn end it. See [Gofer setup](docs/usage/gofer-personal-agent.md#工具拒绝语义) for the exact stream-json and ACP contract.

## Structure and checks

- `pkg/agentcore` runs the model/tool loop; [its API docs](pkg/agentcore/docs/README.md) cover embedding and extension contracts.
- `cmd/suag` is only the entry point (`main`, exit reporting, version stamping); `internal/cli` holds one command per file (run, chat, acp, ps, send, trace, stats, export, version).
- `internal/wiring` assembles providers, tools, the MCP gate, skills and the runner for CLI and ACP; `internal/config` resolves paths, `providers`/`model` settings and provider construction.
- `internal/personal` owns session turns, compaction, handoff and observability; `internal/personal/store` is the SQLite session store with embedded migrations.
- `internal/chat` is the terminal conversation loop; `internal/hooks` runs the configured hook command; `internal/output` builds the text and stream-json output lines; `internal/acpserver` maps ACP messages and permissions to the same runner.
- `internal/platform` holds the LLM providers and tool implementations (native, MCP, approval and deferred-tool wrappers).

Run `go test ./...`, `go list ./...`, and `go build ./cmd/suag` from the Suag root. The old control plane (team, cron, orchestrator) is available at the `v0.1.0` tag.
