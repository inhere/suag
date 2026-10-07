# AGENTS for AI

- 请优先使用中文回复和响应，除非明确要求英文回复
- 如果当前任务有进度跟进文件，需要在完成后更新进度跟进文件
- 多阶段任务，需要在每个阶段完成后更新进度并提交 Git

## 正在进行的工作

> **IMPORTANT**: 需要实时更新，正在进行的工作和关键md文档链接；**已完成的需要移除**，不能一直累计，总数不能超过10条。

<!-- PROCESSING WORKS:START -->

- 当前主线是个人 Agent；设计见 `docs/design/2026-10-06-personal-agent-redesign-design.md`，已批准实施计划见 `docs/plans/2026-10-06-personal-agent-redesign-plan.md`。
- CLI/ACP 的 Gofer 接入见 `docs/usage/gofer-personal-agent.md`。T01–T06 已验收；T07 隔离 legacy、T08 资料与质量收口，阶段状态以 `.gofer/tracker` 的 `suag-ha0z` 子 issue 为准。
- 本次改造用 `gofer issue` / `gofer memory` 跟踪，不使用 `bd`。

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
