# Web + Host 本地联调

> 状态：Current
> 更新时间：2026-04-20

本文档只描述 Axon v0 当前最小可用的本地联调流程。

目标是先把 `Host + Web` 真机链路跑通，方便后续继续补 `Stop`、HITL、多 agent 等交互，而不是一次性引入很重的开发脚本体系。

---

## 1. 当前联调范围

当前本地联调已覆盖：

- 创建 session
- 启动 turn
- 未配置 `host.acp.mode` 时，Host 默认走 internal runner 执行单 agent 回合
- internal runner 当前已经开始消费默认 resolved tool subset；也就是说 Host app 会先通过 capability builder 收口默认工具集合，再传给 intrunner
- app 默认 capability builder 当前已支持按 `mcp.servers` 配置装配 MCP tools；因此未配置 `host.acp.mode` 时，internal runner 暴露的默认 tools 现在可以同时包含 native + configured MCP tools
- internal runner 在返回 waiting 态时不会被错误收尾为 completed，同一 `sessionId` 的下一次 `continue_turn` 会优先尝试复用 suspended runtime
- internal runner 已开始按 `yield reason` 区分等待态；包含 `auth` / `approval` / `authorize` 的 yield 会下发为 `AUTH_REQUIRED`
- ACP runner 当前也已收口到同一 Host contract：远端仍在运行中的 dispatch 会落为 `EXTERNAL_RUNNING` 非终态，而不是错误收尾为 completed
- Web 通过 `/ws` 接收会话快照与增量事件
- 刷新后按需恢复 focused run detail
- 当前 turn 进入 waiting 状态时，通过 `Add Hint` 发送 `continue_turn`
- `continue_turn` 当前仅适用于 `INPUT_REQUIRED`；如果 turn 进入 `AUTH_REQUIRED`，需要通过 HITL 面板响应审批，而不是继续走 Continue / Add Hint

当前还未收口的内容：

- HITL 面板与响应入口
- 一键启动的根级联调脚本
- retrieval / FTS5 / 自动学习闭环
  - 当前已具备 `recaps + memory_candidates + MEMORY.draft.md / USER.draft.md + MEMORY.md / USER.md`
  - intrunner 当前已开始按最小 file-based query-aware lite 方式读取 `MEMORY.md / USER.md` 注入 turn context

---

## 2. 前置条件

需要确保本机已经具备：

- Go 1.24+
- pnpm
- `miglite` CLI

当前默认约定：

- SQLite 文件：`db/axon.db`
- migration 目录：`db/migrations`
- Host internal runner runtime 数据默认目录：`db/agentcore/host/<sessionId>/runs.db`
- 可选 MCP 配置：`config/config.yaml` 中的 `mcp.servers`
- Host 默认监听：`http://127.0.0.1:8180`
- Web 默认监听：`http://127.0.0.1:5173`

---

## 3. 先执行数据库迁移

`internal/host/session` 当前依赖 `miglite` 管理表结构。

如果本地数据库还是空的，需要先初始化 migration 元数据表，再执行迁移。

仓库根目录已经提供 `miglite.yaml`：

```yaml
database:
  driver: sqlite
  dsn: db/axon.db

migrations:
  path: ./db/migrations
  recursive: true
```

执行：

```bash
miglite init
miglite up -y
```

如果已经初始化过，通常只需要再执行：

```bash
miglite up -y
```

---

## 4. 启动 Host

在项目根目录执行：

```bash
go run ./cmd/axon server
```

默认会使用：

- `db/axon.db` 作为 session repository
- `:8180` 作为 gateway 地址

健康检查可用：

```bash
curl http://127.0.0.1:8180/health
```

正常时会返回类似：

```json
{"status":"ok"}
```

---

## 5. 启动 Web

在 `web/app` 目录执行：

```bash
pnpm dev --host 127.0.0.1 --port 5173
```

当前 `web/app/vite.config.ts` 已配置本地代理：

- `/api` -> `http://127.0.0.1:8180`
- `/events` -> `http://127.0.0.1:8180`
- `/ws` -> `ws://127.0.0.1:8180`

