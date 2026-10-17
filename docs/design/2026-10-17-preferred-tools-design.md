<!-- template_id: design; template_version: 1.1.1 -->
# 配置的优先工具设计

> 状态：Draft 0.1 / 待用户审批

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-17 | suga（gofer job） | 按 `suga-jipz` 最后一条方向调整，采用通用 command/MCP 配置、存在性探测和提示优先，不做逐工具适配。 |

### 思考模式与范围冻结

- thinking_mode：RIGOROUS。
- core_objective：让用户可以声明本机已有的强工具；工具确实可用时，Agent 在环境说明中知道应优先使用，工具不可用时不污染提示词并继续使用内置工具。
- allowed_scope：`internal/config` 的配置 registry/reader、`internal/wiring` 的通用探测与 runtime/manifest 接线、`internal/personal` 的 segment/trace 观测、必要的 `pkg/agentcore/tools` 延迟可见性扩展、配置与测试、Linux PTY 冒烟及真实环境验证。
- non_goals：不替换 `file_grep`/`file_find` 等内置实现，不为 rg、xerj、codebase-memory-mcp、codegraph 写专用协议客户端，不解析外部工具输出，不自动安装、建索引或放行工具，不新增“工具用法”专用存储。
- expansion_policy：DEFER_OR_REQUEST。若需要改变既有 MCP 启动失败语义、审批语义、provider 协议或稳定系统提示词，停止实施并回到设计 Gate。
- review budget：小；优先复用现有配置 reader、MCP provider、segment context、approval 和 manifest，避免新增抽象层。
- 停止条件：用户批准本设计和计划前不实施。

## 背景与目标

用户本机可能已有比内置工具更强的 `rg`、`xerj` 或代码图谱 MCP。当前 suga 没有统一的声明入口，也不会把这些工具的适用场景告诉 Agent。目标是增加一个小而通用的“优先工具”层：配置一组 `{name, kind, when}`，只探测是否存在，存在时加入会话环境说明；MCP 优先项还应避开延迟加载。

用户可见结果：

1. 人可以编辑 YAML，或用 `suga config set` 设置 `tools.preferred`；不需要为每个外部工具写 suga 适配。
2. `command` 按 PATH 查找，`mcp` 只有在 `mcp.servers` 中配置且服务启动、`tools/list` 成功时才算存在。
3. 每个有效优先项在环境说明中列出名称和 `when`，并显示固定提示：用法不清先看 `--help`，摸清的用法写进记忆；无效项完全不列出。
4. MCP 优先服务的工具常驻可见，不被 `tools.deferred: auto|always` 放入 `tool_search` 延迟集合；审批仍按既有规则处理。
5. `suga manifest` 能报告每项 `present` 和原因；trace 的 `turn_context` 能看出该回合列出了哪些优先工具。

## 范围与非目标

### 范围

- `tools.preferred` 的列表结构、配置 registry、`config set` 行内 YAML 值和直接编辑。
- command 的跨平台 PATH 存在性探测；MCP 的已配置/已连接/`tools/list` 成功探测。
- 运行时环境说明、segment 快照、MCP 与延迟可见性的衔接。
- run、chat、ACP 及只读子 agent 的提示接线；manifest 与 `turn_context` 观测。
- 单元测试、配置/manifest 测试、Linux PTY 场景和一次真实 rg/xerj/MCP 环境验证。

### 非目标

- 不把外部 command 变成新的 native tool，也不修改 `shell_exec` 的命令语义、边界或输出治理。
- 不为 rg、fd、xerj、某一种代码图谱 MCP 增加专门字段、参数翻译、索引状态 API 或结果解析。
- 不因“优先”而自动放行 `shell_exec` 或 MCP；不自动写 `tools.auto_allow`、`tools.read_only_allow`，不绕过 ACP/chat/run 的审批。
- 不自动安装命令、启动用户未配置的 MCP、创建索引或改变 MCP 的生命周期；manifest 的 MCP 探测只对配置项做有界连接/list 检查。
- 不把外部工具用法写入新的数据库/文件格式；使用现有 `memory` 能力记录 Agent 摸清的用法。

## 已确认事实与规范

### 当前提示词与缓存边界

