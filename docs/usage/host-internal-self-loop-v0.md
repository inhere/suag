# Host Internal Self-Loop v0

> 状态：Current
> 更新时间：2026-04-20
> Version：`v0`

本文档描述当前 Host internal runner 在 `self-loop` 方向的最小语义基线。

目标不是引入重型 orchestration，而是先把下面几件事收口稳定：

- Host 能区分首轮执行与等待后恢复
- waiting / resume / terminal 的状态边界足够清晰
- terminal 后可以挂接统一 follow-up 动作
- 后续 recap、memory、review、skills 沉淀有稳定接入口

---

## 1. 当前边界

当前 `v0` 的 internal self-loop 只解决单 turn 内的持续推进，不解决多 agent 规划编排。

已覆盖：

- 首轮执行
- waiting 后继续执行
- terminal 后统一 follow-up hook
- 轻量 planner hint 的生成与持久化

明确不覆盖：

- DAG / workflow orchestration
- 子 agent 调度
- 异步后台工具长时间自治运行
- 外部 ACP / bridge 的复杂编排策略

---

## 2. 为什么 planner-lite 只做 hint

当前我们把 `planner-lite` 明确限制为“提示信息”，而不是“执行图”。

原因有三点：

- Host 现在最需要的是稳定表达 run 发生了什么，而不是过早固化规划器抽象
- 复杂规划一旦下沉到主链，后续会反向绑死 `pkg/agentcore` 与 Host contract
- 当前 recap、memory、review、skills 沉淀都更适合先消费轻量 hint，再决定是否扩展成真正流程

因此当前只约定两类轻量结构：

- `planner_hint`
  - 表示终态后对“下一阶段应该关注什么”的轻量建议
- `next_action_hint`
  - 表示更贴近 Host / UI 的下一步动作提示

它们都只是 best-effort 元数据：

- 可以由 post-turn hook 产出
- 后续也允许由 runner 或其他 host-side hook 写入
- 当前不直接驱动多 agent 或自动编排

---

## 3. Post-Turn Action 与 Recap 的区别

这两个 hook 都发生在 turn 终态附近，但语义不同。

`RecapHook` 负责：

- 总结这轮发生了什么
- 把结果沉淀成 recap、memory candidate 等资产
- 面向“沉淀和回顾”

`PostTurnActionHook` 负责：

- 判断 turn 结束后，Host 侧下一步最值得做什么
- 产出 `planner_hint` / `next_action_hint`
- 面向“终态后的下一步挂点”

简单理解：

- recap 回答“刚才发生了什么，应该记住什么”
- post-turn action 回答“现在下一步最值得关注什么”

两者分离的原因：

- recap 不应该承担后续调度语义
- post-turn action 也不应该挤进记忆沉淀链路
- 分开后，后续接 review、skills、自继续策略会更自然

---

## 4. 当前 Host contract

当前 terminal run 在 Host 持久化层会保留一组 self-loop 相关 metadata：

- `loop_phase`
  - `initial` / `resumed`
- `resume_source`
  - 例如 `continue_turn`
- `continue_mode`
  - `start` / `message`
- `turn_outcome`
  - `completed` / `failed` / `canceled`
- `planner_hint`
  - 轻量 planning 提示
  - 当前默认 capability hook 会在 `metadata.next_turn_plan` 中记录下一轮建议的 tool/skill/recall 信号
  - 后续 turn 会把上一轮 terminal run 的 `metadata.next_turn_plan` 作为 `previous_next_turn_plan` 注入 runtime context，并最小影响下一轮 tool subset 与 skill matching 偏好
- `next_action_hint`
  - Host / UI 友好的下一步提示

其中：

- `loop_phase`、`resume_source`、`continue_mode`
  - 主要回答 self-loop 当前处在什么阶段
- `turn_outcome`
  - 主要回答这一轮最终结果是什么
- `planner_hint`、`next_action_hint`
  - 主要回答终态后建议关注什么

当前这些字段都属于 Host contract，而不是 `agentcore` kernel contract。

---

## 5. Best-Effort 约束

当前 `PostTurnActionHook` 是 best-effort 机制：

- waiting 态不会触发
- completed / failed / canceled 会统一触发
- hook 失败不会破坏主链
- hint 持久化失败也不会回滚 turn 终态

多 hook 时，当前采取简单合并规则：

- 后执行的 hook 可以覆盖前面同名 hint

这样做的目的：

- 保持规则简单可预期
- 允许后置 hook 基于前置 hook 的结果做细化
- 避免在 `v0` 阶段引入复杂优先级系统

---

## 6. 与 Formal Memory Recall 的当前关系

当前 self-loop 与 formal memory recall 的关系，先约定在 intrunner 内部：

- 每次新 `Run()` 启动前，intrunner 会按当前 `Instruction` 读取并选择 formal `MEMORY.md / USER.md`
- recalled formal memory 只作为 runtime-only context 注入，不进入 transcript
- 当 run 进入 waiting 后，再通过 `continue_turn` 恢复：
  - suspended runtime 仍会复用同一个 working memory / runtime
  - 但 intrunner 现在会在 resume 前刷新 recalled formal memory
  - 因此 resumed run 不会被首次挂起时的 recall 固定住

当前已经验证到：

- 首轮 run 的 recall 会影响最终 summary / output
- resumed run 会基于新的 continue instruction 重新选择 recall
- recalled memory 刷新后，仍不会污染 session history

当前还没有承诺：

- post-turn recap / learning hook 根据 recall 命中结果反向调整策略
- 真正 planner engine 根据 `next_turn_plan` 自动生成执行图

---

## 7. Deferred 能力

下面这些能力明确 defer 到后续阶段，不在当前 `v0` 主线内承诺：

- async background tools
- subagents
- external orchestration

如果后续要做，也应优先复用当前 Host contract：

- 继续沿用 `loop metadata`
- 尽量复用 `planner_hint` / `next_action_hint`
- 避免绕开 Host lifecycle 直接新增一套平行状态机

---

## 8. 手测建议

当前可按下面方式做最小手测：

1. 发起一个普通 turn，确认 terminal 后 run metadata 出现 `turn_outcome`
2. 让 internal runner 进入 waiting，再通过 `continue_turn` 恢复，确认新 run 有 `loop_phase=resumed`
3. 接一个简单 `PostTurnActionHook`，确认 terminal 后 run metadata 出现 `planner_hint` / `next_action_hint`
4. 故意让 post-turn hook 返回错误，确认主链仍然能正常完成 turn

---

## 9. 后续演进建议

如果后续继续推进 self-loop，优先级建议如下：

1. 先把 `planner_hint` / `next_action_hint` 稳定映射到 AG-UI
2. 再决定 Web 是否需要可视化 next action
3. 最后再讨论是否需要从 hint 演进到真正 orchestration

这能保证 `v0` 先可自用，再逐步长出更强的自治能力。
