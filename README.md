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

`run` executes one turn and emits a `session` event, progress events, then one `result` event. `chat` keeps a terminal conversation open; enter `:session` to see its ID and `:quit` to leave. Use `--session-id <id>` to create or reuse a named session. Use `--resume <id>` when an existing session is required; an unknown ID fails instead of creating a new one.

`suag acp` speaks ACP over stdin/stdout. Launch it through an ACP client such as Gofer; stdout is reserved for protocol frames. See [Gofer setup and commands](docs/usage/gofer-personal-agent.md) for the tested `suag-cli` and `suag-acp` configurations.

## Data and tool boundaries

Personal sessions and run records default to `~/.suag/db`. The `database_url` and `agentcore.run_root` settings can point to existing data explicitly; Suag does not move the former repository `db/` contents automatically. Tests use isolated temporary homes and databases.

The CLI starts with read and list tools. `--read-only` makes that intent explicit. `--allow-write` opts into workspace write and shell tools for a trusted local invocation; the CLI has no mid-turn approval prompt. For Gofer tasks that require approval before a tool runs, use `suag-acp` with `permission_policy: ask`. Its `read_only` mode excludes mutating tools, and permission refusal stops a pending write or command before execution.

## Structure and checks

- `pkg/agentcore` runs the model/tool loop; [its API docs](pkg/agentcore/docs/README.md) cover embedding and extension contracts.
- `internal/personal` owns persistent session turns. `internal/app` assembles providers and tools for CLI and ACP; `internal/acpserver` maps ACP messages and permissions to that same runner.
- `legacy/` is a separate Go module containing the former team, cron, orchestrator, and outbound ACP bridge code for reference. It is excluded from the root module build.

Run `go test ./...`, `go list ./...`, and `go build ./cmd/suag` from the Suag root. The local `v0.1.0` tag records the code before the personal-agent redesign.