| 现状 | 代码位置 |
|---|---|
| `basePrompt` 是进程内构建一次的稳定 system prompt，只放身份和工作约定；工具 schema 也不在这段重复渲染。 | `internal/wiring/prompt.go:26-42` |
| 环境说明包含 OS、workspace、只读根、附加目录、shell、解释器和读写模式。 | `internal/wiring/prompt.go:106-154` |
| runtime 将 `environmentPrompt` 和 skills 作为 `personal.Config` 的 source，稳定 prompt 作为 `PromptBuilder`。 | `internal/wiring/runtime.go:553-595` |
| segment snapshot 已持久化 `Environment`、规则、记忆、skills、append prompt 和 deferred 工具清单，并在新段重新读取。 | `internal/personal/segment.go:38-53,83-117` |
| segment context message 在每次模型调用的上下文组装时放在历史前，稳定 system prompt 不随段变化。 | `internal/personal/context.go:215-222,337-355` |

**设计结论：**优先工具列表属于环境说明，放在 `# Environment` 内、工作区/根目录信息之后、模式说明之前；不要放进 `basePrompt` 或工具 schema。命令 PATH 和 MCP 连接状态都可能变化，放入 segment context 不会改变稳定前缀，也不会因安装新命令使跨会话 system-prefix 缓存失效。段记录冻结本段看到的列表，避免同一段中提示词前后不一致。

### 当前配置与写回能力

| 现状 | 代码位置 |
|---|---|
| 配置 registry 用 `ConfigKey` 描述路径、类型、敏感性、可写性和现有 reader；固定键表目前没有 `tools.preferred`。 | `internal/config/settings_registry.go:18-47,85-184` |
| `ParseConfigValue` 已支持 `ConfigTypeYAML`，会把命令行字符串作为 YAML 值解析；列表和结构化值并非只能手工编辑。 | `internal/config/settings_registry.go:277-363` |
| `suga config set` 通过 registry 解析值，再走统一的 YAML writer 和候选配置校验；CLI 不自己写 YAML。 | `internal/cli/config.go:60-89,179-204` |
| `suga config set` 目前拒绝没有 typed reader 的结构化键；`config list` 只列 registry 认可的叶子键。 | `internal/cli/config.go:151-165,207-236` |

**配置形状：**

```yaml
tools:
  preferred:
    - name: rg
      kind: command
      when: 精确搜索文本或正则，优先于反复读取文件
    - name: codebase-memory-mcp
      kind: mcp
      when: 需要跨文件查符号、调用关系或代码图谱时
```

`name`、`kind`、`when` 均为非空字符串；`kind` 只能是 `command` 或 `mcp`；列表顺序是用户声明顺序，但解析后的 manifest/提示/trace 也保留稳定顺序。`command` 的 `name` 是一个可执行文件名，不携带参数；`mcp` 的 `name` 是 `mcp.servers` 中的 server `id`（未写 id 时是现有 reader 以 command 作为的默认 id），不是某个专用工具协议名。这样同一个通用机制可用于 codebase-memory-mcp、codegraph 或其他 MCP 服务。

registry 新增 `tools.preferred`，类型为带专用 reader 的 YAML 列表结构。推荐命令（PowerShell、POSIX shell 均需把整个 YAML 值作为一个参数）为：

```text
suga config set tools.preferred '[{name: rg, kind: command, when: "精确搜索文本或正则"}, {name: codebase-memory-mcp, kind: mcp, when: "代码图谱探索"}]'
```

直接编辑 YAML 与该命令等价。第一版不支持 `tools.preferred[0]` 这样的元素级路径；整列表替换、`config unset tools.preferred` 足够小且避免半套列表校验。reader 拒绝未知字段、空值、重复的 name、非法 kind，并在 `config set` 候选校验阶段报错，不写坏原配置。

### 探测时机、成本与 Windows

- **command：**runtime 打开时做一次 `exec.LookPath(name)`，并在每个新 segment 开始可再次做一次廉价 PATH 检查；不在每个模型 step 或每次 tool call 查找。不存在项只保存诊断，不进入提示词。Windows 交给 Go 的 `LookPath` 使用当前 PATH 与 `PATHEXT`，因此可识别 `.exe`、`.cmd` 等 PATHEXT 中的扩展；配置不应把 shell 参数写进 name。检查不执行命令，不产生审批请求。
- **MCP：**run/chat/ACP 复用 runtime 已启动的 `mcp.Provider`；provider 的启动、initialize 和 `tools/list` 成功才算存在。已有 MCP provider 的启动/list 超时和错误仍沿用 `pkg/agentcore/tools/mcp/provider.go:40-41,69-106` 的有界行为。优先解析只观察 server id 与实际注册的 MCP tool，不另写 codebase-memory-mcp/codegraph 客户端。
- **manifest：**当前 `suga manifest` 只读本地结构且声明“不启动 MCP”（`internal/cli/manifest.go:15-40`，`internal/wiring/manifest.go:174-176`）。为了回答“已配置且连上、tools/list 成功”，本设计允许 manifest 的 preferred 部分使用一个有界的 best-effort MCP probe；它只启动配置的 MCP、执行 initialize/`tools/list` 后立即关闭，不启动 provider/model，不改变运行时 MCP。失败项在 manifest 中给出原因而不是让 manifest 整体失败。该行为变化须在实现计划和帮助文案中明确。
- **会话中途：**segment 是提示词一致性的边界。新安装的 command 在下一段开始可出现；MCP 连接断开时，当前 provider 的既有 tool call 错误语义不变，下一段 probe 重新确认，失败则从下一段的优先列表移除；不在每回合做昂贵的 MCP 重连。若当前 MCP 层没有可复用的健康检查，实施时应在通用 provider 层增加最小 `tools/list` health seam，而不是写服务专用判断；若这会改变 provider 生命周期，停止并回到 Gate。

