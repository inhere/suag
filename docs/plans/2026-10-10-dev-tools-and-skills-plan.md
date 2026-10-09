<!-- template_id: plan; template_version: 1.2.0 -->
# 开发基础工具与 skills 跑通 实施计划

> 状态：Draft 0.1 / 待人工计划批准

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-10 | Claude | 初稿 |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

## 目标与完成定义

思考模式 RIGOROUS；核心目标：落地设计 0.2 的 W1–W7；scope freeze：设计 0.2 的范围、D1–D9；review budget：一轮合并轴评审（低暴露度，SR1409）；停止条件：评审无 CORE_BLOCKING 且用户批准。

完成定义（两阶段，设计 D9）：

- 阶段一（不挂住、可预期）：`shell_exec` 在 Windows 用 PowerShell、中文不乱码、超时必返回且保留输出与退出码；所有 native 工具输出有界；`file_read`/`file_list` 分段与递归；只读注册收口、`web_search` 只读可用；未启用工具给出原因并计数、未知工具不计数；MCP 单次调用超时；基础系统提示词生效；运行状态不再重复存消息。完成后可交 gofer 试用。
- 阶段二（能力补齐）：`file_grep`、`file_find`、`file_edit`、`web_fetch`、`skill` 工具与 skills 列表可用，审批与 ACP 分类正确。
- 每阶段：`go test -race ./...` 全绿；`make smoke-chat` 全部通过；主机 Windows 相关包 `go test` 通过；按 SR1402.1 用真实 provider 完成一次真实验证。

## 范围、排除项与授权

运行目录：除特别注明外均为 Git root `inhere-tools/suag`（容器路径 `/d/work/inhere/my-tools-dev/inhere-tools/suag`）。

范围：设计 0.2 列出的文件与模块。排除：设计非目标（后台 shell、子 agent、向量检索、Claude 思考、gofer 配置改动、`tools.shell=cmd`、grep count/context、chat 完整 diff、会话明细改存 jsonl）。

授权：按任务本地原子提交（SR1211）。本计划批准即同时批准下列验证动作：经 `gofer job run -p my-tools-dev -a exec --runner local` 在主机跑 `go test` 与 `make install`（安装到主机 `D:\env\gopath\bin`），以及每阶段一次用主机已配置的 opencode-go provider 的真实 chat/run 验证（消耗少量 token）。push、发版、修改 gofer 配置不在本计划授权内。

- `host_or_non_offline_action=REQUIRED`

## 输入与批准证据

- 设计：`docs/design/2026-10-10-dev-tools-and-skills-design.md` Draft 0.2（e7d79f1）；用户原话：“批准设计并开始写计划”，Q1–Q4 “按推荐”；W7 来自用户“db 的是否可优化”。
- 设计评审：合并轴评审 PASS_WITH_CHANGES，F1–F12 已在 0.2 处理，F13–F15 中 F14 已纳入、F13/F15 记为后续可选。
- 现状盘点：本会话 2026-10-10 只读调研（事实表见设计）。

## Capability Discovery

### Capability decisions

| capability_id | required_capability | searched_candidates | direct_reuse | thin_adapter_or_owner_extension | decision | proven_gap | duplication_and_lifecycle_risk |
|---|---|---|---|---|---|---|---|
| CAP-01 | 新增/改进 native 工具、审批分类、提示词、skills 接入、运行状态存储 | `internal/platform/tools/native`、`tools.Registry`/approval/visibility、`acpserver/policy.go`、`personal.Config.PromptBuilder`、`capability/skills`、`store.AgentStorage`、`golang.org/x/sys`、`golang.org/x/text`、`golang.org/x/net/html` | 是 | 均为既有 owner 内扩展；新工具作为既有 Registry 的工具实现，非新 Module；`x/net` 为新增官方依赖 | DIRECT_REUSE | none | 删除 skills 旧解析/猜测/resolver 以免两套逻辑并存 |

### New module candidates

