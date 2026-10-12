# scripts/eval 评测运行器

在隔离环境里跑一组固定任务，度量 suag 的调用次数、token、缓存命中、成本与耗时。
不调用 shell 判定结果（Windows 可跑）。

## 用法

```bash
go build -o tmp/smoke/suag ./cmd/suag

# 假模型：只验证管线，任务成败不作数（需要 python3 与 scripts/ptysmoke/fakellm.py）
go run ./scripts/eval --fake --bin tmp/smoke/suag

# 真实模型（主机）：只读读取该目录的 config.yaml 与 .env
go run ./scripts/eval --model opencode-go/deepseek-v4.1-flash --config-from ~/.suag
```

参数：`--bin`（默认 `tmp/smoke/suag`，Windows 自动补 `.exe`）、`--task`（任务名 glob，默认全部）、
`--model`、`--fake`、`--config-from <dir>`、`--out`（默认 `tmp/eval/<run-id>/`）、`--timeout`（每任务，默认 10m）、
`--suag-args` / `--omp-args` / `--repeat`（见“附加参数、tool_counts 与重复运行”）、
`--probe-prefix` / `--probe-repeat` / `--probe-gap`（见“前缀缓存探针”）。
终端输出汇总表；`<out>/results.jsonl` 每任务一行（成败、checks 明细、答案、调用/token/命中率/成本/工具调用数、
`tool_raw_bytes`/`tool_inline_bytes`，后两项在 `suag stats` 提供之前为 null）。个别任务失败退出码仍为 0，运行器自身出错才非 0。

## 任务格式

`testdata/tasks/<name>/task.yaml` + `fixture/`：

```yaml
name: fix-failing-test
mode: allow-write        # read-only（默认，suag run 默认模式）| allow-write（加 --allow-write）
fake_mode: edit          # 可选，fakellm 的 tool|text|edit，默认 text
prompt: ...              # 单轮；多轮改用 prompts（二选一）
prompts:                 # 多轮：同一会话按顺序发送；answer_contains 判定最后一轮的回答
  - 第一轮 ...
  - 第二轮 ...
generate:                # 可选：运行时生成文件，避免提交大文件
  - path: logs/app.log
    count: 6000
    line: "... {i} ..."  # {i} 为行号
    inserts: [{at: 4217, text: "..."}]   # 替换指定行
checks:
  - {type: file_contains, path: a.go, text: "..."}
  - {type: file_not_contains, path: a.go, text: "..."}
  - {type: file_equals, path: a.txt, text: "..."}   # 忽略末尾换行
  - {type: answer_contains, text: "..."}            # 在最终回答中查找
  - {type: command, args: [go, test, -count=1, ./...], timeout: 5m}   # 在工作区内执行，退出码 0 为通过
timeout: 25m             # 可选：任务级超时，覆盖全局 --timeout（omp 的 --max-time 同步使用）
repo: {ref: 202bfa0}     # 可选：仓库模式（与 fixture/ 二选一），见“仓库模式”
acceptance: acceptance   # 可选：任务目录下的子目录名；agent 结束后、判定 checks 之前，其中文件按相对路径复制进工作区（覆盖同名）
```

`command` check 直接 exec `args`（不经 shell，Windows 可用），工作目录为工作区，`timeout` 默认 5m。
结果里 `text` 为拼接后的命令行，另有 `exit_code`（未能启动或超时为 -1）与 `output`（合并输出的尾部，最多 4000 字节）；
汇总表与其余指标不变。`acceptance` 用于注入 agent 看不到的验收测试：它在 agent 运行结束后才复制，因此 agent 自己的文件与验收文件会一起被 checks 执行；
acceptance 目录不存在或复制失败时该任务判为失败。

每个 fixture 根目录自带 `go.mod`（`module evalfixture/<name>`），复制出去后是独立模块；
`testdata` 与 `tmp/` 都不会进入主模块的 `./...`。

多轮任务：suag 用同一个 `--session-id` 依次 `suag run`；omp 用临时 `--session-dir <out>/<task>/omp-sessions`，
第二轮起加 `--continue`（单轮任务也用临时 session-dir，不再用 `--no-session`）。用量按各轮累加，任一轮失败即停止。
现有多轮任务：`read-then-edit`（先读代码回答，再据此改文件）、`config-two-step`（先找隐患，再修复并补测试）。

