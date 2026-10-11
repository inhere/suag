<!-- template_id: design; template_version: 1.1.1 -->
# 状态、记忆与可扩展核心（总纲 + 第一期） 设计
> 状态：Draft 0.3 / 已批准进入计划（2026-10-11 用户：“批准设计 0.3，Q5–Q6 按推荐”；Q1–Q4 此前“待确认按推荐”，Q1 补充为仓库根 `MEMORY.md`）
> 状态：Draft 0.3 / 待人工计划批准（Q1–Q4 已按推荐决定，2026-10-11 用户：“待确认按推荐”；Q1 补充为仓库根 `MEMORY.md`）

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-10 | Claude | 初稿 |
| 0.2 | 2026-10-10 | Claude | 按合并评审修订：污染改为会话粒度、粘住不清除，由观测层按工具声明置位并注入治理层，污染源写全（F1）；索引快照改为按会话冻结在 session meta，压缩时刷新（F2）；脱敏与凭据剥离下沉到 `pkg/agentcore`（F3）；recall 不做通用抽象，只在消费方声明窄接口（F4）；FTS 行与 entries 同 rowid，删除触发器 + 幂等分批回填（F5）；gofer 后端只管理带 suag 标签的条目；查询转义与结果脱敏；能力清单改名 `suag manifest`；更正“系统提示词每进程构建一次”的事实 |
| 0.3 | 2026-10-11 | Claude | 按用户答复：记录 Q1–Q4 决议；项目记忆支持仓库根 `MEMORY.md`（Q1、Q5）；W1 新增输出治理——大输出存会话目录文件、DB 只存元信息与头尾片段、对所有工具（含 MCP）生效、可按 ref 回读与搜索（D16–D18） |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

## 背景与目标

思考模式 RIGOROUS；核心目标：把 suag 的上下文、记忆、状态、工具四块核心能力统一到一份总纲下，并完成第一期——能量化（评测与成本）、能回查（底账检索）、能记住（精选记忆 v1），三者都以“内核定接口、内置实现最强、后端可替换、治理与后端无关”的方式落地，为自我扩展留出口子。scope freeze：W0–W4（见“范围”）；review budget：一轮合并轴评审（低暴露度，SR1409：个人项目、未投产、数据可重建、无出机器动作）；停止条件：评审无 CORE_BLOCKING 且用户批准。

用户原话（2026-10-10）：

- “核心能力定接口，有很强的内置，同时可替换，为自我更新/扩展能力留下口子”
- “在基本安全的前提下尽量不限制它的能力和扩展/成长性（我用 codex 感觉限制很多，麻烦），内置能力已经是极简依赖下的最强（减少依赖，但是能通过依赖外部工具让他更强）”
- 项目记忆可以走 gofer memory，但“不是强绑定”。
- （2026-10-11）“项目下的 MEMORY.md 用仓库根目录；输出治理是需要的，原始输出的大内容不进入 db 避免无意义的膨胀，而是进会话目录下的文件，db 只存 meta 信息/前 N 字符等”。

用户可见结果：

1. `go run ./scripts/eval` 跑一组固定任务，输出每个任务的成败、调用次数、token、缓存命中率和成本；配置模型价格后，`suag stats` / `suag trace` 的成本列不再是 0。
2. 模型可以用 `session_search` 检索所有历史会话（中文可搜），用 `recall` 按引用取回原文；每个回合在 trace 中能看到“当时用的模型、模式、工具集、规则与记忆快照摘要”。
3. 任何工具（含 MCP）的大输出不再灌进上下文、也不再撑大 DB：超过阈值的原文存进会话目录下的文件，上下文与 DB 里只有头尾片段和一个 ref；模型可以按 ref 分段读回或在其中搜索，原文不再丢失。
4. 模型可以用 `memory` 工具记住和修改用户、反馈、项目、参考四类条目；新会话自动带上记忆索引；你能直接打开 Markdown 文件查看、编辑、删除；仓库根有 `MEMORY.md` 时项目记忆就放在仓库里，也可以配置为走 gofer memory，都没有时由 `~/.suag` 下的内置文件完整提供。
5. `suag manifest` 列出每项核心能力当前由哪个后端提供、能否替换；新后端可以用任意语言写成可执行文件，通过配置接入，无需重新编译 suag。

### 定位：和其他 agent/harness 比，suag 的特色在哪里

