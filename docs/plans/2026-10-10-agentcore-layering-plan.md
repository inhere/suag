<!-- template_id: plan; template_version: 1.2.0 -->
# agentcore 独立分层：核心下沉与清理 实施计划

> 状态：Draft 0.1 / 待人工计划批准

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-10 | Claude | 初稿 |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

## 目标与完成定义

思考模式 RIGOROUS；核心目标：落地设计 0.3 的 W1–W5，使 `pkg/agentcore` 成为不依赖 `internal/` 的独立分层库，suag 行为不变；scope freeze：设计 0.3 的范围、D1–D7 与 Q1–Q3 推荐；review budget：一轮合并轴评审（低暴露度，SR1409）；停止条件：评审无 CORE_BLOCKING 且用户批准。

完成定义：

1. 守卫测试通过：`go list -deps -test ./pkg/agentcore/...` 不含 `github.com/inhere/suag/internal/` 前缀的包。
2. `go test -race ./...`、`go vet ./...`（含 `examples/`）、`make smoke-chat` 全部通过；主机 Windows `go test ./...` 通过；`make install` 后经 gofer 用真实 provider 跑一次带工具调用的 `suag run --allow-write`，结果正常。
3. 设计列出的删除项均已删除，`docs/archive/removed-2026-10.md` 逐项记录删除前提交与找回命令。
4. 同一概念一份实现：工具注册表（`tools.Registry`）、用量累加（`api.Usage.Add`）、只读/MCP 判定（工具元数据 + `tools.IsMCP`）。
5. W4 的审批表格测试在改实现前后结果一致。

## 范围、排除项与授权

运行目录：Git root `inhere-tools/suag`（容器路径 `/d/work/inhere/my-tools-dev/inhere-tools/suag`）；`AC = pkg/agentcore`。

范围：设计 0.3「范围」所列目录与文件。排除：设计非目标（独立 module、会话策略下沉、删除 resume/authority/Artifact、改历史设计/计划文档、重写 personal、旧数据兼容）。

授权：本计划批准即授权按任务本地原子提交（SR1211），以及下列验证动作：经 `gofer job run -p my-tools-dev -a exec --runner local --cwd inhere-tools/suag --sync` 在主机跑 `go test ./...` 与 `make install`，经 `gofer job run --runner server -p my-tools-dev -a suag --sync` 用已配置 provider 跑一次真实回归（少量 token）。push、发版不在授权内。

- `host_or_non_offline_action=REQUIRED`

## 输入与批准证据

- 设计：`docs/design/2026-10-10-agentcore-layering-design.md` Draft 0.3（6b8fa5e，修订表顺序修正 9f88700）；用户原话：“还在v0且个人使用中，无需做兼容，按最优考虑；批准设计 0.2，按推荐”。
- 设计评审：合并轴评审 BLOCKED（F1）→ 0.2 已处理 F1–F11；0.3 按“无需兼容”收窄 W2。
- 规模事实：导入方数量 `internal/platform/tools` 50、`native` 15、`llm` 5、`skills` 4、`mcp` 2、`runtimecaps` 3；根包 `NewToolRegistry` 86 处使用（测试、示例、mock）；`FileStorage` 别名 5 处。

## Capability Discovery

### Capability decisions

| capability_id | required_capability | searched_candidates | direct_reuse | thin_adapter_or_owner_extension | decision | proven_gap | duplication_and_lifecycle_risk |
|---|---|---|---|---|---|---|---|
| CAP-01 | 分层后的 provider、工具层、内置工具、MCP、skills、用量与工具元数据 | `internal/platform/llm`、`internal/platform/tools{,/native,/mcp}`、`internal/capability/skills`、`AC` 根包与 `api`、`AC/mock`、`AC/storage/files` | 是 | 现有包整体移动到 `AC` 下并原位扩展；无新增能力，只合并重复 | DIRECT_REUSE | none | 移动与行为修改分提交；删除项统一记录，避免两份实现并存 |

### New module candidates

| candidate_id | capability_id | proposed_module | searched_candidates | direct_reuse_gap | thin_adapter_or_owner_extension_gap | proven_gap | unique_owner_and_lifecycle | deletion_or_merge_handling |
|---|---|---|---|---|---|---|---|---|
| NONE | none | none | none | none | none | none | none | none |

### Rejected new tools

| rejected_candidate | capability_id | deletion_test_and_reason |
|---|---|---|
| NONE | none | none |

## 前置检查与 fail-closed 条件

