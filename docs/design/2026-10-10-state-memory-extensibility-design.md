<!-- template_id: design; template_version: 1.1.1 -->
# 状态、记忆与可扩展核心（总纲 + 第一期） 设计

> 状态：Draft 0.1 / 待人工计划批准

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-10 | Claude | 初稿 |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

## 背景与目标

思考模式 RIGOROUS；核心目标：把 suag 的上下文、记忆、状态、工具四块核心能力统一到一份总纲下，并完成第一期——能量化（评测与成本）、能回查（底账检索）、能记住（精选记忆 v1），三者都以“内核定接口、内置实现最强、后端可替换、治理与后端无关”的方式落地，为自我扩展留出口子。scope freeze：W0–W4（见“范围”）；review budget：一轮合并轴评审（低暴露度，SR1409：个人项目、未投产、数据可重建、无出机器动作）；停止条件：评审无 CORE_BLOCKING 且用户批准。

用户原话（2026-10-10）：

- “核心能力定接口，有很强的内置，同时可替换，为自我更新/扩展能力留下口子”
- “在基本安全的前提下尽量不限制它的能力和扩展/成长性（我用 codex 感觉限制很多，麻烦），内置能力已经是极简依赖下的最强（减少依赖，但是能通过依赖外部工具让他更强）”
- 项目记忆可以走 gofer memory，但“不是强绑定”。

用户可见结果：

1. `go run ./scripts/eval` 跑一组固定任务，输出每个任务的成败、调用次数、token、缓存命中率和成本；配置模型价格后，`suag stats` / `suag trace` 的成本列不再是 0。
2. 模型可以用 `session_search` 检索所有历史会话（中文可搜），用 `recall` 按引用取回原文；每个回合在 trace 中能看到“当时用的模型、模式、工具集、规则与记忆快照摘要”。
3. 模型可以用 `memory` 工具记住和修改用户、反馈、项目、参考四类条目；新会话自动带上记忆索引；你能直接打开 Markdown 文件查看、编辑、删除；项目记忆可配置为走 gofer memory，不配置时由内置文件完整提供。
4. `suag capabilities` 列出每项核心能力当前由哪个后端提供、能否替换；新后端可以用任意语言写成可执行文件，通过配置接入，无需重新编译 suag。

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
- **污染（taint）**：本回合模型读过外部内容（`web_fetch`、`web_search`、MCP 结果），此时写入记忆需要额外确认。
- **墓碑（tombstone）**：被删除条目的记录，防止同一事实被后续写入悄悄复活。
- **exec 协议**：外部后端的统一调用方式：一次调用起一个进程，stdin 一行 JSON 请求，stdout 一行 JSON 响应。
- **能力清单**：当前装了哪些能力、各由哪个后端提供、是否可替换的机器可读描述。

## 范围与非目标

范围（仓库 `inhere-tools/suag`）：

- **W0 评测与成本**：模型价格配置与成本计算；`scripts/eval`（任务定义、运行器、汇总）。
- **W1 底账检索**：FTS5 全文索引与回填；`recall.Ledger` 接口；`session_search`、`recall` 两个只读工具；每回合 `turn_context` 事件。
- **W2 记忆 v1**：`memory` 包（类型、接口、治理层、路由）；内置 `files` 后端；gofer 后端（应用层）；`memory` 工具；索引快照注入提示词；`api.Memory` 改名。
- **W3 扩展面**：exec 协议 v1（首个使用者是记忆的 `exec` 后端）；`suag capabilities` 与只读工具 `capabilities`。
- **W4 文档**：README、`pkg/agentcore/README.md`、配置样例、CHANGELOG。

非目标（各自另起设计）：会话分段与“交接代替压缩”；只读子 agent；工具稳健性（编辑前检查、多处修改、超长输出存文件）；文件检查点与撤销；离线整理（“做梦”）与前瞻记忆；people（suag-frgt）；权限模型重整（安全底线 + 信任档位，见 D12）；向量检索；OTel 导出；配置补丁提案流程。

## 已确认事实与规范

