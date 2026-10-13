# 子 agent（2026-10-13，待确认）

关联：issue suag-dmv6；后台任务设计（docs/design/2026-10-13-background-tasks-design.md）中“子 agent 的衔接”一节。参考：Claude Code 的 Agent 工具（类型化子 agent，Explore 用只读工具集、独立上下文、只返回最终报告、可后台运行）、omp 的子 agent。

本文只做设计，待定项确认后再实现。

## 目标

1. 主 agent 能把探索、调研类工作交给子 agent。典型的有读大量文件、找调用链、比对多个实现、查依赖 API。子 agent 在独立上下文里完成，主上下文只收到结论，主会话的 token 和步数因此减少。
2. 子 agent 默认只读，不能越过父会话的权限；它的用量、耗时、污染状态都能在父会话看到，也计入父会话（gofer 预算依赖这一点）。
3. 可以后台运行：几个子 agent 同时调研，主 agent 继续工作，结束时收到通知。

不做：子 agent 写文件或执行 shell（第一期）；子 agent 再派子 agent；跨进程或重启后接管。

## 现状要点（调研结论）

- `personal.New(Config)` 可以直接构造另一个 Runner：传入同一个 `*store.Store`、另一个 provider、另一个工具 Registry 和 PromptBuilder 即可。会话门控与锁按会话划分，同一进程里父子 Runner 并发运行没有冲突。trace 事件按 ctx 里的 scope 归到子会话（`runner.go:393`）。
- 现在只能整体再调一次 `wiring.Open`：会重开 store、重启 MCP、重扫 skills。`Runtime.repo` 是私有字段，没有“复用 store 与 provider、只换工具集或模型”的入口。
- 只读工具集：`native.BuildTools(env, false)` 只保留 ReadOnly 的工具。chat 的 approver 与 ACP 的 Decide 对只读工具都不询问。
- 同一次回复里的多个工具调用按顺序执行（`pkg/agentcore/runtime.go:463-497`）。唯一的并行是只读本地工具的 Prefetch。
- 用量：子 Run 有自己的 `runUsageMeter`，父会话的 RunUsage、SessionUsage 和 gofer 收到的 usage 都不含子 agent 的消耗。
- 污染：子会话被污染不会传到父会话。
- 会话表没有 parent 字段；`--from` 交接在子会话的 handoff entry 里记来源。
- 步数上限 `max_steps` 加 wrap-up 现成可用，但 RunRequest 没有按次覆盖的字段。没有 token 或成本上限。
- 没有递归保护的标记。
- ACP 的通知 hook 挂在 Runner 的 `cfg.Hooks` 上，子 agent 不能共用父 Runner，否则会以父会话 id 发 tool_call 通知。

## 方案

### 工具 `agent`

参数：

| 参数 | 说明 |
|---|---|
| `description` | 3–8 个词的短描述，显示在 chat、ACP 与 trace |
| `prompt` | 完整任务：要找什么、范围、希望报告写成什么样（结论、文件:行号） |
| `type` | `explore`（默认）或 `research`，见下 |
| `background` | true 时作为后台任务运行，立即返回任务 id，结束时通知 |

类型：

- `explore`：工作区只读工具，即 file_read、file_list、file_grep、file_find、recall。没有网络，不会污染。
- `research`：explore 再加 web_fetch、web_search。子会话被污染时，父会话随之被标记污染（工具结果带 `external`）。

工具描述写明适用场景：探索范围大、要读很多文件、只需要结论时用它；一两个文件的事直接自己读。工具本身标为 ReadOnly，所以 chat 和 ACP 不会询问；只读模式下也可以用。

结果：子 agent 的最终回答（报告）。末尾附一行 `[sub-agent sess_x · 12 steps · in 85k (cache 60k) / out 3k · 41s]`。报告过长时走现有的输出治理（转存加 ref）。

### 子 Runner

在 wiring 里新增工厂 `Runtime.SubRunner(kind)`（按需创建，在 Runtime 生命周期内缓存）：

