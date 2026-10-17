<!-- template_id: plan; template_version: 1.2.0 -->
# 配置的优先工具实施计划

> 状态：Draft 0.2 / 待用户审批

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-17 | suga（gofer job） | 依据 `docs/design/2026-10-17-preferred-tools-design.md` Draft 0.1，拆分通用配置/探测、runtime 提示与延迟、manifest/trace、验证 Gate。 |
| 0.2 | 2026-10-17 | 监督（Claude） | 跟随设计 0.2 审核简化：去掉 manifest MCP probe、分段 MCP 健康检查与 provider health seam、子 agent 接线、trace 计数/hash；T0 的 P5 与 Gate B 随之取消。 |

## 0.2 审核简化（与下文冲突时以本节为准；对应任务里的 MCP probe / health seam / 子 agent / hash 内容不做）

按用户“简化，少依赖定制”的方向，审核收窄四处：

1. **manifest 不启动 MCP**（取代 Q1 的 probe）：`suga manifest` 保持“不启动 MCP”的现有行为；`mcp` 类优先项只报告 `configured`（server id 是否在 `mcp.servers` 中）及“连接状态在会话启动时确认”，不做 initialize/`tools/list`。`command` 类照常 `LookPath`。
2. **不做分段 MCP 健康检查**（取代 Q2）：MCP 优先项以 runtime 启动时 provider 注册成功为准，整个会话不变；中途断开沿用现有工具调用错误，agent 自行改用其他工具。不新增 provider 健康接口。`command` 类在新 segment 重新 `LookPath`（开销小，保留）。
3. **子 agent 不列优先工具**（取代 Q3）：子 agent 没有 shell 与 MCP，列出不能用的工具只是噪音；子 agent 的环境说明不变。
4. **trace 只记名字**：`turn_context` 只加 `preferred_tools`（本回合列出的名字），不加 `preferred_count`/`preferred_hash`。

## 目标与完成定义

目标：实现 `suga-jipz` 的最小通用优先工具能力，支持 `tools.preferred: [{name, kind, when}]`，不为具体 command/MCP 做专用适配。

完成定义：

1. `suga config set tools.preferred '<YAML inline value>'` 与直接编辑都能通过同一 registry/reader 校验；错误列表不写回，`config get/list/unset` 不泄露或生成专用存储。
2. command 用跨平台 PATH 查找（Windows 覆盖 `.exe`/`.cmd`/`PATHEXT`），MCP 只在配置 server、启动并 `tools/list` 成功时 present；不存在项不进入 prompt。
3. run、chat、ACP 及只读子 agent 的环境说明均有清晰的优先工具提示；列表进入 segment context，不改变 stable system prompt；新 segment 能反映 command 安装和 MCP 健康变化。
4. MCP preferred 工具不进入 deferred 集合；`tools.deferred: auto|always|never` 对其他 MCP 保持原语义；preferred 不自动放行，现有 approval/allowlist 仍生效。
5. `suga manifest --json` 显示每个声明项的 `present`/`reason`，`turn_context` 记录本回合列出的 preferred names/count/hash；不记录参数、env、secret 或工具输出。
6. 近端单测、Windows 主机命令、Linux PTY/反向验证和真实 rg/xerj/已配置 MCP 验证均有证据；未能执行的 external gate 标为 `UNVERIFIED_*`，不以 fake LLM 冒充真实环境。
7. 本计划获批后每个功能点原子本地提交，提交前只 stage 对应文件；不 push、不改 `.gofer/tracker`。

## 范围、排除项与授权

