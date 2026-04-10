# agent-gateway 协议整合设计：A2A、AG-UI、A2UI 如何协同

> 日期：2026-04-01
> 适用范围：`Clients -> gateway -> Agent L1 / Agent Mesh -> gateway -> Clients`

---

## 1. 结论先行

### 1.1 三个协议在系统中的职责分层

| 协议 | 放在链路哪一段 | 角色
|---|---|---|---|
| **A2A** | `Agent -> Agent` | 南向通信信封 / 任务语义
| **AG-UI** | `gateway -> Clients` 事件下行 | 事件分类、状态同步、流式输出的参考模型
| **A2UI** | `Agent -> UI` 的结构化界面描述 | 富交互卡片 / 表单 / 面板的载荷格式

**参考资源网站：**

- https://github.com/CopilotKit/OpenGenerativeUI 这是一个开源演示项目，旨在展示如何利用 CopilotKit 和 LangChain Deep Agents 构建丰富且交互式的 AI 生成用户界面。您可以要求智能代理可视化算法、创建 3D 动画、渲染图表或生成交互式图表——所有内容均以实时 HTML/SVG 格式在沙盒化的 iframe 中呈现
- https://a2ui-composer.ag-ui.com/ 这是一个基于 A2UI 协议的快速定义组件站点，用于创建结构化业务卡片 / 表单 / 面板。

- https://github.com/google/A2UI/tree/main/specification

**A2UI 不是 transport protocol，而是 UI payload format**。

- v1 就需要让 Agent 返回结构化业务卡片 / 表单 / 面板，因此应作为某类事件的 `payload` 传输。
- 更适合挂在 AG-UI 的 `CUSTOM` 语义上。