| 维度 | Claude Code / Codex | OpenCode / pi | OpenClaw / Hermes | suag |
|---|---|---|---|---|
| 形态 | 厂商的交互式编程工具 | 开源编程 CLI（TS），pi 走极简 | 常驻个人助理框架（TS/Python），接聊天渠道 | Go 单二进制 + 可嵌入内核 `pkg/agentcore` |
| 会话 | 终端里的一段对话，JSONL/SQLite 落盘 | 同左 | 常驻、跨渠道 | 持久对象：SQLite 底账，跨进程续接，可被 `send` 投递 |
| 谁来驱动 | 人 | 人 | 人（消息）+ 定时 | 人 + gofer（ACP/CLI、异步作业、审批） |
| 模型 | 自家模型为主 | 任意 | 任意 | 任意 OpenAI/Anthropic 兼容，主跑便宜模型 |
| 安全模型 | Codex：OS 沙箱 + 审批策略；Claude Code：权限模式 | 权限配置 | 后台默认只读、规则引擎 | 信任用户 + 审批，无沙箱 |
| 记忆 | 规则文件 + 自动笔记（存在厂商目录） | 规则文件，记忆外包插件 | 五层记忆 / 小预算常驻 | 规划：文件为事实源 + 可替换后端 |

坦白地说：循环、内置工具、压缩、skills、MCP 都已是标配，单拿哪一项都不是特色；工具精细度（编辑容错、诊断回传）、模型、生态、维护人力，suag 都比不过 Claude Code / Codex。如果目标是“做一个比它们更好的编程 CLI”，不值得继续。

suag 真正不同的是这个组合：**会话是持久对象 + 为被编排而生（ACP + gofer）+ 状态与记忆完全归你所有且后端可换 + 可嵌入的 Go 内核 + 对便宜模型的 harness 补偿 + 开放优先**。合起来是“个人常驻 agent 运行时”：厂商工具在这里弱（会话绑在终端和自家产品里，记忆存在别人的目录，限制多），OpenClaw/Hermes 有需求验证但偏聊天渠道、栈重。记忆文章的结论“记忆的价值与载体的开放性成正比”正好站在 suag 这边；构想中的 people（suag-frgt）也是这个方向的自然延伸。

继续的条件（写进验收，避免自我感动）：W0 评测上线后，用同一模型对比 suag 与至少一个开源 harness（opencode 或 pi）；W1–W2 完成后，gofer 派发的只读调研、文档整理和小修改日常由 suag 承担。若评测显示同模型下成功率明显落后且第一期后无改善，则收缩为“agentcore 库 + gofer 组件”，不再追求独立 agent。

## 名词

- **底账（ledger）**：会话的全部历史，即现有 `entries`/`turns`/`events` 表，只追加。
- **投影（projection）**：某次模型调用实际看到的上下文，由规则、记忆索引快照、底账片段组装而成。
- **段（segment）**：两个断点之间的一段投影；断点是会话开始、压缩、交接、子 agent。本期只在名词上定义，实现属于下一期。
- **精选记忆**：跨会话需要想起的少量条目，四类：user（用户偏好）、feedback（对工作方式的纠正）、project（项目目标、约束、进展）、reference（外部资源位置）。
- **范围（scope）**：记忆归属，`user`、`project:<key>`，预留 `people:<name>`。
- **后端（backend）**：某项能力的具体实现，如记忆的 `files`、`gofer`、`exec`。
- **治理层**：包在任意后端外面的统一约束：预算、污染关口、来源、墓碑、事件。
- **污染（taint）**：会话中模型读到过外部内容后，会话被标记为污染，此后写入记忆需要确认；标记粘住不清除（压缩出的摘要同样可能带着外部内容）。
- **墓碑（tombstone）**：被删除条目的记录，防止同一事实被后续写入悄悄复活。
- **exec 协议**：外部后端的统一调用方式：一次调用起一个进程，stdin 一行 JSON 请求，stdout 一行 JSON 响应。
- **能力清单**：当前装了哪些能力、各由哪个后端提供、是否可替换的机器可读描述。
- **会话目录**：`~/.suag/sessions/<session_id>/`，放不适合进 DB 的会话大文件；本期只有 `outputs/`。
- **外置输出（spill）**：超过内联阈值的工具原文写入会话目录 `outputs/` 下的文件，上下文与 DB 中只留头尾片段、元信息和 ref。

## 范围与非目标