如果 Host 地址不是默认值，可以通过环境变量覆盖：

```bash
AXON_HOST_ORIGIN=http://127.0.0.1:8180
```

然后打开：

```text
http://127.0.0.1:5173
```

---

## 6. 建议联调路径

建议按下面顺序快速验证：

1. 打开 Web 工作台
2. 创建一个新 session
3. 输入一条任务消息并启动 turn
4. 观察中栏 turn/runs 与右栏 focused run detail 是否持续更新
5. 如果 turn 进入 `INPUT_REQUIRED`，点击右栏 `Add Hint`
6. 如果 turn 处于运行中，尝试 `Stop`
7. 刷新页面，确认 current session 与 focused run detail 可恢复

### 6.1 最小手测清单

建议每次本地联调至少走完下面一轮：

1. `Create Session`
   预期：
   Web 左侧出现新 session；`GET /api/sessions` 与 `GET /api/sessions/{id}` 能看到同一个 `session_id`

2. `Start Turn`
   预期：
   Web 中栏出现新的 turn/run；右侧 focused run 开始滚动；WS 先收到 `STATE_SNAPSHOT`，随后持续收到该 session 的增量事件

3. `INPUT_REQUIRED -> continue_turn`
   触发方式：
   使用会进入 input wait 的任务，或用当前 internal runner 的等待场景
   预期：
   run `displayState` 进入 `input_required`
   点击 `Add Hint` 后，当前连接会先收到 `command.accepted`
   然后才收到这次 continue 产生的新 run 事件

4. `AUTH_REQUIRED -> HITL respond`
   触发方式：
   使用会触发 approval/auth wait 的任务
   预期：
   run `displayState` 进入 `auth_required`
   `continue_turn` 不应继续该 turn，而应返回 `command.error`
   `Pending HITL` 面板或 `GET /api/hitl` 能看到对应 ticket
   `POST /api/hitl/{id}/respond` 成功后，waiting run 会继续推进并结束

5. `Stop Active Turn`
   触发方式：
   在 turn 仍运行时点击 `Stop`
   预期：
   当前连接先收到 `command.accepted`
   随后收到 `RUN_CANCELED`
   run/turn 刷新后保持 `canceled`，并保留取消原因

6. `Refresh And Recover`
   触发方式：
   在 waiting、failed、canceled 或 completed 之后刷新页面
   预期：
   当前 session 会恢复
   focused run 会恢复到最近 active / focused run
   failed / canceled 的原因文案在刷新后仍可见，而不是只剩空状态

7. `Memory Candidates`
   触发方式：
   完成一轮 turn 后打开右侧 `Memory Candidates`
   预期：
   `GET /api/sessions/{id}/memory-candidates` 能返回候选
   Accept / Reject 后列表会刷新，且草稿文件与正式记忆文件都会重新渲染

### 6.2 推荐辅助请求

联调时如果需要直接确认后端状态，可配合下面几类请求：

```bash
curl http://127.0.0.1:8180/api/sessions
curl http://127.0.0.1:8180/api/sessions/<sessionId>
curl "http://127.0.0.1:8180/api/sessions/<sessionId>/events?after_seq=0"
curl http://127.0.0.1:8180/api/hitl
curl http://127.0.0.1:8180/api/sessions/<sessionId>/memory-candidates
```

---

## 7. 当前已知说明

- `STATE_SNAPSHOT v0` 的消费契约见 `docs/usage/host-state-snapshot-v0.md`
- `/api/sessions/{id}/events?after_seq=` 使用的是 session 级单调递增 `sequence` 游标，不会在新 turn 时重置
- Host 当前已开始下发 `run.status` 非终态事件，可用于在联调时观察 `dispatching`、`external_running`、`input_required`、`auth_required`
- WS command 当前已收口为：
  - 命令成功时再下发 `command.accepted`
  - 命令失败时直接下发 `command.error`
- 当前 Host server 的 session runner 选择规则是：
  - 配置了 `host.acp.mode` 时，走 `internal/host/acpbridge`
