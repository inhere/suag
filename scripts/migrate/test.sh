#!/bin/sh
# 测试 scripts/migrate/suag-to-suga.sh。
# 全部在仓库内的 tmp/migrate-test-sh/ 下构造模拟环境（结束时删除），用 --home-dir、
# --process-list 隔离；测试进程的 HOME 指向模拟目录，不读写真实的 ~/.suag、~/.suga。
# 用法：sh scripts/migrate/test.sh
set -u

here=$(cd "$(dirname "$0")" && pwd)
repo_root=$(cd "$here/../.." && pwd)
tmp_root="$repo_root/tmp/migrate-test-sh"
migrate="$here/suag-to-suga.sh"

# 随机哨兵值：用来证明脚本的输出里不含 .env 的值
sentinel="SENTINEL-$$-$(date +%s)"
passed=0
failed=0
OUT=""
ERR=""
EXIT=0

check() { # $1 0/1, $2 名称
    if [ "$1" -eq 0 ]; then
        printf '  PASS %s\n' "$2"
        passed=$((passed + 1))
    else
        printf '  FAIL %s\n' "$2"
        failed=$((failed + 1))
    fi
}

check_eq() { # $1 期望, $2 实际, $3 名称
    if [ "$1" = "$2" ]; then check 0 "$3"; else check 1 "$3（期望 [$1] 实际 [$2]）"; fi
}

check_contains() { # $1 全文, $2 片段, $3 名称
    case "$1" in
        *"$2"*) check 0 "$3" ;;
        *) check 1 "$3（找不到 [$2]）" ;;
    esac
}

check_not_contains() { # $1 全文, $2 片段, $3 名称
    case "$1" in
        *"$2"*) check 1 "$3（不该出现 [$2]）" ;;
        *) check 0 "$3" ;;
    esac
}

# ---------- 基础工具 ----------

run_migrate() { # 参数即脚本参数，结果放 OUT / ERR / EXIT
    sh "$migrate" "$@" >"$tmp_root/stdout.txt" 2>"$tmp_root/stderr.txt"
    EXIT=$?
    OUT=$(cat "$tmp_root/stdout.txt")
    ERR=$(cat "$tmp_root/stderr.txt")
}

snapshot() { # $1 目录 -> 结构 + 每个文件内容的指纹
    if [ ! -e "$1" ]; then
        printf '<missing>\n'
        return 0
    fi
    (
        cd "$1" || exit 1
        find . -print | LC_ALL=C sort | while IFS= read -r p; do
            if [ -f "$p" ]; then
                printf '%s=%s\n' "$p" "$(cksum <"$p")"
            else
                printf '%s/\n' "$p"
            fi
        done
    )
}

file_mode() { # 兼容 GNU / BSD stat
    if stat -c '%a' "$1" >/dev/null 2>&1; then
        stat -c '%a' "$1"
    else
        stat -f '%Lp' "$1"
    fi
}

# 平台是否能表示 POSIX 权限位（Git for Windows 只能表示只读位，chmod 600 会变成 644）
mode_supported() {
    probe="$tmp_root/mode-probe"
    : >"$probe"
    chmod 600 "$probe" 2>/dev/null
    if [ "$(file_mode "$probe")" = "600" ]; then
        rm -f "$probe"
        return 0
    fi
    rm -f "$probe"
    return 1
}

new_procs() { # $1 名字, 其余为进程名 -> 输出文件路径
    f="$tmp_root/procs-$1.txt"
    : >"$f"
    shift
    for n in "$@"; do printf '%s\n' "$n" >>"$f"; done
    printf '%s\n' "$f"
}

# ---------- 模拟环境 ----------

write_config() {
    cat >"$1" <<'EOF'
# suag configuration
model: bailian/glm-5
hooks:
  command: [gofer, hook, generic, --agent, suag]
memory:
  user_dir: /home/x/.suag/memory
note: SUAG_HOME and SUAG_MODEL are read
EOF
}

write_env_file() {
    cat >"$1" <<EOF
SUAG_X=$sentinel
OTHER_VALUE=keepme
export SUAG_Y=hello
# note: SUAG_Z is not a variable
EOF
}

write_agents() {
    cat >"$1" <<'EOF'
agents:
  suag:
    type: cli-agent
    command: suag
    env:
      SUAG_HOME: /home/x/.suag
      SUAG_MODEL: glm-5
  suag-acp:
    type: acp-agent
    command: suag
EOF
}

# 期望结果（步骤 5 改 agent key、command、指向 .suag 的路径与 env 的变量名）
config_expected() {
    cat <<'EOF'
# suag configuration
model: bailian/glm-5
hooks:
  command: [gofer, hook, generic, --agent, suga]
memory:
  user_dir: /home/x/.suga/memory
note: SUGA_HOME and SUGA_MODEL are read
EOF
}

