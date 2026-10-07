<!-- template_id: design; template_version: 1.1.1 -->
# Suag 接入 Gofer 增强设计（规则、用量、工具、传话）

> 状态：Draft 0.4 / S1–S4 可实施；S5/S6 待 Gofer 侧 G1–G6 发布（v0.119）；Gofer 侧由 Gofer 仓库单独交付

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.4 | 2026-10-07 | Claude | S1 规则收集修正：同级 `AGENTS.md` 与 `CLAUDE.md` 都读（去重规则见正文），同一文件被多次引入只展开一次，`@路径` 后可跟说明文字。原因：Gofer 管理的工作区里 `workspace.md` 常只由 `CLAUDE.md` 引入，只读 `AGENTS.md` 会漏掉项目规则 |
| 0.3 | 2026-10-07 | Claude | 按用户思路改送话通道：Suag 进程本机登记 + 本地 socket，新增 `suag ps` / `suag send`，Gofer 只在会话所在机器执行 agent 配置的 `deliver_command`（G6）；去掉 `--listen` 长轮询；存活判定改由 `suag send` 退出码给出 |
| 0.2 | 2026-10-07 | Claude | 送话改为「先找到原进程」：新增在线收件（`--listen` 常驻长轮询，Gofer G6），送话优先级为 Stop 等待 → 在线收件 → tmux → 接管；接管只允许在原进程已退出/心跳超时时进行，避免两个进程写同一会话分叉；待确认两项按建议确认 |
| 0.1 | 2026-10-07 | Claude | 初稿：在个人 Agent 首切片（2026-10-06 设计 0.4）之上，补齐 Gofer 使用所需的规则注入、用量、模型配置、CLI MCP、ACP 更新，以及终端会话「传话人」（web 送话激活停滞会话） |

> 仅语义变化递增版本；纯措辞/元数据纠正沿用原版本。

## 背景与目标

个人 Agent 首切片已完成（`suag-ha0z`）：`suag run --output-format stream-json`（一次性回合）、`--resume`、`suag chat`（终端多轮）、`suag acp`（ACP stdio，`session/load`、`read_only` 模式、权限请求、stdio MCP），会话持久化在 `~/.suag/db`。Gofer 已能用 cli-agent（`suag`）与 acp-agent（`suag-acp`）跑它。

但 Gofer 后来加入的几类能力 Suag 还接不上：

1. **项目规则不生效**：Gofer 管理的仓库把规则、tracker 用法、提交策略写在 `AGENTS.md` / `CLAUDE.md`（Claude 风格 `@file` 引入），Suag 不读，进了仓库不知道要用 `gofer issue`、不知道提交策略。
2. **用量看不到**：`result` 事件的 `usage` 是无 json tag 的 Go 结构体，输出成 `{"InputTokens":…}`；Gofer 只认 `input_tokens` / `inputTokens` 等拼写，且对通用 cli-agent 没有用量采集入口。
3. **模型配置不稳**：模型/凭据只从用户目录固定文件读，Gofer 无法按 agent/项目/worker 覆盖；续接时曾被迫在 cwd 放临时配置。
4. **CLI 模式用不了 Gofer 工具**：ACP 已支持 stdio MCP（Gofer 管家会注入 `gofer mcp`），但 `run` 不支持，job 里没法调用 `gofer_work_report` / `gofer_issue_*`。
5. **终端会话不可见、不能传话**：`suag chat` 不调用 Gofer hook，不会出现在 Gofer 的 Sessions 页，不会自动成为工作项，用户也无法在 web 里给一个停滞的 Suag 会话「送话」把它激活。

**核心目标**：用户在 Gofer web 里能看见正在/曾经运行的 Suag 终端会话，回合结束时可在 web 回复让它继续；会话已空闲（停滞）时可从 web 送一句话把它唤醒（tmux 注入，或起新进程接管）；Suag 在 Gofer job 里遵守仓库规则、汇报工作项、上报用量。