### MCP 常驻、延迟和审批

| 现状 | 代码位置 |
|---|---|
| MCP provider 启动后把 `tools/list` 得到的工具注册到统一 registry。 | `internal/wiring/capabilities.go:48-79`、`pkg/agentcore/tools/mcp/provider.go:80-106,116-129` |
| `tools.deferred` 的 `auto`/`always`/`never` 由 `Visibility` 按 MCP schema 大小决定延迟集合；延迟工具由 `tool_search` 加载。 | `pkg/agentcore/tools/visibility.go:17-38,117-170` |
| CLI 的 MCP 默认是 deny，chat/ACP 的 MCP 默认分别受 chat/permission gate 约束；`tools.auto_allow` 和 `tools.read_only_allow` 是既有规则。 | `internal/wiring/approval_policy.go:57-79,83-129` |
| run/chat/ACP 的审批层位于 visibility 之后；未加载的 deferred tool 不会绕过审批。 | `internal/wiring/runtime.go:497-508`、`pkg/agentcore/tools/visibility.go:117-121` |

runtime 构建 `Visibility` 时增加一组“pinned MCP tool names”：优先 MCP server 注册成功的所有工具都进入 direct set，永不进入 deferred set；同一 server 的其余工具也按该规则处理。其他 MCP 仍按 `tools.deferred`：`auto` 继续按大小、`always` 继续延迟、`never` 全部直发。优先 MCP 常驻不是新审批等级，也不等于自动允许调用。

如果优先 MCP 不在放行名单，环境说明仍列出它，但在调用时照常 ask/deny：run 使用现有非交互规则失败闭合，chat 询问，ACP 走客户端权限请求；只在 `tools.auto_allow` 或 `tools.read_only_allow`（只读模式）中明确配置后才改变已有行为。例如 CLI 人可以按现有 server/tool pattern 配置：

```yaml
tools:
  auto_allow: ["codebase-memory-mcp/code_search"]
  read_only_allow: ["codebase-memory-mcp/code_search"]
```

实现不自动写这些键，也不把 `preferred` 作为 allowlist。

### 三个入口、skills 与 memory

- run/chat/ACP 都在 `internal/wiring` 组装同一个 runtime：run/chat 经过 `OpenCLI`（`internal/cli/run.go:76-145`），ACP 经过 `Open`（`internal/cli/acp.go:31-73`），因此优先解析应在 wiring owner，不在三个 CLI 复制。
- 子 agent 是独立的只读 runner，只注册 `subAgentTools` 中的 file/web 工具，不复用 `Open` 或 MCP（`internal/wiring/subagent.go:18-52,124-188`）。本设计仍在其环境 section 列出同一份“已发现优先工具”快照，但明确说明子 agent 不能调用 shell/MCP 时不得假装调用；它应把需要父 agent 执行的命令或 MCP 工作写入报告。第一版不扩大子 agent 权限或工具目录。
- 优先工具提示与 skills 解耦：skill 仍由现有 `SkillsSource` 读取；优先工具只是一段环境事实，不自动选择或加载 skill。
- 不新增工具用法存储。固定文案为：“用法不清先看 `--help`；摸清的用法写进记忆。”Agent 可用现有 memory 工具/记忆后端记录具体命令或 MCP 用法，下一次由既有记忆索引提供；preferred 配置只存名字和场景，不存经验。

### 观测与 manifest

`turn_context` 已记录 model/provider/mode、工具 hash/count、规则/记忆/segment hash 和 add_dirs（`internal/personal/turncontext.go:28-75`），并在 `suga trace` 中有专门文本/JSON 渲染（`internal/personal/trace.go:142-150`）。每个回合在现有 `turn_context.attrs` 增加：

- `preferred_tools`: 本回合环境说明实际列出的 name，按声明顺序；
- `preferred_count`: 数量；
- 可选的 `preferred_hash`: 只对 name/kind/present 计算的短 hash，便于查变化但不记录 `when` 长文本。

