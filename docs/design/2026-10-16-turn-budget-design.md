<!-- template_id: design; template_version: 1.1.1 -->
# 回合级 token / 成本预算（budget）设计

> 状态：Draft 0.3（按确认评审 r2 修订，待用户审阅）

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-16 | suga（gofer job） | 初稿 |
| 0.2 | 2026-10-16 | suga（gofer job） | 按评审 r1 关闭 B-C1/B-C2、B-M1–B-M8；明确后台子 agent、gofer 熔断、命名、插话与估算副作用 |
| 0.3 | 2026-10-16 | suga（gofer job） | 按确认评审 r2 合并正文：关闭 AfterTool/会话短路/ACP 映射/最坏超出量/gofer 分工与 Q9–Q10 一致性项 |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

来源：issue suga-zinq；调研 `docs/research/2026-10-16-feature-strengthening-analysis.md` §3 #2、§3 #3；相关：suga-lznj（`max_output_tokens`）。独立评审：`local/reviews/2026-10-16-budget-search-design-review-r1.md`（不入库）。

### 思考模式与范围冻结

- thinking_mode：RIGOROUS。
- core_objective：一次回合（或一个会话、一个 gofer job 注入的运行）花掉的 token 与美元超过配置预算时，以**现有的 wrap-up 收尾机制**停下来并如实告知原因；provider 不报用量时，预算仍按本地保守估算可用，且所有估算值在 trace/stream-json 上可辨。
- allowed_scope：`internal/config`（`budget.*` 读取与优先级）、`internal/cli`（`--budget-*`、文本提示）、`internal/wiring`（装配、price 与估算开关的注入）、`internal/personal`（预算钩子、估算器、事件、`RunResult`）、`internal/output`（stream-json 字段、收尾文案）、`internal/chat`（状态行）、`internal/acpserver`（stopReason）、`pkg/agentcore/llm`（`Attempt` 增加一个 `Estimated` 标记字段，只加字段不改协议）、`pkg/agentcore/api`（估算标记的 metadata 键常量）。
- non_goals：`max_output_tokens`（suga-lznj）；跨进程/跨会话的配额账本；按工具、按 skill、按目录分组预算；达到预算后自动换更便宜的模型（模型路由另议）；gofer 侧改动；`suga stats` 的报表重做。
- expansion_policy：DEFER_OR_REQUEST。
- review budget：低暴露度（个人工具、默认不启用、可一键关掉；SR1409）。
- 停止条件：确认无 CORE_BLOCKING 后请用户批准，进入计划阶段。

## 背景与目标

现状（详见「已确认事实与规范」）：主循环只有**步数**上限 `max_steps`（默认 240，`internal/config/maxsteps.go:14`），token 类软上限只存在于子 agent（`agents.max_tokens`，默认 400k，`internal/config/agents.go:28`），主回合既没有 token 也没有金额预算；用量完全依赖 provider 返回值，provider 不报就是 0。结果是：一个跑偏的回合可以在无人察觉的情况下连续调用几十次模型、烧掉大量 token / 美元；小 provider 或网关不报用量时，连"看到了多少"都不知道。

用户可见目标：

1. 能配置"这一回合最多花多少"：`budget.tokens`（token）与 `budget.cost_usd`（美元），可在 config、环境变量、命令行三处设置，优先级与 `max_steps` 一致。
2. 超限时**不丢已做的工作**：与步数上限同一套 wrap-up——下一次请求变成"不要再调工具，直接汇报"，回合以 completed + `WrapUp` 原因收尾，chat/run/stream-json/trace/ACP 都看得出真实原因。
3. gofer 派活时能按 job 给一个预算（环境变量注入），不需要 suga 与 gofer 之间加协议。
4. provider 不报用量时，预算用本地保守估算仍然生效，并且所有估算数字在 trace 与 stream-json 上标记 `estimated`，不会与真实计量混淆。
5. chat 状态行显示本回合（或本会话）的预算占用比例，接近上限时给出一次提醒。

## 名词

- **回合（turn）**：一次 `Runner.Run`，含多次模型请求与工具调用（沿用 steering 设计的定义）。
- **步（step）**：回合内的一次模型请求及其工具调用，受 `max_steps` 约束。
- **运行（run）**：`usage.RunUsage` 的口径——本回合内所有模型调用（含压缩、含失败的但报了用量的尝试）与工具调用数；stream-json 的 `usage` 行与 ACP `usage_update` 就是它（`internal/personal/usage.go:102-124`）。
- **会话累计（session usage）**：本会话历史所有回合 + 其子 agent 会话的合计（`Store.SumUsageTree`，`internal/personal/runner_finish.go:55-64`）。
- **预算（budget）**：`budget.tokens` / `budget.cost_usd` 在某个 scope 上的上限。
- **scope**：预算的计量范围，`turn`（默认）或 `session`。
- **口径（spend）**：token 口径 = `max(输入 token − 缓存读, 0) + 输出 token`；成本口径 = provider 报的 `CostUSD` 优先，否则按价格表算。
- **估算（estimate）**：provider 未报用量时按字符/rune 本地推算的用量，标记 `estimated`。
- **wrap-up 收尾**：`Control.RequestWrapUp(reason, instruction)` 让下一次请求变成"最后一次作答"，回复结束整个回合（`pkg/agentcore/api/hooks.go:48-59`、`pkg/agentcore/runtime.go:139-141、202-206`）。
- **gofer job**：`gofer job run` 起的一次 `suga run/chat` 进程；本文只用环境变量向它注入预算。

## 范围与非目标

范围：