| candidate_id | capability_id | proposed_module | searched_candidates | direct_reuse_gap | thin_adapter_or_owner_extension_gap | proven_gap | unique_owner_and_lifecycle | deletion_or_merge_handling |
|---|---|---|---|---|---|---|---|---|
| NONE | none | none | none | none | none | none | none | none |

### Rejected new tools

| rejected_candidate | capability_id | deletion_test_and_reason |
|---|---|---|
| NONE | none | none |

## 前置检查与 fail-closed 条件

- workspace baseline：仓库 `inhere-tools/suag`，branch `main`，HEAD e7d79f1，`git status --short` 为空；Go 1.25（容器 `/d/env/linux-env/sdk/gosdk/go1.25.10`）；主机 Windows Go 与 `make install` 可用（本会话已验证）。
- 测试隔离：所有测试 `SUAG_HOME` 指向临时目录，不得触碰真实 `~/.suag`；伪终端冒烟用 `scripts/ptysmoke/` 的 fakellm。
- fail-closed：任一任务 `go test -race` 回归 → 停止修复；`make smoke-chat` 回归 → 停止；W7 等价性验证不成立 → 走设计 W7 的退路，不强行删除；Windows 主机测试失败 → 不进入下一阶段；单文件超 1000 行（GR104）→ 先拆分。

## 波次与依赖

| 阶段 | 任务 | 依赖 |
|---|---|---|
| 0 | T0 基线 | — |
| 一 | T1 输出上限与注册收口 → T2 shell → T3 file_read/list → T4 web_search → T5 未启用工具与 MCP 超时 → T6 基础系统提示词 → T7 运行状态 → T8 阶段一验证 | T0；T2–T7 依赖 T1 |
| 二 | T9 grep/find → T10 file_edit → T11 web_fetch → T12 skills → T13 阶段二验证与文档 | T8 |

单 Agent 顺序执行，每任务一个本地提交；进度记在 `gofer plan`（建 plan 时同步全部任务 todo）。

## 任务

### T0 基线

- 文件: 无
- 动作: 记录 HEAD、全量 race 测试与冒烟结果。
- 验证: `go test -race ./...`；`make smoke-chat`。
- 完成标准: 全绿、22/22，写入 gofer plan 备注。
- 依赖: —

### T1 统一输出上限与 native 工具清单

- 文件: `internal/platform/tools/native/output.go`(新)、`internal/wiring/capabilities.go`、`internal/wiring/runtime.go`(:290-300 只读分支)、对应测试
- 动作: 新增按字节、UTF-8 边界的截断函数（头尾与按行两种），默认 30000，读 `tools.max_output_bytes`；抽出“native 工具清单 + 读写标记”函数，run/chat/ACP 三处注册改用它，删除 `runtime.go:294-299` 硬编码。
- 验证: `go test -race ./internal/platform/tools/... ./internal/wiring/...`；新增用例：中文边界不截断字符、头尾标注省略字节数、只读模式工具集与写模式工具集精确等于清单。
- 完成标准: 测试通过，三种模式注册结果与改动前一致（`web_search` 除外，见 T4）。
- 依赖: T0

### T2 shell_exec 重写

- 文件: `internal/platform/tools/native/code_exec.go`、`code_exec_unix.go`(新)、`code_exec_windows.go`(新)、`code_exec_test.go`(新)、`internal/wiring/capabilities.go`(超时与 shell 配置)
- 动作: 按设计 W2：shell 选择（Windows pwsh→powershell，参数 `-NoLogo -NoProfile -NonInteractive -Command`，UTF-8 前缀与 `exit $LASTEXITCODE` 后缀；其他 `sh -c`；`tools.shell` 显式覆盖）；`timeout` 参数（默认 120，上限 `tools.shell_timeout_max` 600）；`cmd.Cancel` + `WaitDelay` 2s；Unix 进程组、Windows job object；超时/非零退出返回已有输出与退出码；GBK 兜底解码；环境 `GIT_TERMINAL_PROMPT=0`、`GIT_PAGER=cat`、`PAGER=cat`；输出经 T1 截断；工具描述写明当前 shell。
- 验证: 容器 `go test -race ./internal/platform/tools/native/ -run Shell`；用例：退出码、超时返回部分输出、后台子进程持有管道时 3s 内返回、输出超限截断、超时上限钳制。主机 Windows：`gofer job run -p my-tools-dev -a exec --runner local --cwd inhere-tools/suag --sync -- go test ./internal/platform/tools/native/ -run Shell -v`，用例含中文输出不乱码、`cmd /c exit 3` 得到 3、`Start-Sleep` 超时终止。
- 完成标准: 两端测试通过；反向验证：去掉 `WaitDelay` 后“子进程持有管道”用例失败。
- 依赖: T1