不记录 command 路径、MCP 参数、环境变量或 secret。segment context 的 `Environment` 仍按现有规则只记录长度到 `context` event（`internal/personal/context_event.go:22-64`）；完整提示文本已在 segment 记录中，trace 只增加可检索的名字。

manifest 增加独立字段，不混入实际 tool catalog：

```json
"preferred_tools": [
  {"name":"rg","kind":"command","when":"精确搜索文本或正则","present":true,"reason":"found on PATH"},
  {"name":"codebase-memory-mcp","kind":"mcp","when":"代码图谱探索","present":false,"reason":"configured server failed tools/list: timeout"}
]
```

`present=false` 的项保留，便于解释“为什么没列入提示”；提示词只消费 `present=true`。manifest 命令的其他既有字段和脱敏约束不变。

## 总体方案

主线：**配置 reader 解析通用声明；wiring 在 runtime/manifest 做一次 best-effort 解析；environment source 只渲染 present 项；visibility 把 present 的 MCP 工具 pin 到 direct；personal 在已有 turn_context 中记录本回合列出的名字。**

```text
config.yaml / suga config set
  -> internal/config preferred reader
       -> wiring.ResolvePreferredTools
            ├─ command: exec.LookPath（按 runtime/segment 边界）
            └─ mcp: configured server + successful tools/list
                 ├─ environmentPrompt: present items only
                 ├─ Visibility: preferred MCP tools are direct/pinned
                 ├─ Manifest: all items + present/reason
                 └─ Runner.turn_context: present names
```

解析结果是纯数据快照，例如 `PreferredToolStatus{Name, Kind, When, Present, Reason, MCPTools}`。它不带 command args、不调用外部 command、不承载审批决定。runtime 已有 MCP provider 时直接使用其 server/tool 元数据，避免重复连接；manifest 采用关闭即止的 probe。配置变更只在新 runtime 生效；segment 边界只刷新外部存在性，不重新读取用户配置。

## 架构

| 组件 | 改动与 owner |
|---|---|
| `internal/config/settings_registry.go` + preferred reader（建议同包新文件） | 注册 `tools.preferred`，解析/校验列表，提供 typed effective value；不负责 PATH、MCP 或提示词。 |
| `internal/wiring/preferred_tools.go`（建议新文件） | 唯一的通用状态解析器、command PATH probe、已注册 MCP 匹配、manifest reason 和稳定排序；不识别 rg/xerj/codegraph 的字段。 |
| `internal/wiring/runtime.go` / `capabilities.go` | 组装一次解析结果，向 environment source、Visibility 和 manifest 传递；所有 CLI 入口复用。 |
| `pkg/agentcore/tools/visibility.go` | 将通用 `Pinned` 名单排除出 deferred MCP 集合；不改变 approval。 |
| `internal/wiring/prompt.go` / `internal/wiring/subagent.go` | 在环境 section 渲染 present 项和固定用法提示；不改 stable base prompt。 |
| `internal/personal/turncontext.go` | 记录本回合 present 名称/计数/hash；不记录 secret 或全文 when。 |
| `internal/wiring/manifest.go` / `internal/cli/manifest.go` | 增加结构化字段和文本/JSON 输出；解释 false reason，显式标注 MCP probe 的有界行为。 |

配置 reader 的职责边界是关键：它只验证数据形状和重复项；PATH 与 `tools/list` 是运行时事实，不能在 `suga config set` 时探测，也不能把一次探测结果写回 config。

## 关键流程

### Runtime（run/chat/ACP）

1. `internal/config` 读取并校验 preferred 声明；无配置等价于空列表。
2. runtime 按既有流程启动已配置 MCP；对成功注册的 provider 形成 server id → tool names 映射。
3. 对 command 做 `LookPath`；对 MCP 仅把“配置存在且 provider startup/list 成功”的 server 标为 present；每一项填 reason。
4. 创建 Visibility 时传入 pinned MCP tool names；然后创建 Runner，environment source 捕获本次解析结果。
5. 新 session/新 segment 生成环境说明；存在项按声明顺序列出，不存在项不写入文本。放在 `# Environment` 的工作区/roots 后。
6. 每回合第一次 model request 的 `turn_context` 写入本回合 present names。配置不会在现有 session 中途热替换；新 runtime 才重新读取 config。

### MCP 断开与新安装