- `config`：`budget.*` 键、默认、校验、优先级（flag > 环境变量 > config）。
- `cli`：`--budget-tokens` / `--budget-cost-usd` / `--budget-scope`（run 与 chat 共用，超过 0 才生效）；文本模式的收尾提示与一次性警告。
- `personal`：预算钩子（`AfterModel` + `BeforeRun`；AfterTool 不用于预算）、用量估算器、`budget` 事件、`RunResult` 上不需要新字段（沿用 `WrapUp` 与 `RunUsage`）。
- `output`：stream-json 的 `budget` 对象与 `wrap_up` 取值；`WrapUpNotice` 增加成本预算文案。
- `chat`：状态行 `budget NN%`、接近上限的一次性提醒、超限时的回合末提示。
- `acpserver`：预算收尾的 `stopReason`。
- `llm`：`Attempt.Estimated` 标记（只加字段）。
- 测试：单测 + fake 模型冒烟（`scripts/ptysmoke/`）+ 现有子 agent 预算测试回归。

非目标：

- `max_output_tokens` 与截断语义（suga-lznj，见后文分界）。
- 预算的持久化配额、跨进程并发扣减、跨会话共享池；「本月已花多少」这类账本（属 `suga stats` / 计费话题）。
- 达预算自动降级模型或自动压缩；按工具/会话组/子 agent 单独的美元预算（子 agent 继续用 `agents.max_tokens`）。
- gofer 侧代码改动（只写用法文档）。
- chat 新增 `:usage` 命令（命令面扩充与本设计无关，见 §3 #15）。

## 已确认事实与规范

以下在 worktree 分支基线 `626747f` 上核对；行号可能随改动漂移，引用时以符号名为准。

**A. 主循环、步数上限与收尾**

- 步数上限：`pkg/agentcore/runtime.go:28`（`Config.MaxSteps`）、`:429-434`（`effectiveMaxSteps`：`RunInput.MaxSteps > 0` 优先）、`:133-138`（`state.Step >= maxSteps` 时：没有收尾文案或已经收尾过 → 返回 `max steps exceeded: %d` 错误；否则本次请求转为收尾）。
- 收尾原因：`pkg/agentcore/runtime_wrapup.go:18` `wrapUpReasonMaxSteps = "max_steps"`，注释明确写了「hook 的 `Control.RequestWrapUp` 自带原因（token 预算、超时）」——**机制现成**。
- hook 请求收尾：`pkg/agentcore/api/hooks.go:48-59` `Control.RequestWrapUp(reason, instruction)`，只认第一次请求；`pkg/agentcore/runtime.go:139-141` 在循环顶部读取它，`runtime.go:172-176` 把 `instruction` 作为**仅本次请求**的 user 消息追加（不落库）。
- 收尾可见性：`runtime.go:151-157` 把本步是否收尾写进 `RunState.WrappingUp`（`pkg/agentcore/api/types.go:103-109`：瞬态、`json:"-"`、每步重置）；`runtime.go:214-222` 正常作答时 `result.WrapUp = wrapUp`；`runtime.go:202-206` 模型无视收尾仍要工具时走 `finishIgnoredWrapUp`（`runtime_wrapup.go:88-134`：取其文本 → 用 `ToolChoiceNone` 重试一次 → 固定兜底文案），回合仍然是 completed。
- 步数上限的文案来源：`internal/personal/runner.go:768-778` `maxStepsWrapUp(maxSteps)`，由 `runner.go:600` 注入 `agentcore.Config.MaxStepsWrapUp`；文案强制模型说明真实原因（"不得写成超时、被中断或其它原因"）。
- hook 装配点：`internal/personal/runner.go:633-651` `turnHooks`——用量收集器必须排在最前（注释：后面的 hook 短路不能让模型调用从用量里消失），子 agent 的 `limitHook` 在 `:640-646` 追加。

**B. 子 agent 已有的预算先例（本设计的口径基准）**

- `internal/config/agents.go:12-28`：`agents.max_tokens` 默认 400_000，注释写明是「uncached input plus output」。
- `internal/personal/subagent.go:208-219` `limitHook.AfterModel`：`spent := max(u.InputTokens-u.CacheReadTokens, 0) + u.OutputTokens; if spent >= maxTokens → ctrl.RequestWrapUp(StopMaxTokens, …)`；指令同样要求模型如实声明原因。
- `internal/personal/subagent.go:22-28`：`StopFinal` / `StopMaxSteps="max_steps"` / `StopMaxTokens="max_tokens"` / `StopTimeout="timeout"`。
- `internal/wiring/subagent.go:144`：子 agent runner 的 `MaxSteps/MaxRunTokens/RunTimeout` 就是这么装配的——**主 agent 的预算可以走完全相同的通道**。

**C. 用量与成本的计量**

