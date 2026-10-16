<!-- template_id: plan; template_version: 1.2.0 -->
# 回合级 token / 成本预算（budget）实施计划

> 状态：Draft 0.1（待用户审阅）

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-17 | suga（gofer job） | 依据已批准设计 Draft 0.3 起草实施计划；拆分配置、计量、估算、呈现与验证波次。 |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

## 目标与完成定义

目标：按 `docs/design/2026-10-16-turn-budget-design.md` Approved Draft 0.3 落地回合/会话 token 与美元预算，在不改变默认行为的前提下，复用现有 wrap-up 机制止损，并让 provider 用量缺失时的估算全程可辨。

完成定义（全部满足）：

1. `budget.tokens`、`budget.cost_usd`、`budget.scope`、`budget.estimate`、`budget.estimate_factor`、`budget.notice_ratio` 已按设计实现，优先级为 flag > 环境变量 > config；未配置预算时旧行为和旧输出保持不变。
2. 普通回合只在 `AfterModel` 检查预算，超限通过 `Control.RequestWrapUp` 收尾；会话累计已超限时 `BeforeRun` short-circuit 为 completed + `WrapUp`，不发模型请求；不新增 `RunInput` / `RunResult` 字段。
3. token 口径为 `max(input-cache_read, 0)+output`，成本沿用 `costOf`；子 agent、compact/交接等内部请求按设计计入，后台子 agent 不被父回合硬取消。
4. provider 估算采用 `observed(estimating(real))`，覆盖主 provider、fallback Pool 成员、compact provider 与子 agent provider；估算值和 `estimated` 标记同步到 state/meter、model 事件、trace、stream-json、chat 状态与 stats 口径。
5. run、chat、stream-json、trace、ACP 均能表达预算 scope、limit、used、remaining、exceeded 与真实收尾原因；ACP 的预算收尾仍为 `end_turn`，不误报为单次输出截断的 `max_tokens`。
6. 新增单测、`scripts/ptysmoke/chat_budget.py` 冒烟及反向验证全部成立；`make smoke-chat`、容器 `go test -race ./...`、主机 `go test -count=1 ./...` 和 `staticcheck ./...` 按项目规则完成。能使用真实 provider 时完成一轮带工具调用的核对，否则明确记录 `UNVERIFIED_REAL_PROVIDER`。
7. 无 schema 迁移；文档说明 gofer 的环境变量注入、软收尾与硬熔断关系，且不修改 gofer 代码。

## 范围、排除项与授权

- 范围：`internal/config`、`internal/cli`、`internal/wiring`、`internal/personal`、`internal/output`、`internal/chat`、`internal/acpserver`、`pkg/agentcore/llm`、`pkg/agentcore/api`，预算相关测试、伪终端冒烟及 `docs/usage/gofer-personal-agent.md`。
- 排除：`max_output_tokens`、持久化配额账本、跨进程/跨会话共享池、按工具/skill/目录分组预算、自动模型降级、gofer 侧改动、`suga stats` 报表重做、ACP 新协议字段、`RunInput`/`RunResult` 新字段。
- 估算只按设计复用现有字符/rune 估算器，不引入真实 tokenizer，不把估算宣称为计费真值。
- 本计划只授权本地实施与按功能点本地提交；不 push、不 tag、不发版，不改 `.gofer/tracker`。
- `host_or_non_offline_action=REQUIRED`（T9 含主机全量测试与真实 provider 核对；计划批准不自动授权这些出机器动作，需在人工 Gate 另行确认。）

## 输入与批准证据

- 主设计：`docs/design/2026-10-16-turn-budget-design.md` Draft 0.3 / 已批准（2026-10-16 用户：“全部按建议批准”；Q1–Q10 均按文中建议选项定稿）。
- 调研输入：`docs/research/2026-10-16-feature-strengthening-analysis.md` §3 #2、#3；设计中的现状核对、实测与独立评审记录。
- 相关现有能力：`max_steps` 的配置优先级、子 agent `limitHook`、`runUsageMeter`、`priceOf`、wrap-up、stream-json usage、trace 事件与 chat 状态行。
- 基线事实：预算当前未实现；设计要求不改 agentcore 回合协议，只增加 `llm.Attempt.Estimated` 与 API metadata 常量。