agents_expected() {
    cat <<'EOF'
agents:
  suga:
    type: cli-agent
    command: suga
    env:
      SUGA_HOME: /home/x/.suga
      SUGA_MODEL: glm-5
  suga-acp:
    type: acp-agent
    command: suga
EOF
}

new_mock() { # $1 名字 -> MOCK_ROOT / MOCK_HOME / MOCK_TARGET / MOCK_AGENTS / MOCK_SHELL_HOME
    MOCK_ROOT="$tmp_root/$1"
    rm -rf "$MOCK_ROOT"
    MOCK_HOME="$MOCK_ROOT/.suag"
    MOCK_TARGET="$MOCK_ROOT/.suga"
    MOCK_AGENTS="$MOCK_ROOT/gofer/agents.yaml"
    MOCK_SHELL_HOME="$MOCK_ROOT/home"
    mkdir -p "$MOCK_HOME" "$(dirname "$MOCK_AGENTS")" "$MOCK_SHELL_HOME"
    write_config "$MOCK_HOME/config.yaml"
    write_env_file "$MOCK_HOME/.env"
    chmod 600 "$MOCK_HOME/.env"
    write_agents "$MOCK_AGENTS"
    # shell 配置文件里带 SUAG_，只用来看“只列行号、不打印内容”
    {
        printf '%s\n' '# 注释'
        printf '%s\n' 'export PATH=/usr/bin'
        printf '%s\n' 'export SUAG_A=hidden-value'
    } >"$MOCK_SHELL_HOME/.bashrc"
    export HOME="$MOCK_SHELL_HOME"
}

# ---------- 场景 ----------

# 1. 预览模式：零修改，且列出将要改的每一项
test_preview() {
    new_mock preview
    pl=$(new_procs empty)
    before=$(snapshot "$MOCK_ROOT")
    run_migrate --home-dir "$MOCK_HOME" --process-list "$pl" --gofer-agents "$MOCK_AGENTS"
    check_eq 0 "$EXIT" '预览：退出码为 0'
    check_eq "$before" "$(snapshot "$MOCK_ROOT")" '预览：目录结构与全部文件内容零修改'
    check_contains "$OUT" '模式：预览' '预览：输出标明预览模式'
    check_contains "$OUT" '--agent, suga' '预览：列出 config.yaml 的 agent 名改动'
    check_contains "$OUT" '/home/x/.suga/memory' '预览：列出 .suag 路径改动'
    check_contains "$OUT" '变量名 SUAG_X -> SUGA_X' '预览：只列出 .env 的变量名'
    check_contains "$OUT" 'suga-acp:' '预览：列出 agents.yaml 的 agent key 改动'
    check_not_contains "$OUT" "$sentinel" '预览：stdout 不含 .env 的值'
    check_not_contains "$ERR" "$sentinel" '预览：stderr 不含 .env 的值'
    check_not_contains "$OUT" 'hidden-value' '预览：不打印 shell 配置文件的行内容'
    check_contains "$OUT" "$MOCK_SHELL_HOME/.bashrc 中带 SUAG_ 的行号：3" '预览：列出 shell 配置文件与行号'
    [ -d "$MOCK_HOME" ] && check 0 '预览：源目录仍在原处' || check 1 '预览：源目录仍在原处'
    [ -e "$MOCK_TARGET" ] && check 1 '预览：没有创建目标目录' || check 0 '预览：没有创建目标目录'
}