- 用量结构：`pkg/agentcore/api/types.go:62-70`（`InputTokens/OutputTokens/TotalTokens/CacheReadTokens/CacheWriteTokens/CostUSD`）、`:72-82`（`Add` 逐字段求和）；位置在 `GenerateResult.Usage`（`pkg/agentcore/api/provider.go:74`）。
- provider 层归一：`InputTokens` **包含**缓存读（OpenAI `prompt_tokens` 含 cached；Claude provider 把 `cache_read + cache_creation` 加进 input），所以 `internal/personal/usage.go:87-100` `ReportUsage` 对外报告时要减掉缓存——预算口径必须建立在同一约定上（见「计量口径」）。
- 回合内累加：`pkg/agentcore/runtime.go:195-197` `state.Usage = state.Usage.Add(res.Usage)`（每次模型调用）。
- 观察层计量（预算该用的那个数）：`internal/personal/observe.go:114-161` `attemptObserver` —— 每次 provider 尝试写一条 model 事件（`:123-131` 带 in/out/cache token），`:145-150` 成本「provider 报的优先，否则按配置价格算」，`:155-160` 交给 `runUsageMeter.addModel`。
- `internal/personal/usage.go:16-44` `usageCollector`（turn hook 口径，包含失败/取消的回合）、`:253-271` `addModel`（`raw` 判空、`priced` 入账、发 `usage` 事件、`forwardChildUsage`）、`:222-233` `forwardChildUsage`（子 agent 的用量进父回合 meter，回合没在跑就先挂起）、`:316-327` `newRunUsageMeter`（`base` = 本回合之前的会话累计，`Session = base + run`）。
- 会话累计：`internal/personal/runner_finish.go:51-64` `withSessionUsage` → `Store.SumUsageTree`（含子 agent 会话）。
- 成本：`internal/personal/price.go:6-8`（每百万 token 的 `Input/Output/CacheRead/CacheWrite`）、`:13-17` `Price.Cost`（按 `ReportUsage` 归一后计费）、`:22-33` `costOf`（`u.CostUSD != 0` 优先，否则查价表，查不到 0）；`internal/config/providers.go:47-51` `Price` 结构、`:256-292` `readModelConfigs`/`readPrice`（`providers.<id>.models.<m>.price.{input,output,cache_read,cache_write}`）、`:559-580` `PriceLookup`；`internal/wiring/runtime.go:418` 把它交给 runner。
- 估算器（现成）：`internal/personal/tokens.go:20-27`（窄字符 1/4 token、宽字符 1 token，按 quarter 累计后向上取整）、`:32-45` `tokensOfText`、`:47-57` `wideRune`（East Asian Wide/Fullwidth）、`:59-83` `tokensOfMessages/tokensOfMessage`（content + reasoning + tool result + tool call 名与参数）。文件头注释记录了实测：单纯 bytes/4 会把中文会话读低 15–20%（同一请求 provider 报 522760，估算 440734）。

**D. 配置与包含**

- 加载：`internal/config/load.go:16-46`——只读 `SUGA_HOME/config.yaml`（或 `SUGA_CONFIG`），`.env` 只取 `SUGA_HOME/.env`，`gkconfig.ParseEnv` 只展开值里的 `${VAR}`，环境变量预算层由读取函数显式处理，不负责把环境变量映射为点分键；项目目录的 `config.yaml` **故意不读**。
- 优先级范例（本设计照抄）：`internal/config/maxsteps.go:14`（`DefaultMaxSteps = 240`）、`:22-36` `ReadMaxSteps`：`--max-steps`（>0）> `SUGA_MAX_STEPS` > `config max_steps`；越界报错而不是静默取 0。
- 子 agent 设置读取：`internal/config/agents.go:31-62`（`agents.max_tokens` 等，非法值报错）。
- 装配：`internal/wiring/runtime.go:261-264`（解析 maxSteps）→ `:444`（`personal.Config.MaxSteps`）；`internal/cli/run.go:186-189`（run 独有 flag `--injected`、`--max-steps`）、`:195-208`（`wiringOptions`）、`:229-231`（校验）。
- 环境变量注入的既有用法：`SUGA_MAX_STEPS`（`maxsteps.go:29`）、`SUGA_MODEL`、`SUGA_ADD_DIRS`（`internal/cli/run.go:79`）；gofer 侧文档 `docs/usage/gofer-personal-agent.md` 记录的就是这类变量。
- 运行期配置结构：`internal/personal/runner.go:81-86`（`MaxSteps` / `MaxRunTokens` / `RunTimeout`，注释：子 agent runner 用后两个）。

**E. 展示与观测**

- 文本模式：`internal/cli/run.go:284-292`——回合后若 `result.WrapUp` 非空，往 stderr 打一行 `output.WrapUpNotice`（stdout 只有答案）；`:363-370` `runSummary` 是收尾的用量行。
- `internal/output/wrapup.go:14-29` `WrapUpNotice`：`max_steps` 有专门文案，`max_tokens` 已有「本轮因 token 预算用尽收尾」，其它 reason 走 `本轮因 %s 收尾`。
- stream-json：`internal/output/streamjson.go:23-34`（`turn_end` 带 `usage`、每次模型调用后一行 `usage`）、`:110-118`（`result` 行带 `wrap_up`）；`internal/output/usage.go:11-14` `WithUsage` 给 result/input_required/error 行加 `usage` 与 `session_usage`。
- chat：`internal/chat/layout.go:154-174` `TurnStatus`、`:179-206` `BuildTurnStatus`（`in %d / out %d tokens · ctx 55% · 1m23s · 15:04 · 插话 N`）、`:212-217` `contextShare`；`:219-260` 退出汇总。
- trace：`internal/personal/trace.go:15-29`（jsonl 字段含四个 token 列）、`:143-224` `traceLine`（`in= out= cached=` 与 `finish=truncated`、`attrs` 各部分的渲染点）；事件存储列见 `internal/personal/store/types.go:151-168`，事件 kind 常量在 `:46-100`（新增 kind 只是加常量，无需迁移）。
- ACP：`internal/acpserver/server.go:278-285`（拒绝 → `refusal`、yield → `max_turn_requests`、否则 `end_turn`）；`acp.StopReasonMaxTokens = "max_tokens"` 在 `github.com/coder/acp-go-sdk@v0.13.5/types_gen.go:6148` 已有。
- 统计：`internal/cli/stats.go:60-111` 与 `internal/personal/store/report.go:58-68`（model 事件的 token/cost 聚合）。

**F. 现状缺口（调研条目）**

- §3 #2：主循环只有 `MaxSteps`（`runtime.go:125-138`），`MaxTokens` 类软限只在子 agent；建议加 `budget.tokens` / `budget.cost_usd`，超限以 wrap-up 收尾。
- §3 #3：用量完全依赖 provider 返回，不报即 0，无本地 tokenizer 兜底；`internal/personal/tokens.go` 目前只用于窗口估算。

## 总体方案