范围（仓库 `inhere-tools/suag`）：

- **W0 评测与成本**：模型价格配置与成本计算；`scripts/eval`（任务定义、运行器、汇总）；工具事件记录原文与内联字节数，评测汇总“每回合工具输出字节数”。
- **W1 底账检索与输出治理**：FTS5 全文索引、删除同步与回填；`session_search`、`recall` 两个只读工具（消费方窄接口，见 D2）；每回合 `turn_context` 事件；会话污染标记；工具输出包装层（对所有工具含 MCP 生效）与会话目录外置输出、gc 同步删除。
- **W2 记忆 v1**：`memory` 包（类型、接口、治理层、路由）；内置 `files` 后端；gofer 后端（应用层）；`memory` 工具；索引快照按会话冻结并注入提示词；脱敏下沉到 `pkg/agentcore/redact`；`api.Memory` 改名。
- **W3 扩展面**：exec 协议 v1（首个使用者是记忆的 `exec` 后端；MCP 的凭据剥离一并移入）；`suag manifest` 与只读工具 `suag_manifest`。
- **W4 文档**：README、`pkg/agentcore/README.md`、配置样例、CHANGELOG。

非目标（各自另起设计）：会话分段与“交接代替压缩”；只读子 agent；工具稳健性（编辑前检查、多处修改、超长输出存文件）；文件检查点与撤销；离线整理（“做梦”）与前瞻记忆；people（suag-frgt）；权限模型重整（安全底线 + 信任档位，见 D12）；向量检索；OTel 导出；配置补丁提案流程。

## 已确认事实与规范

- 用量（主机 `suag stats --since 7d`，2026-10-10）：`opencode-go/deepseek-v4.1-flash` 203 次调用，缓存命中 89.8%，压缩 0 次；`skill` 是调用最多的工具（79 次）；成本列为 0。
- 成本只来自 provider 返回的 `cost_usd`（`internal/personal/observe.go:100`），没有价格表。
- 底账表：`sessions(id, cwd, …, meta_json)`、`turns`、`entries(session_id, seq, turn_id, type, json, created_at)`（有 rowid）、`events(kind, name, tokens…, attrs_json)`（`internal/personal/store/migrations/20261008-230000-create-core-store.sql`）；`entries` 只靠 `ON DELETE CASCADE` 随会话删除（`store.go:64` 开启外键，`gc.go:64`、`sessions.go:120` 只删 sessions）。
- `modernc.org/sqlite` v1.60.1 各平台都开启了 `SQLITE_ENABLE_FTS5`；评审实测 `tokenize='trigram'` 可建表，中文 `MATCH` 与 `snippet()` 正常，少于 3 个字符的查询 `MATCH` 返回 0 条不报错，需按 rune 数判断后退回 `LIKE`。
- 系统提示词：环境与 skills 部分每进程构建一次（`internal/wiring/prompt.go:15` 的 `staticPrompt`）；项目规则每回合经 `RuleSource` 重读、按 hash 比对后存进 session meta，变化时会话中途生效（`internal/personal/runner.go:222-228,405-420`）；ACP 每次 Prompt 重新 `open` 一个 runtime（`internal/acpserver/server.go:209`），所以“每进程一次”不能作为冻结单位。
- 规则加载：include 只展开一层（`internal/personal/rules.go:19`）。
- `api.Memory`（Hydrate/ObserveMessages/BuildContext）只被 runtime 默认实现使用（`pkg/agentcore/runtime.go:20,41,59`，`runtime_helpers.go`），`internal/` 无引用；语义是“组装历史”，与长期记忆无关。
- `api.Storage` 只按 runID（suag 中即回合）读写（`pkg/agentcore/api/storage.go:6-11`），不能列会话、不能按 `(session, seq)` 定位。
- 工具输出：内置工具各自按 `tools.max_output_bytes`（默认 30000）保留头尾截断，中间部分永久丢弃（`pkg/agentcore/tools/native/output.go:12,44`）；MCP 工具结果**没有任何上限**，全文原样返回并写入底账（`pkg/agentcore/tools/mcp/client.go:403`）；上下文只在接近阈值时按 16000→2000 逐级应急截断（`internal/personal/context.go:260`）。
- DB 体积：dev-tools 设计时主机实测 `entries` 0.54 MB 中工具结果 64 条占 0.47 MB（`docs/design/2026-10-10-dev-tools-and-skills-design.md:56`）；用户当时提出的 DB 优化（W7）只落地了“状态不重复存消息”（eed9479），“会话明细改存文件”当时列为非目标、未实施。
- 现有会话外文件只有 `~/.suag/transcripts/<id>.jsonl` 导出（gofer 集成设计），gc 会一并删除。
- `RedactSecretValues` 在 `internal/personal/transcript.go:143`；MCP 子进程的凭据剥离在 `pkg/agentcore/tools/mcp/env.go`（未导出）。
- hooks 已有“每次调用起一个进程、stdin 一行 JSON”的协议（`internal/hooks/hooks.go:3`）。
- `gofer memory` 有 `ls [kw] --json`、`show <keys...>`、`set`（`--summary`、`--kind rule|note|handoff`、`--tag`、`--source`）、`rm <key>`；没有 title 字段；rule/note 会注入所有 agent 的开场上下文。
- 工具分层为 observe → Visibility → Approval → Registry（`internal/wiring/runtime.go:187`）；工具通过 `tools.Describer` 声明类别与只读（`pkg/agentcore/tools/meta.go:30`）；`layering_test.go` 禁止 `pkg/agentcore` 依赖其外的任何 suag 包。`internal/wiring/capabilities.go` 已用“capabilities”指工具/MCP 组装，能力清单另起名。
- 记忆的行业做法与数字见 `local/wechat_article_agent_memory_management.md`（参考材料，非事实源）；上下文与缓存的取舍见 `local/wechat_article_kv_cache_coding_agent.md`。
- 适用规范：SR1107、SR1403、SR1405 / SR1405.1（接口以两个实现为准入，见 D2）、SR1409、SR1413、GR104、GR105（迁移文件命名）、GR106。