- workspace baseline：Git root `inhere-tools/suag`，branch `main`，HEAD 以 T0 实测为准，`git status --short` 为空；Go 1.25；主机 gofer 可用。
- 测试隔离：测试 `SUAG_HOME`/`HOME` 指向临时目录（wiring `TestMain` 已设置），不触碰真实 `~/.suag`。
- 每个任务：改动 → `gofmt -l` 为空 → `go build ./... && go vet ./...` → 该任务验证命令 → 提交；测试命令以输出到 `tmp/` 的日志加退出码判定，不以 grep 结果代替。
- fail-closed：任一任务后 `go test -race ./...` 回归即停止修复；W4 表格测试在改实现后有任何差异即停止；出现 import cycle 或守卫失败即停止并回到设计；单文件超 1000 行（GR104）先拆分。
- 移动类任务只用 `git mv` + 导入路径改写（可用 `gofmt -r` 或 `sed` + `goimports`），不在同一提交里改行为。

## 波次与依赖

| 波次 | 任务 | 依赖 |
|---|---|---|
| 0 | T0 基线 | — |
| 1 清理 | T1 应用层死代码 → T2 内核死代码与 mock 合并 → T3 子包与示例 → T4 文档归档与删除记录 | T0，顺序执行 |
| 2 契约 | T5 Usage.Add → T6 运行用量 → T7 IsError | T1–T4 |
| 3 下沉 | T8 根包默认存储与私有注册表 → T9 skills → T10 llm → T11 tools → T12 native → T13 mcp | T5–T7，严格按序 |
| 4 去重 | T14 审批表格测试 → T15 元数据与 MCP 判定 | T13 |
| 5 收尾 | T16 守卫与 Run 拆分 → T17 文档 → T18 全量与真实验证 | T15 |

单 Agent 顺序执行，每任务一个或两个本地提交（移动与行为分开），进度记在 gofer plan `plan-20261010-143149-80f0b78d`。

## 任务

### T0 基线

- 文件: 无
- 动作: 记录 HEAD、`git status --short`；跑全量测试与冒烟。
- 验证: `go test -race ./... > tmp/al-t0.log 2>&1; echo $?`；`make smoke-chat`。
- 完成标准: 全绿，结果记入 gofer plan 备注。
- 依赖: —

### T1 应用层死代码

- 文件: 删除 `internal/capability/runtimecaps/`；`internal/wiring/capabilities.go`（`Manager`、`toolRefs`、`registerNativeTool` 的 refs、`capabilityToolProvider` 中的 `runtimecaps.ToolProvider`）及其测试；`internal/platform/tools/mcp/provider.go`（`ListToolRefs`）及测试；`internal/platform/tools/registry.go`（`Subset`、`Execute`、未用 `Kind` 常量）及测试
- 动作: 删除并修正编译；在新建的 `docs/archive/removed-2026-10.md` 记录各项（删除前提交、原用途、找回命令）。
- 验证: `go build ./... && go vet ./...`；`go test -race ./internal/... > tmp/al-t1.log 2>&1; echo $?`。
- 完成标准: 编译与测试通过；`rg -n runtimecaps --type go` 为空。
- 依赖: T0

### T2 内核死代码与 mock 合并

- 文件: 删除 `AC/hooks/`、`AC/hooks_chain_test.go`、`AC/storage/sqlite/`；Embedding：`AC/api/provider.go`（`EmbeddingProvider`）、`internal/platform/llm/{openai,claude,pool}.go` 的 `Embed` 与 Pool embedder 参数、`OpenAIConfig.EmbedModel`、`internal/wiring/runtime.go` 的 `NewPool` 调用；`AC/runtime_helpers.go` 的 `"tool_error"` 键与过时注释；删除 `internal/platform/llm/mock.go`(+`mock_test.go`)，`AC/mock/provider.go` 增加 `WithName` 与错误排队选项，改写 `internal/personal/observe_test.go`；改写 `docs/usage/llm-mock-usage.md`
- 动作: 删除与合并；补 `AC/mock` 新选项的测试；追加删除记录。
- 验证: `go build ./... && go vet ./...`；`go test -race ./pkg/... ./internal/... > tmp/al-t2.log 2>&1; echo $?`。
- 完成标准: 测试通过；`rg -n "Embed\(|EmbeddingProvider|agentcore/hooks\"|storage/sqlite" --type go` 为空。
- 依赖: T1

### T3 无使用者子包与示例