command 新安装：下一次 segment snapshot 的 probe 能看到 PATH 变化；若当前 session 没有 segment break，则维持本段冻结文本，不在回合中途修改上下文。MCP 断开：当前 call 继续使用现有 provider 的错误/重试语义；下一 segment 对 provider 做通用 health/list 检查，失败项从新的 environment section 与 pinned set 移除并填 reason。若服务恢复，下一个 segment 可重新出现。任何自动重连都不在本期实现。

### Manifest

`suga manifest` 读取 config，列出所有 preferred item。command 直接 `LookPath`。MCP 对每个 configured server 运行一次带 `mcp` startup timeout 的 probe，成功 `tools/list` 为 present，失败则 `present=false` + 稳定的错误类别（not configured、start failed、tools/list failed、timeout），不回显 command args/env。probe 结束关闭 provider。manifest 的模型、native tools、skills、memory 等现有内容照旧。

### 配置变更

`config set` 只在整列表解析和候选配置验证通过后写 YAML；例如单引号/双引号由调用者按 shell 规则包住，YAML 值本身可用 flow-style list。配置 I/O、注释保留、原子写、备份与 reload 沿用 `internal/config` 现有 writer。`config get/list` 返回安全的 YAML/JSON 形态，不把 probe reason 当配置值。

## 安全、数据、运维与回滚

- command probe 只查 PATH，不执行命令；真正调用仍是现有 `shell_exec`，继续经过 boundary、审批、超时和输出上限。
- MCP probe 只连接用户已经配置的 server，使用现有启动 timeout；不把密钥、env、args、URL 写进 manifest、trace 或提示词。
- `preferred` 是提示优先级，不是权限。任何 allow/ask/deny 仍来自原 approval policy；read-only 的 MCP 限制不变。
- segment 记录中的环境文本可能包含工具名和 `when`，配置本身也属于用户数据；不写外部工具输出。
- 回滚实现时可先移除 `tools.preferred` registry/接线，旧配置键应被明确报 unknown；已有 native/MCP/visibility/approval 行为恢复到当前版本。若 pinning 或 health 检查导致现有 `tools.deferred` 语义变化，应回滚到仅提示版本。

## 决策

| ID | 决策 | 理由 |
|---|---|---|
| D1 | `tools.preferred` 只含 name/kind/when，name 不含参数 | 通用、可编辑，避免为每个工具设计协议字段。 |
| D2 | 列表放 segment context，不放 stable system prompt | 命令/MCP 状态会变；保护跨会话稳定前缀和缓存。 |
| D3 | command 用 `exec.LookPath`，MCP 复用 provider 的 startup/`tools/list` 事实 | 跨平台且不重复定制；Windows 的 PATHEXT 交给 Go。 |
| D4 | MCP preferred 工具 pin 到 direct，但不改变 approval | “常驻加载”和“免审批”是两个独立问题，避免安全回归。 |
| D5 | runtime 入口统一在 wiring，子 agent 只展示继承的发现结果，不扩权 | run/chat/ACP 一致，子 agent 不可调用的工具不被伪装成可用。 |
| D6 | manifest 保留 false 项及 reason；prompt 只列 true 项 | manifest 用于排障，提示词保持短且不诱导使用失效工具。 |
| D7 | 用法经验写现有 memory，不新增存储 | 避免配置、skills、memory 三套工具知识来源。 |
| D8 | probe 不自动放行、不安装、不建索引、不解析输出 | 保持本任务范围小，沿用现有审批与工具边界。 |

## 待确认事项

| ID | 问题 | 建议结论 |
|---|---|---|
| Q1 | `suga manifest` 为了判断 MCP 是否连上，是否允许启动并立即关闭已配置 MCP？ | 建议允许；只做有界 initialize/`tools/list` probe，并在帮助/文档中标明，不启动模型。若不接受，则 manifest 只能报告 configured，不能满足“连上”语义。 |
| Q2 | MCP 断开是否在每个新 segment 做 `tools/list` health 检查？ | 建议做；不在每回合做，segment 是成本与缓存边界。若 provider 没有安全 health seam，先补通用 seam，不写服务专用逻辑。 |
| Q3 | 子 agent 环境说明是否保留不可调用的 command/MCP 名称？ | 建议保留但加“子 agent 不能调用 shell/MCP，需交父 agent 执行”的明确句子；不扩大子 agent 工具目录。 |
| Q4 | `config get/list` 的 `when` 是否原文显示？ | 建议显示；它不是 secret。manifest/trace 只记录名字，避免长文本污染观测。 |

## 结论与人工计划 Gate

本设计为 `suga-jipz` 的 Draft 0.1，待用户审批。批准后再按对应计划实施；在审批前不新增 registry 键、不改变 manifest 的 MCP 生命周期、不调整 visibility 或 approval。
