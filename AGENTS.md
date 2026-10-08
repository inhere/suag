# AGENTS for AI

- 请优先使用中文回复和响应，除非明确要求英文回复
- 如果当前任务有进度跟进文件，需要在完成后更新进度跟进文件
- 多阶段任务，需要在每个阶段完成后更新进度并提交 Git

## 当前项目入口

- 个人 Agent 的构建与命令以 `README.md` 为准；Gofer CLI/ACP 接入见 `docs/usage/gofer-personal-agent.md`。
- 设计与实施依据分别是 `docs/design/2026-10-06-personal-agent-redesign-design.md` 和 `docs/plans/2026-10-06-personal-agent-redesign-plan.md`；旧控制面仅在 `legacy/` 供参考。
- 本项目本轮跟踪使用 `.gofer/tracker` 的 `gofer issue` / `gofer memory`；阶段结果以 issue 状态和实际验证证据为准。

## 正在进行的工作

> **IMPORTANT**: 需要实时更新，正在进行的工作和关键md文档链接；**已完成的需要移除**，不能一直累计，总数不能超过10条。

<!-- PROCESSING WORKS:START -->

- [plan-20261008-222122-6cf0bc1e] 收尾修复批 B1–B10（MCP 重名/凭据环境、SUAG_HOME/.env、Windows DACL、路径、用量持久化、送话/hook 细节、chat 终端模式）+ D1 核心能力演进设计（计划持久化、tool_search、执行观测、存储收敛）；进度以 `gofer plan show` 为准。

<!-- PROCESSING WORKS:END -->

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
