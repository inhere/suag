# 子 agent（2026-10-13）

关联：issue suag-dmv6；后台任务设计（docs/design/2026-10-13-background-tasks-design.md）中“子 agent 的衔接”一节。参考：Claude Code 的 Agent 工具（类型化子 agent，Explore 用只读工具集、独立上下文、只返回最终报告、可后台运行）、omp 的子 agent。

本文只做设计。待定项已由用户确认按建议执行。初稿经过一次独立审核（对照代码），审核指出的问题已在本稿修订，修订处见文末“审核后的修订”。

## 目标

1. 主 agent 能把探索、调研类工作交给子 agent。典型的有读大量文件、找调用链、比对多个实现、查依赖 API。子 agent 在独立上下文里完成，主上下文只收到结论，主会话的上下文和步数因此减少。
2. 子 agent 默认只读，不能越过父会话的权限。它的用量计入父会话的本次运行与会话累计，gofer 预算依赖这一点。它带回的外部内容会使父会话被标记为污染。
3. 可以后台运行：几个子 agent 同时调研，主 agent 继续工作，结束时收到通知。

不做：子 agent 写文件或执行 shell（第一期）；子 agent 再派子 agent；跨进程或重启后接管。

## 现状要点（调研与审核核实）

- `personal.New(Config)` 可以构造另一个 Runner。会话门控与锁按会话划分，同一进程里父子 Runner 并发没有冲突。trace 事件按 ctx scope 归到子会话（`runner.go:393`）。
- `wiring.Open` 会重开 store、重启 MCP、重扫 skills。`Runtime` 没有保存原始 provider 与模型设置，`repo` 是私有字段。
- 有 `fallback_models` 时 provider 是 `*llm.Pool`。`observeProvider` 会对它调用 `SetObserver`，修改的是共享实例（`observe.go:190-195`），所以子 Runner 不能复用父 Runner 的 provider 实例。
- 只读工具集：`native.BuildTools(env, false)`。chat approver 与 ACP 的 Decide 对 ReadOnly、AgentState 工具不询问。ACP 通过 `LookupTool` 查工具元信息，工具必须注册进 Registry，否则会被当成需要询问。
- `recall` 声明为 External（`native/ledger.go:202-204`），`memory.taint_tools` 与 `taint_on_spill` 也会污染会话。父会话只在工具没有返回 Go error 时检查污染（`observe.go:269-272`）。
- 同一次回复里的多个工具调用按顺序执行（`pkg/agentcore/runtime.go:463-497`）。
- 用量：
  - 会话累计来自 `SumUsage(会话)`，只算该会话自己的 model 事件（`store/events.go:95-101`）。`finish` 用它覆盖 SessionUsage，下一轮 usage meter 的 base 也取它。
  - meter 把每次调用的输入作为 `LastInputTokens` 发出（`usage.go:187`），ACP 用它报上下文占用。
  - chat 状态行与 `run` 文本行用的是 `result.Usage`，不是 RunUsage。
- 收尾（wrap-up）只由步数上限触发，状态在 agentcore 运行时内部（`runtime.go:125-160`）。收尾那一步模型若仍调用工具，运行直接以 max_steps 报错（`runtime.go:186`）。hook 没有请求收尾的入口。
- `UpdateSession` 整行覆盖 `meta_json`（`store/sessions.go:121-130`），`markRunning` 与 `finish` 都是先读后写。有并发写入方时，会丢掉其间写入的 meta（例如 `tainted_at`）。
- 任务能力（`task` 工具、TaskNotices）只在写模式注册（`runtime.go:207-214,303-305`），而 chat 默认只读。
- ACP 每个 prompt 的 runtime 结束就 close，store 一并关闭（`acpserver/server.go:243`）。
- 环境段按父会话模式写“可写/只读，请告诉用户如何开启写入”（`prompt.go:86-93`），禁用工具提示会让模型提示用户加 `--allow-write`（`native_tools.go:26-43`）。

