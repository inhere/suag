# Web + Host 本地联调

> 状态：Current
> 更新时间：2026-04-16

本文档只描述 Axon v0 当前最小可用的本地联调流程。

目标是先把 `Host + Web` 真机链路跑通，方便后续继续补 `Stop`、HITL、多 agent 等交互，而不是一次性引入很重的开发脚本体系。

---

## 1. 当前联调范围

当前本地联调已覆盖：

- 创建 session
- 启动 turn
- 未配置 `host.acp.mode` 时，Host 默认走 internal runner 执行单 agent 回合
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
- 终态 recap / memory hook 的真实 sink 接入
  - 当前已具备 `recaps + memory_candidates` 落库，但尚未真正写入 `MEMORY.md / USER.md`

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
- Host 默认监听：`http://127.0.0.1:8180`
- Web 默认监听：`http://127.0.0.1:5173`

---

## 3. 先执行数据库迁移

`internal/host/session` 当前依赖 `miglite` 管理表结构。

如果本地数据库还是空的，需要先初始化 migration 元数据表，再执行迁移。

可参考下面的最小配置文件：

```yaml
driver: sqlite
dsn: db/axon.db
migrations:
  path: ./db/migrations
```

假设配置文件名为 `miglite.yaml`，执行：

```bash
miglite init -c ./miglite.yaml
miglite up -c ./miglite.yaml -y
```

如果已经初始化过，通常只需要再执行：

```bash
miglite up -c ./miglite.yaml -y
```

---

## 4. 启动 Host

在项目根目录执行：

```bash
go run ./cmd/axon server
```

默认会使用：

- `db/axon.db` 作为 session store
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
5. 如果 turn 进入 waiting 状态，点击右栏 `Add Hint`
6. 如果 turn 处于运行中，尝试 `Stop`
7. 刷新页面，确认 current session 与 focused run detail 可恢复

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
- 未显式配置 `agentcore.run_root` 时，Host internal runner 的 runtime 数据默认收纳到 `db/agentcore/host`
- 如果显式配置了 `agentcore.run_root`，Host internal runner 会改为使用 `<agentcore.run_root>/host`
- `session.Service` 当前已具备 post-turn recap hook，默认会把 turn 终态写入 `recaps` 与 `memory_candidates`
- `memory_candidates` 当前只是候选中间层，便于后续去重、审核和渲染 `MEMORY.draft.md / USER.draft.md`
- 当前仍未真正自动写入 `MEMORY.md` / `USER.md`
- `vite` 代理仅用于本地开发，不影响生产构建输出
- 本地 `db/axon.db`、`db/axon.db-shm`、`db/axon.db-wal` 都属于开发产物，不应作为功能变更提交