## 名词

| 名词 | 含义 |
|---|---|
| 传话 / 中继（relay） | Gofer 的终端会话 ↔ web 消息通道：agent 回合结束（Stop）时 hook 阻塞等待 web 回复，回复作为下一条用户输入注入，会话继续 |
| 送话（deliver） | 会话没有在等回复（已空闲/停滞）时，从 web 送一句话。按优先级：C) 本机直送（Gofer 在会话所在机器执行 `suag send`，经本地 socket 送进原进程）；A) tmux 里敲进原进程的终端（未配 C 的 agent）；B) 原进程已不在时，起新进程 `--resume` 接管并把话作为首条输入 |
| 本机直送（local send） | 每个 `suag chat` 进程在 `SUAG_HOME/run/` 登记自己并监听一个本地 socket；`suag ps` 列出本机活会话，`suag send --session <id>` 把话直接送进对应进程。Gofer 与本机其它 agent 都可调用 |
| 唤醒（wake） | 对停滞会话非交互地续一轮：Gofer 以 cli-agent 的 `session_resume` 起一个 job（`suag run --resume <id> --prompt …`） |
| hook | Suag 在会话生命周期事件上调用的外部命令 `gofer hook generic --agent <key>`，stdin 为一段 JSON |
| transcript | Suag 为每个会话追加写的 jsonl 对话记录，供 Gofer 读取最后消息与整理工作项 |
| gofer agent key | Gofer 配置里 Suag 的 agent 名（建议 `suag` / `suag-acp`），决定续接/接管模板 |

## 范围与非目标

- 范围：Suag 侧的规则注入、用量输出、模型/配置覆盖、版本命令、CLI MCP 与工具权限、ACP 会话更新补全、hook 客户端（传话）、transcript 导出、`chat --resume` 的接管兼容；以及 Gofer 侧需要配合的五个接口点（只定义合同，Gofer 侧在 Gofer 仓库实现）。
- 非目标：Suag 自建 web / 消息通道（统一走 Gofer）；把 Suag 做成 Gofer 管家（可在 S3/S4 完成后由 Gofer 配置选用，不在本设计）；多用户/远程 Suag 服务；Gofer 以外的编排平台。

## 已确认事实与规范

| 项 | 事实 |
|---|---|
| Suag CLI | `run` 与 `chat` 共用选项：`--prompt/-p`、`--session-id`、`--resume/-r`、`--output-format text|stream-json`、`--read-only`、`--allow-write`（`internal/app/cli.go`） |
| stream-json 事件 | `session`、`turn_start`、`turn_end`、`error`、`input_required`、`result{session_id, run_id, result, status, usage}` |
| usage 结构 | `pkg/agentcore/api/types.go` 的 `Usage{InputTokens, OutputTokens, TotalTokens}`，无 json tag |
| ACP | stdio MCP 已支持（`internal/acpserver/policy.go` 的 `mcpSettings`，仅 stdio transport）；`SetSessionMode`/`read_only`；未知 MCP 工具一律要求权限 |
| Gofer cli-agent 配置（现用） | `args: [run, --output-format, stream-json, --prompt, "{{prompt}}"]`，`session_inject: [--session-id, "{{session_id}}"]`，`session_resume: [run, …, --resume, "{{session_id}}", --prompt, "{{prompt}}"]`，`output_format: ndjson`，`ndjson_stdout: final_text`，`ndjson_stdout_path: result` |
| Gofer 模板变量 | `{{prompt}}` `{{session_id}}` `{{system_prompt}}` `{{cwd}}` `{{home}}` `{{job_id}}` `{{result_dir}}` |
| Gofer 用量解析 | 接受 `inputTokens/input_tokens/input`、`outputTokens/output_tokens/output`、`totalTokens/total_tokens/total`、cache 读写多种拼写、`cost_usd` 等；只对 claude/omp/codex/ACP 来源采集 |
| Gofer hook 输入（claude/codex 同形） | stdin JSON：`session_id`、`cwd`、`transcript_path`、`hook_event_name`、`prompt`、`last_assistant_message`、`stop_hook_active`、`source`、`tool_name`、`tool_output`、`notification_type`、`message`；未知字段忽略 |
| Gofer hook 输出 | Stop 等到 web 回复时 stdout 为 `{"decision":"block","reason":"[gofer web 回复] <文本>"}`；UserPromptSubmit/SessionStart 可能输出 `{"hookSpecificOutput":{"additionalContext":"…"}}`（补投 job 完成通知等）；其余为空 |
| Gofer 送话 | 无 OPEN turn 时：A) tmux 注入（前台进程须在 `session.inject_commands` 白名单内，文本前缀 `[gofer web 回复] `）；B) `allow_takeover` 时用 agent 的 `session_resume_interactive` 起交互 pty job，首条输入为 `[gofer web 回复] <文本>\r` |
| Gofer 在 job 内 | 环境里有 `GOFER_JOB_ID`；hook 对 job 内的 agent 直接放行 |

