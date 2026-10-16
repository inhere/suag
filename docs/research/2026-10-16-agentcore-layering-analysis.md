# agentcore 分层审查（2026-10-16）

回答一个问题：`pkg/agentcore` 的层次划分**是否成立、是否被实际遵守、守卫挡不挡得住声明的规则**。

基线 `a3fee80`（2026-10-16，v0.25.0）。证据来自四条静态路径——分层声明（`pkg/agentcore/README.md`、`pkg/agentcore/docs/architecture.md`、`docs/design/2026-10-10-agentcore-layering-design.md`）、守卫测试源码（`pkg/agentcore/layering_test.go`）、`go list` 实测依赖图、全仓 grep；另有三路只读子调查（`tasks→llm`、应用层去重核查、泄漏与环境耦合）。

守卫测试实跑过一次：`go test ./pkg/agentcore/ -run TestLayeringNoAppImports -count=1` → `ok`（0.316s）。除此之外**没有运行时验证**（本文不改代码、不跑行为测试）。本文是分析记录，不是实现合同；落地前按 AGENTS.md 另出 `docs/design/` 设计与计划。

## 1. 分层声明（文档怎么说）

| 来源 | 声明 |
|---|---|
| `pkg/agentcore/README.md:16-31` | 三层：应用层 `internal/*` → 能力层（`llm`、`schedule`、`tools`、`tools/native`、`tools/mcp`、`skills`、`memory`、`redact`、`extproc`、`storage/files`、`mock`、`testing`）→ 内核（根包 + `api`） |
| `pkg/agentcore/README.md:33` | "依赖只能自上而下"；"根包不依赖能力层"；守卫判据是"agentcore 下任包（含测试）依赖了 `pkg/agentcore` 之外的 `github.com/inhere/suga/...` 即失败" |
| `pkg/agentcore/docs/architecture.md:12` | 本文只描述内核；能力层列举为 `llm`、`tools`、`tools/native`、`tools/mcp`、`skills`、`storage/files` |
| `docs/design/2026-10-10-agentcore-layering-design.md:109`（W5） | 守卫只要求"`go list -deps -test ./pkg/agentcore/...` 的结果中不得出现 `github.com/inhere/suag/internal/` 开头的包" |
| `docs/design/2026-10-10-agentcore-layering-design.md:131-141`（mermaid） | 依赖边：`wiring → llm & tools & native & mcp & skills`、`native → tools & skills`、`mcp → tools`、`llm & tools & skills & files & rt → api` |

## 2. 实测依赖图

`go list -f '{{.ImportPath}}|{{join .Imports " "}}' ./pkg/agentcore/...` 的生产代码导入（不含 `examples/`）。行数为 `Get-Content | Measure-Object -Line` 的近似值（含空行与注释；`File` 含测试文件）。

| 层 | 包 | 生产行 | 测试行 | 文件 | 依赖的 agentcore 包 |
|---|---|---|---|---|---|
| 底座 | `internal/runid` | 28 | 0 | 1 | — |
| | `internal/utils` | 49 | 0 | 3 | — |
| 契约 | `api` | 574 | 35 | 13 | `internal/utils` |
| 内核 | `agentcore`（根包） | 1903 | 6098 | 31 | `api`、`internal/runid`、`internal/utils` |
| 能力 | `skills` | 339 | 110 | 3 | — |
| | `redact` | 54 | 51 | 2 | — |
| | `schedule` | 334 | 570 | 3 | — |
| | `extproc` | 196 | 120 | 4 | — |
| | `llm` | 2902 | 1981 | 38 | `api` |
| | `tools` | 1236 | 1125 | 14 | `api` |
| | `memory` | 790 | 633 | 8 | `redact` |
| | `memory/files` | 650 | 364 | 3 | `memory` |
| | `memory/execstore` | 216 | 269 | 2 | `memory`、`extproc` |
| | `storage/files` | 272 | 335 | 2 | `api`、`internal/runid`、`internal/utils` |
| | `tasks` | 863 | 349 | 4 | **`llm`** ← 唯一横向依赖 |
| | `tools/mcp` | 524 | 361 | 4 | `api`、`extproc`、`tools` |
| | `tools/native` | 7510 | 5176 | 82 | `api`、`internal/proctree`、`memory`、`redact`、`skills`、`tasks`、`tools` |
| 宿主侧 | `mock` | 328 | 328 | 3 | `api` |
| | `testing` | 882 | 629 | 12 | **`agentcore`（根包）**、`mock`、`tools` |
| | `testutil` | 42 | 21 | 2 | — |
| | 合计 | 19787 | 18555 | 237 | |