## Capability Discovery

### Capability decisions

| capability_id | required_capability | searched_candidates | direct_reuse | thin_adapter_or_owner_extension | decision | proven_gap | duplication_and_lifecycle_risk |
|---|---|---|---|---|---|---|---|
| CAP-01 | 预算配置与三级优先级 | `internal/config/maxsteps.go:16-36` 的 `ReadMaxSteps`、`maxsteps_test.go:9-`、配置加载约定 | 是：复用 flag > env > config 的现有解析优先级 | 在 `internal/config` 原位新增 `ReadBudgetSettings` | DIRECT_REUSE | budget 多字段与 env 显式读取尚不存在 | 不复制 max-steps 解析约定；统一在 budget owner 中维护校验 |
| CAP-02 | 回合/会话用量计量 | `internal/personal/usage.go:253-327` 的 `runUsageMeter`、`withSessionUsage`、usage 事件写入 | 是：现有 run/session meter 与子 agent forwarding 可复用 | `internal/personal` 增加 budget hook/快照 | OWNER_EXTENSION | 当前无主 agent 预算判定 | 继续使用现有 meter 与 `SumUsageTree`，不另造累计账本 |
| CAP-03 | 超限收尾 | `pkg/agentcore/api/hooks.go:36-59` 的 `Control.RequestWrapUp`、`pkg/agentcore/runtime_wrapup.go:13-`、子 agent `limitHook` | 是：first-wins wrap-up control seam 直接复用 | budget hook 按同一 control seam 扩展 | DIRECT_REUSE | 需要 token/cost 双阈值与 BeforeRun short-circuit | first-wins 保证同一回合只请求一次收尾 |
| CAP-04 | provider 缺失用量估算 | `internal/personal/tokens.go:10-83`、provider Pool/compact 装配、`pkg/agentcore/llm/observe.go:62-` | 部分：已有 rune/token 估算可复用，但没有 provider 返回值封装 | 新增 `estimatingProvider`，放在 observer 内侧 | MINIMAL_NEW_MODULE | 现有估算只供窗口判断，未回填 GenerateResult，Usage=0 无法进入 state/meter | 所有 provider 构造路径统一包裹，估算器只在 personal owner 生命周期内存在 |
| CAP-05 | 估算可观测性 | `pkg/agentcore/api/provider.go:69-` 的 `GenerateResult`、`pkg/agentcore/llm/observe.go:10-50` 的 `Attempt`、model/usage/trace 事件 | 部分：GenerateResult metadata 与 Attempt observer 可复用 | API metadata 常量与 `Attempt.Estimated` 字段 | OWNER_EXTENSION | 当前 Attempt 无 estimated 字段 | 只增加可选字段，旧消费端保持兼容 |
| CAP-06 | 文本/JSON/chat/ACP 呈现 | `internal/output/wrapup.go:9-29` 的 `WrapUpNotice`、`internal/chat/layout.go:181-219` 的 `BuildTurnStatus`、stream-json/trace/ACP owners | 是：现有 wrap-up、usage 与状态呈现 owner 直接复用 | 各 owner 增可选 budget 视图 | OWNER_EXTENSION | 当前无 budget 对象、提醒与成本原因 | 不改旧未配置输出；呈现由现有层负责，不新增跨层协议 |
| CAP-07 | 交互式预算验收 | `scripts/ptysmoke/fakellm.py:1-49`、`scripts/ptysmoke/chat_observe.py:1-50`、`Makefile` smoke-chat | 部分：既有 PtySession/fakellm/trace 检查可复用 | 新增 `chat_budget.py` 与 fake usage 选项 | OWNER_EXTENSION | 当前无预算冒烟 | 与现有 PtySession/trace JSON 断言共用，不留人工验证 |

### New module candidates