- 文件: 删除 `AC/context/`、`AC/memory/`、`AC/prompt/`（含测试）与 `AC/runtime_memory_test.go` 中只针对这些包的用例（若有）；`AC/_examples/` → `AC/examples/`（`git mv`）；删除示例 context、memory、prompt、bridge；改写 host、recommended 去掉对已删子包的依赖；保留 minimal、custom、authority、policies、policyhost、resume、stream
- 动作: 删除、移动、改写；追加删除记录（含各示例原用途）。
- 验证: `go build ./... && go vet ./...`（此时 `examples/` 已参与）；`go test -race ./pkg/... > tmp/al-t3.log 2>&1; echo $?`。
- 完成标准: 编译、vet、测试通过；`go list ./pkg/agentcore/examples/...` 列出 9 个示例。
- 依赖: T2

### T4 文档归档与删除记录

- 文件: `AC/docs/` 中设计、路线、发布准备类篇目（block-result-design、prompt-layered-design、streaming-design、context-presets-design、working-memory-design、roadmap、release-readiness、migration、versioning、compatibility、kernel-stability、adoption-paths、contract-matrix、conformance-testing、host-conformance、embedding-patterns、recommended-runtime）`git mv` 到 `docs/archive/agentcore-2026-10/`；保留 README、architecture、api、getting-started、host-integration、runtime-policies 待 T17 改写；`docs/archive/removed-2026-10.md` 补“归档文档”一节与新旧路径对照
- 动作: 归档与记录；`AC/docs/README.md` 只保留现存篇目的目录。
- 验证: `rg -n "docs/(block-result|roadmap|release-readiness)" pkg/agentcore` 无断链引用（README 内链接随 T17 再核对）。
- 完成标准: 归档完成，删除记录覆盖 T1–T4 全部项。
- 依赖: T3

### T5 用量累加统一

- 文件: `AC/api/types.go`（`Usage.Add`）及测试；替换 `AC/runtime.go:174` 附近、`internal/personal/usage.go`（`AddUsage`）、`internal/acpserver/usage.go`、`internal/chat/chat.go` 的手写累加
- 动作: 新增方法与单测，四处改用。
- 验证: `go test -race ./pkg/agentcore/... ./internal/personal/... ./internal/acpserver/... ./internal/chat/... > tmp/al-t5.log 2>&1; echo $?`。
- 完成标准: 测试通过；`rg -n "InputTokens \+=" --type go` 只剩 `Usage.Add` 内部。
- 依赖: T4

### T6 运行用量暴露与 ACP 收集器删除

- 文件: `internal/personal/runner.go`、`internal/personal/usage.go`（`RunResult` 增加最后一次输入 token 与模型调用次数，finish 中由 `usageCollector` 填入）；删除 `internal/acpserver/usage.go` 的 `turnUsage` 并改用结果；`internal/acpserver/*_test.go` 增加“取消回合仍上报用量”用例
- 动作: 按设计 W2 第 2 条。
- 验证: `go test -race ./internal/personal/... ./internal/acpserver/... > tmp/al-t6.log 2>&1; echo $?`。
- 完成标准: 新用例通过；ACP 用量上报相关既有测试不变。
- 依赖: T5

### T7 ToolResult.IsError

- 文件: `AC/api/tools.go`（`IsError` 字段）；写入方 `AC/runtime.go:497`、`AC/runtime_helpers.go:194`；读取方 `internal/platform/llm/claude.go:115-118`、`internal/personal/observe.go:191`、`internal/personal/runner.go:500`；`rg -n '"is_error"' --type go` 列出的其余位置与测试
- 动作: 全部改用字段，删除 `Metadata["is_error"]` 约定（不兼容旧数据）。
- 验证: `go test -race ./... > tmp/al-t7.log 2>&1; echo $?`。
- 完成标准: 测试通过；`rg -n '"is_error"' --type go` 只剩 provider 协议 JSON 字段（如 Claude `is_error`）的序列化处。
- 依赖: T6

### T8 根包默认存储与私有注册表

- 文件: `AC/runtime_helpers.go`（`defaultStorage` 改为根包内存实现，实现 `Storage` 与 `ArtifactStorage`、沿用 files 的 runID 校验；`defaultTools` 改为私有空注册表）、`AC/types.go`（删 `FileStorage`/`NewFileStorage` 别名）、新文件 `AC/memstore.go`（或同名）及测试；改写 `AC/runtime_test.go:89-92,853-881`、`AC/runtime_resume_test.go:166` 与示例中的别名用法
- 动作: 按设计 W3 最后一条；根包此时不再 import `storage/files`。
- 验证: `go list -deps ./pkg/agentcore | rg storage/files` 为空；`go test -race ./pkg/... > tmp/al-t8.log 2>&1; echo $?`。
- 完成标准: 测试通过；非法 runID 仍报 `*StorageError`。
- 依赖: T7

