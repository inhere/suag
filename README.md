# Suag

[![Unit-Tests](https://github.com/inhere/suag/actions/workflows/go.yml/badge.svg)](https://github.com/inhere/suag/actions/workflows/go.yml)

English | [简体中文](README.zh-CN.md)

Suag is a personal coding agent written in Go. One persistent conversation engine serves two interfaces: a terminal CLI (`chat` and one-shot `run`) and an ACP stdio server (`acp`). [Gofer](https://github.com/inhere/gofer) can launch either one and adds jobs, workers, logs and interactive approval on top.

- **Development tools**: read files in line windows, list, grep and find files, edit by exact replacement, run shell commands (PowerShell on Windows), fetch web pages, search the web.
- **Safe by default**: read-only unless writing is enabled; chat asks before writing or running commands; file tools stay inside the workspace; web fetches refuse private networks.
- **Skills**: standard `SKILL.md` skills from the workspace and `~/.agents/skills`, listed or searched on demand.
- **Persistent sessions**: one SQLite database with resume, automatic compaction, plans, handoff and an event log for tracing and stats.
- **MCP**: stdio MCP servers with allowlists and deferred loading for large tool sets.
- **Persistent memory**: user and project memory that outlives sessions. Plain `MEMORY.md` files are the source of truth, and the backend can be swapped (files, gofer or an external command).
- **History search**: full-text search over earlier sessions, and paging through saved tool output.
- **Large-output offloading**: oversized tool results are saved under the session directory; the context keeps only the head, the tail and a ref.

## Install

Requires Go 1.26+. The repository is private, so build from a checkout:

```bash
git clone https://github.com/inhere/suag.git && cd suag
make install            # or: go install ./cmd/suag
```

`make install` builds (compressing with `upx` when available) and installs to `$(go env GOPATH)/bin`. On Windows it first moves aside a binary that a running chat is still using. With access to the repository configured (`GOPRIVATE=github.com/inhere/*`), `go install github.com/inhere/suag/cmd/suag@latest` works too. Pushing a `v*` tag runs the release workflow, which attaches prebuilt binaries for Linux, macOS and Windows. Make sure the install directory is on the `PATH` of your shell (and of the Gofer server, if you use it), then check `suag version`.

## Quick start

Create `~/.suag/config.yaml` (see [config/config.yaml](config/config.yaml) for every field):

```yaml
model: deepseek/deepseek-chat   # <provider>/<model>
# providers: openai, anthropic and deepseek are built in and read
# OPENAI_API_KEY / ANTHROPIC_API_KEY / DEEPSEEK_API_KEY.
tools:
  allow_write: true             # let chat write files and run commands (asks first)
```

```bash
suag chat                                         # interactive chat in the current directory
suag run --prompt "Summarize this project"        # one turn, read-only
suag run --allow-write --prompt "Fix the failing test"
suag run --resume <session-id> --prompt "Continue"
```

In chat, `:session` shows the session id, `:compact` starts a new segment (see [Sessions](#sessions)) and `:quit` leaves. Pressing Ctrl+C once at the prompt clears the line; pressing it again within 3 seconds exits. Ctrl+D exits immediately. On exit chat prints token usage and the command that resumes the session.

## Configuration

Suag reads only `$SUAG_HOME/config.yaml` and `$SUAG_HOME/.env` (default `~/.suag`); the working directory's config and `.env` files belong to your project and are never read. `SUAG_CONFIG` selects one config file.

| Setting | Meaning |
|---|---|
| `model` | Main model, `<provider>/<model>`; overridden by `SUAG_MODEL`, then `--model` |
| `providers.<id>` | `type` (openai/anthropic), `base_url`, `api_key`, `headers`, `models.<model>.context_window` |
| `providers.<id>.models.<model>.price` | USD per million tokens (`input`, `output`, `cache_read`, `cache_write`); `suag stats` computes cost from it when the provider reports none |
| `compact_model`, `fallback_models` | Cheaper model for summaries; models tried in order when the main one fails |
| `context.idle_after`, `context.idle_min_ratio` | Chat only: after this much quiet (default `15m`; `0` turns it off) and when the context has reached this share of the segment threshold (default `0.5`), start a new segment in the background |
| `providers.<id>.prompt_cache` | Claude/`anthropic` type only, default off: add `cache_control` breakpoints (system becomes a block array); some gateways reject them |
| `tools.allow_write` | Chat only: enable write and shell tools (default false) |
| `tools.approval` | Chat only: `ask` (default) confirms write/shell tools, `auto` does not |
| `tools.max_output_bytes`, `tools.max_spill_bytes` | Inline threshold: largest tool result kept whole in the session (default 8000; larger ones are saved under `$SUAG_HOME/sessions/<id>/outputs/` and kept as a ref plus head/tail); raw output cap per call (default 1 MiB) |
| `tools.shell`, `tools.shell_timeout_max` | Shell override (default pwsh/powershell on Windows, sh elsewhere); max seconds per command (600) |
| `tools.web_fetch.proxy`, `tools.web_fetch.allow_private` | Proxy for `web_fetch`; allow private addresses |
| `tools.auto_allow`, `tools.read_only_allow` | MCP allowlists, entries `server/toolglob` |
| `tools.deferred` | Defer MCP tool schemas: `auto` (default), `always`, `never` |
| `mcp.servers`, `mcp.call_timeout_sec` | Default MCP servers; timeout of one MCP call (120) |
| `skills.list_max`, `skills.always_load` | Skills listed in the prompt before falling back to search (15); skills preloaded into the prompt |
| `hooks.*` | Chat hook command (SessionStart, UserPromptSubmit, PostToolUse, Stop, …) |
| `memory.write` | `auto` (default) lets the `memory` tool write; `off` makes it read-only |
| `memory.write_tainted` | What a session that has read external content does on a memory write: `ask` (default), `deny`, `allow` |
| `memory.user.backend`, `memory.project.backend` | `files`, `gofer` (project only) or `exec`. Unset: user = files; project = auto (see [Memory](#memory)) |
| `memory.<scope>.command`, `.args`, `.timeout_sec` | The external command when that scope's backend is `exec` (required then) |
| `memory.taint_tools`, `memory.taint_on_spill` | Extra tools whose results mark the session as tainted: always, or only when the output was saved to a file (default `[shell_exec]`) |
| `serpapi_key` | Enables `web_search` |

All data lives in `$SUAG_HOME/db/sessions.db`. Model and provider details: [LLM config](docs/usage/llm-config-v0.md).

## Tools and permissions

| Tool | Kind | What it does |
|---|---|---|
| `file_read` | read | Numbered lines in windows (`offset`/`limit`, 2000 lines by default); reports binary files |
| `file_list` | read | Directory listing, recursive with `depth` (≤ 5) |
| `file_grep` | read | RE2 search with `glob`, `ignore_case`, `output: content\|files` |
| `file_find` | read | `**` glob search, newest first |
| `web_fetch` | read | Page text (headings, links and code kept); private and local addresses refused |
| `web_search` | read | SerpAPI search (needs `serpapi_key`) |
| `skill` | read | Load a skill, read its files, or search skills (`query`, `"*"` lists all names) |
| `session_search` | read | Full-text search of earlier messages, tool results and summaries (`scope`: workspace, current, all); with `ref=out:...` it searches inside one saved output |
| `recall` | read | Read around a hit by ref: messages from `<session>/<seq>`, or numbered lines of a saved output `out:<session>:<hex>` |
| `suag_manifest` | read | This instance's capabilities as JSON (same data as `suag manifest`) |
| `memory` | state | `add`, `replace`, `remove`, `get`, `search` on long-term memory (`scope` project or user); see [Memory](#memory) |
| `file_write` | write | Write a whole file |
| `file_edit` | write | Replace an exact, unique string (or all with `replace_all`); keeps CRLF |
| `shell_exec` | write | Run a command (PowerShell on Windows, `sh` elsewhere) with a timeout; returns output and exit code |

- **Read-only by default.** Write tools exist only with `run --allow-write`, chat with `tools.allow_write: true`, or an ACP session in `default` mode. `--read-only` always wins. If the model calls a disabled tool, it is told how to enable it, and the call counts as a refusal.
- **Chat asks first.** With `tools.approval: ask`, chat asks before each write or shell call and shows the full command, path or old/new text. Answer `y` (once), `a` (this tool for the rest of the process) or anything else to refuse. Three refusals end the turn.
- **Large outputs.** A tool result above `tools.max_output_bytes` is saved (redacted) to `$SUAG_HOME/sessions/<id>/outputs/`; the context keeps the head and tail (2000 bytes each) plus a ref such as `out:<session>:<hex>`. Read it with `recall`, search it with `session_search`. `file_read` pages by itself and is never saved.
- **Boundaries.** File tools go through `os.OpenRoot`, so they cannot follow symlinks out of the workspace. `shell_exec` kills the whole process tree on timeout. `web_fetch` checks the address it actually connects to, which also covers redirects. Every tool result is capped.
- **MCP.** Use `--mcp-config <file>` (Claude-style `mcpServers` JSON) or `mcp.servers` in the config. MCP calls are refused unless allowed by `tools.auto_allow`, or by `tools.read_only_allow` under `--read-only`. Large tool sets are deferred and loaded through `tool_search`.

Exact semantics: [Gofer setup and tools](docs/usage/gofer-personal-agent.md#内置工具与相关配置).

## Skills

Suag scans `<workspace>/.agents/skills`, `<workspace>/skills` and `~/.agents/skills` once at startup. If two skills share a name, the workspace one wins. If there are no more than `skills.list_max` skills, all of them are listed in the system prompt. Otherwise only workspace skills are listed, and the model finds the rest with `skill(query=…)`. To put a skill's body into every prompt, mark it with `metadata: {always_load: true}` in its frontmatter, or list it in `skills.always_load`.

## Memory

Long-term memory shared across sessions, written by the model through the `memory` tool.

- **Four kinds**: `user` (who the user is, preferences), `feedback` (corrections and confirmed choices), `project` (goals, decisions, progress the code does not show), `reference` (where external resources live). Not stored: anything derivable from code or git, what rule files already say, session-local details, secrets. Procedures belong in skills.
- **Scopes and backends.** User memory lives in `$SUAG_HOME/memory/user` (files). Project memory picks its backend in this order: `memory.project.backend`; a `MEMORY.md` at the repository root (files, repository mode); a `.gofer/tracker` at the repository root (gofer); otherwise files under `$SUAG_HOME/projects/<key>/memory`. In repository mode the index is `MEMORY.md` and each entry is `.suag/memory/<id>.md`, so memory can be committed with the project. suag never creates the root `MEMORY.md`. With the gofer backend each project entry is a gofer memory tagged `suag`; only those are managed. `suag manifest` shows what was chosen and why.
- **Frozen index.** The memory index (titles only) goes into the segment context message and stays unchanged for the segment. Writes in this session show up in the next segment; a new segment refreshes it. Read a body with `memory get`.
- **Tainted sessions.** Once a session has read external content (`web_fetch`, `web_search`, MCP tools, `session_search`, `recall`, or the tools in `memory.taint_tools` / `memory.taint_on_spill`) it is marked tainted for good, and memory writes need confirmation (`memory.write_tainted`): chat asks (`y`, `n`, or `a` for the rest of the session), ACP sends a permission request, and `run` refuses. `deny` never writes, `allow` skips the question. Under `--read-only` (and ACP `read_only`) the tool only reads. Plain `run` may write memory, since it is suag's own state rather than the workspace.

## Sessions

- **Resume and handoff.** `--session-id` creates or reuses a named session. `--resume` requires an existing one; a missing session exits with code 4. `run --from <session>` starts a new session that inherits the old one's summary, plan and loaded tools.
- **Segments.** A session is a chain of segments. Each segment starts with a record (reason: start, threshold, `:compact`, idle, `--from`) that holds the context message and, after the first segment, a handoff: a summary for the current task, the last user requests verbatim and the refs that still matter. When the context nears the model window, or on `:compact`, a new segment starts; only the last complete tool call/result group is kept verbatim. Earlier segments stay in the database: fetch them with `session_search` and `recall`.
- **Idle consolidation (chat).** After `context.idle_after` (default 15m) without input, if the context has reached `context.idle_min_ratio` (default 0.5) of the threshold, chat starts a new segment in the background. It never fires during a turn, and typing cancels it.
- **Plans.** The `update_plan` tool records the model's plan. stream-json emits it as `plan` events, and ACP sends it as plan updates.
- **System prompt and segment context.** The system prompt holds only stable content (identity and working conventions), so it stays byte-identical across sessions and workspaces of one mode. Environment (OS, shell, workspace), project rules (`AGENTS.md`, `CLAUDE.md`, `~/.suag/AGENTS.md`), `--append-system-prompt`, the memory index, the skills list and deferred tools go into a context message at the start of each segment. A change inside a segment (for example edited rules) is appended as a "context update" message instead of rewriting earlier text. `--no-project-rules` skips project rules.
- **Delivery and hooks.** `suag send --session <s> --text <t>` hands text to a running chat. `hooks.command` receives chat lifecycle events as JSON on stdin.

## Using with Gofer

Gofer launches `suag run` (CLI jobs) or `suag acp` (persistent sessions with per-call approval through `permission_policy`). For agent configuration, read-only and writable runs, the stream-json events and the denial contract, see [docs/usage/gofer-personal-agent.md](docs/usage/gofer-personal-agent.md).

## Observability

```text
suag trace <session> [--turn <id>] [--json]       # timeline: model calls, tools, denials, plans
suag stats [--session <id>] [--since 7d] [--json] # calls, latency, tokens, cache hits
suag export <session> [--redact] [-o <file>]      # entries and events as jsonl
suag gc --older-than 30d [--apply]                # list (default) or delete idle sessions
suag ps                                           # running chat processes
suag manifest [--json] [--model <p/m>]            # models, tools, MCP, skills, memory backends, output limits
```

`suag trace` also shows a `turn_context` line per turn (model, mode, tool set hash, rules and memory index hashes, tainted), a line for each segment break (`segment=#N reason=... tokens=before->after`) and `schedule` events (chat idle consolidation: fired, done, cancelled, skipped). `suag stats` shows cost per model (from the provider, or from `price` in the config) and, with `--json`, tool output bytes (`raw_bytes`, `inline_bytes`, `spilled`).

`run --output-format stream-json` prints a `usage` line after every model call, with the run's running totals: `input_tokens` (cache excluded), `output_tokens`, `cache_read_tokens`, `cache_write_tokens`, `total_tokens`, `cost_usd` and `tool_calls`. `turn_end` and `result` carry the same `usage`, so a consumer can enforce a budget mid-run. ACP sends `usage_update` at the same moments ([details](docs/usage/gofer-personal-agent.md#acp-用量上报)).

## Development

```bash
go test -race ./...     # unit tests
make smoke-chat         # PTY end-to-end chat tests incl. chat_memory and chat_idle (Linux/macOS, Python 3)
go run ./scripts/eval --fake --bin tmp/smoke/suag   # task evals, see scripts/eval/README.md
make build-all          # cross-compile to dist/
```

The eval has multi-turn tasks (`prompts` in `task.yaml`, one session) and reports `first_call_uncached` (FIRST column: uncached input of the first main model call). `--probe-prefix` runs three probes of the prefix cache: same workspace, a different workspace, and a control with a random nonce in the system prompt. `--agent omp` runs the same tasks on omp for comparison.

- `cmd/suag`: entry point; `internal/cli`: one file per command.
- `internal/wiring`: builds providers, tools, skills, the system prompt and the runner for CLI and ACP.
- `internal/personal`: turns, segments, handoff and events; `internal/personal/store` is the SQLite store.
- `internal/chat`: terminal chat; `internal/acpserver`: the ACP server.
- `pkg/agentcore`: the reusable layers below the app, independent of `internal/*` ([README](pkg/agentcore/README.md)): the kernel (model/tool loop, run state, transcript) plus `llm` (providers), `schedule` (in-process After/Every/Idle timers), `tools` (registry, approval, deferred tools), `tools/native` (built-in tools), `tools/mcp` (MCP client), `skills`, `memory` (store interface, governance, router; `memory/files` and `memory/execstore` backends), `redact` (secret and invisible-text cleaning) and `extproc` (exec protocol v1 and credential stripping for external commands).

Release notes are in [CHANGELOG.md](CHANGELOG.md); design notes and plans are in [docs/](docs/README.md). CI runs tests on Ubuntu and Windows with Go 1.26 and stable. The old control plane (team, cron, orchestrator) is kept at the `v0.1.0` tag.