| candidate_id | capability_id | proposed_module | searched_candidates | direct_reuse_gap | thin_adapter_or_owner_extension_gap | proven_gap | unique_owner_and_lifecycle | deletion_or_merge_handling |
|---|---|---|---|---|---|---|---|---|
| NEW-01 | CAP-04 | `internal/personal/estimate.go`（`estimatingProvider`） | `internal/personal/tokens.go:10-83`、`pkg/agentcore/llm/observe.go:62-`、provider 构造点 | 现有估算没有 provider 返回值封装 | 需要单一包装 seam，并覆盖主、fallback、compact、子 agent provider | provider Usage=0 时无法进入 state/meter，且估算标记不能贯穿 metadata | provider 包装器唯一拥有补估算生命周期；estimate policy 由 budget 配置 owner 控制 | 若 provider 全部始终上报用量，仍保留可关闭实现；不复制到 observer |

### Rejected new tools

| rejected_candidate | capability_id | deletion_test_and_reason |
|---|---|---|
| 在 `AfterTool` 中执行预算硬中断 | CAP-03 | 当前内核会丢弃工具级 control；删除该方案不损失已批准行为，预算只在 `AfterModel` 检查。 |
| 在 `usageCollector` 内直接估算 | CAP-04 | model 事件与 stream-json 会继续看到 0，口径分裂；删除该方案保持 D5 的单一 provider 包装器。 |
| 新增 `RunInput`/`RunResult` 预算字段 | CAP-03 | `WrapUp`、事件与输出已能表达原因；删除字段避免内核契约扩张。 |

## 前置检查（preflight）

1. 读取设计 Draft 0.3 的状态行与 Q1–Q10；只有“全部按建议批准”仍成立才开始实施。若用户改变任何定稿选项，回到设计/计划 Gate，不在实施中自行改语义。
2. 记录基线：Git root、当前分支与 HEAD、`git status --short`、目标 worktree/分支；确认本计划 owner 路径没有他人改动。发现冲突即停止。
3. 确认 `internal/personal` 当前仍有 `runUsageMeter`、`withSessionUsage`、`turnHooks`、`priceOf` 与现有 wrap-up 路径；若符号已迁移，只做不改变语义的定位修正并记录。
4. 先验证 provider Pool、fallback、compact 和子 agent 的实际装配路径，形成清单；遗漏任何构造路径不得进入估算实现。
5. 确认无数据库迁移文件、无新增持久化字段；budget 事件使用现有自由文本 `events.kind`。确认 `pkg/agentcore` 不依赖 `internal/*` 的 layering 检查仍适用。
6. 运行改动前最近的窄回归：`go test -race ./internal/config/... ./internal/personal/... ./internal/output/... ./internal/chat/... ./internal/acpserver/...`；失败先归因，不能把基线失败算作本计划完成。
7. 交互任务只在 Linux/macOS 容器运行 `make smoke-chat`；Windows 仅执行项目规定的主机测试，不能把 Windows 控制台差异假设成 pty 结果。

## 波次与依赖

| 波次 | 阶段 | 依赖 | 执行方式 |
|---|---|---|---|
| W0 | 预检与 owner 冻结 | 无 | 监督会话；不改代码 |
| W1 | 配置、协议元数据与估算器 | W0；T2 可与 T1 并行但不共享 owner 文件 | T1、T2 分离 job |
| W2 | personal 预算计量、hook 与装配 | T1、T2 | T3 后 T4；同一 owner 依赖链 |
| W3 | 输出与交互呈现 | T3、T4 | T5、T6 可并行；T6 含 ptysmoke |
| W4 | ACP、gofer 文档与跨层回归 | T5、T6 | T7、T8 可并行 |
| W5 | 完整验收与真实 provider | T1–T8 | 监督会话 + gofer host job |

## 任务

### T1 配置读取与 run/chat 预算 flags（W1，T1/T2 可并行）

- 文件：`internal/config/budget.go`、`internal/config/budget_test.go`、`internal/config/maxsteps.go`（仅复用约定时不改）、`internal/cli/run.go` 及其相关命令注册/测试文件。
- 动作：新增 `BudgetSettings`/`BudgetFlags`/`ReadBudgetSettings`；实现 `budget.*`、`SUGA_BUDGET_*`、flag 的优先级与非法值 fail-closed；注册 `--budget-tokens`、`--budget-cost-usd`、`--budget-scope` 到 run 与 chat 共用入口，0 表示未给；不新增 `estimate` 等未批准 CLI flag。
- 验证命令：`go test -race ./internal/config/... ./internal/cli/...`；针对 flag/env/config 的优先级、负数、非法 scope/estimate、cost 小数、默认零值运行新增用例。
- 完成标准：配置未启用返回零值且无行为变化；所有合法字段解析正确，非法输入启动报错；run/chat 都能把预算选项传到 wiring。
- 反向验证方法：临时交换 env/config 优先级或移除 chat flag 注册，新增优先级/`chat --budget-tokens 1` 用例必须失败；验证后恢复。
- 预计改动规模：中（约 2–4 个 Go 文件，150–250 行含测试）。