主线：**在 personal 层给每个回合装一个"预算钩子"**，复用子 agent `limitHook` 已经跑通的通道（读 `runUsageMeter` 的回合/会话累计 → 超限时 `ctrl.RequestWrapUp(reason, instruction)`），收尾与呈现全部沿用现有 wrap-up 机制。预算的**输入**（用量）来自观察层，provider 不报时由**估算 provider 包装器**补齐并打标；预算的**配置**照 `max_steps` 的三级优先级读，环境变量即 gofer 的注入通道。

三个要点：

1. **计量口径与统计口径一致**：预算读的就是 `runUsageMeter`（回合）与 `SumUsageTree`（会话）那两个数，不另造一套。token 口径 `max(输入−缓存读,0)+输出` 与 `agents.max_tokens` 一致；成本口径用 `costOf` 的结果（provider 报的 `CostUSD` 优先，否则 `price` 表）。
2. **检查点受内核约束**：主回合仅在 `AfterModel` 检查。AfterTool 的 control 是工具级且会被 runtime 拒绝，不能作为预算检查点；子 agent hook 每次 AfterModel 读取父 meter，父预算超限时子 agent 先收尾，父回合随后在 AfterModel 收尾。
3. **估算只在缺失处补，且全程可辨**：provider报了就不估；估算值进 model 事件列、`usage` 事件与 stream-json（`estimated: true`），chat 状态行在估算时用 `~` 前缀。

## 架构

```text
  config.yaml / SUGA_BUDGET_* / --budget-*        （三级优先级，同 max_steps）
            │
            ▼
   internal/config.ReadBudgetSettings()  ──► personal.Config.Budget{Budget{Tokens,CostUSD,Scope,…}}
            │
   wiring: personal.New(… Budget …)              （可选）provider = observed(estimating(real)); Pool 的每个 provider 成员、compact provider、子 agent provider 均包裹
            │                                                  │
            ▼                                                  ▼
   Runner.Run ─► turnHooks: [usageCollector, estimate?, budgetHook, …]
                                        │
      agentcore 回合循环                │ addModel/addChild 累计
      ┌───────────────────────────────┴───────────────────────────────┐
      │ 每次模型调用后 budgetHook.AfterModel → spent = turn|session 口径│
      │ 子 agent 每次 AfterModel 读取父 meter；父回合 AfterModel → 同上    │
      │ 超限 → ctrl.RequestWrapUp("max_tokens"|"max_cost_usd", 指令) │
      └───────────────────────────────┬───────────────────────────────┘
                                      ▼
   下一次请求 = 收尾请求（runtime.go:139-141、172-176）→ 回复结束回合
                                      │
      RunResult.WrapUp + usage ──► 文本提示 / stream-json(budget,wrap_up) /
                                   trace(budget 事件、estimated) / chat 状态行 / ACP stopReason
```

## 配置与包含

### 配置键（新）

```yaml
budget:
  # token 上限（口径：非缓存输入 + 输出）；0 或省略 = 不启用
  tokens: 2000000
  # 金额上限（美元）；0 或省略 = 不启用。用 providers.*.models.*.price
  cost_usd: 5.0
  # 计量范围：turn（默认，每次 Runner.Run）| session（会话累计，含子 agent 会话）
  scope: turn
  # provider 不报用量时：auto（默认，只补缺失项）| on（总是估算并对照）| off（不估算）
  estimate: auto
  # 估算系数，> 0，默认 1.0；> 1 让预算更保守（估算值乘该系数）
  estimate_factor: 1.0
  # 子 agent 与内部请求固定计入 v1
  # 占用达到该比例时 chat 提醒一次（0 关闭，默认 0.8）
  notice_ratio: 0.8
```

- 结构体与读取：`internal/config/budget.go`，`type BudgetSettings struct { Tokens int; CostUSD float64; Scope string; Estimate string; EstimateFactor float64; NoticeRatio float64 }`，`ReadBudgetSettings(flags BudgetFlags) (BudgetSettings, error)`；非法值（负数、`scope` 不是 turn/session、`estimate` 不是 auto/on/off、`estimate_factor <= 0`、`notice_ratio < 0`）**启动即报错**，与 `ReadMaxSteps` 的"不静默"风格一致。
- token 上限是整数；`cost_usd` 允许小数（`floatValue` 已有）。

### 包含优先级（与 `max_steps` 完全一致）

| 层 | token | cost | scope |
|---|---|---|---|
| CLI flag | `--budget-tokens N` | `--budget-cost-usd X` | `--budget-scope turn\|session` |
| 环境变量 | `SUGA_BUDGET_TOKENS` | `SUGA_BUDGET_COST_USD` | `SUGA_BUDGET_SCOPE` |
| config | `budget.tokens` | `budget.cost_usd` | `budget.scope` |

- flag 传 0 = 未给；环境变量给了非法值 → 报错（同 `SUGA_MAX_STEPS`）；`--budget-scope` 只接受 `turn`/`session`。
- `--budget-tokens 1` 这类极小值用于冒烟。
- chat 与 run 共用同一组 flag（与 `--max-steps` 目前只在 run 注册不同：预算同样值得在 chat 里用；见决策 D10）。

### gofer 的双层预算与单位换算

suga 的预算是**软收尾**：达到阈值后在 `AfterModel` 请求 wrap-up，尽量保留已完成工作；gofer 的 `--max-tokens` / `--max-cost` 是 job 级**硬熔断**，超限直接终止并标记 job failed。两者同时启用时，运维约定是让 `SUGA_BUDGET_TOKENS` / `SUGA_BUDGET_COST_USD` 早于 gofer 硬上限触发，给 suga 留出收尾空间；gofer 硬熔断优先级更高，可能截断 suga 的收尾，这是已知边界。

两套 token 口径不同：suga 是 `max(输入 token - 缓存读, 0) + 输出 token`，gofer 是输入 + 输出 + 缓存读/写。因此不能直接复制数值；应按历史缓存比例把 gofer 的硬上限换算成更高的 suga 软阈值余量，并在实际 job 类型变化时复测。成本统一为美元，但 gofer 不读取 `estimated` 标记；估算值会进入 ACP/stream-json 的平铺 usage，因此也会被 gofer 熔断计入。

