/*
Package agentcore provides a minimal but extensible agent runtime core.

The package is intentionally small in scope. It focuses on the execution path
that every agent needs:

  - prepare or resume run state
  - build prompt and context
  - call a model provider
  - execute requested tools
  - persist transcript and run state
  - surface lifecycle hooks and stable error categories

The public contracts are provider-neutral and storage-neutral so the runtime can
be embedded into different products, CLIs, servers, or higher-level multi-agent
systems.

The package should be thought of as a runtime kernel, not a complete agent
platform. It intentionally keeps planner, orchestration, channel integration,
governance, and product-level memory policy out of core.

Core extension points:

  - Provider: connect any LLM backend
  - Storage: persist snapshots and append-only transcripts
  - Memory: maintain summaries, recall context, and other derived memory views
  - ToolRegistry: expose tool schemas and execute tool calls
  - PromptBuilder: produce the system prompt
  - ContextBuilder: assemble model-facing messages
  - Hook: observe or modify key lifecycle stages

Package shape:

  - agentcore: current public runtime entrypoint and root facade
  - api: stable contracts and shared runtime data types

Without Config.Storage the runtime keeps runs in an in-process MemoryStorage;
pass a persistent storage to resume runs across processes.

Built-in companion packages:

  - storage/files: filesystem-backed storage
  - mock: test provider and tool implementations

Transcript semantics are explicit. When the provider returns tool calls, the
runtime persists both the assistant tool-call message and the tool result
message so runs can be resumed, replayed, and bridged across providers through a
single Message contract.

Resume semantics are also explicit. A stable RunID identifies the run being
continued, and ResumeMode controls whether a new user input is appended or the
runtime should continue strictly from persisted state.

Repository-specific first-party implementations can live in repository-private
packages while agentcore itself stays independent from internal runtime layers.
*/
package agentcore
