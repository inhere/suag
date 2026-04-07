# Agentcore Adoption Paths

这份文档是一页入口式的选择器。

如果你第一次看到 `agentcore`，不确定该从哪份文档开始，先看这里。

它回答的问题很简单：

- 我是哪一类使用者
- 我应该走哪条接入路径
- 我先看哪些文档和示例最省时间

---

## 1. 先选你属于哪种情况

### A. 我在做一个新系统，想直接嵌入一个最小但可扩展的 agent runtime kernel

你更适合走：

- 直接嵌入路径

先看：

1. [getting-started.md](./getting-started.md)
2. [recommended-runtime.md](./recommended-runtime.md)
3. [api.md](./api.md)

再跑这些示例：

1. [../examples/minimal/main.go](../examples/minimal/main.go)
2. [../examples/recommended/main.go](../examples/recommended/main.go)

### B. 我已经有聊天服务、CLI、worker 或任务执行器，想把 agentcore 封装成宿主层的一部分

你更适合走：

- host integration 路径

先看：

1. [getting-started.md](./getting-started.md)
2. [recommended-runtime.md](./recommended-runtime.md)
3. [host-integration.md](./host-integration.md)

再跑这些示例：

1. [../examples/recommended/main.go](../examples/recommended/main.go)
2. [../examples/host/main.go](../examples/host/main.go)

### C. 我已经有一套 runtime 或一堆 internal adapter，想逐步迁到 agentcore

你更适合走：

- migration 路径

先看：

1. [embedding-patterns.md](./embedding-patterns.md)
2. [host-integration.md](./host-integration.md)
3. [migration.md](./migration.md)

再配合看：

1. [release-readiness.md](./release-readiness.md)
2. [versioning.md](./versioning.md)

### D. 我主要在评估它现在是否已经值得依赖，或者离独立发布还有多远

你更适合走：

- release posture 路径

先看：

1. [compatibility.md](./compatibility.md)
2. [versioning.md](./versioning.md)
3. [release-readiness.md](./release-readiness.md)
4. [roadmap.md](./roadmap.md)

---

## 2. 如果你只想花 10 分钟

推荐最短路径：

1. 看 [../README.md](../README.md)
2. 看 [getting-started.md](./getting-started.md)
3. 看 [recommended-runtime.md](./recommended-runtime.md)
4. 跑 [../examples/recommended/main.go](../examples/recommended/main.go)

这条路径最适合：

- 想快速判断 `agentcore` 适不适合自己
- 想先看到一条成型的主路径
- 不想先陷入所有细节

---

## 3. 如果你只想看代码，不想先读太多文档

推荐顺序：

1. [../examples/minimal/main.go](../examples/minimal/main.go)
2. [../examples/prompt/main.go](../examples/prompt/main.go)
3. [../examples/recommended/main.go](../examples/recommended/main.go)
4. [../examples/host/main.go](../examples/host/main.go)
5. [../examples/resume/main.go](../examples/resume/main.go)
6. [../examples/stream/main.go](../examples/stream/main.go)

推荐这样读的原因是：

- 先看最小 runtime
- 再看 prompt 等单点能力
- 再看推荐组合
- 最后看 host、resume、streaming 这些更接近真实系统的路径

---

## 4. 如果你最关心边界

推荐顺序：

1. [architecture.md](./architecture.md)
2. [api.md](./api.md)
3. [embedding-patterns.md](./embedding-patterns.md)
4. [host-integration.md](./host-integration.md)

你会重点看到这些边界：

- `Storage = authority`
- `Memory = derived context`
- streaming 不改 authority completion
- host 管产品流程，kernel 管 runtime semantics

---

## 5. 如果你最关心未来演进

推荐顺序：

1. [compatibility.md](./compatibility.md)
2. [versioning.md](./versioning.md)
3. [release-readiness.md](./release-readiness.md)
4. [roadmap.md](./roadmap.md)

这样读更容易区分：

- 现在已经能依赖什么
- 未来哪些变更仍可能发生
- 哪些地方会优先继续收紧

---

## 6. 一句话路线图

可以把当前 `agentcore` 的采用路径理解成：

- 想上手：`getting-started`
- 想直接用：`recommended-runtime`
- 想做宿主封装：`host-integration`
- 想做迁移：`migration`
- 想评估成熟度：`compatibility + versioning + release-readiness`

如果你已经选好了自己的路径，下一步回到：

- [README.md](../README.md)
- [README.md](./README.md)