### gofer 按 job 注入

不改 gofer 协议：job 里通过环境变量注入即可（`SUGA_BUDGET_TOKENS` / `SUGA_BUDGET_COST_USD` / `SUGA_BUDGET_SCOPE`），或直接在命令行加 `--budget-tokens`。`docs/usage/gofer-personal-agent.md` 增加一段说明与示例：

```text
gofer job run --cwd … --env SUGA_BUDGET_TOKENS=1500000 -- suga run --prompt … --injected
```

语义说明（写进文档）：job 的预算默认按**回合**计（`scope: turn`），因为 job 常续用既有会话，会话级累计会把历史花费算进来、一进门就超限；要按整个 job（含多回合）计，用 `SUGA_BUDGET_SCOPE=session` 并注意上面的双层预算约定。

## 计量口径

**token**（与 `agents.max_tokens` 一致，`internal/personal/subagent.go:211`）：

```text
spent_tokens = max(InputTokens - CacheReadTokens, 0) + OutputTokens
```

- 缓存读不计数：缓存读的单价通常低一个量级，长会话里同一个大上下文反复命中缓存（`ReportUsage` 的 `cache_read_tokens`）不该被当成新花费，否则会话级预算会在命中缓存时突然爆掉。
- `CacheWriteTokens` 仍在 `InputTokens` 里（Claude 的 `input_tokens` 含 `cache_creation`），会被计入——这是已知的口径差异，写进文档；如需要可另加开关（见待确认 Q4）。
- 子 agent 的用量默认计入（`forwardChildUsage` 已经进父回合 meter，`SumUsageTree` 已经进会话累计）；v1 固定计入子 agent；meter 当前没有独立子 agent 小计，因此不提供排除开关。
- 压缩 / 交接 / 标题等内部请求默认计入（`runUsageMeter` 现在就是这么记的，stream-json 的 `usage` 与 `suga stats` 同口径）；v1 固定计入内部请求；现有 purpose 是 `compact`（不是 `compaction`），标题不是模型调用，不提供排除开关。

**最坏超出量（运维上界）**：预算钩子只能在模型尝试完成后观察用量，因此一次普通回合的超出量不能简化为“触发点加一次收尾”。核对上界时按以下尝试集合计算：

```text
触发模型尝试
+ 收尾模型尝试
+ 收尾前可能的 compact/segment handoff
+ 空回复重试（最多 2 次）
+ finishIgnoredWrapUp 的 ToolChoiceNone 重试
+ provider 已报告用量的失败重试
+ 同批后台/前台子 agent 各自的 agents.max_tokens
```

这些是可能发生的尝试集合而非固定次数；后台子 agent 另按自身限制运行，父回合收尾不取消它们。该口径必须写进用户文档和计划中的最坏路径测试。

**成本**：

```text
spent_cost = Σ（每次模型调用）provider 报的 CostUSD，没有则 Price.Cost(usage)
```

- 与 `costOf`（`internal/personal/price.go:22-33`）完全一致，价格表键是 `providers.<id>.models.<m>.price.{input,output,cache_read,cache_write}`，单位是**美元/百万 token**（`internal/config/providers.go:47-51`）。
- provider 不报 cost 且价表里没有该模型时成本恒为 0 → 成本预算**不会生效**。此时在回合开始打一次警告（stderr + trace 事件）：`成本预算已配置，但 <provider>/<model> 既未配置 price 也不返回 cost_usd，本次成本预算不会生效`。
- 用估算 token 算出来的成本同样是估算（`estimated: true`）。

**估算（provider 不报用量时）**

- 触发条件：`budget.estimate` 为 `auto`（默认）时，仅当 `res.Usage` 的 `InputTokens`/`OutputTokens` 为 0 才补；`on` 时总是算一份并记录（用于对照真实值，不包含真实值）；`off` 时不估算，此时启动/回合开始警告「provider 不报用量且已关闭估算，预算按 0 计」。
- 输入侧：`tokensOfText(system) + tokensOfText(序列化后的 tools schema) + tokensOfMessages(messages)`（复用 `internal/personal/tokens.go:32-83`，含工具调用参数与 tool result）。
- 输出侧：`tokensOfText(content) + tokensOfText(reasoning) + Σ tokensOfToolCall(名称 + 参数 JSON)`（口径同 `tokensOfMessage`）。
- 系数：`tokensOfText` 已经按"窄字符 1/4 token、宽字符（CJK/全角）1 token"估算，再乘 `estimate_factor`（默认 1.0）。不宣称估算天然偏高：现行规则是可解释的近似，误差可能高也可能低；`estimate_factor > 1` 才是显式保守旋钮。
- 误差与定位：`tokens.go:11-19` 记录了实测——朴素 bytes/4 对中文会话低读 15–20%，现行宽字符规则已修正这一偏差；真实 tokenizer 与字符规则的残差仍存在（英文长词、代码、emoji、工具 JSON），**估算会用于预算、窗口判断与 `lastInput`（因此可能提前压缩），但不是未标记的真实计费**；因此：
  - model 事件的 `input_tokens/output_tokens` 记估算值（保持"回合用量"这个数的一致性），事件 attrs 加 `estimated: true` 与 `est_input_tokens/est_output_tokens`（`estimate: on` 时两者都有，可对照）；
  - `usage` 事件与 stream-json 的 `usage`/`result` 行加 `"estimated": true`（缺省 false 时不出现在 JSON 里，旧消费端不受影响）；
  - chat 状态行的估算数字加 `~` 前缀（`in ~12.3k / out ~1.2k tokens`）；
  - `suga stats` 的聚合沿用事件里的数，同样带 estimated 属性，报告里标注"N 次调用为估算"。
