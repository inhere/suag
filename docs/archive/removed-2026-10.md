# 2026-10 agentcore 分层清理：删除记录

> 依据 [agentcore 分层设计](../design/2026-10-10-agentcore-layering-design.md) 与 [实施计划](../plans/2026-10-10-agentcore-layering-plan.md)。
> 每项写明删除前的提交与原路径，需要参考时用 `git show <commit>:<path>` 查看单个文件，或 `git checkout <commit> -- <path>` 恢复到工作区。

## 应用层（T1）

| 删除项 | 原路径 | 删除前提交 | 原用途 / 删除原因 |
|---|---|---|---|
| runtimecaps 能力管理器 | `internal/capability/runtimecaps/` | `2ef8f5a` | 按回合解析工具能力集（只读/写模式推断、工具引用表）。生产代码只创建、从不读取，只有自身测试使用 |
| wiring 中的 Manager 接线 | `internal/wiring/capabilities.go`（`Manager`、`registerNativeTool`、`nativeToolProviderID`） | `2ef8f5a` | 为 runtimecaps 登记工具引用；随 runtimecaps 删除 |
| MCP `Provider.ListToolRefs` | `internal/platform/tools/mcp/provider.go` | `2ef8f5a` | 向 runtimecaps 报告工具引用；改由注册表本身查询 |
| `tools.Registry.Subset`、`Registry.Execute` | `internal/platform/tools/registry.go` | `2ef8f5a` | 子集注册表与按名执行的便捷方法，仅测试使用 |
| `KindSkill`、`KindBrowser`、`KindExtAgent` | `internal/platform/tools/registry.go` | `2ef8f5a` | 预留的工具类型常量，从未使用 |

## 内核死代码与重复 mock（T2）

| 删除项 | 原路径 | 删除前提交 | 原用途 / 删除原因 |
|---|---|---|---|
| hooks.Chain | `pkg/agentcore/hooks/`、`pkg/agentcore/hooks_chain_test.go` | `56a45df` | 钩子链；与 `runtime_helpers.go` 内部的 `runtimeHookChain` 逐行相同，只有自测使用 |
| SQLite 存储 | `pkg/agentcore/storage/sqlite/` | `56a45df` | agentcore Storage 的 SQLite 实现；无使用者，suag 用自己的 `internal/personal/store` |
| Embedding 链 | `pkg/agentcore/api/provider.go`（`EmbeddingProvider`）、`internal/platform/llm/{openai,claude,pool,provider}.go` 的 `Embed`/`EmbedModel`/Pool embedder | `56a45df` | 向量生成能力；全仓无调用方 |
| 旧 MockProvider | `internal/platform/llm/mock.go`、`mock_test.go`、`public_contract_mapping_test.go` | `56a45df` | 脚本与手动步进两种模式的假模型；与 `pkg/agentcore/mock` 重复，后者增加 `WithName` 覆盖原用法；手动模式无使用者 |
| 旧 MockProvider 文档 | `docs/usage/llm-mock-usage.md` → [agentcore-2026-10/llm-mock-usage.md](agentcore-2026-10/llm-mock-usage.md) | `56a45df` | 原样归档；同路径改写为 `pkg/agentcore/mock` 用法 |
| `"tool_error"` 元数据键 | `pkg/agentcore/runtime_helpers.go` | `56a45df` | 注释称供记忆压缩读取，实际无读取者 |

## 无使用者子包与示例（T3）

| 删除项 | 原路径 | 删除前提交 | 原用途 / 删除原因 |
|---|---|---|---|
| context 子包 | `pkg/agentcore/context/` | `3973cdb` | 上下文预设：`TranscriptWindow`、`MemoryFirst`、`MemoryThenRecent`、`ArtifactsThenRecent`；suag 用自己的上下文构建（`internal/personal/context.go`），无使用者 |
| memory 子包 | `pkg/agentcore/memory/` | `3973cdb` | 第一方 `WorkingMemory`：summary + recent window；无使用者。根包的 `Memory` 接口保留 |
| prompt 子包 | `pkg/agentcore/prompt/` | `3973cdb` | 静态与分层（base + metadata + sections）提示词构建器；无使用者 |
| 示例 context / memory / prompt | `pkg/agentcore/_examples/{context,memory,prompt}/` | `3973cdb` | 演示上述三个子包 |
| 示例 bridge | `pkg/agentcore/_examples/bridge/` | `3973cdb` | 用 internal 的 llm/tools 桥接 agentcore；反向依赖 internal，独立后无法编译 |
| `_examples/` 目录名 | `pkg/agentcore/_examples/` → `pkg/agentcore/examples/` | `3973cdb` | 改名后示例参与编译与 vet；host、recommended 去掉对已删子包的依赖 |

## 根包默认存储（T8）