## 总体方案

分两侧、六期。Suag 侧 S1–S4 不依赖 Gofer 新版本，可立即开始；S5/S6（传话）依赖 Gofer 侧 G1/G3/G4/G5 先发布。

| 期 | Suag 侧内容 | 依赖 |
|---|---|---|
| S1 | 规则与提示注入：`AGENTS.md`/`CLAUDE.md`（含 `@file` 一层引入）+ `--append-system-prompt` | 无 |
| S2 | 用量 snake_case、`--model`/env 覆盖、`suag version` | G2（用量入 Gofer 统计） |
| S3 | `run` 支持 `--mcp-config`；MCP 工具权限 allowlist；非交互下不挂起 | 无 |
| S4 | ACP 补全：`usage_update`、`plan`、`tool_call` 的 kind/locations/diff | 无 |
| S5 | 传话：hook 客户端（SessionStart/UserPromptSubmit/PostToolUse/Stop/Interrupt/SessionEnd）+ Stop 后台等待 + transcript 导出 | G1、G3 |
| S6 | 送话与唤醒：进程登记 + 本地 socket + `suag ps`/`suag send`、会话锁、`chat --resume` 适配接管（仅原进程已退出时）、配置样例与端到端验证 | G4、G5、G6 |

Gofer 侧（在 Gofer 仓库实现，目标版本 v0.119）。原则：**送话总是先找到原进程**（Stop 等待、本机直送、tmux），只有原进程已不在才起新进程接管：

| 编号 | Gofer 侧接口 |
|---|---|
| G1 | `gofer hook generic --agent <key>`：接受上表 claude 同形 stdin，事件语义与 claude 一致；会话登记的 agent 为 `<key>`（续接/接管按该 agent 配置找模板） |
| G2 | cli-agent 配置 `ndjson_usage_path`（如 `usage`）：从 ndjson 结果行读取用量，入 job 用量与统计 |
| G3 | transcript 通用方言 `generic`（格式见下），agent 配置 `transcript_dialect: generic`；整理器与 worker 尾读都支持 |
| G4 | agent 配置 `inject_process: [suag]`：并入 tmux 送话前台进程白名单 |
| G5 | agent 配置 `session_family: suag`：`suag` 与 `suag-acp` 会话互通（CLI 续 ACP、ACP `session/load` CLI 会话） |
| G6 | agent 配置 `deliver_command`（argv 模板，如 `[send, --session, "{{session_id}}"]`）与 `deliver_stdin: true`：送话时 Gofer 在会话登记的 runner 上执行它（同 tmux 注入的内部 job 机制），退出码 0=已送达、3=会话进程不在线、其它=失败；deliver 优先级 OPEN turn → deliver_command → tmux（未配时）→ 接管；接管前存活检查：配了 deliver_command 的以退出码 3 为准，其余 agent 看 hook 心跳（`session.takeover_alive_sec`，原因 `session_alive`） |