- 范围：`internal/config` registry/reader、`internal/wiring` 通用解析与 runtime/manifest 接线、`internal/personal` turn_context 观测、必要的 `pkg/agentcore/tools` visibility seam、测试、Linux `scripts/ptysmoke` 和文档验证证据。
- 不做：内置 grep/find 替换、rg/xerj/codebase-memory-mcp/codegraph 专用客户端或输出解析、自动安装/建索引/放行、工具用法专用存储、provider 协议修改。
- 兼容边界：不改变现有 MCP provider 的启动错误、工具调用错误、approval 和 `tools.deferred` 非 preferred 行为；若要改变这些语义，停止并回到设计 Gate。
- 配置：`name` 不带参数；MCP name 按 server id 匹配；列表整值替换，不支持 element-index path。
- `host_or_non_offline_action=REQUIRED`：Windows 主机的 build/vet/test/staticcheck，Linux 容器的 PTY/反向冒烟，以及使用主机已安装 rg、xerj 和一个已配置 MCP 的真实存在性/manifest 验证，均是实施完成 Gate 的外部环境动作，计划阶段不执行。
- 授权：用户批准设计和本计划后才实施；批准仅授权当前 worktree 的本地修改和提交，不授权 push/tag/release/tracker 修改。

## 输入与批准证据

- 设计：`docs/design/2026-10-17-preferred-tools-design.md`，Draft 0.1 / 待用户审批。
- issue：`suga-jipz`，最后一条评论“方向调整（用户 2026-10-17）”确定为简化方案：只探测存在、提示优先、MCP 常驻、manifest 显示 reason、command 不自动放行。
- 环境/segment 事实：`internal/wiring/prompt.go:26-42,106-154`、`internal/wiring/runtime.go:553-595`、`internal/personal/segment.go:38-117`、`internal/personal/context.go:215-222,337-355`。
- 配置写回事实：`internal/config/settings_registry.go:18-47,85-184,277-363`、`internal/cli/config.go:60-89,151-204`。
- MCP/visibility/approval 事实：`internal/wiring/capabilities.go:48-79`、`pkg/agentcore/tools/mcp/provider.go:40-41,69-106`、`pkg/agentcore/tools/visibility.go:117-170`、`internal/wiring/approval_policy.go:57-129`。
- 入口/观测事实：`internal/cli/run.go:76-145`、`internal/cli/acp.go:31-73`、`internal/wiring/subagent.go:18-52,124-188`、`internal/personal/turncontext.go:28-75`、`internal/wiring/manifest.go:29-42,174-218`。
- 实施前必须重新确认 HEAD、branch、dirty 状态；代码事实与设计不一致时停止进入 T1。

## Capability Discovery

### Capability decisions