## 方案

### 工具 `agent`（分层）

- `pkg/agentcore/tools/native` 放 `AgentTool` 与接口 `SubAgents`（参照 TaskTool 与 LedgerSearcher 的写法），只做参数解析、结果格式与元信息（ReadOnly，Category Other）。
- 实现在 internal：wiring 组装，personal 提供运行子 agent 的方法。
- 注册进 capabilities 的 Registry，所以 ACP 的 `LookupTool` 能看到它是 ReadOnly，chat 与 ACP 都不询问；只读模式下也注册。
- 配置开关 `agents.enabled`（默认 true），用于对照评测与关闭。

参数：

| 参数 | 说明 |
|---|---|
| `description` | 3–8 个词的短描述，显示在 chat、ACP 与 trace |
| `prompt` | 完整任务：要找什么、范围、希望报告写成什么样（结论、文件:行号） |
| `type` | `explore`（默认）或 `research` |
| `background` | true 时作为后台任务运行，立即返回任务 id，结束时通知 |

类型：

- `explore`：工作区只读工具，即 file_read、file_list、file_grep、file_find。不含 `recall`，因为它会污染会话，而子会话里也没有可召回的父会话输出。
- `research`：explore 再加 web_fetch、web_search。

污染不按类型判断：子 agent 结束后读子会话 meta 的 `tainted_at`，有就在工具结果的 Metadata 里带 `external: true`，由父会话现有的污染路径标记（同步与后台两条路径都是如此，见下）。

结果：子 agent 的最终回答（报告），末尾附一行 `[sub-agent sess_x · 12 steps · in 85k (cache 60k) / out 3k · 41s · stop=final]`。子 agent 的提示词要求报告控制在约 4000 字符以内：超过 8000 字节时父会话只看到首尾片段，而读全文要用 `recall`，会污染父会话。所有失败（模型最终失败、上限、取消以外的错误）都返回 `IsError` 的结果而不是 Go error，这样父会话的污染检查与 trace 照常生效。

### 子 Runner

personal 层新增 `Runner.RunChild(ctx, ChildRequest)`，wiring 层新增子 agent 工厂。每次调用都新建：

- **自己的 store 句柄与 provider**：按 `agents.model`（默认主模型）重新 `BuildProvider`/`buildMainProvider`，不复用父实例，避免共享 Pool 的观察者被改，也让后台子 agent 不依赖 ACP 每个 prompt 的 runtime 生命周期。store 打开很便宜（P1 的任务结束记录已经这样做）。窗口、价格、thinking、fallback 都按子 agent 使用的模型取。
- **自己的 Registry 与 Ledger**：按类型注册只读工具，不注册 `agent`（从结构上杜绝递归）、`task`、`shell_exec`、写工具、memory、skill、MCP 工具。审批层：只读放行，其余拒绝；同样经过危险删除守卫（实际不会触发）。
- **不带父 Runner 的 hooks**：ACP 通知、chat 渲染、外部 hook 都不挂。
- **专用提示词**：系统提示词说明“你是子 agent，只读，最终回答就是交给主 agent 的报告，包含结论与证据（文件:行号），控制长度”。环境段固定写只读，并说明需要写入时在报告里写明，不提示用户开启写入；禁用工具的提示同样改写。段上下文保留项目规则，不注入记忆索引与 skills。
- **上限**：`agents.max_steps` 默认 30，用子 Runner 自己的 `Config.MaxSteps`。`agents.max_tokens` 默认 400k，按子 agent 各次模型调用的“未命中缓存的输入加输出”累计，避免长上下文下缓存读取把上限迅速耗尽；S2 评测时实测再调。`agents.timeout` 默认 10m。三者到达时都走收尾（见下），不直接报错。

### 收尾机制（agentcore）

