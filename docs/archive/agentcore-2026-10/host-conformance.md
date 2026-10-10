# Host Conformance

这份文档说明 `agentcore` 当前已经验证到什么程度，不是从 runtime 内部视角，而是从“first-party host 是否已经能自然承接 kernel contract”这个角度来回答。

它回答的是：

- 当前仓库里的 first-party host wrapper，已经覆盖了哪些 kernel contract
- 哪些结论已经有 parity / behavior tests 保护
- `AgentcoreRunner` 和 legacy `ReactRunner` 之间，哪些地方已经对齐，哪些地方仍然只是“contract 等价、实现不同”

它不是 roadmap，也不是 migration guide。

它更像一份“host 接入成熟度”说明。

## 1. 为什么需要这份文档

当 `agentcore` 被当成一个可独立发布的 kernel 时，只看 runtime 自己的测试还不够。

还需要回答另一个更实际的问题：

- 一个真实 host wrapper，能不能自然地围绕这些 contract 组织自己的行为

如果答案是否定的，那么 contract 可能还只是“runtime 内部概念”。

如果答案是肯定的，那么这些 contract 才更接近真正的 kernel surface。

当前仓库已经围绕两类 first-party host wrapper 做了这层验证：

- `AgentcoreRunner`
- legacy `ReactRunner`

它们并不完全等价，但都已经能承接同一组核心 policy contract。

## 2. 当前已验证的 Host Contract 覆盖面

### 2.1 Resume Policy

当前已经验证：

- host wrapper 可以把恢复前置校验交给 `ResumePolicy`
- host wrapper 可以把“恢复时是否追加新输入”交给 `ResumePolicy`
- `AgentcoreRunner` 与 `ReactRunner` 都能承接“抑制 resume instruction 注入”的 contract

这意味着：

- resume 语义已经不再只存在于 runtime 内部
- host wrapper 也已经能围绕同一套 resume contract 组织状态转换

### 2.2 Tool Execution Policy

当前已经验证：

- host wrapper 可以把 tool batch validation 交给 `ToolExecutionPolicy`
- host wrapper 可以把 tool failure 的继续 / 升级策略交给 `ToolExecutionPolicy`
- `AgentcoreRunner` 与 `ReactRunner` 都能承接“工具失败直接升级为终止错误”的 contract

这意味着：

- tool loop 的主路径决策已经不必继续散落在 host wrapper 自己的硬编码里
- 即使存在不同实现风格，tool error policy 也已经形成了跨 host 的公共语义

### 2.3 Error Policy

当前已经验证：

- host wrapper 可以把 recoverable provider error 的去向交给 `ErrorPolicy`
- `AgentcoreRunner` 与 `ReactRunner` 都能承接“将 recoverable error 转成 yielded result”的 contract

这意味着：

- recoverable error 不再只能通过 hook 或宿主外部逻辑去间接表达
- error policy 已经是 host-facing 的正式 kernel contract

### 2.4 HITL Related Tool-Failure Semantics

当前已经验证：

- `AgentcoreRunner` 的连续工具失败触发 HITL，已经主要收敛到 tracker + `ToolExecutionPolicy` 包装一侧
- hook 现在更接近 side-effect adapter：消费 pending intervention、调用 HITL manager、回写人工指令

这意味着：

- host wrapper 的 HITL 主路径，已经开始从“hook 内隐式协议”转向“policy + side-effect adapter”
- 这更符合 kernel contract 的长期方向

## 3. 当前已有的 First-Party Conformance 结论

可以把当前结论概括为三点：

### 3.1 `AgentcoreRunner` 已经是主路径承接者

它已经自然承接：

- `ResumePolicy`
- `ToolExecutionPolicy`
- `ErrorPolicy`

并且这些 contract 不只是“能透传进去”，而是已经有行为级测试保护。

### 3.2 `ReactRunner` 已经进入 contract 对照体系

legacy `ReactRunner` 现在也能通过 first-party adapter 方式承接同一组 policy contract。

这很重要，因为它让仓库内的 legacy path 不再是完全孤立的一套 runtime 语义，而是可以和 `agentcore` kernel shape 做直接对照。

### 3.3 Adapter / Storage Bridge 边界当前是健康的

当前仓库里围绕 provider / tool / memory / storage 的 first-party bridge，已经能与 `agentcore` 的公共 contract 自然协作。

这说明：

- `agentcore` 没有反向依赖宿主内部实现
- 宿主内部 bridge 也没有要求 runtime 暴露额外私有 contract

## 4. 当前仍然存在的语义差异

虽然两类 first-party host wrapper 已经能承接同一组 contract，但它们仍然不是完全同构的实现。

下面这些差异仍然存在，而且是合理的：

### 4.1 Resume 生命周期不同

`AgentcoreRunner` 更像：

- host wrapper 维护 pending resume state
- 下一次 `Run(...)` 再把恢复输入交给 runtime 消化

legacy `ReactRunner` 更像：

- `Resume(...)` 直接推进自身状态
- 更接近旧式 host-local control flow

这说明：

- 两者当前是 contract 等价
- 但还不是 lifecycle 等价

### 4.2 Loop 实现方式不同

`AgentcoreRunner` 把主循环交给 `agentcore` runtime。

legacy `ReactRunner` 仍然自己维护一套 loop。

这说明：

- policy contract 已经对齐
- 但 loop ownership 还没有彻底统一

### 4.3 默认语义仍有 legacy 兼容层

为了保持 parity 和 fallback 路径可用，legacy `ReactRunner` 当前仍保留一层默认行为适配。

这意味着：

- contract 已统一
- 默认实现细节仍然允许保留历史口径

## 5. 这对 `agentcore` 的 kernel 判断意味着什么

这组 conformance 结果支持一个更强的判断：

- `agentcore` 当前的 kernel shape 已经不只是 runtime 内部自洽
- 它已经足够承接仓库内部主路径 host wrapper 的核心策略语义

换句话说：

- 这些 contract 已经具有 host-facing 现实意义
- 不再只是“未来也许会稳定”的接口

当前尤其值得视为已形成稳定姿态的，是：

- `ResumePolicy`
- `ToolExecutionPolicy`
- `ErrorPolicy`

## 6. 接下来还缺什么

虽然已经有 first-party host conformance，但仍然有几类工作值得继续补：

- 更系统的 contract matrix，而不是只覆盖关键主路径
- 更明确地区分“contract parity”和“lifecycle parity”
- 继续减少 legacy host wrapper 中与 kernel 默认语义重复的分散逻辑

如果未来要继续推动 internal host 全量切向 `agentcore`，这份文档应该持续更新，而不是停留在一次性的说明。

## 7. 搭配阅读

如果你想继续看：

- [runtime-policies.md](./runtime-policies.md)
- [host-integration.md](./host-integration.md)
- [kernel-stability.md](./kernel-stability.md)
- [compatibility.md](./compatibility.md)