## 合同细节

### S1 规则与提示注入

- 启动（run/chat/acp 新会话）时，从 cwd 向上查找到 git 根（无 git 时到 cwd）收集规则文件：每一级**两个都读**，`AGENTS.md` 在前、`CLAUDE.md` 在后；外层在前、内层在后。另读用户级 `~/.suag/AGENTS.md`（若有，放最前）。
- 去重：按「展开后的文件路径」去重——同一个文件（含经 `@` 引入的文件，如两边都引入的 `workspace.md`）在整个收集过程中只出现一次；`CLAUDE.md` 若只含 `@AGENTS.md`（或两文件内容相同）则等同于已读。
- 实际例子：某工作区 `AGENTS.md` 不引入 `workspace.md`、`CLAUDE.md` 引入了 `@workspace.md`，则规则 = `AGENTS.md` + `CLAUDE.md`（其中 `@workspace.md` 展开一次）；若两者都引入 `workspace.md`，它只展开一次。
- 行首为 `@<相对路径>` 时（路径后可跟空白与说明文字，如 `@workspace.md 需要先加载阅读。`）展开为该文件内容（相对当前文件所在目录，只展开一层，防环，单文件上限 64KB，总上限 128KB，超限截断并在末尾注明）。
- 新增 `--append-system-prompt <text>`（及 `SUAG_APPEND_SYSTEM_PROMPT`）：追加在规则之后。Gofer 用 `{{system_prompt}}` 注入规则与 skill。
- 规则只在会话创建时读入并持久化到会话元数据；`--resume` 时不重复注入，但若规则文件哈希变化，在下一轮前追加一条「项目规则已更新」的系统消息（带新内容）。
- 在 job 内（`GOFER_JOB_ID` 存在）额外追加一段固定提示：当前运行在 Gofer job 中，可用 `gofer work report` / `gofer issue` 汇报。

验收：单测覆盖向上查找、`@` 展开、去重、上限、resume 不重复；真机在一个 Gofer 管理的仓库跑 `suag run -p "本仓库用什么跟踪 issue？"`，回答提到 `gofer issue`。

### S2 用量、模型与版本

- `Usage` 加 json tag：`input_tokens`、`output_tokens`、`total_tokens`，可选 `cache_read_tokens`、`cache_write_tokens`、`cost_usd`（provider 给得出时）。`result` 事件的 `usage` 用此形状；同时累计整个 turn（多 step）的用量。
- 模型与配置覆盖，优先级 flag > env > 配置文件：`--model`/`SUAG_MODEL`、`--provider`/`SUAG_PROVIDER`、`SUAG_HOME`（替代 `~/.suag` 作为数据与配置根）、`SUAG_CONFIG`（指定配置文件）。凭据仍只从配置文件或 provider 标准 env 读取，不接受命令行明文。
- `suag version`：输出一行 `suag <version> (<commit>)`，退出码 0；Gofer `detect` 改用它。

验收：stream-json 的 result 行 `usage.input_tokens` > 0；设置 `SUAG_MODEL` 后会话元数据记录的模型变化；`suag version` 输出符合格式。Gofer G2 发布后，Gofer job 详情显示 Suag 用量。

### S3 CLI MCP 与工具权限

- `run`/`chat` 新增 `--mcp-config <file>`（JSON，Claude 同款 `{"mcpServers":{"<name>":{"command":…,"args":[…],"env":{…}}}}`，仅 stdio），可重复；配置文件也可声明默认 MCP。
- 工具权限 allowlist（配置 `tools.auto_allow`，glob）：默认放行 `gofer_*_list`、`gofer_*_get`、`gofer_work_report`、`gofer_session_list` 等只读/汇报类；其余 MCP 工具按现有策略需要许可。
- 非交互（`run`）遇到需要许可的工具：不挂起；以工具错误「需要许可：<工具>，本次运行未授权」返回模型，并发 `input_required` 事件后正常结束回合（现有行为保持），`--allow-write` 不放开 MCP 写工具（MCP 另需 allowlist）。