- 未配置 `host.acp.mode` 时，默认走 `internal/host/intrunner`，并按 `sessionId` 复用本地上下文历史
- `internal/host/intrunner` 当前已支持从 `RuntimeRequest.Context["resolved_tool_names"]` 消费 turn 级 tool subset；当前 app 侧先注入默认 resolved tools，后续再扩到 session/turn 动态 capability resolution
- Host `session.Service` 现已在首轮 `start_turn` 时把默认 `CapabilitySet` 写入 `session.metadata.capability_set`，之后 `start_turn / continue_turn` 都会优先从这份 session baseline 解析 `resolved_tool_names`
- 当前最小 turn-time dynamic resolution 已接线：如果消息明显是“查看 / 分析 / review / inspect / 不要修改”类只读意图，runtime context 会把本轮 tool subset 自动收敛到 read-only 模式，默认收掉 `file_write`、`shell_exec`
- 当前 recall-aware filtering 也已接线：runtime context builder 会先读取 formal memory recall metadata；如果 recall preview 里出现“查看 / 分析 / inspect / review / 不要直接修改”类线索，也会继续把本轮 tool subset 收敛为 read-only
- 当前最小 skills capability 也已接线：`<workspace>/skills/**/SKILL.md`、`<workspace>/.agents/skills/**/SKILL.md` 与 `config/skills/**/SKILL.md` 会被解析为 capability recipe，并通过 `resolved_skill_names`、`resolved_skill_matches`、`resolved_system_fragments`、`resolved_resource_hints` 与 skill-derived tool hints 影响本轮 runtime context
- 当前 skill resource hints 已支持最小文本资源注入：会从 skill 相邻 `references/`、`assets/` 目录中挑选小型文本文件，作为 runtime-only `system` context 注入 provider request，不写回 session history
- 当前默认 post-turn planner hook 已开始消费 capability signals：terminal run 的 `planner_hint` / `next_action_hint` 会附带 `resolved_tool_mode`、`resolved_tool_names`、`resolved_tool_refs`、`resolved_skill_names`、`resolved_skill_matches`、`resource_hint_count`、`recall_hit`
- app 默认 capability builder 当前会在启动时尝试连接 `mcp.servers` 中配置的 stdio MCP server；如果配置了 MCP 但启动失败，当前采用 fail-fast，Host 进程不会静默带着缺失能力继续启动
- 未显式配置 `agentcore.run_root` 时，Host internal runner 的 runtime 数据默认收纳到 `db/agentcore/host`
- 如果显式配置了 `agentcore.run_root`，Host internal runner 会改为使用 `<agentcore.run_root>/host`
- `session.Service` 当前已具备 post-turn recap hook，默认会把 turn 终态写入 `recaps` 与 `memory_candidates`
- `memory_candidates` 当前是候选中间层，默认还会继续聚合渲染到 `db/agentcore/host/memory/MEMORY.draft.md` 与 `db/agentcore/host/memory/USER.draft.md`
- `accept/reject` 当前也会刷新 `db/agentcore/host/memory/MEMORY.md` 与 `db/agentcore/host/memory/USER.md`
- `*.draft.md` 当前按 session 聚合、去重，并过滤掉不再需要继续处理的候选状态
- `MEMORY.md` / `USER.md` 当前按 session 聚合 `applied` 候选，并按 `kind + content` 去重
- `internal/host/intrunner` 当前会在每次新 turn 启动时重新读取 `MEMORY.md` / `USER.md`，并以最小 query-aware lite 策略选择条目后，再以 `[Host MEMORY]` / `[Host USER]` 的 `system` context 注入 provider request
- recalled formal memory 当前只参与运行上下文，不会被回写进 session history snapshot
- 后端当前已具备最小 `memory candidate` 状态流转能力：`draft -> rendered -> applied/rejected`
- REST 当前已补最小入口：
  - `GET /api/sessions/{id}/memory-candidates`
  - `POST /api/memory-candidates/{id}/accept`
  - `POST /api/memory-candidates/{id}/reject`
- `vite` 代理仅用于本地开发，不影响生产构建输出
- 本地 `db/axon.db`、`db/axon.db-shm`、`db/axon.db-wal` 都属于开发产物，不应作为功能变更提交
