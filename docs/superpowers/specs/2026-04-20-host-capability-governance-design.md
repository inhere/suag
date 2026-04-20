# Host Capability Governance Design

> 状态：Draft
> 日期：2026-04-20
> 适用范围：Axon v0 self-host 主线

## 1. 背景与目标

当前仓库已经具备：

- `pkg/agentcore` 作为 runtime kernel
- `internal/platform/tools` 作为第一方工具实现与 registry 基座
- Host session / intrunner / gateway 主链
- formal memory recall、post-turn recap、memory candidate 闭环

但 `tools`、`mcp`、`skills` 仍然没有进入统一治理链：

- native tools 目前主要在 app 启动时直接注册
- MCP 还没有作为正式 tool provider 并入当前主链
- `SKILL.md` 形态的 skill 还没有被建模为 Host/runtime 可消费的一等能力对象

这会带来几个长期问题：

1. 能力注册入口分散，后续很容易在 `app / host / team / orchestrator` 各自长一套
2. MCP 如果单独走一套执行模型，会让 runtime、HITL、trace、policy、UI 都出现双分支
3. Skill 如果被直接塞成 tool，会失去它作为“能力配方”的长期扩展空间
4. 如果过早把这些产品语义下沉到 `agentcore`，会污染 kernel 边界

本文档目标是定义一套长期可扩展、但能从 v0 平滑落地的能力治理模型：

- 明确 `agentcore` 与 `axon` 的能力边界
- 定义 `tool / mcp / skill` 的统一分层
- 定义 Host/runtime 中的注册、解析、消费链路
- 给出 v0 → v1 的推荐落地顺序

本文档不覆盖：

- skill marketplace
- 自动 skill 评估/优化闭环主链化
- planner orchestration engine
- 多 agent DAG/workflow 编排

---

## 2. 设计原则

### 2.1 Kernel 只负责“怎么跑”

`pkg/agentcore` 只应负责 runtime kernel：

- provider、storage、memory、tool contract
- tool loop、validation、hook、policy
- 最小公共运行语义

`agentcore` 不应承担：

- MCP lifecycle
- `SKILL.md` 解析与分发
- Host capability governance
- 产品级默认工具箱

一句话：

- `agentcore` 负责“怎么跑”
- `axon` 负责“有哪些能力、何时用、怎么装配”

### 2.2 Tool 是 runtime primitive

runtime 最终只消费标准 tool contract：

- `ToolSchema`
- `ToolCall`
- `ToolResult`
- `ToolRegistry`

不额外引入 “MCP tool lane” 或 “Skill execution lane”。

### 2.3 MCP 是 tool source，不是第二套 runtime primitive

MCP 在系统内应被建模为一种 `ToolProvider` 来源。

这样 Host/runtime 最终看到的仍然是一组标准 tools，而不是新的执行模型。

### 2.4 Skill 是 capability recipe，不是 executable primitive

这里的 skill，指类似 [skill-creator](C:/Users/inhere/.agents/skills/skill-creator/SKILL.md) 这类 `SKILL.md` 能力包。

skill 的本质是：

- 触发条件
- 行为指导
- 参考资源
- 推荐工具/能力
- 输出格式与约束

它不应直接等价于一个 tool call。

### 2.5 先做治理，再做智能

先收口：

- 全局注册
- 会话级能力集合
- turn 级能力解析

再做：

- planner 消费 recall / skill / tool 信号
- post-turn learning 产出 skill candidate

---

## 3. 统一能力模型

### 3.1 Executable Tool

`Executable Tool` 是 runtime 直接可调用的执行单元。

特征：

- 有稳定 schema
- 有 call/result
- 能直接进入 `agentcore.ToolRegistry`

典型示例：

- `file_read`
- `file_write`
- `shell_exec`
- `grep_search`
- `web_search`

### 3.2 Tool Provider

`Tool Provider` 不是单个 tool，而是一组 tools 的来源与生命周期载体。

职责：

- 暴露一组 tool descriptors
- 提供对应执行入口
- 携带 provider 级元信息
- 为 policy / tracing / catalog 提供来源标记

预计来源包括：

- native provider
- MCP provider
- future remote provider

### 3.3 Skill

`Skill` 是能力配方，不直接进入 runtime 的 tool registry。

它应包含：

- 基础身份信息
  - `name`
  - `description`
  - source/path/version-like metadata
- 触发与匹配信息
  - tags
  - trigger hints
  - optional scope hints
- 行为内容
  - prompt fragments
  - references/scripts/assets 指引
  - 输出格式、步骤建议、注意事项
- 能力偏好
  - 推荐 tools
  - 推荐 MCP sources
  - optional tool allowlist/denylist