验收：用一个假 stdio MCP（只读工具 + 写工具）测 run：只读自动执行，写工具被拒且回合正常结束；Gofer job 里经 `--mcp-config` 接入 `gofer mcp` 后能调用 `gofer_work_report`。

### S4 ACP 会话更新补全

- `session/update` 发送 `usage_update`（每轮结束，字段同 S2）。
- 计划/待办：模型产生或更新任务清单时发 `plan` 更新（entries: content/status/priority）。
- `tool_call` 与 `tool_call_update`：填 `kind`（read/edit/execute/search/fetch/other）、`locations`（涉及文件路径与行号）、编辑类工具给出 diff 内容（oldText/newText）。

验收：ACP 测试客户端断言三类更新；Gofer acp-agent job 页显示用量、计划与改动文件。

### S5 传话：hook 客户端与 transcript

**配置**（`~/.suag/config` 或 `SUAG_CONFIG`）：

```yaml
gofer:
  hooks: auto        # auto | on | off；auto = PATH 上有 gofer 且不在 Gofer job 内
  command: gofer     # 可写绝对路径
  agent: suag        # Gofer 配置里的 agent key（决定续接/接管模板）
  wait_sec: 7140     # Stop 等待 web 回复的上限
  timeout_ms: 3000   # 非 Stop 事件的调用超时
```

**调用**：`<command> hook generic --agent <agent> [--wait <wait_sec>]`，stdin 为一行 JSON，环境继承 Suag 进程（Gofer 从 `GOFER_CONFIG_DIR` 读连接信息）。`GOFER_JOB_ID` 存在时不调用（job 内由 Gofer 管）。

**事件与字段**（只在 `chat` 交互会话发送；`run`/`acp` 不发）：

| 事件 `hook_event_name` | 时机 | 字段 | 期望输出 |
|---|---|---|---|
| `SessionStart` | 会话创建或 `--resume` 打开 | `session_id`、`cwd`、`transcript_path`、`source`（`startup`/`resume`） | 可能有 `hookSpecificOutput.additionalContext`：作为系统上下文附加到下一轮 |
| `UserPromptSubmit` | 用户在终端提交输入（含送话注入的行） | `prompt` 原文 | 同上 |
| `PostToolUse` | shell/exec 类工具完成 | `tool_name`、`tool_output`（截断到 8KB） | 无（Gofer 据此登记 job watch） |
| `Stop` | 一轮结束、回到等待输入 | `last_assistant_message`（本轮最终回复全文，截断 16KB）、`stop_hook_active`（本轮是否由上次 Stop 注入触发） | 可能为 `{"decision":"block","reason":…}` |
| `Interrupt` | 用户在等待期间按键/输入，取消等待 | — | 无 |
| `SessionEnd` | 退出 chat | — | 无 |

所有事件都带 `session_id`、`cwd`、`transcript_path`、`hook_event_name`。hook 失败（非 0、超时、无 gofer）只写 Suag 日志，绝不影响对话。

**Stop 等待（关键）**：
1. 一轮结束，先把回复打印完，再在后台启动 `hook … --wait <wait_sec>`（Stop 事件），终端照常显示输入提示，并在提示行旁标注「web 可回复」。
2. 后台进程返回 `{"decision":"block","reason":R}`：把 `R`（已带 `[gofer web 回复]` 前缀）作为一条用户输入显示并开启新一轮，本轮 Stop 的 `stop_hook_active=true`。
3. 等待期间用户在终端开始输入：先同步发送 `Interrupt`（≤1s），再结束后台进程，随后按正常输入处理（会发 `UserPromptSubmit`）。顺序必须是 Interrupt → 结束等待进程，避免迟到事件关掉新 turn。
4. 返回空或出错：保持空闲，不提示错误（空闲期间的送话由 S6 的本机直送处理）。
5. 退出 chat：先发 `Interrupt`（若在等）再发 `SessionEnd`。

