# Web Memory Candidates Panel Design

> 状态：Draft for review
> 日期：2026-04-17
> 范围：`web/app` 右侧工作台中接入 `memory candidates` 的最小人工处理面板
> 相关文档：`docs/v0-self-host-design.md`、`docs/usage/host-recap-memory-hooks-v0.md`、`docs/usage/web-host-local-dev.md`

---

## 1. 目标

这份设计文档定义 Suag Web 第一版 `Memory Candidates` 面板的前端接入方式。

当前后端已经具备：

- `memory_candidates` 落库
- `MEMORY.draft.md` / `USER.draft.md` 聚合渲染
- `applied / rejected` 状态流转
- REST 查询与动作入口

因此这轮前端工作的目标不是继续扩展记忆系统，而是先让用户能在现有工作台内完成下面这条最小链路：

`查看当前 session 候选 -> 选择 Accept / Reject -> 后端状态更新 -> draft 文件自动收敛`

---

## 2. 当前上下文

当前 Web 工作台结构是三栏：

- 左侧：session list
- 中间：conversation
- 右侧：run inspector + HITL

其中右侧已经承担了“查看细节 + 执行动作”的职责，例如：

- focus 某个 run
- stop 当前 turn
- add hint
- 响应 HITL ticket

`memory candidates` 的第一版也属于人工介入动作，因此最自然的接入位置不是中间聊天区，而是右侧工作台列。

---

## 3. 设计结论

### 3.1 面板位置

第一版 `Memory Candidates` 采用右侧独立卡片方案：

- 放在右侧工作台列
- 作为 `Run Inspector` 之后、`HITL` 之前的独立面板
- 不塞进 `Focused Run`
- 不打断中间 conversation 主视图

这样可以保持清晰边界：

- `Run Inspector`
  - 关注 run 级执行细节
- `Memory Candidates`
  - 关注 session 级记忆候选人工处理
- `HITL`
  - 关注等待中的显式人工介入票据

### 3.2 第一版只做待处理视图

第一版只展示当前 session 中仍需处理的候选：

- `draft`
- `rendered`

明确不展示：

- `applied`
- `rejected`

原因是第一版目标是快速处理待办，而不是构建完整记忆管理中心。

### 3.3 默认按 `MEMORY / USER` 分组

候选列表默认分成两组：

- `MEMORY`
  - 代表系统经验、流程沉淀、通用工作方法
- `USER`
  - 代表用户偏好、沟通方式、个性化工作习惯

不采用统一混排列表。

原因是用户需要快速判断候选属于哪类长期沉淀，而不是先阅读内容后再推断类型。

---

## 4. UI 结构

### 4.1 卡片结构

右侧新增一个 `Memory Candidates` 面板，结构如下：

1. 标题区
   - 标题：`Memory Candidates`
   - 右侧显示待处理数量
2. 内容区
   - 先渲染 `MEMORY` 组
   - 再渲染 `USER` 组
3. 底部说明
   - `Draft files refresh automatically after candidate updates.`

### 4.2 候选项结构

每条候选只展示最小必要信息：

- `kind`
- `content`
- `status`
- `Accept`
- `Reject`

不显示长元数据，不做详情展开。

### 4.3 空状态

当当前 session 没有待处理候选时，显示：

- `No pending memory candidates.`

### 4.4 错误与加载状态

卡片级：

- 加载中：`Loading memory candidates...`
- 列表加载失败：`Failed to load memory candidates.`
- 列表加载失败时提供 `Retry`

条目级：

- 当某条候选正在执行 `accept/reject` 时，仅禁用该条两个按钮
- 动作失败时显示轻量错误：`Failed to update candidate.`

---

## 5. 数据流

### 5.1 数据来源

前端通过 REST 获取和更新 `memory candidates`：

- `GET /api/sessions/{sessionId}/memory-candidates`
- `POST /api/memory-candidates/{candidateId}/accept`
- `POST /api/memory-candidates/{candidateId}/reject`

第一版不通过 websocket 实时同步候选状态。

### 5.2 加载策略

当 `currentSessionId` 变化时：

- 发起一次 `listMemoryCandidates(sessionId)`
- 将结果保存到 `App` 顶层本地状态

动作成功后：

- 不做本地猜测更新
- 直接重新拉取当前 session 的 candidate 列表

这样可以保证前端始终和后端真实状态一致，避免手工维护额外派生逻辑。

### 5.3 前端状态模型

第一版新增的本地状态建议只有：

- `memoryCandidates`
- `memoryCandidatesLoading`
- `memoryCandidatesError`
- `memoryCandidateActionPendingId`
- `memoryCandidateActionError`

不把这部分数据塞进现有 session snapshot reducer。

原因是：

