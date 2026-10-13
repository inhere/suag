# suag -> suga 本地配置迁移脚本

suag 改名为 suga 后，程序本体**不做任何兼容或自动迁移**：它只读新名字下的配置，也不认识
旧的 `~/.suag`、`SUAG_*`、旧的 gofer agent key。本目录的两个脚本由**用户在自己的机器上手工
执行一次**，把旧的本地配置改名成新名字。默认只预览，加参数才真正修改。

- `suag-to-suga.ps1`：Windows（PowerShell 5.1 与 PowerShell 7 都可以）
- `suag-to-suga.sh`：Linux / macOS（POSIX sh + sed/cmp，不依赖 bash 扩展）
- `test.ps1` / `test.sh`：对应脚本的自动化测试，在仓库内的 `tmp/migrate-test*/` 下构造模拟
  环境，结束时删除；不读写真实的 `~/.suag`、`~/.suga`，也不动真实的用户级环境变量

## 用法

先预览（不做任何修改），确认清单无误后再执行：

```powershell
# Windows
powershell -NoProfile -File scripts/migrate/suag-to-suga.ps1
powershell -NoProfile -File scripts/migrate/suag-to-suga.ps1 -Apply
```

```sh
# Linux / macOS
sh scripts/migrate/suag-to-suga.sh
sh scripts/migrate/suag-to-suga.sh --apply
```

## 参数

| PowerShell | sh | 说明 |
| --- | --- | --- |
| `-Apply` | `--apply` | 真正执行修改；不传时只列出将要改的每一项与原因 |
| `-HomeDir <dir>` | `--home-dir <dir>` | 显式的源目录，优先于 `SUAG_HOME` |
| `-Target <dir>` | `--target <dir>` | 目标目录，缺省为源目录同级的 `.suga` |
| `-ProcessList <file>` | `--process-list <file>` | 测试用：每行一个进程名，代替真实的进程检查 |
| `-EnvStore <json>` | — | 测试用：用 JSON 文件（`name -> value` 对象）代替 Windows 用户级环境变量 |
| `-GoferAgents <file>` | `--gofer-agents <file>` | 可选：gofer 的 `agents.yaml` |
| `-h` / `--help` | `-h` / `--help` | 帮助（PowerShell 也可用 `-?` 或 `Get-Help scripts/migrate/suag-to-suga.ps1`） |

源目录的顺序是 `-HomeDir`/`--home-dir` > `SUAG_HOME` > `$HOME/.suag`；目标目录缺省是源目录
同级的 `.suga`（默认即 `$HOME/.suga`）。

## 迁移内容

1. **目录改名**：源目录整体改名为目标目录（同盘是 rename，不回退数据）。
2. **`config.yaml`**：
   - agent 名：`--agent, suag`（以及 `--agent suag`、`--agent=suag`）→ `suga`，`suag-acp` 同理；
   - 指向 `.suag` 的路径（如 `~/.suag`、`/home/me/.suag/memory`）→ `.suga`；
   - `SUAG_` 开头的变量名 → `SUGA_`。
3. **`.env`**：只把赋值语句左侧以 `SUAG_` 开头的**变量名**改成 `SUGA_`，右侧的值一个字都不动。
   脚本的任何输出（stdout、stderr、预览、错误信息、备份内容）都不含 `.env` 的值，只打印变量名。
   注释行里出现的 `SUAG_` 不当作变量名，不会改。
4. **用户级环境变量**：
   - Windows：把用户级 `SUAG_*` 改名为 `SUGA_*`（值不变，不打印值）。新名字已存在时跳过并提示，
     交给你手工确认。
   - Linux / macOS：**不改任何文件**，只列出 `~/.bashrc`、`~/.zshrc`、`~/.profile` 中出现
     `SUAG_` 的行号，由你手工改（只打印行号与文件名，不打印行内容）。
5. **可选 `-GoferAgents <agents.yaml>`**：agent key `suag` / `suag-acp` → `suga` / `suga-acp`；
   `command: suag` → `command: suga`；`env` 里的 `SUAG_` 变量名 → `SUGA_`。

每个被改动的文件在改动前备份为 `<文件名>.bak-<时间戳>`，例如
`agents.yaml.bak-20261013T2200`。备份名不以 `.yaml` 结尾，避免被 gofer 当成
`config.d/*.yaml` 加载；sh 版用 `cp -p` 保留原权限（`.env` 这类 600 的文件仍是 600）。

## 前提检查

不满足就停止并说明，且不做任何修改：

- 有 `suag` 或 `suga` 进程在运行（gofer 拉起的任务、chat、acp、trace web 都算）。Windows 用
  `Get-Process -Name suag,suga`，Linux/macOS 用 `pgrep -x suag` / `pgrep -x suga`（没有 pgrep
  时用 `ps` 兜底）。
- 目标目录已存在且非空。

源目录不存在时提示无需迁移并返回 0。任一步失败会立即停止，并在输出末尾列出**已完成的步骤**与
**全部备份位置**，方便手工回退。

## 执行之后

1. 重开终端，让新的环境变量生效。
2. Windows 下改过用户级环境变量的话，需要**重启 gofer server**（只 reload 不够，server 进程的
   环境不会更新）。Linux/macOS 下按提示的行号手工改完 shell 配置文件后同样需要重启 server。
3. 用了 `-GoferAgents` 时，在 gofer 侧执行配置重载。
4. 脚本不改 gofer 数据库，不改工作区项目目录，也不删除旧二进制。

## 回退

- 目录：把目标目录改回原名（如 `~/.suga` → `~/.suag`）。
- 文件：用 `<文件名>.bak-<时间戳>` 覆盖回去，再删除自己改名失败的残留文件。
- 环境变量：把 `SUGA_*` 改回 `SUAG_*`（脚本不记录也不打印值，请按需手工恢复）。

脚本每一步失败时都会把这些位置打印出来。

## 限制

- `-GoferAgents` 只改 agent key、`command:` 和 `env` 的变量名，**不改 `env` 的值**：如果那里写了
  指向 `~/.suag` 的路径，需要自己改成 `~/.suga`。
- `config.yaml` 里只改上述三类内容，别的 `suag` 字样（注释、User-Agent 等）原样保留。
- 只处理默认的 `.env` / `config.yaml` 文件名；`SUAG_CONFIG` 指向的其它配置文件请手工处理。
- Windows 的 Git for Windows 只能表示 POSIX 权限的只读位，`test.sh` 里的 `.env` 600 权限断言会
  显示 `SKIP`；在 Linux / macOS 上会真正检查。

## 测试

```powershell
powershell -NoProfile -File scripts/migrate/test.ps1
```

```sh
sh scripts/migrate/test.sh
```

测试覆盖：预览模式零修改、执行后的改名与替换结果、`.env` 的值不外泄（预览与执行都检查）、
备份文件命名与权限、目标目录非空时拒绝且零修改、注入“有 suag 进程”时拒绝且零修改、源目录不存
在时返回 0、`-EnvStore` 的改名、以及“没有碰真实的 `~/.suag` / `~/.suga` / 用户级环境变量”。
