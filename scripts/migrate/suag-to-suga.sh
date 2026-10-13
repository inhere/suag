#!/bin/sh
# 把旧名字 suag 的本地配置迁移为新名字 suga（默认只预览，加 --apply 才执行）。
#
# 程序本体不做任何兼容或自动迁移；本脚本供用户在自己的机器上手工执行一次。
#
# 源目录：--home-dir > $SUAG_HOME > $HOME/.suag
# 目标目录：--target > 源目录同级的 .suga
#
# 步骤：
#   1. 源目录整体改名为目标目录
#   2. config.yaml：agent 名（如 `--agent, suag`）、指向 .suag 的路径、SUAG_ 变量名
#   3. .env：只改以 SUAG_ 开头的变量名，值原样保留（任何输出都不含 .env 的值）
#   4. 用户级环境变量：本脚本不改任何文件，只列出需要手工修改的 shell 配置文件与行号
#   5. 可选 --gofer-agents <agents.yaml>：agent key、command、env 里的名字
#
# 被改动的文件先备份为 <文件名>.bak-<yyyyMMddTHHmm>（cp -p 保留权限）；备份名不以
# .yaml 结尾，避免被 gofer 当成 config.d/*.yaml 加载。
#
# 前提检查（不满足就停止并说明）：有 suag / suga 进程在跑；目标目录存在且非空；
# 源目录不存在时提示无需迁移并返回 0。任一步失败立即停止，并在末尾列出已完成的
# 步骤和全部备份位置，便于手工回退。

set -u

stamp=$(date +%Y%m%dT%H%M)
apply=0
home_dir=""
target=""
process_list=""
gofer_agents=""

done_steps=""
backups=""
rename_from=""
rename_to=""

usage() {
    cat <<'EOF'
用法: sh suag-to-suga.sh [选项]

  --apply                 真正执行修改；不传时只列出将要改的每一项（预览）
  --home-dir <dir>        显式的源目录，优先于 $SUAG_HOME
  --target <dir>          目标目录，缺省为源目录同级的 .suga
  --process-list <file>   测试用：文本文件，每行一个进程名，代替真实的进程检查
  --gofer-agents <file>   可选：gofer 的 agents.yaml，改名其中的 agent key / command / env
  -h, --help              显示本帮助
EOF
}

say() { printf '%s\n' "$*"; }
sayerr() { printf '%s\n' "$*" >&2; }

add_done() { done_steps="${done_steps}${done_steps:+
}$1"; }
add_backup() { backups="${backups}${backups:+
}$1"; }

# 已执行模式打印“已执行”，预览模式打印“将执行”
show_step() {
    if [ "$apply" -eq 1 ]; then
        printf '  [已执行] %s\n' "$1"
    else
        printf '  [将执行] %s\n' "$1"
    fi
}

show_summary() {
    say ""
    say "已完成的步骤："
    if [ -z "$done_steps" ]; then say "  （无）"; else printf '%s\n' "$done_steps" | sed 's/^/  - /'; fi
    say "备份位置："
    if [ -z "$backups" ]; then say "  （无）"; else printf '%s\n' "$backups" | sed 's/^/  - /'; fi
    say "手工回退方法："
    if [ -n "$rename_from" ]; then say "  - 把目录 $rename_to 改回 $rename_from"; fi
    if [ -n "$backups" ]; then
        printf '%s\n' "$backups" | while IFS= read -r b; do
            orig=$(printf '%s\n' "$b" | sed 's/\.bak-[0-9]\{8\}T[0-9]\{4\}$//')
            say "  - 用 $b 覆盖 $orig"
        done
    fi
    say "  - 用户级环境变量：把 SUGA_* 改回 SUAG_*（请按需手工恢复）"
}

# 前提检查失败或某一步出错：说明原因、列出已完成的内容后退出 1
fail() {
    sayerr "错误：$1"
    show_summary
    exit 1
}

# ---------- 逐行替换 ----------