| capability_id | required_capability | searched_candidates | direct_reuse | thin_adapter_or_owner_extension | decision | proven_gap | duplication_and_lifecycle_risk |
|---|---|---|---|---|---|---|---|
| CAP-01 | 解析并校验 `tools.preferred` 列表，接入 `suga config set` | `internal/config/settings_registry.go:18-47,277-363`；`internal/cli/config.go:60-89,151-204`；现有 YAML writer/Reload | `ConfigKey`、`ConfigTypeYAML`、`ParseConfigValue`、`ValidateConfigCandidate`、`config set` | `internal/config` 增加一个 preferred reader/typed effective value；CLI 不新增 writer | MINIMAL_NEW_MODULE | 当前无列表项 schema、kind/name/duplicate 校验 | 不能在 CLI 另做 JSON/YAML 解析；reader 是唯一形状 owner |
| CAP-02 | command/MCP 仅存在性探测和稳定 reason | `internal/wiring/capabilities.go:48-79`；`pkg/agentcore/tools/mcp/provider.go:69-106`；Go `exec.LookPath` | 已注册 MCP tool/server 元数据、provider startup/list timeout、标准库 PATH | `internal/wiring/preferred_tools.go` 统一解析器；必要时 MCP provider 增加通用 health seam | MINIMAL_NEW_MODULE | 当前没有 preferred 状态模型或 manifest reason | 不得按 rg/xerj/codegraph 分支；runtime 与 manifest 不应各探测一遍 |
| CAP-03 | present 优先 MCP 不被 `tools.deferred` 延迟 | `pkg/agentcore/tools/visibility.go:117-170,240-328`；`internal/wiring/runtime.go:497-508` | `Visibility` 的 MCP 分类、direct/deferred 集合、现有 tool_search | 在 `VisibilityOptions` 增加 pinned names/server tools；wiring 传入结果 | THIN_ADAPTER | 现有 deferred 只按 mode/大小，没有保留名单 | pinned 只改变可见性，不得进入 approval 或改变非 preferred MCP |
| CAP-04 | environment/segment/三入口统一列出 | `internal/wiring/prompt.go:106-154`；`internal/personal/segment.go:83-117`；`internal/wiring/runtime.go:553-595`；`subagent.go:226-240` | `EnvironmentSource`、segment snapshot、OpenCLI/Open、sub-agent environment | prompt 增加一个纯渲染段；wiring 捕获解析快照；子 agent 仅展示并声明不可调用 | DIRECT_REUSE | 当前 environment 没有 preferred section | 不要把动态列表放 stable `basePrompt`；不得在 run/chat/ACP 各复制探测 |
| CAP-05 | manifest reason 和 turn_context 观测 | `internal/wiring/manifest.go:29-42,174-218`；`internal/cli/manifest.go:15-40`；`internal/personal/turncontext.go:28-75` | Manifest JSON/text、现有 turn_context attrs/trace JSON | 扩展 preferred 字段和 attrs；manifest probe 复用 resolver | OWNER_EXTENSION | 当前 manifest 没 preferred 字段，turn_context 无 preferred names | manifest 保留 false 项，prompt/trace 只记安全的 present name；不能把 args/env 写出 |
 CAP-06 | 真实入口、PTY、Windows、真实工具验证 | `internal/cli/run.go:76-175`；`internal/cli/acp.go:31-73`；`scripts/ptysmoke/README.md`；Makefile smoke list | 现有 fakeLLM/PtySession、trace JSON、Go tests、主机 job | 新 preferred PTY 场景和 resolver/manifest fixture | OWNER_EXTENSION | 当前无优先工具场景和反向检查 | PTY 只在 Linux/macOS；主机 rg/xerj/MCP 才能证明外部环境语义 |

### New module candidates

| candidate_id | capability_id | proposed_module | searched_candidates | direct_reuse_gap | thin_adapter_or_owner_extension_gap | proven_gap | unique_owner_and_lifecycle | deletion_or_merge_handling |
|---|---|---|---|---|---|---|---|---|
| MOD-PT-01 | CAP-01 | `internal/config/preferred_tools.go` | settings registry、config YAML parser/writer | 没有 nested list item reader/duplicate validation | 只新增 schema/reader，不接触文件写回 | `tools.preferred` 尚不存在 | 配置形状和有效值的唯一 owner | 删除后 config set 应回到 unknown key；不保留 CLI 特判 |
| MOD-PT-02 | CAP-02 | `internal/wiring/preferred_tools.go` | MCP capabilities、manifest builder、native runtime assembly | 没有跨 command/MCP 的 present/reason 结果 | 薄 resolver + manifest/environment adapter | 需要一次统一快照和不同 probe reason | runtime 与 manifest 共用 probe 结果协议 | 删除后提示/manifest 回到当前无 preferred 行为；不得留下专用工具客户端 |

### Rejected new tools

| rejected_candidate | capability_id | deletion_test_and_reason |
|---|---|---|
| `rg`/`fd` 专用 native replacement | CAP-02 | 删除后 command preferred 仍能通过 `LookPath` 出现在提示；专用替换会扩大边界并解析输出，违反非目标。 |
| `xerj` 专用 client、索引 status/autoindex | CAP-02 | 删除后 `xerj` 仍作为 command name 被发现；任务不需要 URL、API key、索引或 `_search` 语义。 |
| codebase-memory-mcp/codegraph 专用 MCP adapter | CAP-02 | 删除后任意 `mcp.servers[].id` 均可用同一 server/tool-list 流程；不建立服务耦合。 |
| preferred MCP 专用延迟绕过工具 | CAP-03 | 删除后通用 `Visibility` pinned option 仍可表达“不延迟”；不建立服务耦合。 |
| `preferred_allow` 自动审批规则 | CAP-03 | 删除后现有 `tools.auto_allow`/`read_only_allow` 仍可显式放行；“优先”不应变成权限。 |
| 工具用法数据库/skill 自动生成器 | CAP-04 | 删除后固定文案加现有 memory 足以记录经验；避免第三套持久化来源。 |