## 总体方案

总纲四条原则，本期和以后各期都按它取舍：

1. **状态显式，上下文是投影。** 一切先落成可查的状态（底账、记忆文件、技能文件）；上下文由状态投影出来。投影只在断点重算，断点之间只追加，保住缓存。
2. **核心能力接口化。** 接口放在 `pkg/agentcore`，尽量小；内置实现只依赖标准库和 SQLite，开箱即用、能力完整；探测到外部工具（gofer、rg 等）时可以增强，但绝不依赖它们才能工作。
3. **治理与后端无关。** 预算、污染关口、来源、墓碑、事件做成包装层，和现有工具分层同构；换后端不丢治理，内置后端也不享受特权。
4. **开放优先。** 守住安全底线（D12），其余默认放开、可配置；扩展分三级：数据（模型直接改）、配置（提案后审批）、代码（新后端以可执行文件接入，升级走发版）。本期实现数据级与代码级的接入口，配置提案留到 people。

## 架构

```text
应用层  internal/*
        wiring: 组装 Router、治理层、工具；污染标记与索引快照接到 session meta
        adapters/gofermem: 记忆的 gofer 后端（调用 `gofer memory`）
        personal: 污染置位（观测层）、索引快照冻结与刷新、会话目录（outputs/）的 Spill 实现
        personal/store: 底账检索的 SQLite 实现（FTS5）、memory_meta 旁表；gc 同时删会话目录
        cli: manifest 子命令；scripts/eval 只经 CLI 交互
          │
能力层  agentcore/memory        类型、Store 接口、可选接口、Governed、Router
        agentcore/memory/files  内置后端：MEMORY.md 索引 + 一事一文件
        agentcore/extproc       exec 协议 v1：请求/响应信封、超时、凭据剥离（MCP 共用）
        agentcore/redact        密钥脱敏、隐形字符与伪装标签清理（personal 改用它）
        agentcore/tools         Output 包装层（内联阈值、头尾片段、Spill 接口），挂在 observe 与 Visibility 之间
        agentcore/tools/native  memory、session_search、recall、suag_manifest 工具
          │
内核    agentcore + api         History（原 Memory）、ContextBuilder 等不变
```

接口草案（以实现时为准，原则是小）：

