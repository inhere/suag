# Host Learning Retrieval v0

> 状态：Current
> 更新时间：2026-04-20

本文档说明当前 Host v0 已经落地的最小记忆检索基线。

当前目标不是做“聪明的 memory system”，而是先让已经人工采纳并写入正式文件的记忆，能在后续 internal runner 回合里真正被用起来。

---

## 1. 当前能力

当前 Host internal runner 已具备：

- 每次新 `Run()` 开始时读取 formal memory 文件
- 读取来源固定为：
  - `host.memory.root/memory/MEMORY.md`
  - `host.memory.root/memory/USER.md`
- 读取后会转成前置 `system` messages 注入运行上下文
- provider 最终能看到：
  - `[Host MEMORY]`
  - `[Host USER]`
- 当前 run metadata / snapshot 还会保留一份最小 recall 观测信息：
  - `memory_recall.enabled`
  - `memory_recall.entry_count`
  - `memory_recall.sources`
  - 每个 source 当前包含 `name`、`entry_count`、简短 `preview`

当前明确不做：

- 读取 `*.draft.md`
- SQLite / FTS5 检索
- query-aware retrieval
- 运行中动态刷新
- ACP runner 远端 retrieval 对齐

---

## 2. 注入语义

当前 retrieval 只影响 prompt context，不改 session transcript 语义。

也就是说：

- formal memory 会进入 `GetContext()`
- formal memory 不会被写回会话 history snapshot
- formal memory 不是“这一轮新产生的对话消息”
- 每次 turn 都会重新从文件读取一次，而不是沿用上轮注入结果
- recall 观测 metadata 会写入 Host run metadata，供 gateway / AG-UI / Web 调试展示

这样做的原因是：

- 避免 recalled memory 被重复持久化，导致 history 越跑越脏
- 保持 retrieval 是“外部上下文注入”，不是“转录结果”
- 后续替换成 FTS5 / 更复杂 recall 时，不需要再改 transcript contract

---

## 3. 文件内容处理规则

当前解析规则保持极简：

- 文件不存在：静默跳过
- 文件读取失败：写 warn 日志，但不打断主链
- 空内容：不注入
- 解析时会过滤：
  - 一级标题
  - `Updated:` 行
  - 空行
  - `_No applied memory entries yet._`
- 当前还会按文件产出：
  - entry_count
  - 最多前 2 条 preview，用于 run detail 观测

其余内容会按原始顺序拼接到 `system` message 中。

---

## 4. 与 recap / memory files 的关系

当前 learning 主链已经分成两段：

1. 写入阶段
   - turn 终态 -> `recaps` / `memory_candidates`
   - 人工 accept/reject
   - 刷新 `MEMORY.md` / `USER.md`
2. 使用阶段
   - intrunner 在后续 turn 启动时读取 `MEMORY.md` / `USER.md`
   - 把 formal memory 注入运行上下文

这意味着当前 v0 已经具备最小闭环：

- 会后总结
- 人工采纳
- 正式沉淀
- 后续回合使用

---

## 5. 当前边界提醒

当前 retrieval 仍然只是 baseline：

- 所有 formal memory 都会整文件注入
- 不会按 query 做筛选
- 不会做 relevance 排序
- 不会区分 workspace / project 范围
- 当前 `memory_recall.preview` 只用于观测，不应被误解为真实注入内容的完整镜像

所以它适合作为 v0 自用阶段的最小能力，但不是最终长期方案。

后续如果继续演进，建议优先考虑：

1. SQLite + FTS5 检索
2. query-aware recall
3. memory scope 与 workspace/project 绑定
4. ACP runner 与 intrunner 的 retrieval 语义对齐
