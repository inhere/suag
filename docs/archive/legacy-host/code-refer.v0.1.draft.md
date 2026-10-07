
## 一些代码参考

```go
type Agent struct {
    llm      LLMClient
    memory   *Memory
    tools    *ToolRegistry
    maxSteps int
}

func (a *Agent) Run(ctx context.Context, task string) error {
    a.memory.AddMessage("user", task)

    for step := 0; step < a.maxSteps; step++ {
        // 1. 从 LLM 获取回复
        resp, err := a.llm.CompleteWithRetry(ctx, a.memory.GetContext())
        if err != nil {
            return err
        }

        if resp.ToolCall != nil {
            result, err := a.tools.Execute(ctx, resp.ToolCall)
            if err != nil {
                a.memory.AddMessage("system", "tool error: "+err.Error())
                continue
            }
            a.memory.AddMessage("tool", result)
            continue
        }
 
        a.memory.AddMessage("assistant", resp)
        if resp.IsComplete {
            return nil
        }
    }

    return errors.New("max steps reached")
}
```

llm client:

```go
func (c *LLMClient) CompleteWithRetry(ctx context.Context, prompt string) (*Response, error) {
    var lastErr error

    for attempt := 0; attempt < 3; attempt++ {
        resp, err := c.Complete(ctx, prompt)
        if err == nil {
            return resp, nil
        }

        lastErr = err

        backoff := time.Duration(1<<attempt) * time.Second

        select {
        case <-time.After(backoff):
        case <-ctx.Done():
            return nil, ctx.Err()
        }
    }

    return nil, fmt.Errorf("failed after retries: %w", lastErr)
}
```

context memory:

```go
type Memory struct {
    messages []Message
}

type Message struct {
    Role   string
    Content string
}

func (m *Memory) AddMessage(role, content string) {
    m.messages = append(m.messages, Message{Role: role, Content: content})
}

func (m *Memory) GetContext() string {
    if len(m.messages) <= 10 {
        return m.formatAll()
    }

    summary := m.getSummary()
    recent := m.messages[len(m.messages)-10:]
    return summary + "\n" + formatMessages(recent)
}
```

tool registry:

```go
type Tool struct {
    Name        string
    Description string
    Parameters  map[string]string
    Execute     func(context.Context, map[string]interface{}) (string, error)
}

type ToolRegistry struct {
    tools map[string]*Tool
    mu    sync.RWMutex
}

func (r *ToolRegistry) Register(tool *Tool) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.tools[tool.Name] = tool
}

func (r *ToolRegistry) GetSchema() []map[string]interface{} {
    r.mu.RLock()
    defer r.mu.RUnlock()

    schemas := make([]map[string]interface{}, 0, len(r.tools))
    for _, t := range r.tools {
        schemas = append(schemas, map[string]interface{}{
            "name":        t.Name,
            "description": t.Description,
            "parameters":  t.Parameters,
        })
    }
    return schemas
}
```

Mock LLM：

```go
type MockLLMClient struct {
    responses []Response
    callCount int
}

func (m *MockLLM) Complete(ctx context.Context, prompt string) (*Response, error) {
    if m.callCount >= len(m.responses) {
        return nil, errors.New("no more responses")
    }
    resp := m.responses[m.callCount]
    m.callCount++
    return &resp, nil
}
```