- 用量（主机 `suag stats --since 7d`，2026-10-10）：`opencode-go/deepseek-v4.1-flash` 203 次调用，缓存命中 89.8%，压缩 0 次；`skill` 是调用最多的工具（79 次）；成本列为 0。
- 成本只来自 provider 返回的 `cost_usd`（`internal/personal/observe.go:100`），没有价格表。
- 底账表：`sessions(id, cwd, …)`、`turns`、`entries(session_id, seq, turn_id, type, json, created_at)`、`events(kind, name, tokens…, attrs_json)`（`internal/personal/store/migrations/20261008-230000-create-core-store.sql`）。
- `modernc.org/sqlite` v1.60.1 编译时开启了 `SQLITE_ENABLE_FTS5`。
- 规则加载：用户与项目规则按权威顺序读取，include 只展开一层（`internal/personal/rules.go:19`）；系统提示词每个进程构建一次（`internal/wiring/prompt.go`）。
- `api.Memory`（Hydrate/ObserveMessages/BuildContext）只被 runtime 的默认实现使用（`pkg/agentcore/runtime.go:20,59`），应用层没有实现；语义是“组装历史”，和长期记忆无关。
- hooks 已有“每次调用起一个进程、stdin 一行 JSON”的协议（`internal/hooks/hooks.go:3`）；MCP 子进程会剥离凭据环境变量。
- 工具分层为 observe → Visibility → Approval → Registry；工具通过 `tools.Describer` 声明类别与只读（`pkg/agentcore/tools/meta.go:30`）；`layering_test.go` 禁止 `pkg/agentcore` 依赖 `internal/*`。
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
        wiring: 组装 Router、治理层、工具；注入索引快照
        adapters/gofermem: 记忆的 gofer 后端（调用 `gofer memory`）
        personal/store: recall.Ledger 的 SQLite 实现（FTS5）、memory_meta 旁表
        cli: capabilities 子命令；scripts/eval 只经 CLI 交互
          │
能力层  agentcore/memory        类型、Store 接口、可选接口、Governed、Router
        agentcore/memory/files  内置后端：MEMORY.md 索引 + 一事一文件
        agentcore/recall        Ledger 接口、线性扫描参考实现（测试与嵌入用）
        agentcore/extproc       exec 协议 v1：请求/响应信封、超时、环境清理
        agentcore/tools/native  memory、session_search、recall、capabilities 工具
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
```

```go
package recall

type Ledger interface {
	Search(ctx context.Context, q Query) ([]Hit, error) // Hit 带 Ref{Session, Seq} 与片段
	Fetch(ctx context.Context, ref Ref, before, after int) ([]api.Message, error)
}
```

包与职责：

| 包 | 职责 | 依赖 |
|---|---|---|
| `agentcore/memory` | 类型、接口、`Governed`（预算、污染、来源、墓碑、事件回调）、`Router`（按范围分派后端） | 标准库 |
| `agentcore/memory/files` | 每个范围一个目录：`MEMORY.md` 索引 + `<id>.md`（frontmatter 记 kind/updated）；搜索为逐文件匹配 | 标准库 |
| `agentcore/recall` | 接口；基于 `api.Storage` 的线性扫描实现（第二个实现，供嵌入与测试） | api |
| `agentcore/extproc` | exec 协议信封、调用、超时、凭据环境变量清理；`memory` 的 `exec` 后端建在其上 | 标准库 |
| `internal/adapters/gofermem` | 记忆后端：`project` 范围映射到 `gofer memory ls/show/set/rm`；没有来源与墓碑能力，由治理层补 | extproc 或 os/exec |
| `internal/personal/store` | `entries_fts` 写入与回填、`Ledger` 实现、`memory_meta` 旁表 | sqlite |

## 关键流程

**会话开始（投影的冻结快照）**：wiring 构建系统提示词时，按 `user` → `project:<key>` 的顺序取每个范围的 `Index`，在规则之后注入“记忆索引”一节（每条：kind、标题、id），并附固定说明（四类条目、不记清单、怎么用 `memory` 工具）。本进程内不再重取；本进程写入的条目在下一个进程（以后是下一段）可见，当前进程的写入结果由工具结果告诉模型。快照摘要写进 `turn_context` 事件。

**写入记忆**：

```text
模型调用 memory(op=add, scope=project, kind=feedback, title, body)
  → Governed：
      1. 清理：去掉隐形字符、HTML 注释与伪装标签；按 RedactSecretValues 去掉密钥
      2. 预算：该范围索引超过上限 → 返回错误，要求先合并或删除旧条目（不静默截断）
      3. 墓碑：与已删除条目标题/正文高度相同 → 返回错误并说明
      4. 污染：本回合读过外部内容 → 按 memory.write_tainted 处理（默认 ask；非交互时拒绝并提示）
  → Router → 后端（files / gofer / exec）
  → 记录 memory_write 事件（范围、id、来源、是否污染）；回合结束 chat 显示“已记住：…（:memory 查看）”
