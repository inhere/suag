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

Core extension points:

  - Provider: connect any LLM backend
  - Storage: persist snapshots and append-only transcripts
  - ToolRegistry: expose tool schemas and execute tool calls
  - PromptBuilder: produce the system prompt
  - ContextBuilder: assemble model-facing messages
  - Hook: observe or modify key lifecycle stages

Built-in companion packages:

  - files: filesystem-backed storage
  - context: default message-window context builder
  - prompt: static prompt builder
  - hooks: ordered hook-chain helper
  - mock: test provider and tool implementations
  - adapters/internalllm: bridge internal llm.Provider into agentcore.Provider
  - adapters/internaltools: bridge internal tools.Registry into agentcore.ToolRegistry

Transcript semantics are explicit. When the provider returns tool calls, the
runtime persists both the assistant tool-call message and the tool result
message so runs can be resumed, replayed, and bridged across providers through a
single Message contract.
*/
package agentcore