```go
package memory

type Kind string // user | feedback | project | reference

type Scope struct{ Kind, Key string } // {"user",""} / {"project","<key>"} / {"people","<name>"}

type Entry struct {
	ID, Title, Body string
	Kind            Kind
	Updated         time.Time
	Prov            Provenance // 会话、回合、来源类别、是否污染
}

type Store interface {
	Index(ctx context.Context, s Scope) (Index, error) // 常驻部分：每条一行
	Get(ctx context.Context, s Scope, id string) (Entry, error)
	Write(ctx context.Context, s Scope, op Op) (Entry, error) // add | replace | remove
	Search(ctx context.Context, s Scope, q string, limit int) ([]Hit, error)
}

// 可选：后端自己能存来源和墓碑时实现；否则由治理层记在 suag 的 memory_meta 旁表。
type ProvenanceStore interface{ /* … */ }

// Governed 需要的外部输入都由宿主注入，agentcore 不认识会话与污染的存储方式。
type Policy struct {
	Tainted  func(ctx context.Context) bool          // 当前会话是否污染
	Confirm  func(ctx context.Context, op Op) error  // 污染时的确认（chat 询问、ACP 请求权限、run 拒绝）
	Sanitize func(string) string                     // 默认 redact.Clean
	Budget   Budget                                  // 每范围 200 行 / 16 KB
	Observe  func(ctx context.Context, ev Event)     // memory_write / memory_denied 事件
}
```

```go
package native // 底账检索只有一个真实实现，接口在消费方声明（同 ToolSearcher、PlanSink）

type LedgerSearcher interface {
	Search(ctx context.Context, q LedgerQuery) ([]LedgerHit, error) // Hit 带 Ref{Session, Seq} 与片段
	Fetch(ctx context.Context, ref string, offset, limit int) (string, error) // 底账消息窗口或外置文件行窗口（D18）
}
```

包与职责：

| 包 | 职责 | 依赖 |
|---|---|---|
| `agentcore/memory` | 类型、接口、`Governed`（预算、污染、来源、墓碑、事件回调）、`Router`（按范围分派后端） | 标准库 |
| `agentcore/memory/files` | 每个范围一个目录：`MEMORY.md` 索引 + `<id>.md`（frontmatter 记 kind/updated）；搜索为逐文件匹配 | 标准库 |
| `agentcore/extproc` | exec 协议信封、调用、超时、凭据环境变量剥离（从 `tools/mcp/env.go` 上移，MCP 改用它）；`memory` 的 `exec` 后端建在其上 | 标准库 |
| `agentcore/redact` | `RedactSecretValues` 下沉于此，另加隐形字符、HTML 注释、伪装标签清理 | 标准库 |
| `internal/adapters/gofermem` | 记忆后端：`project` 范围映射到 `gofer memory`（映射见“数据”）；只读写带 `suag` 标签的条目；没有来源与墓碑能力，由治理层补 | os/exec |
| `internal/personal/store` | `entries_fts` 写入、删除同步与回填，`LedgerSearcher` 实现，`memory_meta` 旁表 | sqlite |

## 关键流程

**会话开始（投影的冻结快照）**：新建会话时，按 `user` → `project:<key>` 取每个范围的 `Index`，渲染成“记忆索引”一节，连同 hash 存进 session meta（`memory_index_content` / `memory_index_hash`，与规则的 `personal_rules_*` 同一机制）。系统提示词每回合从 session meta 取这一节，放在规则之后，并附固定说明（四类条目、不记清单、`memory` 工具用法）。与规则不同，记忆索引**不按回合重读**：会话内的写入只通过工具结果告诉模型，提示词不变，缓存不破；只在压缩时（缓存反正要重建）和以后的分段断点刷新。这样 chat 长进程、`run`、ACP 每回合重建 runtime 三种场景的语义一致。快照 hash 写进 `turn_context` 事件。

**污染置位**：观测层（`personal/observe.go` 记录工具事件处）在工具结果返回时检查工具是否为外部来源，是则在 session meta 写 `tainted_at`（只写一次，不清除）。外部来源由工具元数据声明（`tools.Meta` 增加 `External`），首批为：`web_fetch`、`web_search`、全部 MCP 工具、`session_search`、`recall`、`memory` 的 get/search（gofer、exec 后端的读取结果）。`shell_exec` 默认不算（它在编程会话里几乎总被调用，算进去会让关口失去意义），可用 `memory.taint_tools` 追加。`Governed` 通过注入的 `Policy.Tainted` 读取该标记。

**写入记忆**：