# 2. 执行：目录改名 + 三个文件替换 + 备份 + 哨兵值不外泄
test_apply() {
    new_mock apply
    pl=$(new_procs empty)
    run_migrate --home-dir "$MOCK_HOME" --process-list "$pl" --gofer-agents "$MOCK_AGENTS" --apply
    check_eq 0 "$EXIT" '执行：退出码为 0'
    [ -d "$MOCK_HOME" ] && check 1 '执行：源目录已改名' || check 0 '执行：源目录已改名'
    [ -f "$MOCK_TARGET/config.yaml" ] && check 0 '执行：目标目录里有 config.yaml' || check 1 '执行：目标目录里有 config.yaml'
    check_eq "$(config_expected)" "$(cat "$MOCK_TARGET/config.yaml")" '执行：config.yaml 内容完全符合预期'
    check_eq "SUGA_X=$sentinel
OTHER_VALUE=keepme
export SUGA_Y=hello
# note: SUAG_Z is not a variable" "$(cat "$MOCK_TARGET/.env")" '执行：.env 只改变量名，值原样保留'
    check_eq "$(agents_expected)" "$(cat "$MOCK_AGENTS")" '执行：agents.yaml 内容完全符合预期'

    # 备份命名、位置与权限
    cfg_baks=$(ls "$MOCK_TARGET"/config.yaml.bak-* 2>/dev/null || true)
    check_eq 1 "$(printf '%s\n' "$cfg_baks" | grep -c . )" '执行：config.yaml 有一个备份'
    case "$cfg_baks" in
        *.bak-[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]T[0-9][0-9][0-9][0-9]) check 0 '执行：config.yaml 备份名带时间戳' ;;
        *) check 1 '执行：config.yaml 备份名带时间戳（实际 '$cfg_baks'）' ;;
    esac
    case "$cfg_baks" in
        *.yaml) check 1 '执行：config.yaml 备份名不以 .yaml 结尾' ;;
        *) check 0 '执行：config.yaml 备份名不以 .yaml 结尾' ;;
    esac
    check_eq "$(cat "$MOCK_HOME/config.yaml" 2>/dev/null || echo missing)" "missing" '执行：源目录里没有残留的 config.yaml'

    env_bak=$(ls "$MOCK_TARGET"/.env.bak-* 2>/dev/null || true)
    check_eq 1 "$(printf '%s\n' "$env_bak" | grep -c .)" '执行：.env 有一个备份'
    if [ "$env_bak" != "" ]; then
        check_eq "SUAG_X=$sentinel
OTHER_VALUE=keepme
export SUAG_Y=hello
# note: SUAG_Z is not a variable" "$(cat "$env_bak")" '执行：.env 备份内容是旧配置'
        if mode_supported; then
            check_eq '600' "$(file_mode "$env_bak")" '执行：.env 备份保留原权限 600'
            check_eq 600 "$(file_mode "$MOCK_TARGET/.env")" '执行：改写后的 .env 仍是 600'
        else
            printf '  SKIP 执行：.env 备份保留原权限 600（本平台只能表示只读位，Linux/macOS 上会检查）\n'
        fi
    fi
    agents_bak=$(ls "$(dirname "$MOCK_AGENTS")"/agents.yaml.bak-* 2>/dev/null || true)
    check_eq 1 "$(printf '%s\n' "$agents_bak" | grep -c .)" '执行：agents.yaml 有一个备份'
    case "$agents_bak" in
        *.yaml) check 1 '执行：agents.yaml 备份名不以 .yaml 结尾（gofer 会加载 config.d/*.yaml）' ;;
        *) check 0 '执行：agents.yaml 备份名不以 .yaml 结尾（gofer 会加载 config.d/*.yaml）' ;;
    esac

    check_contains "$OUT" '重启 gofer server' '执行：结束时提示重启 gofer server'
    check_contains "$OUT" '重开终端' '执行：结束时提示重开终端'
    check_not_contains "$OUT" "$sentinel" '执行：stdout 不含 .env 的值'
    check_not_contains "$ERR" "$sentinel" '执行：stderr 不含 .env 的值'
}

# 3. 目标目录已存在且非空：拒绝执行且零修改
test_target_busy() {
    new_mock target-busy
    pl=$(new_procs empty)
    mkdir -p "$MOCK_TARGET"
    printf 'keep\n' >"$MOCK_TARGET/keep.txt"
    before=$(snapshot "$MOCK_ROOT")
    run_migrate --home-dir "$MOCK_HOME" --process-list "$pl" --apply
    [ "$EXIT" -ne 0 ] && check 0 '目标非空：非零退出码' || check 1 '目标非空：非零退出码'
    check_eq "$before" "$(snapshot "$MOCK_ROOT")" '目标非空：零修改'
    check_contains "$OUT$ERR" '目标目录已存在且非空' '目标非空：说明了原因'
}

# 4. 注入“有 suag 进程”：拒绝执行且零修改
test_process_busy() {
    new_mock process-busy
    pl=$(new_procs busy explorer suag other)
    before=$(snapshot "$MOCK_ROOT")
    run_migrate --home-dir "$MOCK_HOME" --process-list "$pl" --apply
    [ "$EXIT" -ne 0 ] && check 0 '有进程在跑：非零退出码' || check 1 '有进程在跑：非零退出码'
    check_eq "$before" "$(snapshot "$MOCK_ROOT")" '有进程在跑：零修改'
    check_contains "$OUT$ERR" '检测到正在运行的进程' '有进程在跑：说明了原因'

    # 反向验证：别的进程名不算命中
    pl2=$(new_procs notbusy explorer suage suga-dev)
    run_migrate --home-dir "$MOCK_HOME" --process-list "$pl2"
    check_eq 0 "$EXIT" '只有别的进程名时：预览正常'

    # suga.exe 也算命中
    pl3=$(new_procs exe suga.exe)
    run_migrate --home-dir "$MOCK_HOME" --process-list "$pl3"
    [ "$EXIT" -ne 0 ] && check 0 '进程名单里的 suga.exe 也算命中' || check 1 '进程名单里的 suga.exe 也算命中'
}