## 前置检查

- P0：记录 `git status --short`、HEAD、branch；确认只有本任务文档或已知基线改动。提交前每次只 stage 对应文件，并检查 `git diff --stat`、`git diff --cached --name-only`。
- P1：重新读设计和 issue 最后一条评论，确认状态仍为待审批；确认 `tools.preferred` 在 registry 和 config sample 中尚不存在或与设计一致。
- P2：运行近端基线：`go test -count=1 ./internal/config/... ./internal/wiring/... ./internal/personal/... ./internal/cli/...`；记录 exit code 和原始输出。基线失败则标 `BASELINE_FAILED`，不进入实现。
- P3：核对 `goccy/go-yaml` 对 flow-style list、mapping、引号和注释保留；不接受改用 map marshal 的替代方案。
- P4：确认 Go `exec.LookPath` 的 Windows 行为和测试策略：测试使用临时 PATH、临时 `.exe`/`.cmd` fixture（仅在 Windows 测试中验证 PATHEXT），非 Windows 不写 Windows 绝对路径字面量。
- P5：确认 MCP provider 是否能在不暴露 args/env 的前提下提供通用 health/list seam；若只能重复启动服务或改变现有失败语义，停止并回到 Q1/Q2 Gate。
- P6：确认现有 `scripts/ptysmoke` 驱动真实 binary/fakellm、`suga trace --json` 和 `make smoke-chat` 接入方式；不把 PTY 场景留给人工。

## 波次与依赖

| 波次 | 任务 | 依赖 | 执行方式 |
|---|---|---|---|
| W0 | T0 前置检查与 Gate | 无 | 只读；基线失败停止 |
| W1 | T1 配置 reader 与 preferred resolver | T0 | 串行；先近端 config/resolver 测试后提交 |
| W2 | T2 runtime 环境、MCP pinning 与三入口接线 | T1 | wiring/visibility/prompt 串行合入 |
| W3 | T3 manifest、turn_context 和安全文本观测 | T1/T2 | 先 unit，再 CLI/trace 集成测试 |
| W4 | T4 PTY、反向验证、真实环境与 Windows Gate | T1–T3 | Linux/macOS PTY + 主机 job；修复回所属功能点原子提交 |

## 任务

### T0 前置检查与实现 Gate

- 文件：无产品改动；证据写入交付报告。
- 动作：执行 P0–P6；核对设计 Q1–Q3。确认 manifest 是否允许 best-effort MCP probe、segment health 检查是否有通用 seam；记录 provider/visibility API 的实际差异。
- 验证命令：`go test -count=1 ./internal/config/... ./internal/wiring/... ./internal/personal/... ./internal/cli/...`；必要时 `go doc`/源代码定位 `exec.LookPath` 和 MCP provider。
- 完成标准：基线 exit code=0；没有 owner 冲突；决定 Q1/Q2/Q3 并把结论写入实施变更记录。若代码事实冲突，停止，不进入 T1。
- 反向验证：不适用；T0 失败即 fail-closed。
- 提交：不提交。

### T1 配置 registry 与通用存在性解析