```text
模型调用 memory(op=add, scope=project, kind=feedback, title, body)
  → Governed：
      1. 污染：Policy.Tainted 为真 → 按 memory.write_tainted 处理（默认 ask：chat 询问，可答 a 本会话不再问；
         ACP 走权限请求；run 非交互则拒绝并提示）。放在最前，后面任何步骤都不能绕过
      2. 清理：Policy.Sanitize（redact.Clean：隐形字符、HTML 注释、伪装标签、密钥）
      3. 墓碑：与已删除条目标题/正文指纹相同 → 返回错误并说明
      4. 预算：该范围索引超过上限 → 返回错误，要求先合并或删除旧条目（不静默截断）
  → Router → 后端（files / gofer / exec）
  → 记录 memory_write 事件（范围、id、来源、是否污染）；回合结束 chat 显示“已记住：…（:memory 查看）”
```

**工具输出（输出治理）**：

```text
工具返回原文（内置工具的上限改为“原文上限”，默认 1 MB；MCP 同样受此上限）
  → tools.Output 包装层：
      ≤ 内联阈值（tools.inline_output_bytes，默认 8000）→ 原样返回
      > 阈值：
        工具声明可重读（file_read、file_list 等，Meta.Rereadable）→ 头尾片段 + “用 offset/limit 继续读”，不外置
        否则 → redact 后交给 Spill 写入 sessions/<sid>/outputs/<turn>-<call>.txt，返回：
          头 N + 尾 N 字符（各 2000）
          [输出 48213 字节 / 912 行已保存为 ref out:<turn>-<call>；recall(ref, offset, limit) 分段读，session_search(query, ref) 在其中搜]
  → 进上下文与 entries 的都只是上面这段；entries 的消息 metadata 记 {ref, bytes, lines, sha256}
```

`Spill` 是 agentcore 里的小接口，宿主不提供时退化为现有的头尾截断（嵌入 agentcore 的程序行为不变）。外置文件不进 FTS，避免 DB 膨胀；`session_search(query, ref)` 对单个文件做逐行扫描，`recall(ref, offset, limit)` 按行窗口读。读回内容同样过 `redact.Clean` 并使会话污染。

**检索历史**：`session_search(query, scope=current|workspace|all, limit)` → 查询按空白切词，每个词转义成 FTS5 短语；全部词不少于 3 个 rune 时走 `MATCH`（trigram），否则整体退回 `LIKE` → 命中列表，每条带 `ref` 与片段；`recall(ref, offset, limit)` 取回原文窗口（D18）。片段与原文返回前都过 `redact.Clean`；`scope` 默认 `workspace`（同一工作区的会话），`all` 才跨项目。两个工具只读、零模型调用，结果会让会话污染（旧的外部内容可能被取回）。

**外部后端（exec 协议 v1）**：

```text
stdin  {"v":1,"capability":"memory","op":"search","args":{…}}
stdout {"ok":true,"result":{…}}   或   {"ok":false,"error":"…"}
```

一次调用一个进程，超时默认 10 秒，环境变量按 MCP 子进程规则剥离凭据；退出码非 0 或输出不是合法 JSON 视为失败；结果按不可信内容走治理层清理。

**评测**：`go run ./scripts/eval [--model m] [--task glob]` → 为每个任务复制 fixture 到 `tmp/eval/<run>/<task>/`，用隔离的 `SUAG_HOME` 运行 `suag run --session-id eval-…`，再执行任务的检查命令判定成败，读取 `suag stats --session … --json` 汇总，输出表格与 `results.jsonl`。

## 数据

- 迁移 `migrations/<ts>-add-ledger-fts-memory-meta.sql`（GR105 命名）：
  - `entries_fts`：FTS5 表，列 `session_id UNINDEXED, seq UNINDEXED, role UNINDEXED, text`，`tokenize='trigram'`；**行的 rowid 等于对应 `entries` 行的 rowid**；与 `entries` 在同一事务写入；文本取用户/助手消息正文与工具结果（已截断的存储内容）。
  - 删除同步：`AFTER DELETE ON entries` 触发器删除同 rowid 的 FTS 行（外键级联删除会触发它；以“删会话 / gc 后搜不到、ref 不悬空”的测试守住）。
  - `memory_meta`：`backend, scope, entry_id, session_id, turn_id, source, tainted, deleted_at, fingerprint, created_at, updated_at`；墓碑即 `deleted_at` 非空的行。
