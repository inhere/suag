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
`--model`、`--fake`、`--config-from <dir>`、`--out`（默认 `tmp/eval/<run-id>/`）、`--timeout`（每任务，默认 10m）。
终端输出汇总表；`<out>/results.jsonl` 每任务一行（成败、checks 明细、答案、调用/token/命中率/成本/工具调用数、
`tool_raw_bytes`/`tool_inline_bytes`，后两项在 `suag stats` 提供之前为 null）。个别任务失败退出码仍为 0，运行器自身出错才非 0。

## 任务格式

`testdata/tasks/<name>/task.yaml` + `fixture/`：

```yaml
name: fix-failing-test
mode: allow-write        # read-only（默认，suag run 默认模式）| allow-write（加 --allow-write）
fake_mode: edit          # 可选，fakellm 的 tool|text|edit，默认 text
prompt: ...
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
```

每个 fixture 根目录自带 `go.mod`（`module evalfixture/<name>`），复制出去后是独立模块；
`testdata` 与 `tmp/` 都不会进入主模块的 `./...`。

## 隔离原则

- 每个任务把 fixture 复制到 `<out>/<task>/ws/` 并在其中 `git init`，使其自成 git 根，项目记忆后端探测不到上层仓库。
- 使用临时 `SUAG_HOME=<out>/<task>/home/`（同时设 `HOME`，Windows 还有 `USERPROFILE`），不读写真实 `~/.suag`；
  `--config-from` 只读复制 `config.yaml` 与 `.env`。
- 临时 `config.yaml` 强制 `memory.user.backend: files`、`memory.project.backend: files`（旧版本忽略该键）。
- 评测不写任何真实 gofer memory；记忆只落在临时目录，用后删除 `<out>`。