- 文件：`internal/config/settings_registry.go`、建议新增 `internal/config/preferred_tools.go` 及对应测试；`internal/wiring/preferred_tools.go` 及测试。
- 动作：增加 `tools.preferred` 的 YAML descriptor/reader；校验 list/map、非空 name/kind/when、kind 枚举、重复 name、未知字段；支持 `config set` 的 flow-style inline YAML、get/list/unset 和候选 reload。定义安全 `PreferredToolStatus`，command 用 `exec.LookPath`，MCP 仅匹配 configured server 与成功 startup/`tools/list` 的实际结果，reason 分类稳定且不含 args/env/secret。运行时探测不写 config，不执行 command。
- 验证命令：`go test -count=1 ./internal/config/... ./internal/wiring/...`；隔离 `SUGA_HOME` 下真实 CLI：`suga config set tools.preferred '[{name: rg, kind: command, when: "search"}]'`、`config get/list/unset`；用 fixture PATH 覆盖 found/missing 和 malformed YAML/duplicate/unknown field。
- 完成标准：`config set` 成功后 YAML 仍由现有 writer 保留注释；错误退出 2 且原文件 bytes 不变；command 不存在时状态 false、reason 可解释；MCP probe 有界；所有新函数被 runtime 或 manifest 的实际调用点使用并有 grep 证据。
- 反向验证：临时移除 preferred reader，`config set` 和 malformed fixture 必须失败；临时让 resolver 把 missing command 标 present，missing-path test 必须失败；临时把 reason 写入 args/env，脱敏断言必须失败。
- 预计改动规模：M，3–7 个 Go/测试文件。
- 提交：`feat(config): add preferred tool settings and probes`。

### T2 Runtime 环境说明、MCP 常驻与入口接线

- 文件：`internal/wiring/runtime.go`、`internal/wiring/capabilities.go`、`internal/wiring/prompt.go`、`internal/wiring/subagent.go`、`pkg/agentcore/tools/visibility.go` 及相关测试。
- 动作：runtime 在一次组装中解析 preferred，并将 snapshot 传给 environment source、manifest source 和 Visibility；环境 section 只列 present 项，包含名称/适用场景/固定 `--help` 与 memory 提示。列表留在 segment context，不进入 `basePrompt`。为 Visibility 增加 pinned MCP tool names，使 preferred MCP 不进 deferred，其他 MCP 保持 auto/always/never 语义；approval 不读取 preferred。run/chat/ACP 复用 wiring；子 agent 列 inherited 状态并明确不可调用 shell/MCP，不扩展其 registry。按 T0 的结论实现 segment 边界上的 command/MCP refresh。
- 验证命令：`go test -count=1 ./pkg/agentcore/tools/... ./internal/wiring/... ./internal/personal/... ./internal/cli/...`；prompt 测试断言 stable `basePrompt` 不含 preferred；visibility 测试覆盖 preferred MCP + `auto/always/never`、非 preferred MCP 和 approval ask/deny；run/chat/ACP/subagent runtime fixture 检查环境文本。
- 完成标准：同一 segment 的环境文本稳定；新 segment 可反映 command 安装/MCP health；preferred MCP schema 直接出现在 `ListTools`、不需 `tool_search`；不在 allowlist 时仍按原审批结果；三入口接线无复制探测。
- 反向验证：临时把 preferred 写回 `basePrompt`，prefix/stable prompt test 必须失败；临时移除 pinned exclusion，deferred test 必须失败；临时把 preferred 转成 auto-allow，approval denial test 必须失败；临时给 subagent 加 MCP/shell，scope test 必须失败。
- 预计改动规模：L，5–10 个 Go/测试文件。
- 提交：`feat(wiring): surface preferred tools and pin preferred mcp`。

### T3 Manifest、turn_context 与 trace 安全观测

