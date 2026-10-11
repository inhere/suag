# 状态、记忆与可扩展核心（第一期）复盘（2026-10-11）

依据：[设计 0.3](../design/2026-10-10-state-memory-extensibility-design.md)、[计划 0.3](2026-10-11-state-memory-extensibility-plan.md)。范围 T0–T17，全部完成，未 push。

## 结果

- 交付：评测与成本（W0）、底账检索与输出治理（W1）、记忆 v1 与三种后端（W2）、exec 协议与 `suag manifest`（W3）、文档（W4）。用户实施中追加的“shell 大输出置污染”（计划 0.3）一并完成。
- 规模：25 个提交（17 feat、2 refactor、2 fix、2 docs、1 test、1 chore），Go 改动 132 个文件、+13475 行，其中测试 48 个文件、+6027 行。
- 验证：容器 race/vet、`make smoke-chat`（26/26、9/9、新增 chat_memory 17/17）；主机 Windows `go test` 28 包、staticcheck 0；主机 `make install` 后用真实库正常；真实会话 5 步全部符合设计（写记忆、47 KB 输出外置后下一请求仅 5.6k token、recall、session_search、污染后写记忆被拒）；真实模型评测 6/6 通过（`tmp/eval/real-20261011b`）。
- 未验证：真实 MCP 大输出、真实 ACP 客户端中的记忆确认、gofer 后端写入（未授权写真实 gofer memory）。
- 代价：新工具 schema 与记忆索引使每次请求输入增加约 1.2k token（2608 → 3822，最小提示）。

## 做得好的

- 评审前移挡住了大问题：设计合并评审 5 条、计划合并评审 9 条 CORE_BLOCKING 都在动手前改掉（污染可被跨回合绕过、快照单位错误、外置路径可越界、评测会写真实 gofer memory、成本重复计缓存等），实施中没有一处需要回到设计。
- 按任务分派子 agent 并按难度选挡位（机械改名/文档用 sonnet medium，跨包与安全相关用 opus high），每个任务回来后由复核者做至少一次反向验证（改坏实现 → 测试必须失败），共 16 处，全部有效。
- 真实环境验证没有省：主机安装后用真实模型最小提示探测，当场发现 schema 问题；“新旧二进制对比”把原因从“上游故障”与“我们的改动”中快速分开。
- 共享工作区纪律：他人的 `issues.jsonl`、计划文档全程未暂存；子 agent 只用假 gofer，复核时又查了真实 gofer memory 确认没有写入。

## 问题与原因

| 问题 | 影响 | 原因 | 处理 |
|---|---|---|---|
| `suag_manifest` 空 `properties` 让 opencode-go 拒绝整次请求 | 新版对所有请求 400，首次真实评测 6/6 失败 | 网关把空 `properties` 转成 null；单测用假 provider，真实调用到 T17 才第一次发生 | 9082422 加可选参数 + `TestToolSchemasDeclareProperties` 守卫（已反向验证）；记入 gofer memory `tool-schema-nonempty-properties` |
| 外置输出元数据 sha256 与文件不一致 | 无实际影响（复核时发现） | 包装层按原文计算、外置文件是脱敏后内容 | 6276890：`OutputConfig.Sanitize` 先于 hash 与外置，测试比对文件 hash |
| staticcheck（ST1018、U1000）到 T17 才在主机发现 | 多一次修正提交 | 子 agent 门控只有 vet，没有 CI 同款 staticcheck——与 0.9.0 复盘的同一教训重复 | 611e970；见改进 1 |
| `sessions.cwd` 从未写入 | “按工作区检索”原本不可能生效 | 既有缺口，T9 实现时才暴露 | T9 补写新会话与旧会话 |
| 评测任务一次都没触发外置 | 输出治理只在真实会话检查中得到真实验证 | 任务输出都小于阈值，模型偏好 grep 等精确工具 | 列为后续：增加强制大输出的任务 |
| 子 agent 把 TMPDIR 指到仓库内导致两个既有测试失败 | 一次返工 | “不写系统临时目录”与“测试要求临时目录不在 git 仓库内”冲突 | 全量测试保持默认 TMPDIR，自有产物仍放 `tmp/` |
| `issues.jsonl` 无法提交 | 本期新建的 suag-3m48、suag-frgt 未入库 | 同一文件里有他人的改动，规范禁止部分暂存 | 等对方提交后再带上 |
| `suag.exe.old` 直接运行无输出 | 一次误判的探测 | Windows 不执行非 `.exe` 后缀 | 复制为 `.exe` 后再比对 |

## 改进

1. 子 agent 的门控加上主机 staticcheck（`gofer job run ... go run honnef.co/go/tools/cmd/staticcheck@latest ./...`），至少在每个波次结束跑一次，不等到最终验证。
2. 新增或修改工具 schema 的任务，完成即在主机用真实模型跑一次最小提示（`suag run --prompt 'reply with: hi'`，隔离的 SUAG_HOME），而不是只在最终验证时调用真实模型。
3. 评测任务补“必须产生大输出”的场景（例如要求运行固定命令并基于第 N 行作答），让输出治理有量化数据。
4. 规范层面：“临时产物放项目 tmp/”与“测试临时目录不得在 git 仓库内”需要写清边界——前者约束 agent 自己的产物，`go test` 的 `t.TempDir` 不受影响。