### T9 skills 下沉

- 文件: `git mv internal/capability/skills pkg/agentcore/skills`；4 个导入方改路径
- 动作: 纯移动提交。
- 验证: `go build ./... && go test -race ./pkg/agentcore/skills/... ./internal/wiring/... > tmp/al-t9.log 2>&1; echo $?`。
- 完成标准: 测试通过；`internal/capability/` 为空目录已删除。
- 依赖: T8

### T10 llm 下沉

- 文件: `git mv internal/platform/llm pkg/agentcore/llm`；5 个导入方改路径（含 `internal/config`）
- 动作: 纯移动提交。
- 验证: `go build ./... && go test -race ./pkg/agentcore/llm/... ./internal/config/... ./internal/wiring/... > tmp/al-t10.log 2>&1; echo $?`。
- 完成标准: 测试通过。
- 依赖: T9

### T11 tools 下沉与注册表合一

- 文件: 行为提交：`internal/platform/tools/registry.go`（`Register` 接收 `api.Tool`，`Kind()` 改可选接口，缺省 native；保持 panic/TryRegister/MustRegister 语义）、依赖 `AC/testing` 的测试改为 `package tools_test`。移动提交：`git mv internal/platform/tools/{approval,asker,registry,visibility}*.go pkg/agentcore/tools/`；50 个导入方改路径。收口提交：删除 `AC/tools.go` 的导出 `Registry`/`NewToolRegistry`，86 处使用改为 `tools.NewRegistry()`（同名注册改用 `MustRegister` 以保持覆盖语义），`AC/mock`、`AC/testing` 同步。
- 动作: 三个提交按上述顺序。
- 验证: 每个提交后 `go build ./... && go vet ./...`；最后 `go test -race ./... > tmp/al-t11.log 2>&1; echo $?`；`go list -deps ./pkg/agentcore | rg 'agentcore/tools$'` 为空（根包不依赖 tools）。
- 完成标准: 测试通过；根包无导出注册表。
- 依赖: T10

### T12 native 下沉

- 文件: `git mv internal/platform/tools/native pkg/agentcore/tools/native`；15 个导入方改路径
- 动作: 纯移动提交。
- 验证: `go build ./... && go test -race ./pkg/agentcore/tools/... ./internal/wiring/... ./internal/acpserver/... > tmp/al-t12.log 2>&1; echo $?`；主机 `gofer job run … -- go test ./pkg/agentcore/tools/native/` （含 Windows shell 用例）。
- 完成标准: 两端测试通过。
- 依赖: T11

### T13 mcp 解耦与下沉

- 文件: 行为提交：`internal/platform/tools/mcp/provider.go`（`ServerSettings` 本包类型，含 `CallTimeout`，不读全局配置）及测试改用本包类型；`internal/config/mcp.go` 改为产出 `mcp.ServerSettings`；`internal/acpserver/server.go`、`policy.go`、`internal/cli/acp.go`、`internal/wiring/*` 同步。移动提交：`git mv internal/platform/tools/mcp pkg/agentcore/tools/mcp`，2 个导入方改路径；`internal/platform/` 清空后删除。
- 动作: 两个提交。
- 验证: `go test -race ./... > tmp/al-t13.log 2>&1; echo $?`。
- 完成标准: 测试通过；`internal/platform` 不存在。
- 依赖: T12

### T14 审批表格测试（锁定现状）

- 文件: `internal/wiring/approval_table_test.go`(新)、`internal/acpserver/policy_table_test.go`(新)
- 动作: 行 = 旧硬编码只读集合 ∪ Catalog ∪ `update_plan`/`tool_search`/`skill` ∪ 未知工具名 ∪ 与内置同名的 MCP 工具；列 = chat ask/auto/always 是否询问、CLI 只读模式与 `read_only_allow`、ACP `toolKind` 与 read_only 下是否拒绝、nil tool。单独提交。
- 验证: `go test -race ./internal/wiring/ ./internal/acpserver/ > tmp/al-t14.log 2>&1; echo $?`。
- 完成标准: 表格测试在现实现上通过。
- 依赖: T13

### T15 工具元数据与 MCP 判定统一

