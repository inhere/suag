# Host Learning Retrieval Phase 7 Plan

> **Goal:** 在不引入 SQLite FTS5、向量检索或复杂排序策略的前提下，把当前 formal memory retrieval 从“整文件全量注入”升级为“最小 query-aware recall”，验证 Host v0 是否已经能按当前用户任务意图收窄注入内容。

**Architecture:** 继续保持 `pkg/agentcore` 只负责 runtime kernel。Phase 7 只增强 `internal/host/intrunner/memory_recall.go`：先把 `MEMORY.md / USER.md` 解析成条目，再根据当前 turn instruction 做轻量关键词匹配；命中时只注入命中条目，无命中时回退到每个来源的少量 baseline。仍然不引入 FTS5，不改变 session transcript contract。

**Tech Stack:** Go 1.24, existing `internal/host/intrunner`, filesystem markdown memory files, `go test`

---

## Scope

本阶段只覆盖 Host internal runner 的最小 query-aware recall，不包含：

- SQLite + FTS5 全文检索
- 向量召回
- 权重学习或复杂相关性排序
- ACP runner 远端 retrieval 对齐
- workspace / project 级 scope 策略

本阶段完成后，应满足：

- intrunner 会根据当前 `Instruction` 优先注入匹配条目
- 没有匹配条目时，会回退到每个来源的少量 baseline
- `memory_recall` 元数据能区分：
  - `strategy`
  - `matched_entry_count`
  - `fallback_used`
- 文档清晰说明当前只是 file-based query-aware lite，不是最终 memory system

---

## Target Design

### 1. Matching Strategy

采用极简 file-based 规则：

- 把当前 `Instruction` 做小写化、去空白、按非字母数字边界切词
- 忽略很短、噪音较大的 token
- memory line 中只要包含任一有效 token，即视为 matched

当前不做：

- 词形还原
- 同义词扩展
- 复杂评分

### 2. Injection Rule

- 有 matched 条目时：
  - 每个来源只注入 matched lines
- 无 matched 条目时：
  - 每个来源最多回退前 `1-2` 条 baseline

### 3. Observability

继续沿用 `memory_recall`，并补充：

- `strategy`
  - 当前值：`query_aware_lite`
- `matched_entry_count`
- `fallback_used`

---

## Task 1: Query-Aware Recall Lite

- [x] 命中时只注入匹配条目
- [x] 无命中时回退到少量 baseline
- [x] 不影响 recalled memory 不回写 history 的现有边界

Run: `go test ./internal/host/intrunner -run "QueryAware|Recall" -v`

---

## Task 2: Keep Observability Stable

- [x] `memory_recall` 补齐 `strategy / matched_entry_count / fallback_used`
- [x] snapshot / run detail 可继续查看 recall 观测结果
- [x] 文档同步更新

Run: `go test ./internal/host/intrunner ./internal/host/agui ./internal/host/gateway`

---

## Verification

- [x] `go test ./internal/host/intrunner -run "TestRunner_RunReturnsFormalMemoryRecallMetadata|TestRunner_RunUsesQueryAwareRecallWhenInstructionMatches|TestRunner_RunFallsBackWhenInstructionHasNoMemoryMatch" -v`
- [x] `go test ./internal/host/intrunner ./internal/host/agui ./internal/host/gateway`

---

## Exit Criteria

- intrunner 会优先注入匹配条目
- 无命中时仍有可控 fallback
- `memory_recall` 能区分命中和回退
- 测试通过，文档与代码一致