- 实现在 **provider 包装器**（`internal/personal/estimate.go` 的 `estimatingProvider`），放在观察层**内侧**、紧贴具体 provider：`observed(estimating(real))`，这样估算值同时进入 `state.Usage`、`usageCollector`、model 事件、`runUsageMeter` 与会话累计，只在一处补齐；估算标记通过 `GenerateResult.Metadata[agentcore.UsageEstimatedMetaKey]` 向外传，`llm.Attempt` 增一个 `Estimated bool` 字段（`pkg/agentcore/llm/observe.go:12-37`），`internal/personal/observe.go:123-131` 把它写成事件 attrs。

## 关键流程

### 1. 回合级 token 预算触发（主路径）

```text
配置 budget.tokens=200000, scope=turn
步 1..N：每次模型调用后 budgetHook.AfterModel
  spent = max(in-cache_read,0)+out 累计
  spent(=198000) < 200000 → 继续
步 N 的模型调用后 budgetHook.AfterModel → spent(=205000) ≥ 200000
  → ctrl.RequestWrapUp("max_tokens", 指令)
  → 事件 budget{name: exceeded, scope: turn, limit: 200000, used_tokens: 205000, reason: max_tokens}
步 N+1（收尾请求）= 原请求 + 收尾指令（仅本次请求，不落库）
  模型给出汇报，开头如实写明"因 token 预算（200000/205000）收尾"
  → 回合 completed，RunResult.WrapUp = "max_tokens"
  → stderr: 本轮因 token 预算用尽收尾，以上答复只基于此前已获得的信息
  → chat 状态行: budget 100% · in ~… （估算时带 ~）
```

模型若无视指令继续要工具：走 `finishIgnoredWrapUp`（取文本 → 无文本则 `ToolChoiceNone` 重试一次 → 仍无则固定兜底文案），回合仍以 completed + `max_tokens` 结束。

### 2. 子 agent 把父回合推过预算

```text
父回合步 k 调用 agent 工具（子 agent 最多 agents.max_tokens=400k、10m）
  子 agent 每次模型调用 → forwardChildUsage → 父回合 meter 增加
  子 agent 的 budget hook 每次 AfterModel 读取父 meter；超限则子 agent 收尾
  子 agent 返回后，父回合下一次 AfterModel 看到累计并收尾
（这是不改 agentcore 的可实现边界；后台子 agent 不受父回合收尾控制）
```

### 3. 会话级预算已用尽时开新回合

```text
budget.scope=session, budget.tokens=2000000；历史已花 2050000
用户又发一条消息 → Runner.Run → BeforeRun 检查：base（会话累计）已超限
  → short-circuit 为 completed + budget wrap-up，固定说明额度与已用，不发任何模型请求
  → chat 在状态行前提示一行：会话预算已用尽（2000000/2050000），本轮不调用模型
```

### 4. provider 不报用量

```text
provider 返回 usage 全 0（小网关）
estimatingProvider：in = tokensOfMessages(system+tools+messages) × factor
                    out = tokensOfText(content+reasoning+toolcall args) × factor
  → GenerateResult.Usage 填估算值，Metadata[usage_estimated]=true
  → model 事件 in/out = 估算值，attrs.estimated = true（另存 est_* 供对照）
  → runUsageMeter / 会话累计 / 状态行 / stream-json usage 全部按估算值走
  → 真实 provider 一旦开始报用量，估算自动停止（auto 只在字段为 0 时补）
```

## 与 suga-lznj（`max_output_tokens`）的关系

- **不同维度**：`max_output_tokens` 是**单次请求的输出上限**（provider 参数，防静默截断、防一次回答过长）；本设计管的是**累计用量**（回合/会话花了多少，何时停）。前者决定"一个回答能多长"，后者决定"还要不要继续跑"。
- **互补，且互相需要**：
  - 成本预算的上限部分由 `max_output_tokens` 决定（单次输出越长，累计越快撞线）；
  - 收尾请求本身要能给出完整汇报，`max_output_tokens` 太小会把收尾答复截断（`finish=truncated`），因此本设计要求：**收尾请求使用不低于普通请求的输出上限**，并在 `output.WrapUpNotice` 之外，在收尾答复被截断时额外提示（沿用 trace 的 `finish=truncated` 与 chat 渲染）。
- **配置命名空间一致**：`providers.<id>.models.<m>.{max_output_tokens, price, context_window}` 同属模型能力/价格表，两个设计不冲突。
- **顺序建议**：suga-lznj 先做（输出上限确定后，估算器与成本口径才有稳定的输入），但不阻塞本设计实施；本设计不依赖 `max_output_tokens` 存在（默认值下也能工作）。

## 对 chat / run / ACP / gofer 的影响

| 面 | 变化 |
|---|---|
| chat | 状态行（`BuildTurnStatus`）末尾在配置了预算时加 `budget 62%`（会话 scope 时写 `session budget 62%`）；占用 ≥ `budget.notice_ratio` 时回合中打一行淡色提醒（每回合一次）；超限时回合末一行「本轮因 token/成本预算用尽收尾（200000/205000）」；估算数字带 `~`；`chat.budget` 之类开关不需要——不配置预算就完全没有变化 |
| run | `--budget-tokens/--budget-cost-usd/--budget-scope`；文本模式 stderr 多一行收尾原因（已有机制，新增成本文案）与一次性警告；stream-json 的 `usage`/`result`/`turn_end` 行加 `budget` 对象与 `estimated` |
| ACP | `session/prompt` 的预算收尾（`max_tokens` 与 `max_cost_usd`）统一映射为 `end_turn`，与现有 `max_steps` 收尾一致；只有单次 provider 截断才使用 SDK 的 `max_tokens`；ACP 预算只能从 config/env 读取，`usage_update` 增加 `budget_limit/budget_used`（可选字段）与 `estimated` |
| gofer | 不改代码；文档给出用环境变量注入预算的办法；job 里超预算的回合以 completed + `wrap_up` 结束，`suga trace --json` 能看到 `budget` 事件 |

