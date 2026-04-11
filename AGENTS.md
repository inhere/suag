# AGENTS for AI

- 请优先使用中文回复和响应，除非明确要求英文回复
- 如果当前任务有进度跟进文件，需要在完成后更新进度跟进文件
- 多阶段任务，需要在每个阶段完成后更新进度并提交 Git

## 正在进行的工作

<!-- PROCESSING WORKS:START -->
<!-- 需要实时更新: 正在进行的工作和关键md文档链接 -->
- Axon v0 自用主线规划与阶段架构设计进行中，设计总纲见 `docs/v0-self-host-design.md`
- Axon v0 self-host 基础骨架实施计划进行中，计划文档见 `docs/superpowers/plans/2026-04-10-axon-v0-self-host-foundation-plan.md`
- internal 目录结构调整已完成首轮收敛，迁移映射见 `docs/internal-code-status.md`
- `internal/host/session` 首批基础骨架进行中，文件拆分说明见 `docs/superpowers/plans/2026-04-10-host-session-layout-note.md`

<!-- PROCESSING WORKS:END -->

## git worktree 开发规范

- 使用 git worktree 进行开发时，在 `.worktrees` 目录下创建分支

## 项目规范限制

- 关键的方法或逻辑点需要添加注释说明
- 单个go文件最大行数不能操作1000行，超过时需要按情况进行拆分
