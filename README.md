# Suag

[![Unit-Tests](https://github.com/inhere/suag/actions/workflows/go.yml/badge.svg)](https://github.com/inhere/suag/actions/workflows/go.yml)

English | [简体中文](README.zh-CN.md)

Suag is a personal coding agent written in Go. One persistent conversation engine serves two interfaces: a terminal CLI (`chat` and one-shot `run`) and an ACP stdio server (`acp`). [Gofer](https://github.com/inhere/gofer) can launch either one and adds jobs, workers, logs and interactive approval on top.

- **Development tools**: read files in line windows, list, grep and find files, edit by exact replacement, run shell commands (PowerShell on Windows), fetch web pages, search the web.
- **Safe by default**: read-only unless writing is enabled; chat asks before writing or running commands; file tools stay inside the workspace; web fetches refuse private networks.
- **Skills**: standard `SKILL.md` skills from the workspace and `~/.agents/skills`, listed or searched on demand.
- **Persistent sessions**: one SQLite database with resume, automatic compaction, plans, handoff and an event log for tracing and stats.
- **MCP**: stdio MCP servers with allowlists and deferred loading for large tool sets.

## Install

Requires Go 1.25+. The repository is private, so build from a checkout:

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

In chat, `:session` shows the session id, `:compact` compacts the history and `:quit` leaves. Pressing Ctrl+C once at the prompt clears the line; pressing it again within 3 seconds exits. Ctrl+D exits immediately. On exit chat prints token usage and the command that resumes the session.

## Configuration

Suag reads only `$SUAG_HOME/config.yaml` and `$SUAG_HOME/.env` (default `~/.suag`); the working directory's config and `.env` files belong to your project and are never read. `SUAG_CONFIG` selects one config file.

| Setting | Meaning |
|---|---|
| `model` | Main model, `<provider>/<model>`; overridden by `SUAG_MODEL`, then `--model` |
| `providers.<id>` | `type` (openai/anthropic), `base_url`, `api_key`, `headers`, `models.<model>.context_window` |
| `compact_model`, `fallback_models` | Cheaper model for summaries; models tried in order when the main one fails |
| `tools.allow_write` | Chat only: enable write and shell tools (default false) |
| `tools.approval` | Chat only: `ask` (default) confirms write/shell tools, `auto` does not |
| `tools.max_output_bytes` | Cap on one stored tool result (default 30000) |
| `tools.shell`, `tools.shell_timeout_max` | Shell override (default pwsh/powershell on Windows, sh elsewhere); max seconds per command (600) |
| `tools.web_fetch.proxy`, `tools.web_fetch.allow_private` | Proxy for `web_fetch`; allow private addresses |
| `tools.auto_allow`, `tools.read_only_allow` | MCP allowlists, entries `server/toolglob` |
| `tools.deferred` | Defer MCP tool schemas: `auto` (default), `always`, `never` |
| `mcp.servers`, `mcp.call_timeout_sec` | Default MCP servers; timeout of one MCP call (120) |
| `skills.list_max`, `skills.always_load` | Skills listed in the prompt before falling back to search (15); skills preloaded into the prompt |
| `hooks.*` | Chat hook command (SessionStart, UserPromptSubmit, PostToolUse, Stop, …) |
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
| `file_write` | write | Write a whole file |
| `file_edit` | write | Replace an exact, unique string (or all with `replace_all`); keeps CRLF |
| `shell_exec` | write | Run a command (PowerShell on Windows, `sh` elsewhere) with a timeout; returns output and exit code |

- **Read-only by default.** Write tools exist only with `run --allow-write`, chat with `tools.allow_write: true`, or an ACP session in `default` mode. `--read-only` always wins. If the model calls a disabled tool, it is told how to enable it, and the call counts as a refusal.
- **Chat asks first.** With `tools.approval: ask`, chat asks before each write or shell call and shows the full command, path or old/new text. Answer `y` (once), `a` (this tool for the rest of the process) or anything else to refuse. Three refusals end the turn.
- **Boundaries.** File tools go through `os.OpenRoot`, so they cannot follow symlinks out of the workspace. `shell_exec` kills the whole process tree on timeout. `web_fetch` checks the address it actually connects to, which also covers redirects. Every tool result is capped.
- **MCP.** Use `--mcp-config <file>` (Claude-style `mcpServers` JSON) or `mcp.servers` in the config. MCP calls are refused unless allowed by `tools.auto_allow`, or by `tools.read_only_allow` under `--read-only`. Large tool sets are deferred and loaded through `tool_search`.

Exact semantics: [Gofer setup and tools](docs/usage/gofer-personal-agent.md#内置工具与相关配置).

## Skills

Suag scans `<workspace>/.agents/skills`, `<workspace>/skills` and `~/.agents/skills` once at startup. If two skills share a name, the workspace one wins. If there are no more than `skills.list_max` skills, all of them are listed in the system prompt. Otherwise only workspace skills are listed, and the model finds the rest with `skill(query=…)`. To put a skill's body into every prompt, mark it with `metadata: {always_load: true}` in its frontmatter, or list it in `skills.always_load`.

## Sessions

- **Resume and handoff.** `--session-id` creates or reuses a named session. `--resume` requires an existing one; a missing session exits with code 4. `run --from <session>` starts a new session that inherits the old one's summary, plan and loaded tools.
- **Compaction.** When the context nears the model window, older history is summarized automatically. `:compact` in chat does it on demand.
- **Plans.** The `update_plan` tool records the model's plan. stream-json emits it as `plan` events, and ACP sends it as plan updates.
- **System prompt.** Suag builds the system prompt once per process. It covers OS, shell, workspace, mode and conventions, then project rules (`AGENTS.md`, `CLAUDE.md`, `~/.suag/AGENTS.md`) and any `--append-system-prompt`. `--no-project-rules` skips project rules.
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
```

## Development

```bash
go test -race ./...     # unit tests
make smoke-chat         # PTY end-to-end chat tests (Linux/macOS, Python 3)
make build-all          # cross-compile to dist/
```

- `cmd/suag`: entry point; `internal/cli`: one file per command.
- `internal/wiring`: builds providers, tools, skills, the system prompt and the runner for CLI and ACP.
- `internal/personal`: turns, compaction, handoff and events; `internal/personal/store` is the SQLite store.
- `internal/platform/tools`: native tools, MCP client, approval and deferred-tool layers; `internal/platform/llm`: providers.
- `internal/chat`: terminal chat; `internal/acpserver`: the ACP server; `pkg/agentcore`: the model/tool loop ([docs](pkg/agentcore/docs/README.md)).

Design notes and plans are in [docs/](docs/README.md). CI runs tests on Ubuntu and Windows with Go 1.25 and stable. The old control plane (team, cron, orchestrator) is kept at the `v0.1.0` tag.