- 文件: `AC/tools/registry.go` 或 `AC/tools/meta.go`（中性类别与只读的可选接口、`IsMCP(tool)`、`MCPServerOf(tool)`）；`AC/tools/native/catalog.go` 与各工具、`update_plan`、`tool_search`、`skill` 实现元数据；`internal/wiring/chat_approver.go`（只读集合派生）、`internal/acpserver/policy.go`（`toolKind` 由类别映射，`BeforeTool` 经注册表 `Get`）、`internal/wiring/cli_mcp.go`、`internal/cli/acp.go`、`AC/tools/visibility.go`
- 动作: 按设计 W4；不改 T14 表格。
- 验证: `go test -race ./... > tmp/al-t15.log 2>&1; echo $?`；反向验证：把某只读工具的元数据改为写，T14 表格测试失败。
- 完成标准: 测试通过且 T14 表格未改；`rg -n "chatReadOnlyTools|case \"file_read\"" --type go` 不再有硬编码清单。
- 依赖: T14

### T16 守卫测试与 Run 拆分

- 文件: `AC/layering_test.go`(新，或 `internal/` 外的架构测试文件)；`internal/personal/runner.go`（同包拆出 transcript 回放与钩子组装函数）
- 动作: 守卫执行 `go list -deps -test ./pkg/agentcore/...` 并按完整前缀判定；Run 拆分不改行为。
- 验证: `go test -race ./... > tmp/al-t16.log 2>&1; echo $?`；反向验证：临时在 `AC` 某测试 import 一个 internal 包，守卫失败。
- 完成标准: 守卫与全量测试通过；`runner.go` 的 `Run` 主体显著缩短。
- 依赖: T15

### T17 文档

- 文件: `AC/README.md`（分层图、最小只读 Agent 示例、写工具经 `tools.NewApproval`、默认不持久化）、`AC/docs/{README,architecture,api,getting-started,host-integration,runtime-policies}.md`、示例 host/recommended 改用 `llm`/`tools`、`AGENTS.md`、`README.md`、`README.zh-CN.md`、`docs/README.md`、`docs/usage/gofer-personal-agent.md` 中的路径；`docs/archive/removed-2026-10.md` 定稿（含新旧路径对照）
- 动作: 改写与核对链接。
- 验证: `go vet ./...`（示例）；`rg -n "internal/platform|internal/capability|_examples" --glob '!docs/archive/**' --glob '!docs/design/**' --glob '!docs/plans/**'` 为空。
- 完成标准: 无过时路径；README 示例可编译（以 examples 中对应程序为准）。
- 依赖: T16

### T18 全量与真实验证

- 文件: 无
- 动作: 全量测试与冒烟；主机测试与安装；真实回归；推送前 CI 由用户另行决定。
- 验证: `go test -race ./...`；`go vet ./...`；`make smoke-chat`；主机 `gofer job run -p my-tools-dev -a exec --runner local --cwd inhere-tools/suag --sync -- go test ./...` 与 `-- make install`；`gofer job run --runner server -p my-tools-dev -a suag --sync --prompt '用 file_grep 找到 DefaultShellTimeout 的定义并用 shell_exec 运行 go version'`，`suag trace` 核对工具调用。
- 完成标准: 全部通过；真实运行工具调用正常。
- 依赖: T17

## 回滚与恢复

- 每任务独立提交（移动与行为分开），可按提交 `git revert`；T8 前是“清理完成”的恢复点，T13 后是“下沉完成”的恢复点。
- 删除项可按删除记录 `git show <commit>:<path>` 找回。
- 中断时按 SR1407 记录 HEAD、dirty、首个失败边界与下一条命令到 gofer plan 交接说明。

## 人工 Gate

1. 本计划批准（含主机测试、安装与一次真实 provider 回归）。
2. push、发版另行批准。

## 可追溯性

| 设计项 | 任务 | 验证 |
|---|---|---|
| W1 清理与删除记录、D5、Q1 | T1–T4 | 编译/vet/测试；删除记录 |
| W2 契约、D7 | T5–T7 | 定向测试；`rg` 无残留 |
| W3 下沉、D1–D4、D6、Q2 | T8–T13 | 每步测试；根包不依赖 tools/files |
| W4 去重 | T14–T15 | 表格测试前后一致；反向验证 |
| W5 护栏与文档、Q3 | T16–T17 | 守卫测试与反向验证；链接核对 |
| 完成定义 | T18 | 全量、主机、真实回归 |

## 完成 Gate 与剩余工作

T18 后向用户报告并按 plan-closure 询问是否复盘。剩余：push 与 CI（待批准）；独立 Go module（待第二个使用者）；suag 待办（cflag 帮助文案、Claude thinking、Windows 人工确认等）不在本计划内。