# 只打印某个 sed 表达式会改动的行；names_only=yes 时只打印 .env 的变量名
show_rule_col() {
    file=$1
    display=$2
    label=$3
    expr=$4
    names_only=$5
    n=0
    while IFS= read -r line || [ -n "$line" ]; do
        n=$((n + 1))
        new=$(printf '%s\n' "$line" | sed -e "$expr")
        [ "$new" = "$line" ] && continue
        if [ "$names_only" = "yes" ]; then
            name=$(printf '%s\n' "$line" | sed -n 's/^[[:space:]]*\(export[[:space:]][[:space:]]*\)\{0,1\}\(SUAG_[A-Za-z0-9_]*\)[[:space:]]*=.*$/\2/p')
            newname=$(printf '%s\n' "$name" | sed 's/^SUAG_/SUGA_/')
            printf '  [%s] %s:%d 变量名 %s -> %s（值原样保留，不打印）\n' "$verb" "$display" "$n" "$name" "$newname"
        else
            printf '  [%s] %s:%d %s -> %s（%s）\n' "$verb" "$display" "$n" "$line" "$new" "$label"
        fi
    done <"$file"
}

# 备份文件（cp -p 保留权限）；备份名不以 .yaml 结尾
backup_file() {
    bak="$1.bak-$stamp"
    cp -p "$1" "$bak" || return 1
    add_backup "$bak"
    printf '  已备份：%s\n' "$bak"
    return 0
}

# 用 sed 改写文件：先写到临时文件，再 cat 回原文件（保留原 inode 与权限）
apply_file() {
    file=$1
    shift
    tmp="$file.migrate-tmp.$$"
    if ! sed "$@" "$file" >"$tmp"; then rm -f "$tmp"; return 1; fi
    if ! cat "$tmp" >"$file"; then rm -f "$tmp"; return 1; fi
    rm -f "$tmp"
    return 0
}

# ---------- 前提检查 ----------

# 正在运行的进程名（空格分隔）；--process-list 给的文件以每行一个进程名代替真实检查
find_processes() {
    if [ -n "$process_list" ]; then
        [ -f "$process_list" ] || return 0
        tr 'A-Z' 'a-z' <"$process_list" |
            sed 's/^[[:space:]]*//; s/[[:space:]]*$//; s/\.exe$//' |
            grep -x -e suag -e suga | tr '\n' ' '
        return 0
    fi
    if command -v pgrep >/dev/null 2>&1; then
        for name in suag suga; do
            if pgrep -x "$name" >/dev/null 2>&1; then printf '%s ' "$name"; fi
        done
        return 0
    fi
    # 没有 pgrep 时的兜底：从 ps 的进程名里找
    list=$(ps -eo comm= 2>/dev/null || ps -A -o comm= 2>/dev/null || true)
    for name in suag suga; do
        if printf '%s\n' "$list" | sed 's#.*/##' | grep -qx "$name"; then printf '%s ' "$name"; fi
    done
    return 0
}

# ---------- 步骤实现 ----------

step_rename_dir() {
    if [ -n "$rename_from" ]; then return 0; fi
    if [ -d "$dst" ]; then
        # 只在目标目录为空时接管它（前提检查已经确认过）
        rmdir "$dst" || return 1
    fi
    mv "$src" "$dst" || return 1
    rename_from="$src"
    rename_to="$dst"
    add_done "1. 目录改名：$src -> $dst"
    return 0
}

# 替换后与原文件完全一致时 cmp 返回 0
file_unchanged() {
    cmp -s - "$1"
}

step_config() {
    file="$1"
    if [ ! -f "$file" ]; then say "  config.yaml：不存在，跳过"; return 0; fi
    if sed -e "$agent_sed" -e "$path_sed" -e "$var_sed" "$file" | file_unchanged "$file"; then
        say "  config.yaml：无需修改"
        return 0
    fi
    if [ "$apply" -eq 1 ]; then
        backup_file "$file" || return 1
        apply_file "$file" -e "$agent_sed" -e "$path_sed" -e "$var_sed" || return 1
        printf '  [已执行] %s：已改写\n' "$(basename "$file")"
    else
        show_rule_col "$file" "config.yaml" "agent 名（--agent, suag）" "$agent_sed" no
        show_rule_col "$file" "config.yaml" "指向 .suag 的路径" "$path_sed" no
        show_rule_col "$file" "config.yaml" "SUAG_ 变量名" "$var_sed" no
    fi
    return 0
}