## 长编码任务

`long-coding-schedule`：fixture 只有 `go.mod`（`module evalfixture/longcoding`）与说明 README，agent 需从零写出
`schedule/` 包（进程内定时组件，接口与 `pkg/agentcore/schedule` 的公开 API 一致，说明改写自 T9）及其单元测试。
`timeout: 25m`；`acceptance/schedule/zz_acceptance_test.go` 是黑盒验收测试（真实时钟、短时长，含观察者重入不死锁），运行结束后注入；
checks 为 `go vet ./...` 与 `go test -count=1 ./schedule/...`（agent 自己的测试与验收测试一起跑）。
`--fake` 下假模型不会写代码，checks 失败属预期，只用来确认注入与 command 执行的管线。
验收测试对 `pkg/agentcore/schedule` 参考实现应全部通过（把 `schedule.go` 复制到临时工作区的 `schedule/` 下验证）。

## 仓库模式

task.yaml 写 `repo: {ref: <git ref>}`（不要同时有 `fixture/`）时，工作区不是 fixture 副本，而是 eval 启动目录所在 git 仓库的 `<ref>` 快照：
运行器执行 `git archive --format=tar <ref>`（exec，不经 shell），用 Go 的 `archive/tar` 解包到 `<out>/<task>/ws/`（不依赖 tar 命令，Windows 可用），
再在 ws 里 `git init -q`、`git add -A`、`git -c user.name=eval -c user.email=eval@local commit -q -m base`，agent 看到的是只有一个 `base` 提交的干净仓库。
`generate`、`acceptance` 与 checks 照常工作（generate 在 base 提交之前写入）。

**仓库模式只能在 suag 仓库内运行 eval**（从仓库根目录执行，`--tasks-dir` 与 `--bin` 的相对路径也以此为准；`ref` 必须在该仓库里存在）。
示例：`repo-t9-schedule`（ref `202bfa0`，该提交里还没有 `pkg/agentcore/schedule`，agent 需新建；prompt 即 T9 说明，
验收测试注入到 `pkg/agentcore/schedule/zz_acceptance_test.go`，checks 为 `go vet`、`go test -count=1` 与 `go test ./pkg/agentcore/ -run Layering`）：

```bash
go run ./scripts/eval --task repo-t9-schedule --model <model> --config-from ~/.suag --suag-args "--no-project-rules"
```

## 附加参数、tool_counts 与重复运行

- `--suag-args "<args>"` / `--omp-args "<args>"`：按空白切分（无引号处理），suag 附加在 `suag run` 参数末尾（`--prompt` 之后；run 只有 flag，位置无关），
  omp 放在 `omp -p ...` 参数之后、prompt 之前（prompt 是最后的位置参数）。记录在 results.jsonl 的 `agent_args`，表格新增 ARGS 列（超过 24 字符截断，空为 `-`）。
- `--suag-set dotted.key=value`（可重复）：改写临时 SUAG_HOME 的 config.yaml（值按 YAML 标量解析），例如 `--suag-set "tools.shell=C:/Program Files/Git/usr/bin/bash.exe"`；同样记入 `agent_args`。
- `tool_counts`（results.jsonl，`map[工具名]次数`，无工具调用时省略；表格不加列）：suag 取 `suag stats --session <id> --json` 的 `tools[].name/calls`；
  omp 取事件流 `tool_execution_start` 的 `toolName`，没有该事件时退回统计 assistant 消息里 toolCall 块的 `name`。
- `--repeat N`（默认 1）：每个任务依次跑 N 轮。N>1 时输出目录为 `<out>/<task>-r<i>/`（N=1 保持 `<out>/<task>/`），session id 同样带 `-r<i>`；
  results.jsonl 每轮一行，新增 `round`（从 1 起）。表格逐轮显示（TASK 列为 `<task>-r<i>`），每个任务末尾追加一行 `<task> (avg)`：
  RESULT 为通过轮数/总轮数，CALLS、TOOLS、PROMPT、TOKENS(in/out)（未缓存输入/输出）与 TIME 为各轮平均，其余列为 `-`。

