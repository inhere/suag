# Memory & Tracing 设计

> 所属层级：能力层 | 相关文档：[架构总纲](./00-overview.md) | [Agent Core](./01-agent-core.md#6-上下文压缩三层策略s06)

---

## 1. Memory System

### 1.1 设计目标

- 短期上下文管理：维护当前会话消息列表，控制 token 窗口
- 长期语义记忆：跨会话持久化记忆片段，支持向量语义检索
- 上下文隔离：每个 Agent/Task 拥有独立 MemoryStore 实例

### 1.2 核心接口

```go
type MemoryStore interface {
    // 短期上下文
    AddMessage(role, content string)
    GetContext(maxTokens int) []Message    // 近期消息，按 token 预算裁剪

    // 长期记忆
    SearchSimilar(query string, topK int) ([]MemoryFragment, error)
    SaveFragment(content string, source string) error

    // 压缩
    Summarize(messages []Message) string   // 长对话 LLM 压缩摘要
}

type VectorStore interface {
    Insert(id string, embedding []float32, metadata map[string]any) error
    Search(query []float32, topK int) ([]SearchResult, error)
    Delete(id string) error
}
```

### 1.3 实现层次

```
MemoryStore
├── InMemoryStore（短期：当前会话消息列表）
│   └── 超过 N 条时自动调 LLM 压缩摘要（见上下文三层压缩策略）
└── SQLiteMemory（长期持久化）
    ├── messages 表（对话历史，按 session_id 分区）
    ├── fragments 表（记忆片段 + embedding 向量）
    └── sqlite-vec 虚拟表（ANN 向量索引）
```

### 1.4 数据模型

```go
type Message struct {
    Role      string    // "user" | "assistant" | "tool" | "system"
    Content   string
    ToolCall  *ToolCall
    Timestamp time.Time
}

type MemoryFragment struct {
    ID        string
    Content   string
    Embedding []float32
    Source    string    // agent_id / task_id
    Tags      []string  // 用于过滤检索
    CreatedAt time.Time
}
```

---

## 2. 长期记忆检索流程

```
用户提问 / Agent 推理准备
    ↓
1. 调 Embedding API 生成查询向量
    ↓
2. sqlite-vec ANN 搜索 topK 相关记忆片段
    ↓
3. 拼接相关记忆 + 近期消息列表
    ↓
4. 发给 LLM 推理
    ↓
5. LLM 回复后 → 提取关键结论 → 存储新 MemoryFragment
```

### Embedding 接口

Embedding 面向接口设计，实现可替换，云端/本地均支持：

```go
type EmbeddingProvider interface {
    Embed(ctx context.Context, text string) ([]float32, error)
    Dims() int    // 向量维度（用于建索引）
}
```

**内置实现**：

| 实现 | 说明 | 适用场景 |
|------|------|----------|
| `OpenAIEmbedding` | 调用 OpenAI text-embedding-3-small | 云端，精度高，有费用 |
| `ClaudeEmbedding` | 调用 Anthropic Embedding API | 云端，与 Claude 同生态 |
| `OllamaEmbedding` | 调用本地 Ollama（nomic-embed-text 等） | 本地，零费用，需本地运行 |
| `BM25Search` | 关键词全文检索（SQLite FTS5） | Phase 1 兜底，无需模型 |

**选型策略**：
- Phase 1：BM25 兜底，先跑通流程
- Phase 2：引入 `EmbeddingProvider` 接口 + sqlite-vec，默认用 OpenAI/Ollama，可在配置切换
- Phase 6+：支持 PGVector 等外部向量数据库（替换 `VectorStore` 实现）

配置示例（`config.yaml`）：
```yaml
memory:
  embedding:
    provider: "ollama"          # "openai" | "claude" | "ollama" | "bm25"
    model: "nomic-embed-text"
    endpoint: "http://localhost:11434"
  vector_store: "sqlite-vec"   # "sqlite-vec" | "pgvector"
```

---

## 3. 上下文三层压缩策略

详细策略说明见 [Agent Core - s06](./01-agent-core.md#6-上下文压缩三层策略s06)。

Memory System 的配合实现：

```go
func (m *SQLiteMemory) GetContext(maxTokens int) []Message {
    msgs := m.recentMessages()
    tokens := countTokens(msgs)

    // L1: 滑动窗口
    if len(msgs) > m.windowSize {
        msgs = msgs[len(msgs)-m.windowSize:]
    }

    // L2: LLM 摘要压缩
    if tokens > maxTokens*7/10 {
        summary := m.summarize(msgs[:len(msgs)/2])
        msgs = append([]Message{{Role: "system", Content: summary}}, msgs[len(msgs)/2:]...)
        m.SaveFragment(summary, "auto-summary")
    }

    // L3: 重要性过滤
    if countTokens(msgs) > maxTokens*9/10 {
        msgs = filterImportant(msgs) // 保留 tool_call + 关键结论
    }

    return msgs
}
```

---

## 4. SQLite 并发策略

多 Agent 并发访问 SQLite 需专项处理：

| 操作类型 | 策略 |
|----------|------|
| 读操作 | 多 goroutine 并发读，WAL 模式下无锁竞争 |
| 写操作 | 单 writer goroutine（channel 队列串行化） |
| 向量搜索 | 读操作，并发安全 |

```go
// 单 writer goroutine 模式
type SQLiteWriter struct {
    db      *sql.DB
    writeCh chan writeOp
}

func (w *SQLiteWriter) Write(op writeOp) error {
    w.writeCh <- op
    return <-op.errCh
}
```

---

## 5. Tracing System

### 5.1 设计目标

- 全链路追踪：记录每个 Agent 操作的因果关系
- TraceID 贯穿：一次用户请求唯一 TraceID，子操作通过 ParentSpanID 串联
- 实时推送：Span 完成时通过 EventBus 推送，WebUI 实时展示调用树

### 5.2 核心接口

```go
type Tracer interface {
    StartSpan(ctx context.Context, op string) (context.Context, *Span)
    FinishSpan(span *Span, err error)
    GetTrace(traceID string) ([]*Span, error)
    ListTraces(filter TraceFilter) ([]*Trace, error)
}

type TraceContext struct {
    TraceID  string
    SpanID   string
    ParentID string
}
```

### 5.3 Span 数据模型

```go
type Span struct {
    TraceID   string
    SpanID    string
    ParentID  string    // 父 Span ID，实现调用树
    AgentID   string
    Operation string    // "llm_call" | "tool_call" | "hitl_wait" | "agent_run" | ...
    Input     string    // 操作输入（截断存储）
    Output    string    // 操作输出（截断存储）
    StartTime time.Time
    EndTime   time.Time
    Status    string    // "ok" | "error" | "timeout"
    Error     string
    Tags      map[string]string  // 扩展元数据
}
```

### 5.4 Span 操作类型

| Operation | 记录时机 |
|-----------|----------|
| `agent.run` | Agent 整体执行开始/结束 |
| `react.step` | ReAct Loop 每步 |
| `llm.call` | LLM 推理请求 |
| `tool.call` | 工具调用 |
| `hitl.wait` | HITL 挂起等待 |
| `memory.search` | 向量检索 |
| `team.task` | 子任务分配/完成 |

### 5.5 与 EventBus 集成

```go
// Tracer 实现在 Span 完成时向 EventBus 发布事件
func (t *SQLiteTracer) FinishSpan(span *Span, err error) {
    span.EndTime = time.Now()
    if err != nil {
        span.Status = "error"
        span.Error = err.Error()
    } else {
        span.Status = "ok"
    }
    t.store.Save(span)

    // 广播到 EventBus，SSE Server 订阅后推送前端
    t.bus.Publish(Event{
        Topic:   "trace.span",
        TraceID: span.TraceID,
        Payload: span,
    })
}
```

### 5.6 存储

Tracing 数据存储在 SQLite `spans` 表，按 TraceID 索引，实时写入。
WebUI 通过 SSE 实时接收 Span 事件，渲染调用树可视化。