step_env() {
    file="$1"
    if [ ! -f "$file" ]; then say "  .env：不存在，跳过"; return 0; fi
    if sed -e "$env_sed" "$file" | file_unchanged "$file"; then
        say "  .env：无需修改"
        return 0
    fi
    if [ "$apply" -eq 1 ]; then
        backup_file "$file" || return 1
        apply_file "$file" -e "$env_sed" || return 1
        printf '  [已执行] .env：已改写变量名（值原样保留）\n'
    else
        show_rule_col "$file" ".env" "SUAG_ 变量名" "$env_sed" yes
    fi
    return 0
}

step_agents() {
    file=$1
    if [ ! -f "$file" ]; then
        sayerr "错误：agents.yaml 不存在：$file"
        return 1
    fi
    if sed -e "$key_sed" -e "$cmd_sed" -e "$var_sed" "$file" | file_unchanged "$file"; then
        say "  $(basename "$file")：无需修改"
        return 0
    fi
    if [ "$apply" -eq 1 ]; then
        backup_file "$file" || return 1
        apply_file "$file" -e "$key_sed" -e "$cmd_sed" -e "$var_sed" || return 1
        printf '  [已执行] %s：已改写\n' "$(basename "$file")"
    else
        show_rule_col "$file" "$(basename "$file")" "agent key" "$key_sed" no
        show_rule_col "$file" "$(basename "$file")" "command: suag" "$cmd_sed" no
        show_rule_col "$file" "$(basename "$file")" "SUAG_ 变量名" "$var_sed" no
    fi
    return 0
}

# 步骤 4：只列出需要手工修改的 shell 配置文件与行号，不打印行内容，也不改文件
step_shell_configs() {
    say "  4. 用户级环境变量：本脚本不改任何文件，请手工修改下列位置"
    found=0
    for f in "$HOME/.bashrc" "$HOME/.zshrc" "$HOME/.profile"; do
        [ -f "$f" ] || continue
        lines=$(grep -n '^[[:space:]]*\(export[[:space:]][[:space:]]*\)\{0,1\}SUAG_' "$f" | cut -d: -f1 | tr '\n' ' ')
        [ -n "$lines" ] || continue
        found=1
        printf '    %s 中带 SUAG_ 的行号：%s\n' "$f" "$lines"
    done
    if [ "$found" -eq 0 ]; then
        say "    （这三个文件里没有 SUAG_ 开头的变量）"
    fi
    return 0
}

# ---------- 参数解析 ----------

while [ $# -gt 0 ]; do
    case "$1" in
        --apply) apply=1 ;;
        --home-dir) home_dir=$2; shift ;;
        --home-dir=*) home_dir=${1#*=} ;;
        --target) target=$2; shift ;;
        --target=*) target=${1#*=} ;;
        --process-list) process_list=$2; shift ;;
        --process-list=*) process_list=${1#*=} ;;
        --gofer-agents) gofer_agents=$2; shift ;;
        --gofer-agents=*) gofer_agents=${1#*=} ;;
        -h | --help)
            usage
            exit 0
            ;;
        *)
            sayerr "未知参数：$1"
            usage >&2
            exit 2
            ;;
    esac
    shift
done

if [ "$apply" -eq 1 ]; then verb="已执行"; else verb="将执行"; fi

# 规则：agent 名（suag / suag-acp）、指向 .suag 的路径、SUAG_ 变量名
agent_sed='s/\(--agent[,[:space:]=]\{1,3\}\)suag\([^A-Za-z0-9_]\|$\)/\1suga\2/g'
path_sed='s/\.suag\([^A-Za-z0-9_]\|$\)/.suga\1/g'
var_sed='s/^SUAG_/SUGA_/; s/\([^A-Za-z0-9_]\)SUAG_/\1SUGA_/g'
# .env：只改赋值语句左侧的变量名，右侧的值一个字都不动
env_sed='s/^\([[:space:]]*\(export[[:space:]][[:space:]]*\)\{0,1\}\)SUAG_\([A-Za-z0-9_]*[[:space:]]*=\)/\1SUGA_\3/'
# gofer agents.yaml
key_sed='s/^\([[:space:]]*\)suag\(-acp\)\{0,1\}\([[:space:]]*:\)/\1suga\2\3/'
cmd_sed='s/\(command[[:space:]]*:[[:space:]]*\)suag\([^A-Za-z0-9_]\|$\)/\1suga\2/g'