### T3 file_read 分段与 file_list 递归

- 文件: `internal/platform/tools/native/file.go`（必要时拆出 `file_read.go`、`file_list.go`）、`file_test.go`
- 动作: `file_read` 加 `offset`/`limit`（默认 2000 行）、行号、二进制检测、行边界截断并提示下一段 offset；`file_list` 加 `depth`(≤5)、`max_entries`(500)、默认忽略目录，经 `os.OpenRoot`，跳过 symlink。
- 验证: `go test -race ./internal/platform/tools/native/ -run 'File'`；用例：offset 续读拼接等于原文、二进制报告、越界 symlink 被拒、depth/max 生效、Windows 路径分隔（既有 pathresolve 测试保持通过）。
- 完成标准: 测试通过，旧参数调用（只给 path/dir）行为兼容。
- 依赖: T1

### T4 web_search 修正

- 文件: `internal/platform/tools/native/search.go`、`search_test.go`(新)、清单（T1）
- 动作: `num` 改 integer（兼容旧 string 输入）；归入读工具，只读模式注册（仍需 `serpapi_key`）；结果经 T1 截断。
- 验证: `go test -race ./internal/platform/tools/native/ -run Search`（httptest 模拟 SerpAPI）；注册用例：只读模式有 key 时存在。
- 完成标准: 测试通过。
- 依赖: T1

### T5 未启用工具拒绝与 MCP 单次超时

- 文件: `internal/platform/tools/approval.go`、`internal/platform/tools/registry.go`、`internal/platform/tools/mcp/client.go` 或其工具包装、`internal/wiring/`（传入“已知但未启用”名单与原因）、测试
- 动作: approval 层 Get 失败时：名字在“已知未启用”名单内 → `tools.Denied(原因+开启方法)` 计入拒绝；否则返回“not found + 可用工具列表”不计数。MCP Execute 套 `WithTimeout(mcp.call_timeout_sec，默认 120)`。
- 验证: `go test -race ./internal/platform/tools/... ./internal/personal/...`；用例：只读模式调 `shell_exec` 3 次结束回合且提示含“--allow-write”；调 `bash` 不计数；未加载 deferred MCP 工具仍走自动加载；MCP 假服务器不响应时在超时后返回错误。反向验证：把未知名字也计数后，“调 bash 不计数”用例失败。
- 完成标准: 测试通过。
- 依赖: T1

### T6 基础系统提示词

- 文件: `internal/wiring/prompt.go`(新)、`internal/wiring/runtime.go`(设置 `PromptBuilder`)、测试
- 动作: `wiring.Open` 时生成一次：OS 与 shell、工作区、模式与开启写工具的方法、使用约定；预留 skills 列表位置（T12 填充）；不含工具清单与时间。
- 验证: `go test -race ./internal/wiring/... ./internal/personal/...`；用例：只读与可写两种文本、同一进程多回合文本逐字节一致、不含日期。
- 完成标准: 测试通过；`suag run` 实跑（fakellm）时请求的 system 中含提示词。
- 依赖: T1、T2（shell 名称）

### T7 运行状态不重复存消息（W7）

