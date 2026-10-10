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