**transcript 导出**：每个会话追加写 `<SUAG_HOME>/transcripts/<session_id>.jsonl`（`SUAG_HOME` 默认 `~/.suag`），每条消息一行，UTF-8，追加写后 fsync 不必每行做但退出前必须完成：

```json
{"v":1,"type":"user","ts":"2026-10-07T10:00:00Z","text":"…","injected":false}
{"v":1,"type":"assistant","ts":"…","text":"…"}
{"v":1,"type":"tool","ts":"…","name":"shell","summary":"go test ./... (exit 0)"}
```

- `injected=true` 表示该用户消息来自送话注入（以 `[gofer web 回复]` 开头）或 Gofer 补投上下文，Gofer 不把它当作「人回来了」。
- 工具只写一行摘要（≤200 字符），不写完整输出。
- 文件只追加；会话被 `--resume` 继续时接着写同一文件。`run`/`acp` 也写（便于整理器读取），但不发 hook。

验收：
- 单测：事件序列（含等待中输入 → Interrupt 在前）、block 输出转新一轮、hook 缺失/失败不影响对话、transcript 行格式。
- 真机（Gofer ≥ G1/G3 发布版）：在 tmux 里 `suag chat` → Gofer Sessions 页出现会话（agent=`suag`）、标题来自首条输入；`gofer session relay on` 后一轮结束进入「等回复」，web 回复后 Suag 以新一轮继续；终端输入可打断等待；退出后会话 `ended`；工作项草稿出现并能「整理」出目标（读 transcript）。

### S6 送话与唤醒

- **tmux 注入（送话 A）**：Suag `chat` 的输入行必须能接受整行粘贴/逐行敲入（含中文、多行时按行提交）。Gofer 侧需把 `suag` 加入白名单（G4，配置 `inject_process`）。
- **本机直送（送话 C，首选）**：原进程活着时，Gofer 在会话所在机器执行 `suag send`，经本地 socket 把话送进原进程，不需要 tmux，也不起新进程。
- **进程登记**：`chat` 进程启动后写 `<SUAG_HOME>/run/<pid>.json`：`{pid, session_id, name, cwd, kind:"chat", socket, started_at, host}`，退出时删除；读取方发现 pid 已不存在即视为陈旧并清理。
- **本地 socket**：`<SUAG_HOME>/run/<pid>.sock`（Unix domain socket；Go 在 Windows 10 1803+ 同样支持 AF_UNIX），文件权限仅当前用户。协议为单行 JSON 请求/应答：请求 `{"type":"deliver","session_id":…,"text":…,"from":"gofer|cli|<agent>"}`；应答 `{"ok":true,"queued":false}`。进程空闲时立即作为用户输入开新一轮；正在执行一轮时排队到本轮结束后（应答 `queued:true`），队列上限 8 条，超出应答 `{"ok":false,"error":"queue_full"}`。另支持 `{"type":"ping"}` 用于存活探测。
- **`suag ps`**：列出本机活会话（session id、名称、cwd、状态 idle/busy、pid、启动时间），`--json` 输出数组；顺带清理陈旧登记。
- **`suag send --session <id|前缀|名称> [--text <t>]`**：文本缺省从 stdin 读（避免命令行转义与长度问题）；退出码：0 已送达或已排队、3 该会话没有活进程、2 匹配到多个会话、1 其它错误（stderr 给原因）。Gofer 的 `deliver_command` 用 `[send, --session, "{{session_id}}"]` + `deliver_stdin: true`。
- **送进来的话**：以 `[gofer web 回复] …`（Gofer 送）或 `[来自 <from>] …`（其它来源）显示在终端并作为用户输入处理，transcript 记 `injected=true`，同时发 UserPromptSubmit hook。若 Stop 等待正在进行，先结束等待再处理（同一句话不重复送达）。
- **接管（送话 B，仅原进程已不在时）**：Gofer 先做存活检查（G6：`suag send` 返回 3 才视为原进程不在；否则拒绝，原因 `session_alive`），通过后才用 `session_resume_interactive: [chat, --resume, "{{session_id}}"]` 在 pty 起 `suag chat --resume <id>`，并在首次输出后写入 `[gofer web 回复] <文本>\r`。要求：`chat --resume` 在 pty 下尽快打印提示并读取首行；首行作为正常用户输入（`injected=true` 记入 transcript，发 UserPromptSubmit）。Suag 侧另需保证：同一会话被第二个 Suag 进程打开时（例如原进程其实仍在但心跳异常），后打开的进程检测到会话被占用（会话锁，见下）并拒绝写入或只读打开，防止分叉。
- **会话锁**：`chat`/`run` 打开会话时在 `SUAG_HOME` 下取会话级排他锁（含 pid、主机、时间，进程退出释放、陈旧锁可回收）；锁被占用时 `chat --resume` 报错退出（Gofer 接管 job 随之失败并提示），`run --resume` 等待短时（如 30s）后报错。
- **唤醒（非交互）**：Gofer 已能用 `session_resume` 起 `suag run --resume <id> --prompt …`（0.118 起继承 agent/job env 并按源 agent 投影输出），Suag 侧无需改动，只需保证 run 续接后的事件与首轮一致。
- **会话族**：同一会话可被 `suag` 与 `suag-acp` 交替续接（共享 `~/.suag/db`）；Gofer 侧用 G5 声明族。
- **配置样例**（交付 `docs/usage/gofer-personal-agent.md` 更新）：