要点：

- **内核干净**：根包生产代码只 import `api` 与 `internal/*`，**没有** import 任何能力层子包；`api` 也只 import `internal/utils`。
- **无反向依赖、无环**：`llm`/`tools` 不依赖 `tools/native`，`tools` 不依赖 `native`；`agentcore/testing` 之外的任何包都不 import 根包。
- **`tools/native` 位于能力层顶端**，依赖 6 个兄弟包（`api`、`memory`、`redact`、`skills`、`tasks`、`tools`）。
- **`internal/*` 是真实存在的第四格**：`api`、根包、`storage/files` 都依赖它，而 README 的分层图里没有这一层。

守卫与声明覆盖关系：

| 违规形态 | 设计文档 W5 要求 | `layering_test.go` 是否拦得住 |
|---|---|---|
| 依赖 `internal/*`（应用层） | 要拦 | 拦得住 |
| 根包 import 能力层（README:33 明令禁止） | 未要求 | **拦不住** |
| 能力层反向 import 内核根包 | 未要求 | **拦不住**（`testing` 现在就是这样） |
| 能力层之间的横向依赖 | 未要求 | **拦不住**（`tasks→llm` 现在就是这样） |

## 3. 结论速览

1. **分层方向成立**：内核↛能力层、agentcore↛应用层都守住了，能力层内部是 DAG，没有循环。
2. **守卫强度低于 README 的说法**：`layering_test.go:29-38` 只检查"是否依赖 agentcore 之外的 suga 包"，README:33 的"依赖只能自上而下"在测试里没有任何一条对应规则。代码与*设计*（W5）一致，是 README 的表述超出了守卫能力。
3. **一处真实的层次错位**：`tasks → llm`，且只为一对 context key 读写函数，容易消除。
4. **文档与实现多处不一致**：分层图漏了 `tasks`、`testutil` 与 `internal/*`；`testing` 在图上被画进能力层，实际依赖内核根包；`architecture.md:12` 与设计文档 mermaid 的依赖边同样不全。
5. **颗粒度问题**：`tools/native` 7510 行生产代码 / 82 文件，占 agentcore 生产代码约 38%，是事实上的"第二内核"。
6. **未发现**应用层概念泄漏、`init()` 副作用、根包写临时目录（设计 D6 遵守）、能力层内的重复实现（2026-10 去重目标已达成）。

## 4. 合理之处

1. **契约与实现分离到位。** `api` 只放类型与接口（574 行生产代码，13 个文件），根包只放 Runtime 循环、hooks 与内存存储；`Config.Tools`/`Config.Storage` 为 nil 时用私有空注册表与 `MemoryStorage`（`memstore.go:20-41`），不反向 import 能力层。
2. **无应用层泄漏。** 全 `pkg/agentcore` 生产代码无 ACP 概念（无 `acp.`/`ToolKind`）、无会话分段/压缩/交接策略、无终端 UI（不依赖 `golang.org/x/term`）、无 gofer CLI 调用；跨层的 ACP 映射留在应用层（`internal/acpserver/policy.go:363-371` 的 Category→`acp.ToolKind` 表）。
3. **额外保险来自 Go 的 internal 规则。** `pkg/agentcore/internal/{runid,utils,proctree}` 只能被 `pkg/agentcore/...` 引用，编译器保证外部嵌入方拿不到，比测试守卫更硬。
4. **运行时副作用可控。** 唯一的 `init()` 副作用是 `llm/compat_openai.go:6`、`compat_deepseek.go:13`、`compat_commandcode.go:8` 向包级表注册 provider 适配器（纯内存，形如 `database/sql` 驱动注册）；无 `os.Exit`/`log.Fatal`；`panic` 只出现在 `output_rules.go:140,144`（内嵌规则解析失败）与 `registry.go:76`（重复注册），都是"不可能发生即编程错误"的不变量；根包对 `os` 的使用只有 `os.ErrNotExist`（`memstore.go:196`），确实没有隐式写临时目录。
5. **去重目标已达成**（子调查核实）：全仓只有一套工具注册表（`pkg/agentcore/tools/registry.go:59`），一个 mock（`pkg/agentcore/mock`），用量统一走 `api.Usage.Add`（`pkg/agentcore/runtime.go:197`、`internal/personal/usage.go:207`、`internal/chat/chat.go:629`），只读与"是否 MCP"统一从 `tools.MetaOf`/`tools.IsMCP` 派生（`internal/wiring/chat_approver.go:96,110`、`internal/acpserver/policy.go:135,388`），`internal/` 内没有自建 provider/HTTP/SSE/重试。