- `memory candidates` 当前不是 websocket snapshot 的一部分
- 它更接近右侧附加工作面板数据
- 保持独立局部状态更容易演进

---

## 6. 组件边界

### 6.1 新增组件

建议新增：

- `web/app/src/features/memory-candidates/memory-candidates-panel.tsx`

如果候选项渲染稍微复杂，再拆：

- `web/app/src/features/memory-candidates/memory-candidate-item.tsx`

### 6.2 现有组件改动边界

#### `App.tsx`

负责：

- 加载当前 session 的 candidate 列表
- 处理 accept / reject 动作
- 管理 loading / error / pending

#### `AppShell.tsx`

负责：

- 把 candidate 数据和动作 props 往右侧列传递

#### `RunInspector.tsx`

继续只负责 run 相关内容，不直接承载 `memory candidates` 业务逻辑。

#### `HITLPanel`

保持现有职责，不与 `memory candidates` 混合。

---

## 7. 类型与 Client 设计

### 7.1 `host-types.ts`

新增：

- `MemoryCandidate`

建议字段与后端 REST 返回保持一致：

- `candidateId`
- `sessionId`
- `turnId`
- `runId`
- `sourceRecapId`
- `target`
- `kind`
- `content`
- `score`
- `status`
- `createdAt`
- `metadata`

### 7.2 `session-client.ts`

新增：

- `listMemoryCandidates(sessionId)`
- `acceptMemoryCandidate(candidateId)`
- `rejectMemoryCandidate(candidateId)`

UI 层只依赖 typed client，不直接拼 REST 细节。

---

## 8. 测试范围

### 8.1 `session-client.test.ts`

补三类测试：

- `GET /api/sessions/{id}/memory-candidates`
- `POST /api/memory-candidates/{id}/accept`
- `POST /api/memory-candidates/{id}/reject`

### 8.2 `App.test.tsx`

补下面几类行为：

- session 切换后会加载 memory candidates
- 面板按 `MEMORY / USER` 分组显示
- `Accept` 成功后重新拉列表并从待处理视图消失
- `Reject` 成功后重新拉列表并从待处理视图消失
- 列表加载失败显示错误和 `Retry`
- 动作失败显示轻量错误
- 某一条动作 pending 时，仅禁用该条按钮

### 8.3 组件级测试策略

如果 `MemoryCandidatesPanel` 只是纯展示 props，则不强制单独拆组件测试。

优先保证：

- client mapping 测试
- `App` 级交互测试

避免第一版为了测试结构而增加过多低价值样板。

---

## 9. 非目标

这一轮明确不做：

- `applied / rejected` 历史展示
- candidate 编辑
- 批量 accept / reject
- 搜索与筛选
- 独立记忆管理页面
- websocket 实时 candidate 状态流
- draft 文件内容直接展示到页面

---

## 10. 实施顺序

建议实现顺序固定为：

1. 补 `host-types.ts` 与 `session-client.ts`
2. 补 `session-client.test.ts`
3. 新增 `MemoryCandidatesPanel`
4. 在 `AppShell` 右侧接面板
5. 在 `App.tsx` 接列表加载与 accept/reject
6. 补 `App.test.tsx`
7. 最后补样式

这个顺序的好处是：

- 先锁定数据契约
- 再锁定展示结构
- 最后补交互与样式

---

## 11. 风险与取舍

### 11.1 为什么不进 websocket

当前 `memory candidates` 还是人工处理面板数据，不值得在第一版就扩展 websocket / snapshot contract。

先走 REST，可以更快收口前后端行为。

### 11.2 为什么不展示历史

如果第一版就把 `applied / rejected` 历史也塞进右侧，会让卡片从“待处理动作面板”迅速膨胀成“记忆管理中心”。

这不符合当前 v0 阶段目标。

### 11.3 为什么不塞进 `RunInspector`

`memory candidates` 当前是 session 级聚合对象，不是 run 级详情。

把它塞进 `Focused Run` 会制造错误心智模型，后面很可能还要拆。

---

## 12. 完成标准

如果达到下面这些条件，就说明这轮前端接入已完成：

- 当前 session 切换后可稳定加载 `memory candidates`
- 右侧出现独立 `Memory Candidates` 卡片
- 候选按 `MEMORY / USER` 分组显示
- `Accept / Reject` 可调用后端并成功刷新列表
- draft 文件状态变化不会再通过额外手工刷新才能反映到前端待处理列表
- 错误和 loading 反馈清晰
- 不影响现有 conversation / run inspector / HITL 主链

---

## 13. 下一步

这份 spec 完成后，下一步应是：

1. 由用户确认文档内容
2. 再写实现计划
3. 然后进入前端实现

后续如果这条链路真正开始被频繁使用，再考虑第二阶段：

- 历史视图
- 搜索筛选
- candidate 编辑
- 更完整的记忆管理面板