- 复用父 Runtime 的 store、provider（或 `agents.model` 指定的模型）、ledger 与输出转存、sessions 目录。
- 独立的工具 Registry：按类型注册上述只读工具，不注册 `agent`（从结构上杜绝递归）、`task`、`shell_exec`、写工具、memory、skill、MCP 工具。
- 不带父 Runner 的 hooks（ACP 通知、chat 渲染），审批层用“只读放行、其余拒绝”。
- 子 agent 专用的系统提示词：你是一个子 agent，只读，完成后给出结论与证据（文件:行号）；不要寒暄，最终回答就是交给主 agent 的报告。段上下文保留项目规则与环境，不注入记忆索引和 skills（见待定 4）。
- 上限：`agents.max_steps`（默认 30，到达后走现有的 wrap-up，让它基于已有信息给出报告）；`agents.max_tokens`（默认 400k，按子 agent 各次模型调用的输入加输出累计，超过后同样 wrap-up；在 BeforeModel 钩子里判断，参照 denialLimitHook）；`agents.timeout`（默认 10m，超时取消并返回已有部分）。

### 子会话

- 每次调用新建一个子会话，标题取 description。meta 记 `kind: agent`、`agent_type`、`parent_session_id`、`parent_turn_id`、`parent_call_id`。不改表结构，用 `PatchSessionMeta` 写入。
- `suag sessions` 与 `session_search` 默认不列子会话（`--all` 显示）。`suag trace <子会话>` 照常可看。

### 用量与观测

- 子 agent 的 RunUsage 并入父 Run 的 usage meter：父会话的 run 和 session 用量、stream-json 和 ACP 的 usage 更新、gofer 预算都包含子 agent 的消耗。
- 父会话的 tool 事件 attrs 记录：`child_session_id`、`agent_type`、`steps`、`input_tokens`、`cache_read_tokens`、`output_tokens`、`cost_usd`、`stop`（final / max_steps / max_tokens / timeout / cancelled）。
- `--analyze` 单列子 agent 的次数、步数、token、耗时；`--web` 时间线上的 agent 行可跳到子会话的 trace。
- `suag stats --session` 可以带上子会话（按 meta 的 parent 汇总）。

### 显示

- chat：`⏺ agent <description>`，运行中把子 agent 的工具调用显示成缩进的灰色进度行（`  · file_grep pattern=...`），结束时显示 `⎿` 摘要。子 agent 的流式文本不显示。
- ACP：工具调用 kind 为 `think`（或 other），开始与结束照常通知。第一期不做子 agent 的进度更新。

### 后台与并行

- `background: true`：作为任务管理器里 `agent` 类的函数任务运行。结束通知带报告的前几行与 ref；`task wait` 返回完整报告的位置；`task kill` 取消。
- 并行：同一次回复里的多个工具调用目前顺序执行。第一期并行靠后台：派出几个 `background: true` 的子 agent，再 `task wait` 或等通知（见待定 3）。

### 取消与错误

- 同步调用：父回合取消（Ctrl-C、session/cancel）时子 agent 一并取消，返回“已取消”和已有的部分。
- 子 agent 的模型错误照常重试；最终失败时，工具结果是错误，带子会话 id 便于查看。

## 分阶段

1. **S1**：wiring 的子 Runner 工厂、子 agent 提示词、子会话 meta、递归保护；personal 层支持按次覆盖步数上限，并新增 token 上限钩子。
2. **S2**：同步的 `agent` 工具（explore、research）、上限、用量并入父会话、污染传递、结果格式；单测和真实模型评测（新增一个“大范围探索”任务，对比开与不开子 agent 时主会话的 token）。
3. **S3**：后台模式（`agent` 类任务）、`task wait`、通知。
4. **S4**：chat 进度行与 ACP 显示、会话列表过滤、trace、analyze、web（展示部分交给 suag job）；伪终端冒烟与反向验证。

## 待定（请用户确认，建议在前）

1. 工具名 `agent`，类型 `explore`（无网络）与 `research`（加 web）两种。另一种做法是一个类型加 `tools` 参数自选工具。
2. 模型：默认用主模型；`agents.model` 可配成更便宜的模型（建议你配一个）。另一种做法是默认用 `compact_model`。
3. 并行：第一期靠后台任务（`background: true` 加 `task wait`），不改 agentcore 的顺序执行；之后看评测是否需要“同一回复里多个 agent 调用并行”。
4. 子 agent 的上下文：保留项目规则与环境，不注入记忆索引和 skills；使用专用的简短系统提示词。
5. 上限默认值：步数 30、token 400k、超时 10 分钟；到达时收尾，基于已有信息给出报告，而不是直接报错。
6. 子会话默认不出现在 `suag sessions` 与 `session_search` 中。
7. 第一期子 agent 不能用 shell（即使只读命令也需要审批，放到之后再议）。