### T2 估算器与可观测 metadata（W1，T1/T2 可并行）

- 文件：`internal/personal/estimate.go`（新）、`internal/personal/tokens.go`、`pkg/agentcore/api/provider.go` 或 API 常量所在文件、`pkg/agentcore/llm/observe.go`、对应测试。
- 动作：实现 `estimatingProvider`，按设计的 `auto/on/off`、输入/输出正文与工具 JSON、`estimate_factor` 补齐缺失 Usage；provider 有真实值时不覆盖；把估算标志经 `GenerateResult.Metadata[UsageEstimatedMetaKey]` 传出，`llm.Attempt` 增 `Estimated bool`；复用现有 CJK/rune 规则，不改 provider 协议。
- 验证命令：`go test -race ./internal/personal/... ./pkg/agentcore/llm/... ./pkg/agentcore/...`；覆盖中文/英文/混合、emoji/全角、部分真实 Usage、off/on 与 metadata/Attempt 标志。
- 完成标准：估算只在设计条件下补值；真实值不被估算覆盖；标志可被 observer 读取；agentcore layering 与既有 provider 测试通过。
- 反向验证方法：临时让 observer 之外的包装器不返回估算值，或清掉 metadata 转换，要求 estimated/model-event 回归测试失败；恢复后再测。
- 预计改动规模：中（约 3–5 个 Go 文件，180–300 行含测试）。

### T3 回合预算 hook、事件与 session short-circuit（W2）

- 文件：`internal/personal/budget.go`（新或同 owner 文件）、`internal/personal/runner.go`、`internal/personal/usage.go`、`internal/personal/store/types.go`/事件写入处、相关测试。
- 动作：在 `turnHooks` 中保持 usage collector 最前，增加 budget hook；只在 `AfterModel` 按 token/cost 口径检查，复用 `RequestWrapUp` first-wins；实现 turn/session scope、`BeforeRun` 已超限的 completed short-circuit、一次性 notice/预算事件；子 agent 与内部请求沿用 meter forwarding/`purpose=compact` 口径；不添加 RunResult 字段，不取消后台子 agent。
- 验证命令：`go test -race ./internal/personal/...`；覆盖缓存扣除、cost provider 优先/price fallback/无 price 警告、父子 agent、compact、session base+run、首次收尾、收尾请求仍计费、无模型请求 short-circuit。
- 完成标准：超限普通回合进入 wrap-up；session 已用尽不发模型请求；预算事件包含 scope/limit/used/reason/estimated 所需 attrs；未配置预算不安装有效 hook且行为不变。
- 反向验证方法：临时删除 `AfterModel` 判定、取消 `BeforeRun` short-circuit 或让成本无价表静默计入，针对对应测试必须失败；恢复所有临时改动。
- 预计改动规模：大（约 4–7 个 Go 文件，300–500 行含测试）。

### T4 wiring 装配所有 provider 路径（W2，依赖 T2/T3）

- 文件：`internal/wiring/runtime.go`、`internal/wiring/subagent.go`、`internal/personal/runner.go` 的配置注入与装配测试。
- 动作：读取并传递 Budget 配置、price lookup 与 notice 配置；确保主 provider、fallback Pool 每个成员、compact provider、子 agent provider 均按 `observed(estimating(real))` 顺序装配；保持子 agent `agents.max_tokens` 不变；不改 gofer。
- 验证命令：`go test -race ./internal/wiring/... ./internal/personal/...`；以 fake provider 记录每个装配路径的 Usage/Estimated 进入同一 meter 的测试。
- 完成标准：所有设计列出的 provider 路径均被覆盖，预算 hook 只装一次且顺序正确；配置/价格缺失的警告行为稳定。
- 反向验证方法：临时跳过 fallback 或 compact 的包装，针对各路径的 estimated/预算累计断言必须失败；恢复后验证全路径。
- 预计改动规模：中（约 2–4 个 Go 文件，150–280 行含测试）。