### 3.4 三者关系

```text
native / mcp / future adapters
        ↓
   Tool Providers
        ↓
   Executable Tools
        ↓
 agentcore runtime

skills
  ↓
host-side capability resolution
  ↓
prompt/context/tool preference
  ↓
agentcore runtime
```

结论：

- tool 是 runtime 一等公民
- mcp 是 tool source
- skill 是 orchestration-side capability

---

## 4. 分层与目录建议

推荐目录形态如下：

```text
pkg/
  agentcore/

internal/
  platform/
    llm/
    memory/
    tracing/
    tools/
      native/
      providers/
      mcp/
      catalog/
  capabilities/
    skills/
      loader/
      parser/
      registry/
      resolver/
    runtimecaps/
  host/
    intrunner/
    acpbridge/
    session/
    gateway/
  app/
```

### 4.1 `internal/platform/tools`

这里放纯工具执行体系：

- `Registry`
- tool descriptors
- native tools
- MCP -> tools provider adapter
- tool catalog helper

它应该保持尽量贴近 `agentcore.ToolRegistry` 的 contract。

### 4.2 `internal/capabilities/skills`

这里放 skill 能力体系：

- `SKILL.md` loader
- parser
- registry
- resolver
- optional selector/matcher

它不应该挤进 `platform`，因为 skill 语义已经明显属于 agent/product capability，而不是纯基础设施。

### 4.3 `internal/capabilities/runtimecaps`

这个包建议放“运行时能力装配对象”：

- `CapabilityManager`
- `CapabilitySet`
- `ResolvedTurnCapabilities`
- 相关 policy/scope structs

它是连接：

- app/global registration
- host/session/turn resolution
- intrunner runtime consumption

的轻量中枢。

### 4.4 `internal/host`

Host 只消费整理好的能力结果，不直接持有复杂能力注册逻辑。

Host 应关注：

- session lifecycle
- turn/run orchestration
- gateway / snapshot / event flow
- 与 capability resolution 的接线

---

## 5. `agentcore` 与 `axon` 的内置边界

### 5.1 `agentcore` 应该内置的东西

- tool contract
- tool validation
- runtime tool loop
- mocks / examples / testing fixtures

### 5.2 `agentcore` 不应该内置的东西

- filesystem/shell/web 等产品级工具
- MCP client / server 管理
- skill loader / parser / resolver
- host capability governance
- 默认产品工具箱

原则：

- `agentcore` 可以内置机制
- `agentcore` 不内置 Axon 产品能力集合

### 5.3 `axon` 应该内置的东西

Axon 应提供一组最小 self-host 可用能力底座：

- first-party native tools
  - `file_read`
  - `file_write`
  - `shell_exec`
  - `search/list_dir` 类
  - optional `web_search`
- tool providers
  - native provider
  - MCP provider
- skill runtime support
  - registry
  - loader
  - resolver
- host capability assembly
  - session scope
  - workspace scope
  - allowlist / denylist / approval hints

---

## 6. Host Runtime Capability Flow

推荐把整条链路拆成四层。

### 6.1 Global Registration

应用启动时完成全局能力注册，只回答“系统里有什么能力”。

建议在 `internal/app` 中构建一个全局 `CapabilityManager`：

- 注册 `ToolProviderRegistry`
- 注册 `SkillRegistry`
- 构建 `ToolCatalog`

此时不要直接生成某个最终 runtime registry。

### 6.2 Session Capability Assembly

进入 Host session 层后，生成会话级 `CapabilitySet`。

建议结构包含：

- `Tools []ToolRef`
- `Skills []SkillRef`
- `Providers []ProviderRef`
- `Policies CapabilityPolicy`
- `Scope CapabilityScope`

语义：

- `CapabilitySet` 是 session 稳定底座
- 它表达“这次会话理论上可用什么”

### 6.3 Turn-Time Resolution

每次 `StartTurn / ContinueTurn` 时，基于：

- 当前 user/continue message
- recall metadata
- session scope/policy
- previous turn outcome

解析出一个：

- `ResolvedTurnCapabilities`

它应至少包含：

- `ToolRegistry`
- `SelectedSkills`
- `PromptFragments`
- `ContextResourceHints`
- `ToolAllowlist`
- `Metadata`

当前阶段建议 skill resolution 最多只选 0..2 个技能，避免 context 膨胀。

### 6.4 Runtime Consumption

intrunner / agentcore runtime 最终只消费：

- resolved tool subset
- resolved prompt/context additions
- resolved metadata/policy hints

runtime 不需要再关心：

- skill 文件在哪
- mcp source 是怎么连的
- workspace/project 配置从哪来