- agentcore 新增 hook 请求收尾的入口：`Control.RequestWrapUp(reason)`，在 BeforeModel 里调用。运行时下一次请求按现有的 wrap-up 方式进行（不带工具、追加收尾提示），提示文案按 reason 区分：步数、token、时间。这在 pkg 层，不依赖 internal。
- 收尾那一步模型仍调用工具时：子 agent 场景下不报 max_steps 错误，改为取子会话里最后一条 assistant 文本作为报告，`stop` 记为对应 reason。
- 超时是软截止：到时请求收尾；再过宽限 60 秒仍未结束才硬取消，硬取消时同样取最后一条 assistant 文本。
- 父回合被取消（Ctrl-C、session/cancel）时，子 agent 一并取消。父回合本身随之失败，工具结果回不到父模型，已有的部分只留在子会话里，可以用 `suag trace` 查看。
- `RunResult` 新增 `StopReason`（final、max_steps、max_tokens、timeout、cancelled），供结果行与 trace 使用。

### 子会话

- `NewSession` 直接带 meta 创建子会话（原子操作，不留可见的孤儿会话）：`kind: agent`、`agent_type`、`parent_session_id`、`parent_turn_id`、`parent_call_id`，标题取 description。
- 修复 meta 覆盖：`markRunning` 与 `finish` 改为只更新状态相关的列（status、last_turn_id、cwd、updated_at），不再整行写回 meta。这一条对现有的污染标记同样有益。
- 隐藏：
  - `suag sessions` 默认在 SQL 里按 `json_extract(meta_json,'$.kind') = 'agent'` 过滤，加 `--agents` 显示子会话（`--limit 0` 已表示全部，所以不叫 `--all`）。
  - `session_search` 的 workspace 与 all 范围同样在 SQL 里过滤子会话。
  - `suag trace <子会话>` 照常可看。
- gc：子会话随父会话一起处理，不单独删除；父会话被删时一并删除子会话。

### 用量口径

- **本次运行（RunUsage）**：子 agent 的模型调用计入父会话当前进行中的 run。子 meter 持有父 meter 的引用，转发带价格的用量，`LastInputTokens` 置 0，不覆盖父会话的上下文占用；不并入 `usageCollector`，它用于压缩阈值。`ToolCalls` 只计父会话自己的工具调用（`agent` 本身算一次），子 agent 的工具调用数记在 tool 事件的 attrs 里。
- **会话累计（SessionUsage）**：定义为父会话加它的全部子会话。新增 `SumUsageTree`，按 `parent_session_id` 汇总，meter 的 base、`finish`、`suag stats --session` 都用它。这样回合中的 usage 事件、回合结束的 `session_usage`、ACP `_meta.session` 与成本口径一致。
- **chat 状态行**：改用 RunUsage，或者在状态行单列子 agent 的用量。
- **后台子 agent**：用量计入该会话当前进行中的 run；没有进行中的 run 时暂存，下一个 run 开始时一次计入。不在已结束的 run 之后发 usage，否则 stream-json 会在 result 行之后出现 usage 行，ACP 会在 prompt 之外发 usage_update。

### 观测

- 父会话的 tool 事件 attrs：`child_session_id`、`agent_type`、`steps`、`tool_calls`、`input_tokens`、`cache_read_tokens`、`output_tokens`、`cost_usd`、`stop`。在 `addCallAttrs` 加 `case "agent"`，从结果 Metadata 拷贝；跳过 exit code 解析，避免误匹配报告正文。
- `--analyze` 单列子 agent 的次数、步数、token、耗时；`--web` 的 agent 行可跳到子会话的 trace（展示部分交给 suag job）。

### 进度显示