```yaml
agents:
  suag:
    type: cli-agent
    command: suag
    args: [run, --output-format, stream-json, --append-system-prompt, "{{system_prompt}}", --prompt, "{{prompt}}"]
    session_inject: [--session-id, "{{session_id}}"]
    session_resume: [run, --output-format, stream-json, --resume, "{{session_id}}", --prompt, "{{prompt}}"]
    session_resume_interactive: [chat, --resume, "{{session_id}}"]
    read_only_args: [--read-only]
    output_format: ndjson
    ndjson_keep: [session, turn_start, turn_end, result, error, input_required]
    ndjson_stdout: final_text
    ndjson_stdout_path: result
    ndjson_usage_path: usage          # G2
    transcript_dialect: generic       # G3
    inject_process: [suag]            # G4
    session_family: suag              # G5
    deliver_command: [send, --session, "{{session_id}}"]   # G6
    deliver_stdin: true               # G6
    env: {SUAG_MODEL: "<model>"}
    detect: {command: suag, args: [version]}
  suag-acp:
    type: acp-agent
    command: suag
    args: [acp]
    session_family: suag
    acp: {modes: {read_only: read_only}, permission_policy: ask, load_session: true}
```

验收（真机，Gofer 发布 G1–G6 后）：
1. 本机两个终端各开一个 `suag chat`，`suag ps` 列出两者；`suag send --session <其一>` 送话，该进程立即作答，另一进程无反应；进程忙时送话排队到本轮结束后处理。
2. 普通终端（不在 tmux）里 `suag chat`，空闲 10 分钟以上后在 web 会话抽屉送话，Gofer 在本机执行 `suag send`，原进程作答；全程未起新进程。
3. 原进程仍在运行时 web 选「起新进程接管」，Gofer 拒绝并提示 `session_alive`；原进程退出后再送话，`suag send` 返回 3，选接管后 web 终端里 `suag chat --resume` 打开原会话并以送话作答。
4. 原进程运行中，另开终端 `suag chat --resume <同一会话>`，被会话锁拒绝。
5. 工作项卡片「唤醒」对已退出的 Suag 会话起续接 job，最终答复正确提取。
6. `suag-acp` 续接一个已退出的 `suag chat` 会话，复述首条用户消息原文（防止模型凭记忆蒙对）。