# 源目录与目标目录
if [ -n "$home_dir" ]; then
    src=$home_dir
elif [ -n "${SUAG_HOME:-}" ]; then
    src=$SUAG_HOME
else
    src="$HOME/.suag"
fi
# 去掉结尾的斜杠，便于取同名同级的 .suga
src=$(printf '%s\n' "$src" | sed 's#/*$##')
[ -n "$src" ] || src=/
if [ -n "$target" ]; then
    dst=$(printf '%s\n' "$target" | sed 's#/*$##')
    [ -n "$dst" ] || dst=/
else
    parent=$(dirname "$src")
    dst="$parent/.suga"
fi

say "suag -> suga 本地配置迁移"
say "  源目录：$src"
say "  目标目录：$dst"
if [ "$apply" -eq 1 ]; then say "  模式：执行（会修改文件）"; else say "  模式：预览（不做任何修改，加 --apply 执行）"; fi

# ---------- 前提检查 ----------

if [ ! -e "$src" ]; then
    say "源目录不存在，无需迁移。"
    exit 0
fi
if [ ! -d "$src" ]; then
    fail "源目录不是目录：$src"
fi

if [ -n "$process_list" ] && [ ! -f "$process_list" ]; then
    fail "进程名单文件不存在：$process_list"
fi

busy=$(find_processes)
if [ -n "$busy" ]; then
    fail "检测到正在运行的进程：$busy（gofer 拉起的任务、chat、acp、trace web 都算）。请先结束它们再重试。"
fi

if [ -e "$dst" ] && [ ! -d "$dst" ]; then
    fail "目标路径已存在而且不是目录：$dst"
fi
if [ -d "$dst" ] && [ -n "$(ls -A "$dst" 2>/dev/null)" ]; then
    fail "目标目录已存在且非空：$dst。请先备份并移走它，或改用 --target。"
fi
if [ -d "$dst" ]; then
    say "  目标目录已存在但为空，执行时会先删除它：$dst"
fi

# ---------- 步骤 1：目录改名 ----------

show_step "1. 目录改名：$src -> $dst"
if [ "$apply" -eq 1 ]; then
    step_rename_dir || fail "目录改名失败：$src -> $dst"
fi

# 后续步骤都在目标目录里操作（预览模式读源目录）
if [ "$apply" -eq 1 ]; then base=$dst; else base=$src; fi

# ---------- 步骤 2、3：config.yaml 与 .env ----------

step_config "$base/config.yaml" || fail "config.yaml 改写失败"
[ "$apply" -eq 1 ] && add_done "2. config.yaml：agent 名、.suag 路径、SUAG_ 变量名"

step_env "$base/.env" || fail ".env 改写失败"
[ "$apply" -eq 1 ] && add_done "3. .env：SUAG_ 变量名改名为 SUGA_"

# ---------- 步骤 4：用户级环境变量（只提示，不改文件） ----------

step_shell_configs || fail "列出 shell 配置文件失败"

# ---------- 步骤 5：gofer agents.yaml（可选） ----------

if [ -n "$gofer_agents" ]; then
    step_agents "$gofer_agents" || fail "gofer agents.yaml 改写失败"
    [ "$apply" -eq 1 ] && add_done "5. gofer agents：$gofer_agents"
else
    say "  gofer agents.yaml：未指定 --gofer-agents，跳过"
fi

# ---------- 结束提示 ----------

say ""
if [ "$apply" -eq 0 ]; then
    say "预览结束：没有做任何修改。确认无误后加 --apply 执行。"
    exit 0
fi

say "迁移完成。接下来需要："
say "  1. 重开终端，让新的环境变量生效。"
say "  2. 在 shell 配置文件里把 SUAG_* 改名为 SUGA_*（见上面的行号），并重启 gofer server"
say "     （只 reload 不够，server 进程的环境不会更新）。"
if [ -n "$gofer_agents" ]; then say "  3. 用了 --gofer-agents：在 gofer 侧执行配置重载。"; fi
say "  本脚本不改 gofer 数据库、不改工作区项目目录、不删除旧二进制。"
exit 0