- 新增事件种类 `agent_progress`，通过 ctx emitter 传到父回合（参照 `withPlanEmitter`），按白名单只转发子 agent 的工具调用摘要。不复用 `tool_call` 或 `tool_result`：那样会触发 chat 的 PostToolUse hook、变成 stream-json 的工具行，子会话的 `run_finished` 还会被当成 `turn_end`。
- chat：`⏺ agent <description>`，进度显示为缩进的灰色行（`  · file_grep pattern=...`），结束时显示 `⎿` 摘要。
- stream-json：输出 `agent_progress` 行（gofer 不认识的种类会忽略）。
- ACP：tool_call 的标题改用 description（现在用工具名，`policy.go:103,152`），开始与结束照常通知；第一期不发进度更新。

### 后台模式

- `background: true`：作为任务管理器里 `agent` 类的函数任务运行，带 `NoEndNotice`。结束时用 `Notify` 发出“报告开头若干行加 ref”，不用默认的“最后 5 行”。过程中只用 `Count` 记步数，不调用 `Progress`，否则每一步都会让 `task wait` 提前返回；结束才调用一次 `Progress`。
- 只读模式也要有后台子 agent：只读模式下注册 `task` 工具（只能 list、output、wait、kill 本会话的任务；shell 的 `background` 仍只在写模式出现），并照常取通知。这修订了后台任务设计 P2 的相应条目。
- 污染：后台子 agent 结束时，若子会话已被污染，在返回前对父会话执行 `PatchSessionMetaIfAbsent(tainted_at)`。因为报告会经通知、`task output`、`task wait` 三条路进入父上下文，三条都不带 External 标记。
- 用量：见“用量口径”。
- 并行：同一次回复里的多个工具调用仍按顺序执行。并行靠后台：派几个 `background: true` 的子 agent，再 `task wait` 或等通知。之后看评测是否需要让同一回复里的多个 agent 调用并行。
- `task` 工具的描述补上 agent 类任务；`max_running`（每会话 4 个）与 shell 任务共用。

## 分阶段

1. **S1（内核与存储）**：
   - agentcore 的 `Control.RequestWrapUp`、按 reason 的收尾文案、收尾时仍调用工具的兜底，以及 `RunResult.StopReason`；
   - `NewSession` 带 meta；`markRunning` 与 `finish` 只更新状态列；
   - `SumUsageTree` 与 meter 的父子转发；
   - 子会话在 sessions、session_search 与 gc 中的处理。
   
   验证：单测，加上 meta 并发写入的回归测试。
2. **S2（同步子 agent）**：
   - 子 agent 工厂（自建 store、provider、Registry、Ledger、提示词、环境段）；
   - `agent` 工具（explore、research），上限、污染传递、结果格式；
   - `agent_progress` 事件、chat 进度行、ACP 标题、`agents.enabled`。
   
   验证：
   - 单测；
   - fakellm 新增子 agent 模式（按系统提示词识别子 agent 请求），伪终端冒烟覆盖进度行与 Ctrl-C 取消同步子 agent，并做反向验证；
   - 真实模型评测：新增一个大范围探索任务，开与不开子 agent 对照，比较父会话加子会话的总 token、成本、耗时，以及父会话的上下文大小。只比父会话的 token 会得出虚假的节省。
3. **S3（后台）**：
   - agent 类任务、只读模式的任务能力、后台用量暂存与计入、后台污染传递、通知格式；
   - 伪终端冒烟：后台子 agent 结束时唤醒空闲的 chat。
4. **S4（展示与文档）**：
   - trace、`--analyze`、`--web` 的子 agent 视图（交给 suag job）；
   - README 中英与配置说明；CHANGELOG。

每阶段提交前 gofmt、vet、容器测试；阶段结束主机全量测试与 staticcheck。

## 已定（2026-10-13 用户确认按建议）