### T5 stream-json、文本提示、trace 与 stats 口径（W3，依赖 T3/T4）

- 文件：`internal/output/streamjson.go`、`internal/output/usage.go`、`internal/output/wrapup.go`、`internal/cli/run.go`、`internal/personal/trace.go`、`internal/personal/store/types.go`/事件渲染与相关测试。
- 动作：增加可选 `budget` 对象、`estimated` 字段和 `max_cost_usd` 文案；未配置预算时不输出新键；trace 文本/JSON 展示 budget 事件的 scope/limit/used/exceeded 与 estimated；保持 stdout 只输出答案、旧事件和旧库可读。
- 验证命令：`go test -race ./internal/output/... ./internal/cli/... ./internal/personal/...`；断言 usage/result/turn_end、wrap-up、trace JSON 与文本、stats 聚合口径。
- 完成标准：输出能区分 token/cost、turn/session、estimated/真实；预算收尾原因不被改写；旧无预算黄金断言逐字节通过。
- 反向验证方法：临时去掉 result 的 budget 对象或把 `estimated` 默认为无标记，新增 stream-json/trace 断言必须失败；恢复后验证。
- 预计改动规模：中（约 4–6 个 Go 文件，220–360 行含测试）。

### T6 chat 状态、提醒、回合末提示与伪终端冒烟（W3，依赖 T3/T4）

- 文件：`internal/chat/layout.go`、`internal/chat/layout_status_test.go`、`internal/chat` 回合状态构造处、`scripts/ptysmoke/fakellm.py`（仅增加可控 usage/延时所需能力）、新 `scripts/ptysmoke/chat_budget.py`、`Makefile`、冒烟 README/场景注册处。
- 动作：配置预算时在状态行显示 `budget NN%` 或 `session budget NN%`，估算数字加 `~`，达到 `notice_ratio` 每回合提醒一次，超限显示 token/cost 收尾文案；未配置预算逐字节不变。用 PtySession 驱动真实二进制 + fakellm，结束后用 `suga trace --json` 断言预算事件与 estimated；加入 `make smoke-chat`。本任务不留人工 chat 验证。
- 验证命令：`make smoke-chat`；`go test -race ./internal/chat/...`。场景至少覆盖小 token 预算触发收尾、session 已用尽不调用模型、estimate 标记、未配置回归。
- 完成标准：新增场景稳定通过，trace 可证实 limit/used/scope/reason，旧 steering/tool/agent 场景不回归；反向验证能证明检查点有效。
- 反向验证方法：临时移除 budget hook 的 `AfterModel` 检查，或让 chat 不传 budget snapshot，`chat_budget.py` 必须失败；恢复后重新跑完整 `make smoke-chat`。
- 预计改动规模：中（约 3–6 个 Go/Python/Makefile 文件，250–450 行）。

### T7 ACP 预算收尾与 usage 元数据（W4，依赖 T5；可与 T8 并行）

- 文件：`internal/acpserver/server.go`、`internal/acpserver/usage.go`、ACP 测试文件。
- 动作：预算 `max_tokens`/`max_cost_usd` 收尾统一返回 `stopReason=end_turn`；仅 provider 单次输出截断保留 SDK `max_tokens`；usage_update/_meta 增加设计规定的可选 budget_limit、budget_used、estimated 信息；不新增 ACP 协议字段。
- 验证命令：`go test -race ./internal/acpserver/...`；覆盖预算收尾、yield/refusal、单次输出截断与旧 usage。
- 完成标准：ACP 能如实区分累计预算收尾和单次输出截断，旧客户端兼容。
- 反向验证方法：临时把 budget wrap-up 映射为 `max_tokens`，预算 ACP 测试必须失败；恢复后验证。
- 预计改动规模：小至中（约 2–3 个 Go 文件，100–180 行含测试）。

### T8 gofer 用法文档与跨层命令示例（W4，依赖 T1/T5；可与 T7 并行）