---

## 7. Skill 在 Runtime 中的生效方式

v0/vNext 阶段，skill 只通过三种方式影响运行：

### 7.1 System Prompt Fragment

提供少量高价值指导：

- 输出格式
- 工作方式
- 关键注意事项

### 7.2 Context Resource Hints

skill 推荐加载：

- references
- scripts
- assets

但不要求每次把 skill 全量 body 或所有资源强塞进 prompt。

### 7.3 Tool Preference / Allowlist

skill 可以影响：

- 推荐优先使用哪些 tools
- 对当前 turn 暴露哪些 tools 子集
- optional safety preference

当前明确不做：

- skill 直接变 workflow engine
- skill 自动多步执行图
- skill-to-skill orchestration engine

---

## 8. MCP 在系统中的位置

MCP 应作为 `ToolProvider` 的一种来源。

建议链路：

1. app/global registration 注册一个或多个 MCP providers
2. provider 暴露标准 tool descriptors
3. turn-time resolution 过滤为当前 turn 的 tool subset
4. runtime 只看到标准 tools

这意味着 runtime 不区分：

- native tool
- mcp tool

好处：

- HITL 不需要单独兼容第二套能力类型
- tracing / event / AG-UI 无需多一套语义
- policy / allowlist 仍然统一作用于 tool 层

---

## 9. Self-Loop / Continue 与 Capability Resolution

这里必须和当前 Host self-loop 语义保持一致。

建议规则：

- session 级 `CapabilitySet` 是稳定底座
- turn 级 `ResolvedTurnCapabilities` 是动态快照
- 每次 `continue_turn` 时重新做 turn-time resolution

也就是说：

- 不重新发明整套 session/global registries
- 但会基于新的 continue message、recall、scope 重新解析：
  - selected skills
  - tool subset
  - runtime-only prompt/context additions
  - recall-aligned metadata

这与当前已修复的 resumed recall 语义一致：

- persistent base
- runtime-only refresh

---

## 10. v0 → v1 的推荐落地顺序

### Phase A：收口能力治理骨架

先做：

- `CapabilityManager`
- `ToolProvider`
- `CapabilitySet`
- `ResolvedTurnCapabilities`
- app 启动时的默认能力构建入口

此阶段先不要求 skill 真正影响 runtime。

### Phase B：统一 Tools 与 MCP

先把 tool 主链做扎实：

- native provider
- MCP provider
- ToolCatalog
- Host/runtime 改为消费 resolved tool subset

完成标准：

- native / mcp 在 runtime 看起来是同一种 tool source

### Phase C：接入 Skills 最小闭环

先做：

- `SKILL.md` loader
- parser
- descriptor
- registry
- resolver
- 通过 prompt/context/tool allowlist 影响当前 run

明确不做：

- workflow engine
- marketplace
- 自动 skill ranking 主链化

### Phase D：最后再接 Planner / Post-Turn Learning

等 tool/mcp/skill 都进入治理链后，再做：

- planner 消费 recall + matched skills + tool subset
- post-turn learning 产出 skill candidate
- review flow 把 candidate skill 转成正式 skill

---

## 11. 风险与约束

### 11.1 过早抽象风险

如果现在直接上一个“大而全 capability layer”，容易出现：

- 为整齐而整齐
- 抽象先于真实使用场景
- v0 试错成本上升

因此推荐：

- 先按上述目录收口
- 只抽最小公共对象
- 不提前实现复杂引擎

### 11.2 Skill 过度执行化风险

如果 skill 一开始就直接变成自动 workflow：

- 会绑死 runtime
- 会让调试和 UI 复杂化
- 会与 planner/post-turn learning 职责重叠

因此 skill v0 先作为 recipe，而不是 execution engine。

### 11.3 MCP 双系统风险

如果 MCP 不并入 tool provider 体系：

- Host/runtime 会出现双执行模型
- trace/HITL/UI/policy 要重复接线

因此 MCP 必须统一下沉为 tool source。

---

## 12. 结论

推荐的长期方向是：

```text
agentcore = runtime kernel
axon tools = executable capability layer
axon mcp = external tool providers
axon skills = capability recipes
host = session/runtime orchestrator consuming resolved capabilities
```

配套原则：

- `agentcore` 只做 kernel 与 tool contract
- `axon` 负责能力治理
- skill 不直接等于 tool
- mcp 不直接等于第二套 runtime primitive
- Host 运行时始终消费“解析后的能力快照”

这样既能支持 v0 先快速可自用，也能为后续：

- planner
- post-turn learning
- skill 沉淀
- workspace/project scope
- 多 agent capability governance

留下清晰扩展空间。