# 5. 源目录不存在：提示无需迁移并返回 0
test_no_source() {
    pl=$(new_procs empty)
    missing="$tmp_root/not-created/.suag"
    run_migrate --home-dir "$missing" --process-list "$pl"
    check_eq 0 "$EXIT" '源目录不存在：退出码为 0'
    check_contains "$OUT" '无需迁移' '源目录不存在：提示无需迁移'
    [ -e "$missing" ] && check 1 '源目录不存在：不会创建任何目录' || check 0 '源目录不存在：不会创建任何目录'
}

# 6. 源目录解析：--home-dir > SUAG_HOME > $HOME/.suag
test_source_resolution() {
    new_mock resolve-home
    pl=$(new_procs empty)
    other_home="$tmp_root/resolve-env/.suag"
    mkdir -p "$other_home"
    write_config "$other_home/config.yaml"

    SUAG_HOME="$other_home"
    export SUAG_HOME
    run_migrate --home-dir "$MOCK_HOME" --process-list "$pl"
    check_contains "$OUT" "源目录：$MOCK_HOME" '--home-dir 优先于 SUAG_HOME'

    run_migrate --process-list "$pl"
    check_contains "$OUT" "源目录：$other_home" '没有 --home-dir 时用 SUAG_HOME'
    check_contains "$OUT" "目标目录：$tmp_root/resolve-env/.suga" '目标目录缺省为源目录同级的 .suga'

    unset SUAG_HOME
    run_migrate --process-list "$pl"
    check_contains "$OUT" "源目录：$HOME/.suag" '都没有时用 $HOME/.suag'
    check_contains "$OUT" "目标目录：$HOME/.suga" '$HOME/.suag 的默认目标目录是 $HOME/.suga'

    custom="$MOCK_ROOT/custom-target"
    run_migrate --home-dir "$MOCK_HOME" --target "$custom" --process-list "$pl"
    check_contains "$OUT" "目标目录：$custom" '--target 指定目标目录'
}

# ---------- 主流程 ----------

mkdir -p "$tmp_root"

# 真实 HOME 的情况，结束时确认没被动过
real_home=$HOME
real_suag_exists=no
real_suga_exists=no
[ -e "$real_home/.suag" ] && real_suag_exists=yes
[ -e "$real_home/.suga" ] && real_suga_exists=yes

# 只清当前进程（及其子进程）的环境变量，不动用户级设置
for v in $(env | sed -n 's/^\(SUAG_[A-Za-z0-9_]*\)=.*/\1/p; s/^\(SUGA_[A-Za-z0-9_]*\)=.*/\1/p'); do
    unset "$v"
done

printf '1. 预览模式\n'
test_preview

printf '2. 执行模式\n'
test_apply

printf '3. 目标目录已存在且非空\n'
test_target_busy

printf '4. 有 suag 进程在运行\n'
test_process_busy

printf '5. 源目录不存在\n'
test_no_source

printf '6. 源目录解析顺序\n'
test_source_resolution

printf '7. 没有触碰真实环境\n'
now_suag_exists=no
now_suga_exists=no
[ -e "$real_home/.suag" ] && now_suag_exists=yes
[ -e "$real_home/.suga" ] && now_suga_exists=yes
check_eq "$real_suag_exists" "$now_suag_exists" '7. 真实的 ~/.suag 未被修改'
check_eq "$real_suga_exists" "$now_suga_exists" '7. 没有创建真实的 ~/.suga'
# cp -p 保留权限：用只读位（444）在 Windows 上也能验证
probe="$tmp_root/cp-probe"
: >"$probe"
chmod 444 "$probe" 2>/dev/null
if [ "$(file_mode "$probe")" = "444" ]; then
    cp -p "$probe" "$probe.bak"
    check_eq 444 "$(file_mode "$probe.bak")" '7. cp -p 保留文件权限（444 探针）'
fi
case "$HOME" in
    "$tmp_root"/*) check 0 '7. 测试全在仓库内的 tmp/migrate-test-sh 下，HOME 指向模拟目录' ;;
    *) check 1 '7. 测试全在仓库内的 tmp/migrate-test-sh 下，HOME 指向模拟目录' ;;
esac

rm -rf "$tmp_root"

printf '\n结果：%s 项通过，%s 项失败\n' "$passed" "$failed"
[ "$failed" -gt 0 ] && exit 1
exit 0