stream-json 新增字段（都向后兼容，只在配置了预算时出现）：

```json
{"type":"usage","usage":{...},"estimated":true,
 "budget":{"scope":"turn","tokens":{"limit":200000,"used":198456,"remaining":1544},
           "cost_usd":{"limit":5,"used":1.23},"exceeded":false}}
{"type":"result","status":"completed","wrap_up":"max_tokens","budget":{...,"exceeded":true}}
```

## 安全、数据、运维与回滚

- 安全：预算不放宽任何权限；它只影响"模型还能不能继续跑"。估算值写入统一累计但始终带 `estimated` 标记，不得被当作未标记的真实计费。
- 数据：**无 schema 迁移**。新增的事件 kind（`budget`）只是 `events.kind` 的新取值（该列是自由文本，`internal/personal/store/types.go:46-100`）；`RunResult` 不加字段（沿用 `WrapUp`、`RunUsage`）。stream-json 新字段是可选键。旧事件与旧库可读。
- 运维：
  - 默认**不启用**（`budget.tokens`/`cost_usd` 都未设置时行为与今天完全一致），因此回滚只需删配置/去掉环境变量；
  - `budget.estimate: off` 可在估算带来困扰时关掉；`scope` 可随时在 turn/session 之间切换（即时生效，不依赖历史数据）；
  - 会话预算已用尽时 `BeforeRun` 直接 short-circuit 为 completed + budget wrap-up，不发模型请求；这也避免 steering 自动续轮反复付费。预算收尾不消费插话；chat 退回的插话若用户继续，作为新回合重新获得 turn 预算。后台子 agent 不受父回合收尾控制，只受自己的 `agents.max_tokens`/超时约束；挂起用量不挪入下一回合的 turn 预算，单独作为 pending 观测并在 session scope 结算。
- 平台/兼容：无新依赖；`estimated` 与 `budget` 字段对旧消费端是未知键（stream-json 消费端按类型分派）。
- 可观测性：会话预算已用尽的 short-circuit 不发模型请求；`budget` 事件在 `suga trace` 文本行渲染为 `budget exceeded scope=turn tokens=200000/205000 cost=$… estimated=false`，JSON 里是完整 attrs，`suga trace --json` 可断言（冒烟用）。

## 决策

- **D1 复用 wrap-up，不做硬中断**：普通回合超限即 `RequestWrapUp`；session 已用尽则 BeforeRun short-circuit，不发模型请求。上界按 B-M1 的完整式子表达，不简化成一次收尾请求。
- **D2 口径照抄子 agent**：`max(输入−缓存读,0)+输出`；理由：一个口径全项目一致、缓存读不该算花费。
- **D3 只在 `AfterModel` 检查**：AfterTool control 在当前内核会被丢弃；父预算下沉到子 agent hook 是不改内核下最及时且可实现的方案。
- **D4 预算是"这一回合/这一会话"，不是进程**：只做 `turn` 与 `session` 两个 scope。备选：再加 `process`（chat 一次进程）——与 session 在实践中近似（chat 一次进程通常就是一个会话），多一个语义只会让 gofer 注入更容易搞错，弃；gofer job 用 `scope: turn`。`RunInput.MaxSteps` 有同名 per-run 包含机制（`runtime.go:429-434`），本设计不新增 per-run 输入字段，靠 flag/env 即可（备选：加到 `RunInput`，弃，避免动内核契约）。
- **D5 估算做成 provider 包装器且位于 observer 内侧**：具体 provider 先 estimating、再 observed；Pool 每个成员分别包裹。估算值同时进 `state.Usage`、hook 收集器、model 事件、run/session 累计；标记经 `GenerateResult.Metadata` + `llm.Attempt.Estimated` 外传。备选：在 `usageCollector.AfterModel` 里估——只有 hook 收集器能看到，model 事件与 stream-json 仍是 0，口径分裂，弃。备选：在 `attemptObserver` 里估——`llm.Attempt` 不带请求与响应正文（只有 `TextChars/ReasoningChars`），估不准，弃。
- **D6 `estimate: auto` 默认只补缺失项**：provider 报了什么就信什么；`on` 供对照与调参。备选：不报就按估算**包含**——会把网关的部分上报当成没有，弃。
- **D7 内部请求默认计入且不提供 v1 排除开关**：压缩/交接的调用是真金白银，且与 stats、stream-json 口径一致。
- **D8 子 agent 默认计入，但子 agent 自己仍有 `agents.max_tokens`**：双层预算；后台子 agent 不受父回合收尾控制。
- **D9 成本上限用现有 price 表**：`providers.<id>.models.<m>.price.*`；provider 报的 `CostUSD` 优先。没有价表也没上报时打警告（宁可明说预算失效，也不假装在管）。
- **D10 flag 在 chat 与 run 都注册**：`--max-steps` 目前只在 run；预算在 chat 里同样有用（长会话止损），且实现是同一个 `personalOptions` 字段。备选：只给 run —— 会把最需要预算的交互场景漏掉，弃。
- **D11 不新增 `RunInput`/`RunResult` 字段**：预算信息经配置 + 事件 + stream-json 暴露，`RunResult.WrapUp` 已有原因位。保持内核契约稳定。
- **D12 `scope` 只用 `turn`/`session` 两个值，大小写不敏感**（写入时归一为小写），非法值报错。

## 测试与冒烟验收口径

单元测试（`github.com/gookit/goutil/x/assert`，多用例 `t.Run`）：

