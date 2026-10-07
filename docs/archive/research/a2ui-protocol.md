# A2UI 协议


- https://github.com/google/A2UI/tree/main/specification


**A2UI 不是 transport protocol，而是 UI payload format**。

- v1 就需要让 Agent 返回结构化业务卡片 / 表单 / 面板，因此应作为某类事件的 `payload` 传输。
- 更适合挂在 AG-UI 的 `CUSTOM` 语义上。