- 文件：`internal/wiring/manifest.go`、`internal/cli/manifest.go`、`internal/personal/turncontext.go`、`internal/personal/trace.go`（如文本需要）、对应单测/CLI 测试。
- 动作：manifest 增加 `preferred_tools` 全量 entries（present true/false、kind、when、reason），JSON/text 稳定排序且脱敏；standalone manifest 的 command/MCP probe 遵循 T0 决定的有界策略，MCP probe 结束关闭。runtime manifest 复用已建立的 provider 状态，不重复启动。turn_context 增加本回合实际列出的 `preferred_tools`、count、短 hash；trace JSON 保持 attrs 结构，text 只显示安全名字/数量或既有简洁行。
- 验证命令：`go test -count=1 ./internal/wiring/... ./internal/personal/... ./internal/cli/...`；隔离配置执行 `suga manifest --json`；检查 `suga trace <sid> --json` 有 preferred attrs，且全文无 command args/env/API key/URL secret。
- 完成标准：manifest 能区分 found on PATH、server not configured、startup/list/timeout 等原因；false 项不进入 prompt；每回合 turn_context 与 prompt 列表一致；旧 manifest/trace 消费者不因缺字段失败。
- 反向验证：临时只输出 present 项，manifest false/reason test 必须失败；临时删除 turn_context attrs，trace assertion 必须失败；临时把 when/args全量塞 trace，脱敏/大小断言必须失败。
- 预计改动规模：M，4–8 个 Go/测试文件。
- 提交：`feat(observe): report preferred tools in manifest and trace`。

### T4 PTY、反向验证、真实工具与平台 Gate

- 文件：`scripts/ptysmoke/preferred_tools.py`（若现有场景不能承载则新增）、`scripts/ptysmoke/README.md`、`Makefile`；测试 fixture/文档只在需要时新增；无功能性旁改。
- 动作：新建 Linux/macOS PTY 场景，使用 `PtySession` 驱动真实 binary + `fakellm.py`，配置一个 found command、一个 missing command 和一个 preferred MCP；在 chat 首个 prompt/segment 断言只出现 found 项、场景和固定提示，`tools.deferred=auto` 时 preferred MCP 不需 `tool_search`，结束用 `suga trace --json` 检查 turn_context。按 house rule 做反向运行：临时去掉 prompt 过滤、pinned 或 trace attrs，场景必须失败，再恢复并加入 `make smoke-chat`。在主机用已安装 rg、xerj 和一个已配置 MCP 验证 `manifest --json` 的 present/reason，并至少走一次真实 runtime/工具列出路径；不把 fake LLM 当真实 provider。
- 验证命令：Linux/macOS `python3 -I scripts/ptysmoke/preferred_tools.py ...`；`make smoke-chat`；反向场景命令按脚本 README；Windows 主机 `go build ./...`、`go vet ./...`、`gofmt -l $(git ls-files "*.go")`、`go test -count=1 ./internal/... ./pkg/...`、`staticcheck ./...`；容器 `go test -race ./...`、`go vet ./...`；真实 host job 使用 `rg`、`xerj`、已配置 MCP 的隔离 SUGA_HOME。
- 完成标准：PTY 正向和反向证据明确；Linux/macOS PTY 通过，Windows 不声称 PTY 通过；所有命令同时 exit code=0 且输出无新告警；真实工具结果记录版本/manifest reason/工具列出成功；无法执行的动作写 `UNVERIFIED_PTY_WINDOWS` 或 `UNVERIFIED_REAL_ENV`。
- 反向验证：至少三项对应 T2/T3：移除 environment present 过滤、移除 Visibility pinned、移除 turn_context preferred attrs，各自使对应检查失败；恢复后再跑正向场景。
- 预计改动规模：M，2–5 个 Python/Make/测试文件；若只需复用现有场景则为 S。
- 提交：`test(smoke): cover preferred tools discovery and observation`。

## 提交与合并顺序

本计划获批后按功能点本地提交，不 push：

1. `feat(config): add preferred tool settings and probes`（T1）
2. `feat(wiring): surface preferred tools and pin preferred mcp`（T2）
3. `feat(observe): report preferred tools in manifest and trace`（T3）
4. `test(smoke): cover preferred tools discovery and observation`（T4）

每次提交前执行 `git status --short`、`git diff --stat`、`git diff --cached --name-only`，只 `git add` 当前功能文件；不改 `.gofer/tracker`。T0/T4 的 external gate 不单独制造空提交；修复回归到所属功能点形成新的原子提交。