- 文件：`docs/usage/gofer-personal-agent.md`，必要时仅更新预算相关使用说明所在文档段；不改 gofer 仓库。
- 动作：说明 `SUGA_BUDGET_TOKENS`/`SUGA_BUDGET_COST_USD`/`SUGA_BUDGET_SCOPE` 与 flags、turn/session 选择、suga 软收尾 vs gofer 硬熔断、缓存口径换算、最坏超出量、无 price 时成本预算失效警告；给出 job 注入示例。
- 验证命令：`git diff --check`；运行文档中的命令形状检查，确认环境变量名、flag 名和实际 CLI 注册一致。
- 完成标准：文档不暗示 gofer 协议改动，明确 job 默认 turn scope 与双阈值边界；内容与 Draft 0.3 D4/D7/D10 一致。
- 反向验证方法：临时删除环境变量示例或把 scope 写成未支持值，文档校验脚本/人工逐项核对必须发现缺失；恢复后检查。
- 预计改动规模：小（1 个 Markdown 文件，40–100 行）。

### T9 全量回归、真实 provider 与交付验收（W5，依赖 T1–T8）

- 文件：不新增 owner 文件；读取各任务测试、`scripts/ptysmoke`、trace 输出与文档；若真实 provider 失败只记录，不在此任务扩大设计范围。
- 动作：先窄后宽执行测试；容器跑 race、vet、smoke 与 staticcheck；主机按规则跑全量测试；使用真实 provider 完成一轮带工具调用的 `suga run`，核对 provider 上报 token/cost 与 trace，确保 `estimated` 未出现；无法执行则记录 `UNVERIFIED_REAL_PROVIDER` 和原因。
- 验证命令：`gofmt -l $(git ls-files "*.go")`、`go vet ./...`、`go test -race ./...`、`make smoke-chat`；主机 `go test -count=1 ./...`、`staticcheck ./...`；真实 provider 命令按环境配置执行。
- 完成标准：命令同时以 exit code 和原始输出判定；所有反向验证已做并恢复；无预算旧行为回归；未验证项明确列出，不把未执行写成通过。
- 反向验证方法：交付前从临时分支移除一条关键 budget 断言/检查，确认新增测试或冒烟失败，再恢复并重跑对应窄验证；不在最终提交中保留破坏性改动。
- 预计改动规模：小（验证与记录，不应产生业务代码改动）。

## 回滚与恢复

- 无数据库/schema 迁移；新增 budget 事件使用既有自由文本 kind，旧库无需转换。
- 默认不启用预算；删除 `budget.tokens`/`budget.cost_usd`、环境变量和 flags 后，chat/run 回到原有路径。`estimate: off` 可作为运行期降级，但不替代代码回滚。
- 代码按任务独立 commit；未通过验收的任务不合入，保留 worktree 供修复。需要撤销时按功能点 `git revert`，不得用 reset/stash 覆盖他人修改。
- stream-json/trace 新字段均为可选未知键，旧消费端和旧事件可读；不要通过清库或删除历史事件回滚。
- session scope 的历史用量只读既有 store 汇总；回滚代码不会改变已保存数据。

## 人工 Gate

1. 用户审阅并批准本计划后，才允许开始实施；批准不等于授权 push/发版。
2. T1–T4 合入前确认 provider 装配清单完整，尤其 fallback、compact 与子 agent 路径。
3. T6 的 ptysmoke 必须自动运行并完成一次反向验证；不能以人工打开 chat 代替。
4. T9 的真实 provider 工具调用是交付 Gate；做不到时由监督方确认 `UNVERIFIED_REAL_PROVIDER` 是否可接受。
5. gofer job 软预算与硬熔断的实际阈值由运维另行配置；本计划不改 gofer、不 push、不发版。

## 可追溯性