1. 工具名 `agent`，类型 `explore`（无网络）与 `research`（加 web）两种。
2. 模型：默认用主模型；`agents.model` 可配成更便宜的模型（建议用户配一个）。
3. 并行：第一期靠后台任务（`background: true` 加 `task wait`），不改 agentcore 的顺序执行。
4. 子 agent 的上下文：保留项目规则与环境（环境段改为子 agent 专用），不注入记忆索引和 skills；使用专用的简短系统提示词。
5. 上限默认值：步数 30、token 400k（未命中缓存的输入加输出）、超时 10 分钟（软截止，宽限 60 秒）；到达时收尾，基于已有信息给出报告。
6. 子会话默认不出现在 `suag sessions` 与 `session_search` 中（`suag sessions --agents` 显示）。
7. 第一期子 agent 不能用 shell。

## 审核后的修订（2026-10-13）

独立审核（对照代码）后改动的要点：

- **用量**：会话累计明确定义为父会话加子会话（`SumUsageTree`）；子 meter 转发时 `LastInputTokens` 置 0；不并入 collector；chat 状态行改用 RunUsage；后台用量计入进行中的 run 或暂存。
- **provider**：子 agent 自建 provider，不复用共享 Pool（避免观察者被替换）。
- **污染**：按子会话实际的 `tainted_at` 传递，不按类型判断；explore 去掉会污染的 `recall`；失败返回 IsError 而不是 Go error；后台路径直接标记父会话。
- **meta 覆盖竞态**：`markRunning` 与 `finish` 只更新状态列。
- **收尾**：agentcore 新增 `RequestWrapUp`；token 与超时都走收尾；补收尾兜底与 `StopReason`。初稿“BeforeModel 里同样 wrap-up”的写法做不到。
- **分层与注册**：native 放接口与工具，internal 实现，注册进 Registry，ACP 才能识别它是只读工具。
- **进度事件**：新增 `agent_progress`，不复用 tool_call 或 tool_result。
- **环境段与禁用提示**：改为子 agent 专用。
- **后台**：ACP 下自建 store（不依赖 prompt runtime）；只读模式注册任务能力，修订后台任务设计 P2 的已定条目；只用 Count、结束时 Notify 报告开头；`NoEndNotice`。
- **子会话**：`NewSession` 带 meta；SQL 过滤；`--agents`；gc 随父会话处理。
- **验证**：`agents.enabled` 开关；评测比较父加子的总量；fakellm 子 agent 模式；Ctrl-C 冒烟提前到 S2。
- **其他**：不需要“按次覆盖步数上限”，子 Runner 有自己的 MaxSteps；报告长度限制；`addCallAttrs` 的 agent 分支跳过 exit code 解析。

## 评测记录（2026-10-13，explore-callers，真实模型，--thinking=high，各 2 次）

任务：30 多个文件里找出 `cfg.Validate` 的全部调用（7 处，另有同名方法、ValidateAll、注释等干扰）。用量取 `suag stats --session`，包含子会话。

| 组 | 通过 | 平均调用 | 平均提示 token | 平均耗时 |
|---|---|---|---|---|
| 关闭子 agent（`agents.enabled=false`） | 2/2 | 6.0 | 64k | 27s |
| 开启子 agent（初版提示词） | 2/2 | 9.5 | 172k | 52s |
| 开启子 agent（调整提示词后） | 2/2 | 11.0 | 138k（51k / 224k） | 52s |

结论：

- 功能正确：两种方式都能找全且不误报。
- 对这种一次 grep 就能回答的窄任务，子 agent 只增加开销。子 agent 读完了全部文件（只为排除别名导入），父会话拿到报告后又复查了一遍。
- 调整提示词后（工具描述写明“报告就是结果，不要重复搜索”；子 agent 提示词强调先精确搜索、少步数），父会话的复查从读文件变成几次 grep，但子 agent 的“周全”倾向仍在，结果波动大。
- 子 agent 的价值在于父会话的上下文保持很小（父会话末次输入约 20k，子 agent 自己涨到 68k），对之后的多轮对话有利；单轮评测反映不出来。后续需要一个多轮或大范围探索的评测来衡量。