- 回填：按 rowid 幂等——只插入 `NOT EXISTS (SELECT 1 FROM entries_fts WHERE rowid = e.rowid)` 的行，每批 500 行一个事务；在打开 store 时同步执行，单次最多处理固定批数，剩余留给下次打开。不用后台 goroutine，也不需要完成标记；新写入与回填交叠时由 rowid 去重。
- gofer 映射：条目 id → gofer key（`suag-<kind>-<slug>`）；标题 → `--summary`；正文 → 内容；kind 统一为 `note`，四类写进 `--tag suag,<kind>`；`--source suag:<session>`。后端只列出、修改、删除带 `suag` 标签的条目，预算与“先删旧条目”只作用于这些条目，不碰其他 agent 的共享记忆。注意 note 会进入所有 agent 的开场上下文——这正是“项目共享记忆”的用意，写进工具说明。
- 会话目录：`~/.suag/sessions/<session_id>/outputs/<turn_id>-<call_id>.txt`（UTF-8，已脱敏，单文件以原文上限为界）；目录权限沿用运行目录的保护（Windows 受保护 DACL）；`suag gc` 删除会话时同时删除目录，`suag export` 默认只导出 ref 与元信息，`--with-outputs` 时附带文件。
- 记忆文件（内置后端）：用户范围 `~/.suag/memory/user/`；项目范围按顺序选择：配置 `memory.project.backend` 显式指定 > 仓库根存在 `MEMORY.md`（索引即该文件，条目文件放 `.suag/memory/<id>.md`，见 Q5）> 工作区有 `.gofer/tracker`（gofer 后端）> `~/.suag/projects/<key>/memory/`（`<key>` 为 git 根路径的可读 slug）。仓库根 `MEMORY.md` 只在已存在时启用，suag 不自动创建；索引里手写的、不符合条目格式的行原样保留、只读。
- 价格配置：`providers.<id>.models.<model>.price: {input, output, cache_read, cache_write}`（美元 / 百万 token）；provider 返回 `cost_usd` 时以返回值为准。

## 安全、数据、运维与回滚

- 记忆和外部后端的输出一律视为不可信输入：清理隐形字符与伪装标签、去密钥、污染标记；外部内容不能在没有确认的情况下进入持久记忆（D7）。
- exec 后端只来自配置文件声明的路径；不在模型可写目录里自动发现可执行文件。
- 迁移只新增表，回滚即删表；记忆文件是纯文本，删除目录即清空；gofer 后端写入的条目在 gofer 侧可用 `gofer memory` 管理。
- 预算超限一律报错不截断；`suag stats` 增加记忆写入、拒绝、污染计数。
- 测试不得触碰真实 `~/.suag`（沿用现有约定），评测运行使用隔离的 `SUAG_HOME`。

## 决策