```bash
go run ./scripts/eval --fake --task repo-t9-schedule --repeat 2 --suag-args "--no-project-rules"
```

## 历史折叠与思考档位

- suag 默认开启历史折叠（`context.fold_every: 10`、`context.fold_keep: 5`）；不折叠的对照组加 `--suag-set context.fold_every=0`。每轮的 `suag trace` 中 `fold` 行给出折叠点与视图字符数。
- 与 omp 对比时两边要用同一思考档位：omp 以 `--thinking`（或其配置）为准，suag 用 `--suag-args "--thinking high"` 或 provider 配置的 `thinking`；不设时 suag 不发送 `reasoning_effort`，用 provider 默认。

## first_call_uncached

`results.jsonl` 的 `first_call_uncached`（表格 FIRST 列）= 第一个主循环模型调用的未缓存输入 token，度量“冷启动前缀”的代价；取不到为 null。

- suag：取 `suag trace <sid> --json` 中第一个 `status=ok` 且 `attrs.purpose` 不是 `compact` 的 model 事件，
  值 = `input_tokens − cache_read_tokens − cache_write_tokens`（下限 0）。**trace 里的 `input_tokens` 含缓存读写**
  （事件原始口径；`suag stats` 与 ACP/stream-json 报告口径才是已扣除缓存的）。
- omp：第一个 assistant `message_end` 的 `usage.input`（omp 的 input 本身不含缓存）。

## 前缀缓存探针

`--probe-prefix`（仅 suag；与 `--task`/`--tasks-dir` 互斥）测 system+tools 前缀能否跨会话、跨工作区命中提供方缓存。
三组各重复 `--probe-repeat` 次（默认 3），相邻两次请求间隔 `--probe-gap`（默认 5s）。每次重复先发一次不计入统计的“预热”请求建立缓存，
再发被测请求；两者都用全新的 `SUAG_HOME` 与新会话，提示固定为“只回复 ok”，只取首个主循环模型调用的 input / cache_read / cache_write。

- (a) `same`：预热与被测使用同一工作区路径 `<out>/probe/ws`（每次清空重建并 `git init`）。
- (b) `cross`：预热在 `<out>/probe/ws-warm`，被测在 `<out>/probe/ws-<i>`，量化换工作区路径的缓存损失。
- (c) `control`：同 (a)，但预热与被测各设一个不同的随机 `SUAG_DEBUG_PROMPT_NONCE`（隐藏变量，把 `nonce: <值>` 放在系统提示词最前），
  前缀必然不同，作为“零命中”基线。

输出表格列：AVG_CACHE_READ、AVG_UNCACHED（= input − cache_read − cache_write）、READ/FIRST%（cache_read 占首次请求 input 的逐次平均）、
PREFIX~（预热请求的平均 input，含缓存，作为 system+tools 前缀规模的粗估：它还含极短的用户消息，约等于前缀）。
明细写入 `<out>/probe.jsonl`（每次被测请求一行，含 `warm_input`、`error`）。

判定：(a) 的 cache_read 显著高于 (c)，且覆盖首个请求 input 的大部分 → 前缀可跨会话缓存；(a)≈(c) → 不成立；
(b) 相对 (a) 的差值即跨工作区的损失（提示词里工作区路径在末尾，理论上前缀仍可命中）。波动过大时无法判定。
`--fake` 下数值无意义，仅验证管线。

## 隔离原则

- 每个任务把 fixture 复制到 `<out>/<task>/ws/` 并在其中 `git init`，使其自成 git 根，项目记忆后端探测不到上层仓库。
- 使用临时 `SUAG_HOME=<out>/<task>/home/`（同时设 `HOME`，Windows 还有 `USERPROFILE`），不读写真实 `~/.suag`；
  `--config-from` 只读复制 `config.yaml` 与 `.env`。
- 临时 `config.yaml` 强制 `memory.user.backend: files`、`memory.project.backend: files`（旧版本忽略该键）。
- 评测不写任何真实 gofer memory；记忆只落在临时目录，用后删除 `<out>`。