- 文件: `internal/personal/store/agentstore.go`、`agentstore_test.go`、必要时 `pkg/agentcore` 只读核对
- 动作: 先写等价性测试：跑一个含工具调用的回合（含 compaction/handoff 注入的上下文消息），比较 `RunState.Messages` 与按 turn 从 entries 重建的消息；等价则 `SaveRun` 去掉 `Messages`、`LoadRun` 从 entries 重建（`Messages` 非空的旧数据直接使用）；不等价则走退路：回合 `completed` 后清空该回合 `state_json` 的 `Messages`。
- 验证: `go test -race ./internal/personal/... ./pkg/agentcore/...`；用例：中断回合续跑结果不变（既有 resume 测试保持通过）、新回合 `state_json` 不含消息、旧格式可读。
- 完成标准: 测试通过；在临时 SUAG_HOME 用 fakellm 跑 3 个回合，`state_json` 总字节明显小于 entries。
- 依赖: T0

### T8 阶段一验证

- 文件: `scripts/ptysmoke/chat_approval.py`（如需）、`README.md`、`docs/usage/gofer-personal-agent.md`
- 动作: 全量 race 与冒烟；主机 `go test ./...` 与 `make install`；真实验证：主机 chat（`tools.allow_write` 开启）让模型执行一条含中文输出的 shell 命令、读一个大文件第二段，并用 `suag run --allow-write` 跑同样任务；文档写明只读 run 与 `--allow-write` 两种 gofer 用法、新配置项。
- 验证: `go test -race ./...`；`make smoke-chat`；主机 job 输出；`suag trace <session>` 记录两次真实会话。
- 完成标准: 全部通过；真实会话中 shell 输出中文正常、退出码正确；gofer plan 标记阶段一完成，可交 gofer 试用（提示 gofer 会话为 run 模板加 `--allow-write`，Q3）。
- 依赖: T1–T7

### T9 file_grep 与 file_find

- 文件: `internal/platform/tools/native/walk.go`(新，共享遍历/忽略/二进制判断)、`grep.go`(新)、`find.go`(新)、测试；清单（T1）；`internal/acpserver/policy.go`(Search)；`internal/wiring/chat_approver.go`(白名单)
- 动作: 按设计 W1；遍历与读取经 `os.OpenRoot`、跳过 symlink；10s 超时返回部分结果。
- 验证: `go test -race ./internal/platform/tools/native/ -run 'Grep|Find' ./internal/acpserver/ ./internal/wiring/`；用例：.gitignore 简单模式、二进制跳过、symlink 越界不读、结果上限、超时标注、ACP kind 为 Search、chat 不询问。
- 完成标准: 测试通过。
- 依赖: T8

### T10 file_edit

- 文件: `internal/platform/tools/native/edit.go`(新)、测试；清单；`policy.go`(Edit 与 `toolDiff`)；`chat_approver.go`(片段预览)
- 动作: 按设计 W1；`toolDiff` 先应用替换再出 diff；chat 询问显示 old/new 片段。
- 验证: `go test -race ./internal/platform/tools/native/ -run Edit ./internal/acpserver/ ./internal/wiring/`；用例：唯一替换、不唯一报次数、`replace_all`、CRLF 文件保持 CRLF、ACP diff 正确、只读模式拒绝并计数。
- 完成标准: 测试通过。
- 依赖: T9

### T11 web_fetch

- 文件: `internal/platform/tools/native/fetch.go`(新)、测试；`go.mod`/`go.sum`（`golang.org/x/net`）；清单；chat 白名单
- 动作: 按设计 W1 的拨号时校验、无代理、跳转限制、大小/超时、HTML 文本提取。
- 验证: `go test -race ./internal/platform/tools/native/ -run Fetch`；用例：httptest 服务（需 `allow_private` 测试开关）、127.0.0.1/10.x/169.254.169.254 拒绝、公网域名解析到私网（自定义 Resolver）被拒、跳转到私网被拒、超过 5MB 截断、script/style 去除。真实验证：主机 chat 抓取一个公网文档页。
- 完成标准: 测试通过；真实抓取得到正文。
- 依赖: T9