## 关键流程

**web 回复让停在回合末尾的会话继续（S5）**

```
suag chat 一轮结束 → 后台: gofer hook generic --agent suag --wait 7140 (Stop, last_assistant_message)
  → Gofer 判定需要等（relay on / auto 判据）→ web 会话页显示「等回复」
  → 用户 web 回复 → hook stdout {"decision":"block","reason":"[gofer web 回复] 继续"}
  → Suag 以该文本开新一轮 → 再次 Stop …
```

**停滞会话送话激活（S6）**

```
web 送话 → POST /v1/sessions/{sid}/deliver
  ├ 有 OPEN turn（Stop 正在等）      → 作答，原进程开新一轮
  ├ agent 配了 deliver_command        → 会话所在机器执行 suag send（stdin 送文本）
  │     ├ 退出码 0 → 原进程收到并开新一轮（忙则排队）
  │     └ 退出码 3 → 原进程不在 → 往下走
  ├ 未配 deliver_command 且有 tmux pane → tmux send-keys "[gofer web 回复] …" + Enter
  └ allow_takeover 且原进程已不在     → pty job: suag chat --resume <sid> → 首行写入送话 → 会话 handed_off
       （原进程仍在线 → 拒绝，原因 session_alive）
```

## 安全、数据与回滚

- hook 只在本机调用本机 `gofer`，连接信息与 token 由 Gofer 自己从 `GOFER_CONFIG_DIR` 读，Suag 不读取、不记录 token。
- transcript 与 hook 负载可能含用户代码与对话：只写本机 `SUAG_HOME`；`tool_output` 截断 8KB，transcript 不写完整工具输出；不写任何凭据（provider key 来自配置/env，绝不进入事件）。
- 本地 socket 只在 `SUAG_HOME/run/` 下创建、仅当前用户可访问；不监听网络端口。跨机器送话只经 Gofer 在目标机器执行 `suag send`。
- 送话注入的文本按普通用户输入处理，不提升权限：`--read-only` / 工具许可策略照常生效。
- 回滚：`gofer.hooks: off` 即停用全部 hook；transcript 导出可配置 `transcripts: off`；S1 规则注入可用 `--no-project-rules` 关闭（调试用）。

## 决策

1. 传话走 Gofer 现有会话中继（hook + tmux + 接管），Suag 不自建消息通道。
2. Gofer 侧新增通用 hook 方言 `generic`（claude 同形 JSON），而不是让 Suag 冒充 `claude`：会话 agent 必须是 Suag 自己的 agent key，续接/接管才找得到正确模板。
3. Stop 等待在 Suag 进程内以后台子进程实现，终端输入可随时打断；不做「每次 Stop 都同步阻塞」。
6. 送话总是先找到原进程：本机进程登记 + 本地 socket（`suag send`）让不在 tmux 的活进程也能收话，且不依赖 Gofer（本机其它 agent 也能用）；Gofer 只在会话所在机器执行 agent 配置的送话命令。接管只用于原进程已退出的会话，并由送话命令的「不在线」结果与 Suag 会话锁双重防止分叉。
4. transcript 采用 Gofer 通用方言（每条消息一行、只追加），不模仿 Claude 的内部格式。
5. 规则文件只在会话创建时注入并持久化，变化时以系统消息补充，避免每轮重复占用上下文。

## 待确认事项

无。原两项已确认（2026-10-07）：用户级规则文件为 `~/.suag/AGENTS.md`；`tools.auto_allow` 默认只含 Gofer 只读/汇报类工具。

## 结论与人工计划 Gate

确认后按 S1 → S4 顺序实施（S1–S4 互不依赖，可调整顺序）；S5/S6 待 Gofer 发布 G1–G6（v0.119）后开始。每期：先写测试，`go test ./...` 通过，按功能点本地提交，并在 Suag tracker 对应 issue 记录验证结果。