```

**检索历史**：`session_search(query, scope=current|all, limit)` → FTS5（trigram 分词，可搜中文；少于 3 个字符的查询退回 LIKE）→ 命中列表，每条带 `ref` 与片段；`recall(ref, before, after)` 取回原文窗口。两个工具只读、零模型调用。

**外部后端（exec 协议 v1）**：

```text
stdin  {"v":1,"capability":"memory","op":"search","args":{…}}
stdout {"ok":true,"result":{…}}   或   {"ok":false,"error":"…"}
```

一次调用一个进程，超时默认 10 秒，环境变量按 MCP 子进程规则剥离凭据；退出码非 0 或输出不是合法 JSON 视为失败；结果按不可信内容走治理层清理。

**评测**：`go run ./scripts/eval [--model m] [--task glob]` → 为每个任务复制 fixture 到 `tmp/eval/<run>/<task>/`，用隔离的 `SUAG_HOME` 运行 `suag run --session-id eval-…`，再执行任务的检查命令判定成败，读取 `suag stats --session … --json` 汇总，输出表格与 `results.jsonl`。

## 数据

- 迁移 `migrations/<ts>-add-ledger-fts-memory-meta.sql`（GR105 命名）：
  - `entries_fts`：FTS5 表，列 `session_id UNINDEXED, seq UNINDEXED, role UNINDEXED, text`，`tokenize='trigram'`；与 `entries` 在同一事务写入；文本取用户/助手消息正文与工具结果（已截断的存储内容）。
  - `memory_meta`：`backend, scope, entry_id, session_id, turn_id, source, tainted, deleted_at, fingerprint, created_at, updated_at`；墓碑即 `deleted_at` 非空的行。
- 回填：首次打开新库时在后台把已有 `entries` 写入 `entries_fts`，用 `sessions.meta_json` 之外的单独标记表记录完成状态，可重入。
- 记忆文件（内置后端）：默认 `~/.suag/memory/user/` 与 `~/.suag/projects/<key>/memory/`（Q1），`<key>` 为工作区 git 根路径的可读 slug。
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
| D2 | 每个新接口上线时至少有两个实现：memory 为 files + gofer（另有 exec），recall 为 SQLite + 线性扫描 | SR1405.1：两个已验证用例才抽象；避免为想象中的后端设计 |
| D3 | 治理做成包装层（`memory.Governed`），与现有 Visibility → Approval → Registry 同构 | 换后端不丢治理 |
| D4 | 文件是记忆的事实源，SQLite 旁表管来源与墓碑 | 行业共识“用户看文件、系统管数据库”；记忆归用户所有 |
| D5 | 四类条目 + 不记清单（能从代码或 git 推出的、规则文件里已有的、会话内临时信息、密钥）；工具操作为 add/replace/remove/get/search | Claude Code 与 ZCode 一致的分类；不记清单写进工具说明 |
| D6 | 索引在进程开始时冻结注入；每个范围上限 200 行或 16 KB，超限报错 | 保住缓存（Hermes）；预算报错不截断（Claude Code） |
| D7 | 写入默认直接生效并在回合结束告知；仅本回合被污染时需要确认（`memory.write: auto`、`memory.write_tainted: ask`，均可配置） | 开放优先；外部内容是记忆最主要的注入面 |
| D8 | 底账检索用 FTS5 trigram，零模型调用；不做向量检索 | 中文可搜；“先 grep 和全文索引，够用就停” |
| D9 | 每回合记录 `turn_context` 事件（模型、模式、工具集摘要、规则与记忆快照摘要） | Codex rollout 的教训：能回答“当时它看到了什么” |
| D10 | 成本由价格配置计算，provider 返回值优先；评测用脚本经 CLI 驱动，不新增用户子命令 | 先能算账，再谈上下文取舍 |
| D11 | 外部后端统一走 exec 协议 v1；gofer 后端在应用层，agentcore 不认识 gofer | 跨语言、最简单；可替换且不强绑定 |
| D12 | 安全底线固定为四条：不泄露凭据（脱敏、子进程剥离凭据）；外部内容不经确认不进入持久状态；对外与不可逆动作可被审批拦截；一切可审计（事件、底账）。底线之外的限制（只读默认、路径限定、逐次确认）在后续“权限与信任档位”设计中改为可配置档位 | 用户“基本安全前提下尽量不限制”；本期只落地前两条中与记忆相关的部分 |
| D13 | `api.Memory` 改名为 `api.History`（含配置字段与错误名），不做兼容 | 把“memory”留给长期记忆；v0 无需兼容 |

## 待确认事项

| ID | 问题 | 推荐 |
|---|---|---|
| Q1 | 项目记忆的内置文件放在哪里 | `~/.suag/projects/<key>/memory/`：不污染仓库；想进 git 的约定写 AGENTS.md |
| Q2 | 工作区有 `.gofer/tracker` 时，项目范围是否自动用 gofer 后端 | 自动探测，可用 `memory.project.backend` 覆盖；没有 gofer 时回到内置文件 |
| Q3 | suag 是否可以向 gofer memory 写入（会让对应仓库的 tracker 变脏） | 可以，写入时带 `--summary`，记 `memory_write` 事件；提交仍由仓库的功能提交带走 |
| Q4 | 评测的真实模型 | `opencode-go/deepseek-v4.1-flash`（当前主力），另跑一次假模型保证可离线回归 |

## 结论与人工计划 Gate

本设计确立总纲与第一期（W0–W4）。批准后写实施计划；计划同样需要批准后才实施。批准语句示例：“批准设计 0.1，Q1–Q4 按推荐”。