## 回滚与恢复

- T1 可独立 revert；旧版本对 `tools.preferred` 报 unknown，其他 config 键和 YAML 不受影响。
- T2 回滚后 stable prompt、环境说明、MCP registry、Visibility deferred 和 approval 回到当前行为；不得通过删除测试隐藏变化。若 T2 的 health seam 改变现有 MCP 生命周期，先回滚该 seam，再保留纯 command/prompt 变更或停止。
- T3 可独立 revert；manifest 继续输出既有模型/tools/MCP/skills/memory 字段，旧 trace 消费者忽略新增 attrs。
- T4 脚本/Makefile 可独立 revert；不要删除单测或反向断言来规避失败。
- 用户配置恢复：`tools.preferred` 是普通 YAML 值，沿用既有 config writer 的备份/原子恢复；probe 不写状态文件，不需要迁移或清理外部工具数据。

## 人工 Gate

1. Gate A：用户批准设计与本计划；批准前只提交两份 Draft 文档。
2. Gate B：T0 确认 manifest MCP probe、segment health refresh 和子 agent 提示语取 Q1–Q3 结论；若需要协议/权限扩展，暂停。
3. Gate C：T1–T3 的近端测试与反向测试均通过后，才运行 PTY 和真实环境动作。
4. Gate D：监督在 Linux/macOS 验证 PTY/反向场景，在 Windows 主机验证命令；真实 rg/xerj/MCP 证据齐全后才宣布完成。不得 push/tag/release。

## 可追溯性

| 设计要求 | 计划任务 | 证据 |
|---|---|---|
| 通用 `{name, kind, when}` 与 `config set` | T1 | registry 单测、隔离 CLI set/get/list/unset、原文件 bytes 保护 |
| command PATH/Windows PATHEXT、MCP startup/list | T1/T4 | resolver fixture、Windows host、真实 rg/xerj/MCP manifest |
| segment context 不破坏 stable prompt | T2/T4 | prompt cache/stable prefix 单测、PTY 正反向场景 |
| MCP preferred 常驻但不自动放行 | T2 | Visibility deferred 矩阵、approval denial/ask 测试 |
| run/chat/ACP/subagent 入口 | T2 | shared wiring fixture、三个入口和 subagent environment tests |
| manifest present/reason | T3/T4 | JSON/text snapshot、false reason、host probe |
| turn_context trace | T3/T4 | trace JSON attrs、PTY trace 检查、去逻辑反向失败 |
| skills/memory 不新增专用存储 | T2 | prompt 文案断言、scope review；现有 memory 记录说明 |
| 非目标与安全边界 | T1–T4 | 无专用 client/解析/安装/auto-allow 的 diff review 与反向测试 |

## 完成 Gate

宣布完成必须同时满足：

- 文档 validator：
  `python -I D:\work\inhere\runtime\idev-std-releases\0.26.1\plugins\inhere-dev-standards\skills\inhere-doc-workflow\scripts\validate_document.py --kind design docs/design/2026-10-17-preferred-tools-design.md`
  和对应 `--kind plan` 返回 exit code=0、`errors=[]`。
- 容器：`gofmt -l $(git ls-files "*.go")` 无输出、`go vet ./...`、`go test -race ./...`、`make smoke-chat` 通过；PTY 正向和反向均有日志。
- Windows 主机：`go build ./...`、`go vet ./...`、`gofmt -l $(git ls-files "*.go")`、`go test -count=1 ./internal/... ./pkg/...`、`staticcheck ./...` 均 exit code=0；输出无新告警。
- 真实环境：rg、xerj、一个已配置 MCP 各有 present/missing 或成功/失败 reason 证据；若 MCP/命令不可用，必须明确 `UNVERIFIED_REAL_ENV`，不能声称通过。
- `git status --short` 无夹带，提交列表只有 T1–T4 对应功能点；不 push。