| 设计条目 / 决策 / 待确认定稿 | 任务 | 验证 |
|---|---|---|
| 用户目标 1、配置三级优先级；D10 flag 同时给 run/chat | T1 | config/CLI 优先级与两命令测试 |
| 用户目标 2、D1 wrap-up；D3 只在 AfterModel；D11 不加 RunInput/RunResult | T3、T5、T7 | hook、short-circuit、stream/ACP 测试 |
| D2 `max(input-cache_read,0)+output` | T3 | 缓存读与输出用量用例 |
| D4 turn/session，不新增 process/per-run；Q5 会话已用尽 short-circuit | T1、T3 | scope 配置与 BeforeRun 无模型请求测试 |
| 用户目标 3、D7 价格表；Q10 gofer 双阈值换算 | T4、T8 | provider cost/price fallback；文档示例检查 |
| 用户目标 4、D5 provider 包装器；D6 auto/on/off；Q6 factor=1.0 | T2、T4、T5 | estimate provider、metadata、事件/stream-json 标记 |
| D8 子 agent 计入、保留 agents.max_tokens；Q9 后台不受父收尾控制 | T3、T4 | 父子 meter、后台生命周期测试 |
| D9 内部请求计入、无排除开关；Q2/Q4 | T3 | compact/内部 purpose 计量测试 |
| 用户目标 5、Q7 chat 常显占用 + 一次提醒 | T6 | chat 单测与 ptysmoke |
| D12 scope 大小写不敏感且非法报错 | T1 | config 解析测试 |
| `estimated` 在 model event/usage/stream/chat/stats 可辨 | T2、T5、T6、T9 | 事件 attrs、JSON、状态行、真实 provider 核对 |
| 成本无 provider/price 警告且预算失效；Q8 | T3、T5 | warning 与不触发测试 |
| 收尾本身计费、最坏尝试集合 | T3、T8 | 收尾 usage 测试；文档上界说明 |
| ACP 映射为 `end_turn`，单次截断才是 `max_tokens` | T7 | ACP server 测试 |
| 默认不启用；Q1 | T1、T5、T6 | 默认零值、旧输出逐字节回归 |
| 无迁移、旧库兼容、trace 可断言 | T3、T5、T9 | schema 检查、trace JSON、全量回归 |
| Q3 缓存写计入、Q10 gofer 硬熔断优先 | T3、T8 | 计量单测与运维文档 |
| 非目标：`max_output_tokens`、stats 重做、模型路由、gofer 改动 | 全任务 | diff scope review |

### 已定稿的 Q1–Q10

- Q1：默认不启用；不配置 token/cost 时无行为变化。
- Q2：内部请求计入，不提供 `include_internal` 开关。
- Q3：收尾本身可能继续产生费用，按完整最坏路径记录，不做二次硬中断。
- Q4：缓存写按现有 `InputTokens` 口径计入。
- Q5：session 已用尽时 `BeforeRun` short-circuit，completed + budget wrap-up，不发模型请求。
- Q6：估算默认系数 1.0；大于 1 才显式更保守。
- Q7：chat 常显预算比例，达到 notice ratio 每回合提醒一次。
- Q8：无 provider cost 且无 price 时警告，成本预算不生效。
- Q9：后台子 agent 不受父回合收尾控制，只受自身限制；挂起用量按设计结算。
- Q10：suga 软阈值应早于 gofer 硬上限，并按缓存比例持续复测。

## 待确认 / 风险

- 设计已批准且 Q1–Q10 已按建议定稿；若用户在实施前改变其中任一项，必须回到设计 Gate，不在 job 中自行调整。
- 估算会影响窗口判断与 `lastInput`，即使标记为 estimated 也可能提前压缩；这是设计接受的可解释近似，不得在计划外引入 tokenizer。
- AfterModel 检查有一次模型尝试和收尾尝试的超出量；后台子 agent 不受父收尾控制，必须在文档与测试中保留完整最坏路径，不许承诺硬上限。
- provider 不报 cost 且无 price 时成本预算失效；只能警告，不能猜价格。
- Windows 控制台不提供 pty 等价证明；主机全量测试和真实 provider 若不可用必须原样记录。

## 完成 Gate

- T1–T9 的完成标准全部满足，且每个任务的反向验证已实际成立。
- `git status --short` 只包含本功能点已提交或明确待提交的 owner 文件；每个功能点单独 conventional commit，提交前只 `git add` 对应文件，不提交 `.gofer/tracker`。
- 交付报告贴出关键命令原始输出、exit code、真实 provider/Windows 未验证项；不以“通过”替代证据。
- 计划实施不改变已批准设计；任何语义修订、协议扩张、迁移需求或 owner 冲突均停止并回到设计 Gate。