1. `internal/config`：`ReadBudgetSettings` 的 flag > env > config 优先级；`scope`/`estimate` 非法值报错；`cost_usd` 小数；未配置时 `BudgetSettings{}`（不启用）。
2. `internal/personal`（预算钩子）：
   - `AfterModel` 用 `max(in-cache_read,0)+out` 判定：缓存读大而输出小不触发（构造 usage 用例）；
   - 子 agent AfterModel 读取父 meter 并触发收尾；父回合下一次 AfterModel 再收尾；
   - 同一回合只请求一次收尾（`Control.RequestWrapUp` 的 first-wins 已有实现，断言不重复发 `budget` 事件）；
   - `scope=session` 用 base+run 判定，且 `BeforeRun` 时已超限直接进入收尾；
   - 子 agent 与内部请求固定计入，且内部 purpose 使用 `compact`；不测试不存在的排除开关；
   - 成本口径：provider 报 `CostUSD` 优先；无上报时按 price 表算；都没有 → 不触发 + 打一次警告。
3. 估算器：中文/英文/混合文本的 `tokensOfText` 回归（含 emoji、全角标点）；只补缺失字段（provider 报一半时的行为）；`estimate: off` 不补；`estimate: on` 时真实值不被包含；`estimated` 标记传到了 model 事件 attrs 与 `GenerateResult.Metadata`。
4. `internal/output`：stream-json 的 `budget` 对象与 `estimated` 字段（配置预算时才出现；`wrap_up` 取值）；`WrapUpNotice` 的成本文案。
5. `internal/chat`：`BuildTurnStatus` 的 `budget NN%` 与估算 `~` 前缀；未配置预算时状态行逐字节不变（回归）。
6. 冒烟（**必做，不能留给人工看**）：新增 `scripts/ptysmoke/chat_budget.py`：`fakellm.py` 返回固定 usage；`--budget-tokens` 设成很小的值 → 断言 chat 里出现收尾提示行、状态行 `budget 100%`，并用 `suga trace <sid> --json` 断言 `budget` 事件（attrs reason/limit/used/scope）与 `estimated` 标记。加入 `make smoke-chat`。**反向验证**：临时去掉 `budgetHook` 的 `AfterModel` 检查，确认该场景失败。
7. ACP：预算收尾时 `session/prompt` 返回 `stopReason=end_turn`；单次 provider 截断才返回 `max_tokens`（现有 acpserver 测试风格，`internal/acpserver`）。
8. 真实 provider 一轮（AGENTS.md 要求 provider 协议改动才必做；本设计不改协议，但改了用量读取与估算）：用真实 provider 跑一轮带工具调用的 `suga run`，核对 trace 里 model 事件的 token 与 provider 上报一致、`estimated` 未出现。
9. 基准/口径核对：`scope=session` 下 `suga stats` 的会话合计与预算判定的数一致（同一函数来源，加一条对照测试）。

## 待确认事项

- **Q1 默认是否启用**：采纳**默认不启用**（不设 `budget.tokens`/`cost_usd` 即无行为变化），只在 config 里显式开启；备选：给一个温和的默认（如 `budget.tokens: 5000000`，防止"忘了配"）——但那会悄悄改变现有用户的行为，需要明确同意。
- **Q2 内部请求是否计入**：已采纳计入；v1 不提供 `include_internal` 开关（meter 尚无可靠 purpose/source 小计）。
- **Q3 收尾本身也花钱**：接受收尾请求可能继续产生费用，不做二次硬中断；最坏上界按 B-M1 的完整式子记录。
- **Q4 缓存写是否计入**：建议计入（`CacheWriteTokens` 落在 `InputTokens` 里，与子 agent 口径一致）；备选：单独减去（更精确的"新 token"口径，但与 `agents.max_tokens` 不一致）。
- **Q5 会话预算已用尽时**：采纳直接 BeforeRun short-circuit，不发起模型请求，返回固定 completed + budget wrap-up；理由是避免每次续用会话和 steering 插话重复付费。
- **Q6 估算方向与系数**：建议默认 `estimate_factor: 1.0`（口径与 `tokensOfText` 一致，含义可解释），把 `>1 更保守`留给需要的人；备选：默认 1.1（保守）。
- **Q7 预算占用提醒的呈现**：建议 chat 状态行常显 `budget NN%` + 到 `notice_ratio` 打一次提醒行；备选：只在超限时提示（更安静，但用户无法预期"还有多久"）。
- **Q8 成本没有价表时**：建议打警告并让成本预算失效；备选：把内置的常见模型价格表补进来（需要维护价格数据，属另一个话题）。
- **Q9 后台子 agent 是否受父回合收尾控制**：建议不受控制，只受自身 `agents.max_tokens` 与 timeout 约束，挂起用量在 session scope 结算；备选是取消后台任务，但会破坏异步工具语义。
- **Q10 gofer/suga 双阈值如何运维**：建议按历史缓存比例把 suga 软阈值设在 gofer `--max-tokens/--max-cost` 硬上限之前，并持续复测；备选是只配一层预算，但可能在 suga 收尾前被 gofer 硬终止。


## 结论与人工计划 Gate

方案把"回合/会话的 token 与成本预算"做成 personal 层的一个钩子：用量取自现有观察层，超限走现有 wrap-up 收尾，配置与优先级照 `max_steps`，provider 不报用量时由 provider 包装器补齐并全程标 `estimated`。内核（`pkg/agentcore`）只加一个估算标记字段，不改协议、不加存储迁移；默认不启用，回滚成本极低。与 suga-lznj 分成两个维度（输出上限 vs 累计预算），互不阻塞。

下一步：请用户就 Q1–Q8 给出选择（可按建议默认）→ 起草 `docs/plans/2026-10-16-turn-budget-plan.md`（建议分两段：S1 计量与配置 + 钩子收尾；S2 估算、chat/stream-json/ACP 呈现与冒烟）。批准设计不授权实施。
