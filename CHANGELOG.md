# Changelog

本文件记录 suag 各版本的用户可见变更，格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循 [SemVer](https://semver.org/lang/zh-CN/)。当前处于 `v0`，次版本之间可能有不兼容变更，会在“不兼容”一节写明。

## [未发布]

### 变更

- `shell_exec` 不再把 suag 自身的 `SUAG_HOME`、`SUAG_CONFIG`、`SUAG_MODEL` 传给执行的命令：项目里读这些变量的代码或测试（如 suag 自己的测试）原先会作用在 agent 的配置上。在 shell 里再调用 suag 时落回默认的 `~/.suag`，需要时在命令中显式设置。

### 修复

- `suag trace --analyze`：只跑部分用例的测试（`go test -run`、`pytest -k` 等，常是为排查 bug 临时写的测试）不再算作“测试转绿”，阶段拆分以整包或整套测试首次通过为准。
- 测试不再受开发者环境中的 `SUAG_HOME`、`SUAG_CONFIG`、`SUAG_MODEL` 影响（原先设置了这些变量时 config 的 5 个 Load 测试与 CLI 的 manifest 测试会失败）；Windows 的“超时杀掉子进程”测试改为按子进程 PID 检查，不再被同机并行的测试误判。

## [0.14.0] - 2026-10-13

工具结果给出可信证据、并发读取与并行调用回传、缓存未命中归因与循环分析、Claude 思考档位；历史折叠改为默认关闭。同模型 T9 对比中模型调用从 44.7 降到 21.7（omp 23.3），依据见 [分析](docs/research/2026-10-11-suag-vs-omp-loop-analysis.md)。

### 新增

- shell 输出整理（`tools.minimize_output`，默认开）：去掉颜色码与进度重绘，折叠连续相同的行；测试与构建输出按输出规则裁剪：全部通过时只留各包结果和一行汇总（如 `go test: 1 package(s) ok, 20 test(s) passed`），有失败时只留失败用例及其信息、日志与编译错误。内置规则覆盖 go test、cargo test、jest/vitest/mocha 与 npm/pnpm/yarn/bun test、pytest、Maven、Gradle；`$SUAG_HOME/output-rules.yaml` 与 `<workspace>/.suag/output-rules.yaml` 可按名字替换、新增或用 `enabled: false` 关闭规则（格式见内置的 `output_rules.yaml`）。命令本身用管道过滤了输出时不套用规则（R6 中模型用 `go test -v | Select-String` 列用例名，被整理掉后只好 `recall` 原文）。原文保存为 ref，结果末尾注明，可用 `recall` 取回；`suag trace` 显示 `tidied(raw=…)`，`--analyze` 统计整理次数。
- `file_edit` 的结果带改动处的行号片段（前后各 2 行，最多 60 行），改 Go 文件时若编辑引入语法错误会附警告；`old_string` 找不到时指出缩进不同的同一行或最相似的行并给出该处内容，匹配多处时列出行号——模型不必为确认或定位再读一遍文件。
- 同一次回复里的多个本地读取调用（`file_read`、`file_list`、`file_grep`、`file_find`）并发执行（`tools.parallel_reads`，默认开）：读取在基础层提前并发进行，审批、污染追踪、trace 记录与持久化仍按调用顺序，参数被 hook 改过的调用照常重新执行。
- `--model`（以及 `SUAG_MODEL`、`model` 等引用）支持简单匹配：名字不是已配置的模型时按其一部分查找（不区分大小写，查键与 `model_id`，`provider/` 前缀限定范围），唯一匹配即用，如 `--model haiku`；多个匹配时报错并列出候选。
- `anthropic` 类型支持思考档位：Claude 4.6 及以后的模型发送 adaptive thinking 与 `output_config.effort`，4.5 及更早的模型发送 `budget_tokens` 并带 interleaved-thinking beta 头；带签名的 thinking 块随工具调用保存并原样回传。主模型为 `anthropic` 类型时不做历史折叠（折叠会改写早先消息，使 thinking 签名失效）。原先设置档位只会警告并忽略。
- 模型级 `type` 与 `base_url`（`providers.<id>.models.<model>.type` / `base_url`）覆盖 provider 的：同一网关的 Claude 模型可走 Anthropic 协议、其余模型走 OpenAI 兼容，不必拆成两个 provider。原先模型下写 `type` 会被忽略，Claude 模型仍按 OpenAI 协议请求而报错。
- `suag trace <session> --analyze [--json]`：循环分析——步数与并行步数、各工具次数与失败、重复读与写后回读、每个文件的编辑次数、重复执行的命令、测试运行次数及首次通过后又跑了几次、转存/折叠/压缩、最慢的模型调用、模型与工具各自的耗时、阶段拆分（探索 / 首次写入到测试首次通过 / 通过后）、编辑步数与单处修改步数、退出码为 0 但有 stderr 的 shell 次数，以及未命中缓存的输入按来源拆分：新增内容、前缀断开后重发的早先内容（按 fold/segment/system/tools/rewrite）与 provider 侧未命中（按请求 JSON 大小估算）。工具事件新增 `arg`（脱敏参数摘要）、`target`（文件路径）、`exit_code`（shell），模型事件新增 `tool_calls`、`reasoning_chars`、`text_chars` 与前缀对比 `prefix`、`prefix_reused`、`prefix_rebuilt`、`prefix_fresh`，shell 事件新增 `stderr`、`test_failed`（按输出判断，管道或 `;` 隐藏的失败也能识别），file_edit 事件新增 `changes`，参数摘要中的工作区路径写作 `<ws>`；`suag trace` 的工具行显示退出码、字节数、转存与参数摘要，模型行显示 `calls=N`、`cached=N`，前缀断开时显示 `prefix=原因(rebuilt=…)`。

### 修复

- 工具调用缺少必填参数时不再让整次运行失败：该调用不执行，模型收到点名缺哪个参数的错误结果，同一回复的其他调用照常执行。
- 回复被输出上限截断（OpenAI 兼容 `finish_reason=length`、Claude `stop_reason=max_tokens`）时，其中的工具调用一律不执行，回一条“请拆小后重发”的错误结果，不再用可能残缺的参数执行。
- 缓存命中统计兼容 DeepSeek 原生字段 `prompt_cache_hit_tokens`（网关只透传它时原先记为 0）。
- 按工具 schema 自动修复常见的参数形态：JSON 字符串形式的数组/对象、数字字符串、`"true"`/`"false"`，并去掉 null 值。

- `file_grep` 的 `glob` 与 `file_find` 的 `pattern` 也按相对 `path` 的路径匹配（原先只按相对工作区或读根的路径）：在模块缓存里搜 `path=…/goutil@v0.8.2`、`glob=x/assert/*.go` 原先返回 no matches。搜索路径不存在时报“… does not exist”。

### 变更

- shell_exec 的输出去掉 ANSI 颜色码（如 gookit/assert 的失败报告即使不在终端也带颜色），不再进入上下文。
- 使用约定新增两条：彼此独立的修改一起发（同一文件多处用 file_edit 的 `edits`，不同文件在同一次回复里）；依赖的 API 用一条命令列出（`go doc <包>` 或 grep 导出函数）。依据 R6 中 suag 一步只改一处、依赖查询逐文件读取的分析。
- 历史折叠改为默认关闭（`context.fold_every` 默认 0，设为 10 恢复原行为）：P5 复测中折叠后重建缓存前缀约占未命中输入的三分之一，且关闭后与 omp 的对比更直接。
- 同一次回复的多个工具调用在之后的请求里按原样作为并行调用回传：一条助手消息带全部调用，其后是各自的结果（OpenAI 兼容与 Anthropic 都是）；原先存成“调用、结果、调用、结果”交替排列，模型在历史里看不到并行的样例。只影响新产生的历史。
- OpenAI 兼容 provider 只在带工具调用的助手消息上回传 `reasoning_content`，普通文本回答不再携带（减少输入 token；DeepSeek 已实测接受）。
- 历史折叠的说明改为 `[folded: N chars sent in an earlier step]`，去掉“需要时重读文件”的提示（它会诱导多余的读取）。
- `file_read` 每页上限与工具结果的内联阈值分开：新增 `tools.read_page_bytes`（默认 32 KiB，原为与 `tools.max_output_bytes` 相同的 8000 字节），多数源码文件一次读完；`paths` 一次最多四页。
- 使用约定：独立的读取、搜索、命令放在同一次回复里；能一次读完的文件整体读；编辑或写入成功无需回读；验证由近及广，通过即停，不为确认而重跑或回读。依据同模型对比的步数分析（研究文档“suag-jz0l 评审”一节）。
- chat 开头一行在模型后显示思考档位与上下文窗口，如 `Suag chat · command-code/deepseek-v4.1-flash · thinking high · ctx 950k`（未设档位时不显示该项）。

## [0.13.0] - 2026-10-12

历史折叠（默认开启）、安全删除工具 `file_remove`、思考档位与模型短名。依据见 [分析](docs/research/2026-10-11-suag-vs-omp-loop-analysis.md) 与 [历史折叠设计](docs/design/2026-10-12-history-folding-design.md)。

### 新增

- 内置工具 `file_remove`：删除工作区内的文件或目录（目录需 `recursive`），移入 `$SUAG_HOME/trash/` 保留 7 天、可恢复，单次最多 1000 个文件。被 `.gitignore` 忽略的目录（及 `node_modules`、`vendor`、`dist`）视为可再生：不遍历、不计数、直接永久删除并在结果中列出（被忽略的 `tmp/` 下的测试仓库可整体删除，回收区不会被缓存撑大）；被忽略的单个文件（如 `.env`）仍进回收区。拒绝工作区根、工作区自己的 `.git`、用户主目录、通配符与工作区外路径。chat 询问显示进回收区的文件数与将直接删除的目录，ACP 报为 `delete` 类型；使用约定要求删除用它而不是 shell。
- `providers.<id>.models.<key>.model_id`：请求里发送的模型 id，可给很长的上游 id 起短名（如 `deepseek-v4.1-flash` → `deepseek/deepseek-v4.1-flash`）；引用、trace、统计与价格按短名，用完整 id 引用也能对应到短名。`suag manifest` 显示实际发送的 id。
- 思考档位：`run`/`chat`/`manifest` 新增 `--thinking`（`off|minimal|low|medium|high|xhigh|max|auto`，与 omp 相同），配置为 `providers.<id>.thinking` 与 `models.<model>.thinking`；`openai` 类型作为 `reasoning_effort` 原样发送，不设或 `auto` 不发送。`anthropic` 类型暂不支持。
- `providers.<id>.context_window`：provider 下所有模型的默认上下文窗口，优先级在模型级之后、按名推断之前。
- 引用的模型不在 provider 已配置的 `models` 中时，启动时警告一次（多半是模型名写错），仍照原样发送。
- `file_grep`、`file_find`、`file_list` 新增 `no_ignore` 参数：包含 `.gitignore` 匹配与 `node_modules`/`vendor`/`dist`（`.git` 始终跳过）。

### 变更

- 历史折叠默认开启（`context.fold_every: 10`、`context.fold_keep: 5`，`fold_every: 0` 关闭）：每次请求中，最近 5 步之前的步骤去掉思考内容、超过 1000 字符的字符串参数换成说明，折叠点每 10 步前移一次以保持缓存前缀；存储的会话不变。T9 对比中每次请求的上下文减半、prompt 总量 −59%、耗时 −35%（未命中输入 +48%）。折叠点前移记录为 `fold` 事件，`suag trace` 可见。设计见 docs/design/2026-10-12-history-folding-design.md。
- 项目记忆的 key（作用域 `project:<key>` 与 `$SUAG_HOME/projects/<key>`）改为“仓库目录名 + 完整路径短哈希”，如 `suag-5cb0fa32`（原为整条路径的 slug，如 `d-work-inhere-my-tools-dev-inhere-tools-suag-5cb0fa32`，哈希不变）。用 SUAG_HOME 下文件存放的项目记忆在首次使用时自动从旧目录改名过来；改名失败时继续用旧目录并警告。
- 搜索与递归列目录按完整的 `.gitignore` 规则跳过路径：各级目录的 `.gitignore` 都生效，支持 `!` 取反、`**`、`/` 锚定与只匹配目录的 `name/`（原先只读工作区根的 `.gitignore`，不支持取反）；结果末尾一行注明跳过了哪些目录与多少文件。`file_list` 递归时不再进入 `.gitignore` 匹配的目录。

## [0.12.0] - 2026-10-12

工具效率与稳健性：同模型对比 omp 时发现的步数与中断问题。依据见 [分析](docs/research/2026-10-11-suag-vs-omp-loop-analysis.md)。

### 新增

- `file_edit` 支持 `edits` 列表：同一文件的多处替换一次调用完成，按顺序作用于上一处的结果，任一处失败则文件不变；ACP 差异预览与 chat 审批提示逐处显示。
- `file_read` 支持 `paths`：一次读最多 10 个文件，按 `==> 路径 <==` 分段输出，`offset`/`limit` 对每个文件生效，某个文件出错只在它的位置报错。
- 只读文件工具（`file_read`、`file_list`、`file_grep`、`file_find`）可读工作区外的依赖源码：自动识别 Go 模块缓存、Go 标准库源码与 Rust crate 源码（`tools.dependency_roots`，默认开），另可用 `tools.read_roots` 列出额外目录。只放源码目录，写入仍只限工作区；这些目录列在提示词的环境一节，grep/find 在其中输出绝对路径。
- 评测脚本：仓库模式任务（`repo: {ref}`）、长编码任务 `long-coding-schedule`（任务级 `timeout`、结束后注入的 `acceptance` 测试、`command` 检查）、`--suag-args` / `--omp-args`、`--suag-set key=value`（改写临时配置）、`--repeat N`（附平均行）、结果中的 `tool_counts`。

### 修复

- Windows 上 `shell_exec` 丢失 PowerShell 的表格输出（`Get-ChildItem`、`Select-Object` 等）：命令结束后立即退出，格式化输出还在约 300ms 的延迟中就被丢弃，模型拿到空结果、退出码 0，只能反复换写法重试。现在命令输出先经 `Out-Default` 写完再退出，并关闭 PowerShell 7 的 ANSI 颜色。
- 评测的仓库模式任务写入仓库级 git 身份：suag 在隔离 HOME 下运行，原先只有它提交失败。
- 工具调用参数的字符串里出现未转义的制表符、换行等控制字符（模型写 Go 代码时常见）时不再让整次运行失败：严格解析失败后转义这些字符再解析一次。
- 模型响应读到一半网络断开（`read openai stream: unexpected EOF` 等）时，若还没有向前端推送任何正文，整次请求最多重发 2 次，不再直接结束运行；已推送正文后仍按失败处理，避免重复显示。OpenAI 兼容与 Claude 两类 provider 都适用。
- 工具调用参数不是合法 JSON（如字符串里有未转义的引号，控制字符修补也救不回）时不再让整次运行失败：该调用不执行，模型收到一条说明解析错误的错误结果，可以重新发送。`api.ToolCall` 新增 `ArgumentsError` 字段。
- 模型返回既无正文也无工具调用的空响应（推理模型偶尔只输出思考内容）时，同一请求最多重发 2 次，不再直接以 `provider error: empty result` 结束运行。

### 变更

- 使用约定增加一条：执行任务时不主动创建或更新 tracker、计划与记忆，除非用户要求。

## [0.11.0] - 2026-10-11

会话分段与上下文投影。依据见 [设计](docs/design/2026-10-11-segments-projection-design.md)、[计划](docs/plans/2026-10-11-segments-projection-plan.md) 与 [复盘](docs/plans/2026-10-11-segments-projection-retrospective.md)。

### 新增

- 会话分段：阈值、`:compact`、空闲整理开启新段；每段以段起始记录开头，带上下文消息和交接包（面向当前任务的摘要、最近 3 条用户请求原文、相关 ref）。旧段内容可用 `session_search` / `recall` 取回。`suag trace` 显示分段行（`segment=#N reason=... tokens=前->后`）。
- chat 空闲整理：`context.idle_after`（默认 `15m`，`0` 关闭）无输入、且上下文达到阈值的 `context.idle_min_ratio`（默认 `0.5`）时，后台开启新段；回合中不触发，输入即取消。`suag trace` 记录 `schedule` 事件，`make smoke-chat` 增加 chat_idle 场景。
- stream-json 在每次模型调用后输出 `usage` 行（本次 run 的累计值：`input_tokens`、`output_tokens`、`cache_read_tokens`、`cache_write_tokens`、`total_tokens`、`cost_usd`、`tool_calls`）；`turn_end` 与 `result` 带同名 `usage`。ACP 在同一时机发送 `usage_update`，`_meta` 另附 `run`、`session` 两个嵌套对象。
- `providers.<id>.prompt_cache`（仅 Claude/`anthropic` 类型，默认关）：加 `cache_control` 断点缓存稳定前缀。
- `pkg/agentcore/schedule`：进程内 After/Every/Idle 定时组件；`llm.SegmentContextMetaKey` 与 `llm.ClaudeConfig.PromptCache`。
- 评测脚本：多轮任务（`prompts`）、`first_call_uncached` 指标（FIRST 列）、`--probe-prefix` 三组前缀缓存探针、`--agent omp` 对照运行。

### 变更

- 系统提示词只含稳定内容（身份与使用约定），同一模式下跨会话、跨工作区字节相同；环境、项目规则、`--append-system-prompt`、记忆索引、skills 列表和延迟工具列表移到每段开头的上下文消息。段内规则变化以“上下文更新”消息追加。
- 记忆索引在每段开始时刷新（原为会话开始与压缩后）。
- `:compact` 与自动压缩的交接摘要改为面向当前任务的模板，并附最近用户请求原文。
- 模型调用事件增加 `purpose`（compact 调用标为 `compact`）（`suag trace --json` 的 `attrs.purpose`），评测据此跳过。

### 不兼容

- 系统提示词内容大幅缩减：规则、记忆索引、环境等不再在系统提示词里，而在段上下文消息中；依赖系统提示词文本的外部处理（解析、断言）需要改读上下文消息。
- `:compact` 与自动压缩改为开启新段，原样保留的只有最后一组完整的工具调用/结果（原为按 token 保留近期尾部）；`personal.Config.CompactKeepTokens` 不再生效。
- Claude provider 开启 `prompt_cache` 时，请求的 `system` 是块数组（关闭时仍为字符串）。
- ACP `usage_update` 的 `_meta` 顶层扁平键改为本次 run 的累计值（原为会话累计）；会话累计在 `_meta.session`，顶层 `cost.amount` 仍为会话累计。
- `context_update` 条目开始回放：段内规则变化会在后续回合以上下文更新消息出现。旧二进制不会回放这些条目；旧会话没有段记录时按旧快照渲染，下一断点起写段记录。

## [0.10.0] - 2026-10-11

状态、记忆与可扩展性。依据见 [设计](docs/design/2026-10-10-state-memory-extensibility-design.md) 与 [计划](docs/plans/2026-10-11-state-memory-extensibility-plan.md)。

### 新增

- 长期记忆：`memory` 工具（`add`、`replace`、`remove`、`get`、`search`），四类条目（user、feedback、project、reference），范围为用户与项目。后端可选 files、gofer、exec（外部命令，exec 协议 v1）；项目后端按 `memory.project.backend` > 仓库根 `MEMORY.md` > `.gofer/tracker`（gofer）> `$SUAG_HOME/projects/<key>/memory` 选择。仓库模式下索引是仓库根 `MEMORY.md`，条目在 `.suag/memory/<id>.md`。记忆索引在会话开始时冻结进系统提示词，压缩后刷新。配置见 `memory.*`。
- 历史检索：`session_search` 全文检索过往会话（消息、工具结果、压缩摘要），`recall` 按 ref 读取命中附近的内容。
- 输出治理：超过 `tools.max_output_bytes` 的工具结果存到 `$SUAG_HOME/sessions/<id>/outputs/`，上下文只留头尾与 ref；新增 `tools.max_spill_bytes` 限制单次原始输出（含 MCP 工具）。`suag stats --json` 增加工具输出字节。
- 污染标记：会话读过外部内容（`web_fetch`、`web_search`、MCP、检索结果，以及 `memory.taint_tools` / `memory.taint_on_spill` 配置的工具，后者缺省含 `shell_exec`）后即标记为已污染；此后写记忆需确认（`memory.write_tainted`）：chat 询问、ACP 权限请求、`run` 拒绝。
- `suag manifest [--json]` 与 `suag_manifest` 工具：列出模型、工具及元数据、MCP、skills、记忆后端及选择原因、输出上限。
- 模型价格与成本：`providers.<id>.models.<model>.price`，provider 不返回成本时 `suag stats` 与 ACP `usage_update` 据此计算。
- `suag trace` 记录每回合的 `turn_context`（模型、模式、工具集哈希、规则与记忆索引哈希、是否已污染）。
- 评测脚本 `go run ./scripts/eval`（见 `scripts/eval/README.md`），`make smoke-chat` 增加 chat_memory 场景。

### 变更

- 工具输出默认策略：`tools.max_output_bytes` 现在是“内联阈值”，默认由 30000 改为 8000；超过的结果不再被截断丢弃，而是存为文件并可用 `recall` 读取。
- 密钥脱敏下沉到 `pkg/agentcore/redact`，并统一清理不可见字符、HTML 注释与伪装标签；MCP 与 exec 后端共用 `pkg/agentcore/extproc` 的去凭据环境。
- 最低 Go 版本提升到 1.26；依赖升级：`modernc.org/sqlite` v1.60.1、`golang.org/x/sys` v0.49.0、`x/net` v0.59.0、`x/term` v0.46.0、`x/text` v0.42.0。CI 改在 Go 1.26 与 stable 上运行。

### 不兼容

- `api.Memory` 改名为 `api.History`（`Config.History`、`HistoryError`、`ErrHistoryStateConflict`），仅影响嵌入 `pkg/agentcore` 的宿主；“Memory”一词改指长期记忆。
- `tools.max_output_bytes` 默认值 30000 改为 8000，含义改为内联阈值（见上）；依赖旧默认值的配置会更早触发外置，需要更大内联结果时显式调高。
- `native.Env.MaxOutputBytes` 拆为 `InlineBytes`（内联阈值）与 `RawBytes`（单次原始输出上限），仅影响嵌入 `pkg/agentcore` 的宿主。
- MCP 的 `env.go`（去凭据环境）移入 `pkg/agentcore/extproc`，仅影响直接引用该实现的嵌入方。

## [0.9.1] - 2026-10-10

### 修复

- 帮助输出：`--read-only` 的别名不再显示为 `--;ro`，`--read-only` 与 `send --text/--source/--wait` 的说明不再在 `;` 处截断；`--no-project-rules` 前不再出现未渲染的颜色标记（升级 goutil v0.8.2）。

## [0.9.0] - 2026-10-10

`pkg/agentcore` 分层重构：内核与能力层下沉为不依赖 `internal/*` 的独立包。依据见 [设计](docs/design/2026-10-10-agentcore-layering-design.md)，删除与迁移记录见 [removed-2026-10](docs/archive/removed-2026-10.md)。

### 变更

- 能力层移入 `pkg/agentcore`：`llm`（provider）、`tools`（注册表、审批、延迟加载）、`tools/native`（内置工具）、`tools/mcp`（MCP 客户端）、`skills`；`internal/platform` 与 `internal/capability` 已删除。
- 工具改为声明元数据（`tools.Meta`：中性类别 + 只读），chat 免确认、`--read-only` 工具集与 ACP 工具 kind 都由元数据推导，不再按名字硬编码；MCP 工具一律不算只读。
- 未配置存储时 agentcore 使用内存存储（不再写入系统临时目录），未配置工具时使用空注册表。
- `mcp.ServerSettings` 自带 `CallTimeout`，mcp 包不再读全局配置。
- ACP 的 `usage_update` 改由运行结果提供每回合用量与最近一次请求大小；补充“取消的回合仍上报已完成调用的用量”。
- agentcore README、文档与 `examples/recommended`（真实 provider + 只读工具 + skills + 可选 MCP + 审批）按新分层重写。

### 不兼容

- `ToolResult.Metadata["is_error"]` 改为 `ToolResult.IsError` 字段，旧会话中工具失败的标记不再被识别。
- 删除根包 `agentcore.Registry` / `NewToolRegistry`、`FileStorage` / `NewFileStorage` 别名，以及 `agentcore/hooks`、`storage/sqlite`、`context`、`memory`、`prompt` 子包和 Embedding 接口；替代用法见删除记录。

### 内部

- 新增分层守卫测试：`pkg/agentcore` 依赖应用层即失败；新增审批与 ACP 策略表格测试。
- `api.Usage.Add` 统一用量累加；`personal.Runner.Run` 拆分；三个超长测试文件按主题拆分。

## [0.8.0] - 2026-10-10

### 新增

- 内置工具：`file_grep`、`file_find`、`file_edit`（精确替换）、`web_fetch`（拨号时拒绝私网地址，可配代理），`file_read` 按行窗口读取，`file_list` 支持递归。
- `shell_exec` 重写：Windows 使用 PowerShell，超时后结束整个进程树。
- Skills：加载 `SKILL.md` 并通过 `skill` 工具按需读取；数量多时只列名称、其余可检索，`always_load` 预加载，`skill(query="*")` 列出全部。
- 系统提示词基础部分（每进程构建一次），加入范围约束与过期规则上报。
- chat 在提示符处连按两次 Ctrl-C 才退出。
- 未启用的工具被调用时返回启用提示；MCP 调用有超时上限。

### 修复

- skills 的 frontmatter 严格 YAML 解析失败时改为宽松读取。
- `suag gc` 同时删除被清理会话的 `transcripts/<id>.jsonl`。
- 回合消息不再在 `state_json` 中重复保存。

## [0.7.0] - 2026-10-09

### 新增

- `suag gc --older-than` 清理空闲会话及其事件。
- chat 中非只读工具执行前在终端询问（`[y]是 [N]否 [a]总是`），`tools.allow_write` 开启写工具。
- stream-json 新增 `tool_call` / `tool_result` 行；`suag trace` 显示 `update_plan` 记录。
- chat 界面：模型消息标记、工具状态横幅、外部投递的输入以洋红显示、退出摘要附恢复命令。

### 修复

- 宽字符（中日韩）行编辑按显示宽度处理；等待输入时提示符变化会重绘。
- 思考模式下每条 assistant 消息都回传 `reasoning_content`。
- 回合早期失败也上报会话用量。

## [0.6.0] - 2026-10-09

### 新增

- providers / model 配置模型：预设、`<provider>/<model>` 引用解析；Claude provider 支持 `base_url`、名称与 SSE 流式输出。
- chat 渲染工具调用、计划与状态行。
- `hooks.command` 运行配置的钩子命令；`send --source`、`run --injected`。
- `tools.read_only_allow`（取代内置 MCP 白名单）。

### 变更

- 代码按职责拆分为 `internal/cli`、`internal/chat`、`internal/hooks`、`internal/wiring`、`internal/output`。

## [0.5.0] - 2026-10-08

### 新增

- OpenAI 兼容接口的 SSE 流式输出与实时打印；可配置请求头（支持 `{{session_id}}` 占位）。
- 会话压缩（摘要 + 工具结果截断）、`--from` 交接、`chat :compact` 与 `llm.compact_model`。
- 审批移入工具层并带每回合拒绝上限；MCP 工具延迟到 `tool_search` 后加载。
- `update_plan` 工具与 stream-json / ACP 计划事件；`suag trace`、`suag stats`、`suag export`。
- 新的核心存储（会话、条目、回合、事件），agentcore 存储适配器。
- Ctrl-C 报告“已中断”并以 130 退出；达到 `max_steps` 时收尾而非失败（默认 100）。

### 修复

- MCP 子进程剥离凭据环境变量；MCP 工具不能冒充内置或 gofer 工具。
- 工具调用转录正确映射为各 provider 的 tool_result / tool 消息。

## [0.4.0] - 2026-10-08

### 修复

- Windows 上运行目录使用受保护 DACL，仅当前用户可访问。
- 配置只从 `SUAG_HOME` 读取 `config.yaml` 与 `.env`；provider 覆盖不继承文件中的凭据。
- 文件工具经 `os.Root` 限定在工作区内；ACP diff 读取同样受限。
- MCP 启动有超时，服务端退出时调用立即失败并回收子进程。

### 新增

- ACP 上报会话累计用量与上下文窗口；恢复会话时更新附加系统提示词。

## [0.3.0] - 2026-10-08

### 新增

- 本地进程投递与会话独占锁；chat 钩子与共享会话转录。
- ACP 输出用量、计划与工具详情；stdio MCP 配置与工具白名单。
- `suag version`；模型与个人目录覆盖；provider 缓存用量统计；持久化的个人规则注入。

### 修复

- 区分复用的 PID，并用操作系统文件锁锁定会话。

## [0.2.0] - 2026-10-07

### 新增

- 个人 Agent：持久会话跨进程续接，会话存放在用户目录；ACP stdio 服务。
- 全局 `--version` 选项。

### 变更

- 旧控制面隔离并移除 web 宿主。

## [0.1.0] - 2026-10-06

- 首个标记版本（旧控制面，代码保留在此 tag）。

[未发布]: https://github.com/inhere/suag/compare/v0.14.0...HEAD
[0.14.0]: https://github.com/inhere/suag/compare/v0.13.0...v0.14.0
[0.13.0]: https://github.com/inhere/suag/compare/v0.12.0...v0.13.0
[0.12.0]: https://github.com/inhere/suag/compare/v0.11.0...v0.12.0
[0.11.0]: https://github.com/inhere/suag/compare/v0.10.0...v0.11.0
[0.10.0]: https://github.com/inhere/suag/compare/v0.9.1...v0.10.0
[0.9.1]: https://github.com/inhere/suag/compare/v0.9.0...v0.9.1
[0.9.0]: https://github.com/inhere/suag/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/inhere/suag/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/inhere/suag/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/inhere/suag/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/inhere/suag/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/inhere/suag/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/inhere/suag/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/inhere/suag/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/inhere/suag/releases/tag/v0.1.0