| ID | 决策 | 出处 / 理由 |
|---|---|---|
| D1 | 采用总纲四原则（状态与投影、接口化、治理与后端无关、开放优先） | 用户 2026-10-10 两次表述；两篇参考文章的共同结论 |
| D2 | 只有已有两个以上真实实现的能力才在 agentcore 建通用接口包：memory（files、gofer、exec）建 `agentcore/memory`；底账检索只有 SQLite 一个实现，只在消费方（native 工具）声明窄接口，出现第二个实现再上升 | SR1405.1：两个已验证用例才抽象；`api.Storage` 不能列会话、不能按 ref 定位，线性扫描撑不起第二实现 |
| D3 | 治理做成包装层（`memory.Governed`），与现有 Visibility → Approval → Registry 同构 | 换后端不丢治理 |
| D4 | 文件是记忆的事实源，SQLite 旁表管来源与墓碑 | 行业共识“用户看文件、系统管数据库”；记忆归用户所有 |
| D5 | 四类条目 + 不记清单（能从代码或 git 推出的、规则文件里已有的、会话内临时信息、密钥）；工具操作为 add/replace/remove/get/search | Claude Code 与 ZCode 一致的分类；不记清单写进工具说明 |
| D6 | 索引快照按会话冻结在 session meta，只在压缩（以后加分段断点）时刷新；每个范围上限 200 行或 16 KB，超限报错 | 保住缓存（Hermes）；ACP 每回合重建 runtime，进程不能作冻结单位；预算报错不截断（Claude Code） |
| D7 | 写入默认直接生效并在回合结束告知；会话被污染后写入需确认（`memory.write: auto`、`memory.write_tainted: ask`，均可配置）；污染按会话粘住、由工具元数据 `External` 声明来源、关口在治理层最前 | 开放优先；外部内容是记忆最主要的注入面；按回合判断会被跨回合绕过 |
| D8 | 底账检索用 FTS5 trigram，零模型调用；查询转义、按 rune 长度退回 LIKE；结果脱敏；默认只搜本工作区；不做向量检索 | 中文可搜；“先 grep 和全文索引，够用就停” |
| D9 | 每回合记录 `turn_context` 事件（模型、模式、工具集摘要、规则与记忆快照摘要） | Codex rollout 的教训：能回答“当时它看到了什么” |
| D10 | 成本由价格配置计算，provider 返回值优先；评测用脚本经 CLI 驱动，不新增用户子命令 | 先能算账，再谈上下文取舍 |
| D11 | 外部后端统一走 exec 协议 v1；gofer 后端在应用层，agentcore 不认识 gofer | 跨语言、最简单；可替换且不强绑定 |
| D12 | 安全底线固定为四条：不泄露凭据（脱敏、子进程剥离凭据）；外部内容不经确认不进入持久状态；对外与不可逆动作可被审批拦截；一切可审计（事件、底账）。底线之外的限制（只读默认、路径限定、逐次确认）在后续“权限与信任档位”设计中改为可配置档位 | 用户“基本安全前提下尽量不限制”；本期只落地前两条中与记忆相关的部分 |
| D13 | `api.Memory` 改名为 `api.History`（含 `Config.Memory`、`MemoryError`、`ErrMemoryStateConflict`、doc 与 4 篇文档），不做兼容；`NewMemoryStorage` 是无关的内存存储，保持原名 | 把“memory”留给长期记忆；v0 无需兼容；`internal/` 不受影响 |
| D14 | 脱敏与凭据剥离下沉到 `pkg/agentcore`（`redact`、`extproc`），personal 与 MCP 改用下沉后的实现 | 治理层与 exec 后端都需要它们，又不能依赖 `internal/*` |
| D15 | 能力清单命名为 `suag manifest` / 工具 `suag_manifest` | `internal/wiring/capabilities.go` 已占用“capabilities”一词 |
| D16 | 输出治理做成 `tools.Output` 包装层，对所有工具（含 MCP）生效；内联阈值默认 8000 字节，超过则头尾各 2000 字符 + ref；可重读的工具（`file_read`、`file_list`）只截断不外置 | 用户 2026-10-11；context-mode 的结论（大输出灌满上下文）；MCP 目前无上限 |
| D17 | 大原文存会话目录文件，DB 只存头尾片段与元信息（ref、字节数、行数、sha256）；外置文件不进 FTS | 用户“原始输出的大内容不进入 db”；DB 体积大部分来自工具结果 |
| D18 | `recall` 统一按 ref 读：`<session>/<seq>` 读底账消息窗口，`out:<turn>-<call>` 按行窗口读外置文件；`session_search` 加 `ref` 参数在单个外置文件内逐行搜索 | 一套取回方式，模型只需记一个 ref |

## 待确认事项

| ID | 问题 | 结论 / 推荐 |
|---|---|---|
| Q1 | 项目记忆的内置文件放在哪里 | 已决（按推荐 + 用户补充）：仓库根有 `MEMORY.md` 时用仓库，否则 `~/.suag/projects/<key>/memory/`；优先级见“数据” |
| Q2 | 工作区有 `.gofer/tracker` 时，项目范围是否自动用 gofer 后端 | 已决：自动探测，可用 `memory.project.backend` 覆盖；优先级低于仓库根 `MEMORY.md` |
| Q3 | suag 是否可以向 gofer memory 写入 | 已决：可以，带 `--summary`，记 `memory_write` 事件 |
| Q4 | 评测的真实模型 | 已决：`opencode-go/deepseek-v4.1-flash`，另跑假模型做离线回归 |
| Q5 | 仓库模式下条目文件放哪 | 已决（按推荐）： `.suag/memory/<id>.md`（根目录只多一个 `MEMORY.md`）；备选：条目全部内联在 `MEMORY.md` 里（一个文件，适合条目少的项目） |
| Q6 | 内联阈值 8000 字节、头尾各 2000 字符是否合适 | 已决（按推荐）：先用这组默认值，W0 评测加“每回合工具输出字节数”指标后再调 |

## 结论与人工计划 Gate

本设计确立总纲与第一期（W0–W4）。批准后写实施计划；计划同样需要批准后才实施。批准语句示例：“批准设计 0.3，Q5–Q6 按推荐”。
