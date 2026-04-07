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

Built-in companion packages:

  - files: filesystem-backed storage
  - memory: default working-memory implementation
  - context: default context presets and message-window builders
  - prompt: first-party prompt builders
  - hooks: ordered hook-chain helper
  - mock: test provider and tool implementations

Transcript semantics are explicit. When the provider returns tool calls, the
runtime persists both the assistant tool-call message and the tool result
message so runs can be resumed, replayed, and bridged across providers through a
single Message contract.

Resume semantics are also explicit. A stable RunID identifies the run being
continued, and ResumeMode controls whether a new user input is appended or the
runtime should continue strictly from persisted state.

Repository-specific adapters that bridge Axon's internal packages into
agentcore live under internal/agentcoreadapters so this package can stay
independent from repository-private runtime layers.
*/
package agentcore