### T12 skills 跑通

- 文件: `internal/capability/skills/{parser,loader,registry,types}.go`、删除 `resolver.go` 与按描述猜工具逻辑、`skills_test.go`；`internal/wiring/skills.go`、`capabilities_test.go`(:202-224)；`internal/platform/tools/native/skill.go`(新)；`internal/wiring/prompt.go`（skills 列表）；`policy.go`(Read)；chat 白名单
- 动作: 按设计 W4：frontmatter+正文解析（兼容 CRLF）、来源与工作区优先、删除 `config/skills` 来源、启动时扫描一次、`skill` 工具（`file` 经 `os.OpenRoot(skillDir)`）、提示词列表。
- 验证: `go test -race ./internal/capability/... ./internal/wiring/... ./internal/platform/tools/native/ -run Skill`；用例：带正文 SKILL.md 解析、CRLF、同名工作区优先、无效文件警告、`skill` 越界 `file` 被拒、提示词含列表。真实验证：主机 `~/.agents/skills` 下现有 skill（如 gofer-usage）在 chat 中被列出并能取正文。
- 完成标准: 测试通过；真实 chat 中模型调用 `skill` 成功。
- 依赖: T6、T9

### T13 阶段二验证与文档

- 文件: `scripts/ptysmoke/chat_approval.py` 或新场景脚本、`README.md`、`docs/usage/`、`AGENTS.md`（入口文档指向本设计/计划）
- 动作: 冒烟增加 grep→edit 场景；主机 `go test ./...`、`make install`；真实验证：主机 chat 与一次 gofer `suag` job 完成“file_grep 定位 → file_read → file_edit → shell_exec 跑 go test”的小任务（在临时目录的小 Go 项目里）；更新文档；写复盘。
- 验证: `go test -race ./...`；`make smoke-chat`；gofer job 结果与 `suag trace`。
- 完成标准: 全部通过；复盘写入 `docs/plans/2026-10-10-dev-tools-and-skills-retrospective.md`。
- 依赖: T9–T12

## 回滚与恢复

- 每任务单独提交，可按任务 `git revert`；阶段一完成点（T8 提交）是可交付的恢复点。
- W7 只改新回合写入格式，旧数据可读；回滚版本即可。
- 主机安装的二进制可用上一版 `make install` 回装。
- 中断时按 SR1407 记录 HEAD、dirty、首个失败边界与下一条命令到 gofer plan 交接说明。

## 人工 Gate

1. 本计划批准（含主机验证与真实 provider 调用）。
2. 阶段一完成后向用户报告，用户可先交 gofer 试用；阶段二无需另行批准即继续（同一计划内）。
3. push、发版、gofer 配置改动另行授权。

## 可追溯性

| 设计项 | 任务 | 验证 |
|---|---|---|
| W1 grep/find/edit/fetch | T9、T10、T11 | 各自单测；T13 真实小任务 |
| W2 输出上限、注册收口 | T1 | T1 用例 |
| W2 shell | T2 | 容器与 Windows 测试、T8 真实验证 |
| W2 file_read/list | T3 | T3 用例 |
| W2 web_search | T4 | T4 用例 |
| W3 提示词 | T6 | T6 用例 |
| W4 skills | T12 | T12 用例与真实 chat |
| W5 未启用/MCP 超时/审批分类 | T5、T9–T12 | T5 用例；ACP/chat 分类用例 |
| W6 验证 | T8、T13 | 全量、冒烟、主机、真实 provider |
| W7 运行状态 | T7 | 等价性与续跑用例 |
| D9 两阶段 | T8 | 阶段一报告 |

## 完成 Gate 与剩余工作

T13 完成并写复盘后向用户报告。剩余：push（待授权）；gofer run 模板加 `--allow-write`（gofer 会话）；设计评审 F13（skill 越界已含于 T12）、F15（MCP 取消通知，可选）；会话存储结构调整另立候选。