## 5. 问题

### P1 守卫只守一条边界，弱于 README 的声明（影响最大）

`pkg/agentcore/layering_test.go:13-39`：取 `go list -deps -test ./...` 的输出，凡以 `github.com/inhere/suga/` 开头且不以 `github.com/inhere/suga/pkg/agentcore` 开头的包即失败（`:29-38`）。也就是只挡"依赖应用层"。以下三种违规它全部放行：根包 import 能力层、能力层反向 import 内核、能力层之间的横向依赖——**今天存在的 `tasks→llm` 就是绿灯通过的**。

两处小瑕疵：`:29` 的 `allowed` 前缀没有尾 `/`（`pkg/agentcoreX` 理论上会被误判为合法）；`:14-17` 硬依赖 `go` 可执行文件，缺 `go` 时 `t.Fatalf` 而非 `t.Skip`。

*建议*：把 `-deps` 换成可直接分层的输出，并为每个包配一张"允许依赖"白名单（表里出现集合外的 agentcore 包即失败，同时保留现有"不得越出 agentcore"的检查）：

```bash
go list -f '{{.ImportPath}}|{{join .Imports " "}}|{{join .TestImports " "}}|{{join .XTestImports " "}}' ./pkg/agentcore/...
```

### P2 `tasks → llm`：唯一的层次错位，且容易消除

- `pkg/agentcore/tasks/tasks.go:30` import `llm`；生产代码里唯一实质用途是 `tasks.go:196-197`：`Config.Session` 为 nil 时兜底为 `cfg.Session = llm.SessionIDFrom`（`Config.Session` 定义见 `tasks.go:77-79`）。
- 被用的符号是 `llm/headers.go:16-27` 的一对 context key 读写（`WithSessionID`/`SessionIDFrom`/`sessionIDKey`）。`llm` 内部用它替换 HTTP header 里的 `{{session_id}}` 占位（`headers.go:32-43`）。
- "会话 id"是通用概念，与 provider 协议无关。当前后果是 `tasks`（后台任务池）的依赖闭包被拖进整个 `llm`（2902 行 + `net/http` + 重试 + 流式）——对一个宣称可独立嵌入的库，后台任务层不该绑住 provider 层。
- 改法：把这两个函数（含 `sessionIDKey`）移到 `api` 或 `internal/*`，`llm` 改从新位置调用，`tasks` 只依赖新位置。`llm` 已依赖 `api`，`api` 不依赖 `llm`/`tasks`，无环；全仓调用点约 6 处，改动只是 import 与限定名。
- 测试侧同源：`pkg/agentcore/tasks/tasks_test.go:19,64` 直接用 `llm.WithSessionID`，一并改。

### P3 `api` 与根包依赖 `internal/*`，而图里没有这一格

`api/hooks.go:7` import `agentcore/internal/utils`（只为 `CloneMetadata`，见 `internal/utils/metadata.go:7`），根包 `memstore.go:11` 用 `internal/runid`，`storage/files` 两个都用。合法且无环，但意味着"契约层"之下还有一层共享底座，读者按 README:16-31 的图会把 `api`/根包当成依赖树的根。*建议*：图上补一行"共享底座 `internal/*`（`runid`、`utils`、`proctree`）"。

### P4 文档与实现不一致（四处）

1. `pkg/agentcore/README.md:16-31` 的分层图**漏了 `tasks`**（863 行生产代码，被 `tools/native` 与 `internal/{wiring,chat,cli,personal,config}` 广泛使用）与 `testutil`。
2. 同一张图把 `testing`/`mock` 画在能力层，但 `agentcore/testing` 依赖内核根包（import `agentcore`+`mock`+`tools`，882 行）——图上的位置与依赖方向矛盾；它们实质是"内核之上的宿主侧测试套件"。
3. `docs/design/2026-10-10-agentcore-layering-design.md:138` 的 mermaid 写 `native --> tools & skills`，实际 `native` 依赖 6 个兄弟包（见 §2）。
4. `pkg/agentcore/docs/architecture.md:12` 列举能力层时漏了 `memory`、`tasks`、`schedule`、`extproc`、`redact`。

