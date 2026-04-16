# Host Recap / Memory Hooks v0

> 状态：Current
> 更新时间：2026-04-16

本文档说明当前 Host v0 在 `internal/host/session.Service` 中新增的 recap hook 骨架。

它的目标不是现在就定义完整记忆系统，而是先把“会话终态后如何挂 recap / memory / review sink”这件事收成一个稳定入口，避免后面接 `MEMORY.md`、`USER.md`、SQLite FTS5 时回头改主状态机。

---

## 1. 当前能力边界

当前 Host recap hook 已具备：

- turn 终态后的统一触发入口
- completed / failed / canceled 三类 phase 区分
- best-effort 调用约束
- 默认 structured recap 落库
  - 当前默认通过 `StructuredRecapHook` 写入 `recaps` 表
- 默认 memory candidate 落库
  - 当前默认通过 `MemoryCandidateHook` 写入 `memory_candidates` 表

当前还不具备：

- 自动写入 `MEMORY.md`
- 自动写入 `USER.md`
- 自动触发 review agent 或后台学习循环

也就是说，这一层现在只是 hook，不是完整产品能力。

---

## 2. 触发时机

当前 `session.Service` 只会在下面三种终态后触发 recap hook：

- run completed
- run failed
- run canceled

当前明确不会触发的场景：

- `INPUT_REQUIRED`
- `AUTH_REQUIRED`
- `EXTERNAL_RUNNING`
- 其他 waiting 非终态

这保证：

- recap 只针对一次真正结束的轮次
- 等待态不会误写总结
- 后续继续执行时不会出现重复 recap

---

## 3. 输入模型

当前 hook 接收的最小输入为：

- `Session`
- `TurnID`
- `RunID`
- `Phase`
  - `completed`
  - `failed`
  - `canceled`
- `Summary`
- `Failure`

当前约定：

- `completed` 时优先看 `Summary`
- `failed` / `canceled` 时优先看 `Failure`
- hook 自己决定后续是否生成更丰富的 recap 文本或结构化记录
- 当前默认 structured sink 会写入：
  - `kind = turn_recap`
  - `target = assistant | host`
  - `content = summary 或 failure`
  - `metadata.phase`
- 当前默认 memory candidate sink 会写入：
  - `target = MEMORY | USER`
  - `status = draft`
  - `source_recap_id = recap_id`
  - `kind = turn_learning | user_preference`
  - `metadata.phase`

---

## 4. 当前落库路径

当前默认 server wiring 已接入两层 sink：

1. `recaps`
   - 保留 turn 终态 recap 的结构化事实
2. `memory_candidates`
   - 保留后续可能写入 `MEMORY.md` / `USER.md` 的候选草案
3. `MEMORY.draft.md` / `USER.draft.md`
   - 当前会将同一 session 的 draft/rendered 候选聚合后渲染成可人工检查的草稿文件
   - 默认会按 `kind + content` 去重，并过滤 `rejected` / `applied`

这里的设计重点是：

- 先把候选写入单独表
- 先渲染 draft 文件，不直接改正式记忆文件
- draft 文件当前是 session 级聚合视图，不是单 turn 原样转储
- 后续可以做人审、去重、合并、拒绝或二次润色

也就是说，当前 v0 已经从“只有 hook 骨架”前进到“候选落库 + draft 渲染”，但仍未进入正式记忆写入阶段。

---

## 5. 失败策略

当前 hook 是 best-effort：

- hook 报错不会中断主 turn 结果
- hook 报错不会回滚 session / turn / run 生命周期
- hook 报错不会改变 WS / REST 对外可见状态

这样设计是为了让后续 memory / review sink 可以渐进接入，而不会把 Host 主链变成脆弱依赖图。

---

## 6. 后续接入建议

下一步如果要继续增强，可以优先接下面几类 sink：

1. recap logger
   - 先把 recap 输入落到日志或调试输出，便于人工观察
2. structured recap store
   - 已完成最小版本，当前默认写入 `recaps` 表
3. memory candidate sink
   - 已完成最小版本，当前默认写入 `memory_candidates` 表
4. memory draft renderer
   - 已完成最小版本，当前默认渲染 `MEMORY.draft.md` / `USER.draft.md`
4. background review sink
   - 把复杂任务结果送到后台 review / skill 提炼流程

建议顺序是先有“稳定 hook”，再有“候选写入”，最后再考虑“自动学习闭环”。
