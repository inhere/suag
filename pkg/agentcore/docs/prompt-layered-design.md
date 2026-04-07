# Agentcore Prompt Layered Builder Design

这份设计文档描述 `pkg/agentcore/prompt` 下一轮默认组合能力的目标与边界。

这轮的目标不是设计一套 prompt policy engine，而是为 `agentcore` 提供一个第一方、可直接使用、边界清晰的默认 `PromptBuilder` 组合，让外部使用者不需要自己手写 base prompt、run metadata 与附加段落的拼接逻辑。

---

## 1. 背景

当前 `agentcore` 已经具备：

- `PromptBuilder` 公共接口
- `prompt.StaticBuilder`
- 第一方 `memory/` 与 `context/` 默认能力

但默认 prompt story 仍然偏薄：

- 目前只有完全静态的 `StaticBuilder`
- 一旦调用方想在 system prompt 中插入 run metadata 或附加段落，就需要自己手拼字符串

这意味着：

- `agentcore` 的默认 memory 与 context story 已经在逐步成形
- 但 prompt 仍然停留在最基础的静态字符串层面

因此，这一轮的目标是：

- 在 `pkg/agentcore/prompt` 提供一个第一方 layered builder
- 支持静态 base prompt
- 支持可选 run metadata 注入
- 支持按顺序拼接附加段落
- 不引入新的 prompt DSL 或 provider-specific prompt system

---

## 2. 目标

这一轮明确要达成的目标：

- 提供 `prompt.Layered(...)` 第一方默认组合入口
- 支持 3 个输入面
  - `Base`
  - `Metadata`
  - `Sections`
- 输出稳定、可预测、易测试的 system prompt 文本
- 保留现有 `StaticBuilder` 兼容
- 不改变 `agentcore.PromptBuilder` 公共接口

---

## 3. 非目标

这一轮明确不做：

- prompt template 语法
- provider-specific prompt shaping
- 自动把 memory/context 内容注入 prompt
- hook-aware prompt policy
- metadata 白名单或复杂格式化
- section 条件开关系统
- prompt DSL / policy engine

换句话说，这一轮做的是第一方 layered prompt builder，而不是 prompt framework。

---

## 4. 设计原则

### 4.1 Prompt Stays Textual

`PromptBuilder` 这一轮仍然只生成一个 system prompt 字符串。

它不变成消息列表，也不跨到 `ContextBuilder` 责任范围。

### 4.2 Stable Output Beats Rich Configuration

输出规则要尽量固定：

- 固定顺序
- 固定 metadata block 标题
- 固定段落拼接方式

比起开放很多配置项，这更适合作为第一方默认 builder。

### 4.3 Keep StaticBuilder Compatible

现有 `StaticBuilder` 不删除、不重命名、不改变行为。

`Layered(...)` 是新增加的推荐入口，而不是对 `StaticBuilder` 的破坏式替换。

---

## 5. 输入面

### 5.1 `Base`

这是主体 prompt 文本。

要求：

- 可以为空
- 放在输出最前面
- 不做自动加工

### 5.2 `Metadata`

这是可选的 run metadata 注入能力。

建议配置形态：

```go
type MetadataConfig struct {
    Enabled bool
}
```

行为：

- `Enabled=false` 时不注入
- `Enabled=true` 时读取 `RunState.Metadata`
- 生成一个稳定的 metadata 文本块

### 5.3 `Sections`

这是附加段落数组。

行为：

- 按顺序追加
- 空字符串或纯空白段落跳过
- 不做内容解释

---

## 6. API 草图

建议公开 API 保持简单：

```go
package prompt

type MetadataConfig struct {
    Enabled bool
}

type Config struct {
    Base     string
    Metadata MetadataConfig
    Sections []string
}

func Layered(cfg Config) agentcore.PromptBuilder
```

内部可以用私有实现：

```go
type layeredBuilder struct {
    config Config
}
```

这里刻意不强调 exported builder struct，是因为这轮重点是第一方默认组合，而不是 builder inheritance 层。

---

## 7. 输出规则

### 7.1 输出顺序

固定顺序：

1. `Base`
2. metadata block
3. `Sections...`

这个顺序不开放配置。

### 7.2 段落拼接

各段之间用双换行连接：

```text
Base

[Run metadata]
key: value

Section 1

Section 2
```

### 7.3 空值规则

- `Base` 允许为空
- 空 sections 跳过
- metadata block 为空时不输出
- 如果所有部分都为空，返回空字符串

---

## 8. Metadata Block 规则

建议 metadata block 固定为：

- title: `[Run metadata]`
- key 按字典序排序
- 每行格式：`key: value`
- value 使用 `fmt.Sprint(...)` 简单字符串化
- 只处理顶层 `map[string]any`

例如：

```go
Metadata: map[string]any{
    "mode": "resume",
    "project": "agentcore",
}
```

输出：

```text
[Run metadata]
mode: resume
project: agentcore
```

这一轮明确不做：

- JSON pretty-print
- YAML 输出
- 嵌套 flatten
- 自定义 formatter

---

## 9. StaticBuilder 兼容策略

这一轮保留：

- `prompt.StaticBuilder`

原因：

- 避免破坏现有引用
- 保持最简单场景的入口
- 让迁移是渐进的

新的推荐路径是：

- 只需要固定字符串 -> `StaticBuilder`
- 需要 base + metadata + sections -> `Layered(...)`

---

## 10. 测试矩阵

### 10.1 基础拼接

至少覆盖：

- 只有 `Base` 时只输出 base
- `Base + Sections` 时按顺序双换行拼接
- 空 section 被跳过

### 10.2 Metadata 注入

至少覆盖：

- `Enabled=false` 时不注入 metadata
- `Enabled=true` 且 metadata 存在时注入 block
- metadata key 稳定排序
- metadata 为空时不输出 block

### 10.3 空值边界

至少覆盖：

- 所有输入为空时返回空字符串
- `Base` 为空但 sections 非空时仍能正确输出
- `Base` 为空但 metadata 存在时仍能正确输出

### 10.4 兼容性

至少覆盖：

- `StaticBuilder` 行为不变
- `Layered(...)` 不影响现有 runtime 默认路径

---

## 11. 文档与示例

这一轮如果落地实现，建议同步更新：

- [pkg/agentcore/README.md](../README.md)
- [pkg/agentcore/docs/getting-started.md](./getting-started.md)
- [pkg/agentcore/docs/api.md](./api.md)

必要时新增或更新 example，展示：

- `StaticBuilder`
- `Layered(...)`

---

## 12. 完成定义

这一轮完成时应达到：

- `prompt/` 除了 `StaticBuilder` 外，还有一个第一方默认组合入口 `Layered(...)`
- 用户不需要自己手拼 base + metadata + sections
- 输出规则稳定、可预测、易测试
- 文档能清楚说明何时使用 `StaticBuilder`、何时使用 `Layered(...)`

如果达成这些目标，`agentcore` 会继续沿着 `v0.3 Useful Default Implementations` 推进，在不做重 prompt framework 的前提下，把默认嵌入体验再往前推一小步。