| 删除项 | 原路径 | 删除前提交 | 原用途 / 替代 |
|---|---|---|---|
| `FileStorage` 别名、`NewFileStorage` | `pkg/agentcore/types.go` | `327eb1a` | 文件存储的兼容别名；改用 `files.NewStorage(dir)`（`pkg/agentcore/storage/files`） |
| 默认文件存储 | `pkg/agentcore/runtime_helpers.go` `defaultStorage` | `327eb1a` | 未传 Storage 时写入 `os.TempDir()/agentcore-runs`；现为进程内 `agentcore.NewMemoryStorage()`，根包不再依赖 `storage/files` |

## 工具注册表合一（T11）

| 删除项 | 原路径 | 删除前提交 | 原用途 / 替代 |
|---|---|---|---|
| 根包 `Registry`、`NewToolRegistry` | `pkg/agentcore/tools.go` | `a91b1c1` | 根包内置的内存工具注册表（同名 `Register` 覆盖）；改用 `tools.NewRegistry()`（`pkg/agentcore/tools`），覆盖语义用 `MustRegister`，`Register` 同名 panic；未传 `Config.Tools` 时 runtime 用私有空注册表 |
| `tools.Tool` 接口（含必需的 `Kind()`） | `internal/platform/tools/registry.go` | `2156068` | 工具接口统一为 `api.Tool`；`Kind()` 改为可选接口 `tools.Kinded`，用 `tools.KindOf(tool)` 读取，缺省 `KindNative`；native 工具删去各自的 `Kind()` |

## 新旧路径与 API 对照（W2–W4）

| 旧 | 新 | 提交 |
|---|---|---|
| `internal/capability/skills` | `pkg/agentcore/skills` | `ecff7ab` |
| `internal/platform/llm` | `pkg/agentcore/llm` | `b418a65` |
| `internal/platform/tools`（registry、approval、asker、visibility） | `pkg/agentcore/tools` | `a91b1c1` |
| `internal/platform/tools/native` | `pkg/agentcore/tools/native` | `f69fd01` |
| `internal/platform/tools/mcp` | `pkg/agentcore/tools/mcp` | `9a46224` |
| `config.MCPServerSettings` + `config.MCPCallTimeout()`（mcp 包内读全局配置） | `mcp.ServerSettings{CallTimeout}`，由 wiring `withMCPCallTimeout` 填值 | `0d0201c` |
| `ToolResult.Metadata["is_error"]` | `ToolResult.IsError`（旧数据不兼容） | `2ee7f44` |
| `personal.AddUsage`、各处逐字段累加 | `api.Usage.Add` | `011c560` |
| ACP `turnUsage` 收集器 | `personal.RunResult.ModelCalls` / `LastInputTokens` | `63ba336` |
| `native.Spec.Write`、chat `chatReadOnlyTools`、ACP 按名字的 `toolKind` | 工具元数据 `tools.Meta`（`ToolMeta()`），`tools.MetaOf` / `IsMCP` / `MCPServerOf`；ACP `Runtime.IsMCPTool` 改为 `LookupTool` | `b964691` |

## 归档文档（T4）

以下文档从 `pkg/agentcore/docs/` 原样移到 [agentcore-2026-10/](agentcore-2026-10/)（删除前提交 `ff052ac`）。原因：多为 2026-10 之前的设计稿、路线图、发布准备与迁移指南，内容与重构后的分层不再一致；保留的 getting-started、architecture、api、runtime-policies、host-integration 按新结构改写。

- [block-result-design.md](agentcore-2026-10/block-result-design.md)：Agentcore Block Result Design
- [prompt-layered-design.md](agentcore-2026-10/prompt-layered-design.md)：Agentcore Prompt Layered Builder Design
- [streaming-design.md](agentcore-2026-10/streaming-design.md)：Agentcore Streaming Design
- [context-presets-design.md](agentcore-2026-10/context-presets-design.md)：Agentcore Context Presets Design
- [working-memory-design.md](agentcore-2026-10/working-memory-design.md)：Agentcore Working Memory Design
- [roadmap.md](agentcore-2026-10/roadmap.md)：Agentcore Roadmap
- [release-readiness.md](agentcore-2026-10/release-readiness.md)：Agentcore Release Readiness
- [migration.md](agentcore-2026-10/migration.md)：Agentcore Migration Guide
- [versioning.md](agentcore-2026-10/versioning.md)：Agentcore Versioning
- [compatibility.md](agentcore-2026-10/compatibility.md)：Agentcore Compatibility
- [kernel-stability.md](agentcore-2026-10/kernel-stability.md)：Kernel Stability
- [adoption-paths.md](agentcore-2026-10/adoption-paths.md)：Agentcore Adoption Paths
- [contract-matrix.md](agentcore-2026-10/contract-matrix.md)：Contract Matrix
- [conformance-testing.md](agentcore-2026-10/conformance-testing.md)：Agentcore Conformance Testing
- [host-conformance.md](agentcore-2026-10/host-conformance.md)：Host Conformance
- [embedding-patterns.md](agentcore-2026-10/embedding-patterns.md)：Agentcore Embedding Patterns
- [recommended-runtime.md](agentcore-2026-10/recommended-runtime.md)：Agentcore Recommended Runtime Path
