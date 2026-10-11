# Changelog

本文件记录 suag 各版本的用户可见变更，格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循 [SemVer](https://semver.org/lang/zh-CN/)。当前处于 `v0`，次版本之间可能有不兼容变更，会在“不兼容”一节写明。

## [未发布]

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

[未发布]: https://github.com/inhere/suag/compare/v0.10.0...HEAD
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