### P5 `tools/native` 是 god package（颗粒度，非方向问题）

7510 行生产代码 + 5176 行测试、82 个文件，一个包内容纳：文件读写/搜索、`shell`/`code_exec`、web fetch + HTML 转文本、输出治理（`minimize.go`、`output_rules*.go`、内嵌 `rules/`）、五种语言工具链探测（`lang_go/java/javascript/python/rust.go`）、危险命令判定（`danger.go`）、路径解析与读根（`pathresolve.go`、`readroots.go`、`canonpath_*.go`）、`task`/`watch`/`agent` 工具、`plan`、`memory`、`skill`、`ledger`（session_search/recall）、`manifest`、`ignore`。方向（能力层顶端）没问题，但任何新工具都落在这里，且它同时依赖 6 个兄弟包。若要动，只能按域拆包（如 `native/fs`、`native/exec`、`native/web`、`native/outputrules`），成本高，不建议顺手做。

### P6 库内硬编码 suga 名字（轻微，与"可独立嵌入"目标摩擦）

- `llm/headers.go:10` `DefaultUserAgent = "suga"`；`:14` `SessionIDPlaceholder = "{{session_id}}"`。
- 跨层 wire 常量：`api/types.go:39` `ToolBatchMetaKey = "suga.tool_batch"`、`llm/claude.go:41,48,55` 与 `llm/responses.go:15` 的 `suga.*` metadata key（其中 `SegmentContextMetaKey` 是宿主必须写同一个 key 的约定，见 `pkg/agentcore/README.md:93`）。
- 默认布局：`memory/files/files.go:49` `repoEntryDir = ".suga/memory"`（`:8-10` 注释含 `~/.suga/...`）。
- 环境名：`tools/native/code_exec.go:380` `privateEnv = []string{"SUGA_HOME","SUGA_CONFIG","SUGA_MODEL"}`（从子进程剥离，见 `:378` 注释）；`extproc/env.go:8` 的凭据白名单（`ANTHROPIC_API_KEY` 等）属 provider 凭据，合理。
- 描述文本写死后端：`tools/native/memory.go:40,80` 提到 gofer 项目记忆后端。

这些都不破坏分层，是命名/默认值层面的耦合；要真正独立成库，应由宿主注入。改名会同时改动宿主约定（`pkg/agentcore/README.md:93`），需一并处理。

## 6. 建议的最小整改（三项，互不耦合，可分别提交）

1. **加固守卫（P1）**：`layering_test.go` 增加"每包允许依赖的 agentcore 包集合"白名单表（今天的实际状态即基线，除了 P2 修完后的 `tasks`），保留"不得越出 pkg/agentcore"的检查；顺带给 `allowed` 补尾 `/`。验收：表外的依赖会失败（临时给 `tools` 加一行 `import agentcore/tasks` 应报错，验证后删掉）。
2. **消除 `tasks→llm`（P2）**：把 `WithSessionID`/`SessionIDFrom` 下沉到 `api`（或 `internal/*`），改 `llm` 与 `tasks` 的调用点。验收：`go list` 显示 `tasks` 不再依赖 `llm`；`go test -race ./...` 通过。
3. **修文档（P3/P4）**：`pkg/agentcore/README.md` 分层图补 `tasks`/`testutil`/`internal/*`，把 `testing`/`mock` 移到内核之上；同步 `pkg/agentcore/docs/architecture.md:12` 与设计文档 mermaid 的依赖边。验收：文档列举的包集合与 `go list ./pkg/agentcore/...` 的输出一致。

P5、P6 建议只记录，不在本轮动手。

## 7. 未验证与不确定

- 除 `TestLayeringNoAppImports`（实跑通过）外，没有跑任何测试或行为验证；行数统计为近似值。
- `native` 的 `lang_*`、`rules/`、`output_rules_rewrite.go` 等子功能只按文件名与导入判断归属，未逐个阅读其实现；"god package"是规模与职责判断，不是行为缺陷。
- P6 中哪些命名是"故意保留的宿主约定"（如 `SegmentContextMetaKey`）由设计决定，本文只记录现状。
- 应用层 `internal/*` 之间的依赖方向不在本次审查范围内（守卫只覆盖 agentcore 一侧）；如需一并加固，另出方案。
