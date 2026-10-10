# agentcore 分层重构复盘（2026-10-10）

依据：[设计 0.3](../design/2026-10-10-agentcore-layering-design.md)、[计划 0.2](2026-10-10-agentcore-layering-plan.md)。范围 T1–T18，全部完成，未 push。

## 结果

- 目标达成：`pkg/agentcore` 成为内核 + 能力层（llm、tools、native、mcp、skills），不依赖 `internal/*`，由 `layering_test.go` 守住；`internal/platform`、`internal/capability` 删除。
- 规模：实施 23 个提交（含 1 个 style），另有 7 个设计/计划文档提交与 1 个 CHANGELOG 提交。行为变更的提交均以全量 `go test -race` 退出码 0 门控；纯移动提交跑 build、vet 与受影响包测试，每个 W 结束再跑全量。
- 验证：容器 race / vet / `make smoke-chat` 9/9；Windows 主机全量 `go test` 通过；`make install` 后版本 commit 等于 HEAD；真实 provider（opencode-go）回归中 `file_grep`、`shell_exec` 均成功。
- 未验证：Claude provider 的 `tool_result.is_error` 映射没有经过真实模型。

## 做得好的

- 先锁行为再改实现：T14 表格测试先于 T11/T15 提交，改造期间期望值未动；双向反向验证（`file_read` 标写、`shell_exec` 标只读）证明表格能抓到回归。
- 移动与行为分开提交：skills/llm/tools/native/mcp 都是“纯 `git mv` + 改导入路径”的独立提交，行为变化（`api.Tool` + `KindOf`、`ServerSettings`、元数据）各自单独提交，便于审查与回滚。
- 删除留痕：`docs/archive/removed-2026-10.md` 记录删除前提交与新旧路径对照，找回只需 `git show`。
- 反向验证覆盖新增护栏：内存存储（改坏 miss 语义即失败）、守卫测试（临时 import `internal/config` 即失败）。

## 问题与原因

| 问题 | 影响 | 原因 | 处理 |
|---|---|---|---|
| 后台命令挂起 80+ 分钟 | T5 提交延迟 | `rg` 不带路径且 stdin 非 tty 时读 stdin，永不结束 | 结束进程；之后所有 `rg` 带路径并 `< /dev/null` |
| 批量脚本的副作用 | 多一次 style 提交 | 插入 import 时放进标准库分组；把局部变量 `tools` 换名时误改了一处字符串 | 514b6ab 修正分组与变量遮蔽；用 diff 检查字符串/注释是否被改 |
| 后台提交可能吞入下一任务的改动 | 无实际影响 | 后台 `git add -u` 与前台编辑并行 | 改为等待后台提交完成后再编辑同一文件 |
| gofer 拒绝 `--max-tokens` | 回归无预算上限 | suag agent 配置没有可读用量（需 ndjson 用量投影） | 去掉参数运行；属于 gofer 配置，留给 gofer 会话 |
| 计划中“tools 测试整体改外部包”只部分需要 | 无 | 设计时假设根包 import tools；实施后仅 `agentcore/testing → tools` 形成测试环 | 只把一致性测试移到 `tools_test` |
| 全量测试偶尔超过 10 分钟 | 等待时间长 | 主机同时跑其他会话的测试，负载高 | 后台运行并按退出码门控提交 |

## 改进

- 后台/非交互 shell 中搜索命令一律带显式路径并重定向 stdin（已记入 gofer memory）。
- 批量改写脚本完成后，固定检查三项：gofmt 分组、标识符遮蔽、字符串与注释是否被误改。
- gofer 中 suag agent 若需要预算控制，应在 gofer 侧为其配置 ndjson 用量来源（提给 gofer 会话，不在本仓库修改）。
- 发布前补一次 Claude provider 的真实工具失败回归。
