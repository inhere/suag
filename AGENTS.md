# AGENTS for AI

- 请优先使用中文回复和响应，除非明确要求英文回复
- 多阶段任务，需要在每个阶段完成后提交 Git；进度用 `gofer plan` / `gofer issue` 跟踪

## 当前项目入口

- 个人 Agent 的构建与命令以 `README.md` 为准；Gofer CLI/ACP 接入见 `docs/usage/gofer-personal-agent.md`。
- 设计与实施依据分别是 `docs/design/2026-10-09-config-layering-chat-design.md` 和 `docs/plans/2026-10-09-config-layering-chat-plan.md`（2026-10-08 的 core-capabilities 与 2026-10-06 的 personal-agent-redesign 设计/计划保留为历史）；配置格式见 `docs/usage/llm-config-v0.md`；旧控制面代码见 `v0.1.0` tag。
- 任务与进度用 `gofer issue`（数据在 `.gofer/tracker`）和 `gofer plan` 跟踪，经验用 `gofer memory`；阶段结果以 issue/plan 状态和实际验证证据为准。

## 项目编码规范

- 关键的方法或逻辑点需要添加注释说明
- 单个go文件最大行数不能操作1000行，超过时需要按情况进行拆分

### git worktree 开发规范

- 使用 git worktree 进行开发时，在 `.worktrees` 目录下创建分支

### 单元测试编写

- 使用 `github.com/gookit/goutil/x/assert` 断言结果
- 同一个方法的多个用例使用 `t.Run()` 包裹

require 断言结果的写法：

```go
Require(t, assert.Eq(t, 1, res.ID))

// Standard assertion
assert.Eq(t, expected, actual)
```
