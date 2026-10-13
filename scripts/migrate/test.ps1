# 测试 scripts/migrate/suag-to-suga.ps1。
# 全部在仓库内的 tmp/migrate-test/ 下构造模拟环境（结束时删除），用 -HomeDir、-EnvStore、
# -ProcessList 隔离；不读写真实的 ~/.suag、~/.suga，也不碰真实的用户级环境变量。
# 用法：powershell -NoProfile -File scripts/migrate/test.ps1
$ErrorActionPreference = 'Stop'

$Here = $PSScriptRoot
$RepoRoot = (Resolve-Path (Join-Path $Here '..\..')).Path
$TmpRoot = Join-Path $RepoRoot 'tmp\migrate-test'
$Migrate = Join-Path $Here 'suag-to-suga.ps1'
$PSExe = (Get-Process -Id $PID).Path

# 随机哨兵值：用来证明脚本的输出里不含 .env 的值
$Sentinel = 'SENTINEL-' + [guid]::NewGuid().ToString('N')
$script:Passed = 0
$script:Failed = 0

# ---------- 断言 ----------

function Check([bool]$Ok, [string]$Name) {
    if ($Ok) { Write-Host ('  PASS ' + $Name); $script:Passed++ }
    else { Write-Host ('  FAIL ' + $Name) -ForegroundColor Red; $script:Failed++ }
}

function CheckEq($Expected, $Actual, [string]$Name) {
    Check ("$Expected" -eq "$Actual") ($Name + '（期望 [' + $Expected + '] 实际 [' + $Actual + ']）')
}

function CheckContains([string]$Haystack, [string]$Needle, [string]$Name) {
    Check ($Haystack -like ('*' + $Needle + '*')) ($Name + '（找不到 [' + $Needle + ']）')
}

# ---------- 基础工具 ----------

function Write-Text([string]$Path, [string]$Text) {
    $dir = [System.IO.Path]::GetDirectoryName($Path)
    if (-not (Test-Path -LiteralPath $dir)) { New-Item -ItemType Directory -Path $dir -Force | Out-Null }
    $utf8 = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText($Path, $Text, $utf8)
}

function Quote-Arg([string]$Arg) {
    if ($Arg -match '[\s"]') { return ('"' + ($Arg -replace '"', '\"') + '"') }
    return $Arg
}

# 运行被测脚本，分别收集 stdout 与 stderr
function Invoke-Migrate([string[]]$Argv) {
    $outFile = Join-Path $TmpRoot 'stdout.txt'
    $errFile = Join-Path $TmpRoot 'stderr.txt'
    Remove-Item -LiteralPath $outFile, $errFile -Force -ErrorAction SilentlyContinue
    $argvQuoted = @($Argv | ForEach-Object { Quote-Arg $_ })
    $all = @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', (Quote-Arg $Migrate)) + $argvQuoted
    $proc = Start-Process -FilePath $PSExe -ArgumentList $all -NoNewWindow -Wait -PassThru `
        -RedirectStandardOutput $outFile -RedirectStandardError $errFile
    $out = ''
    $err = ''
    if (Test-Path -LiteralPath $outFile) { $out = [System.IO.File]::ReadAllText($outFile) }
    if (Test-Path -LiteralPath $errFile) { $err = [System.IO.File]::ReadAllText($errFile) }
    return [pscustomobject]@{ Exit = $proc.ExitCode; Out = $out; Err = $err }
}

# 目录结构 + 全部文件内容的指纹，用于断言“零修改”
function Get-Snapshot([string]$Root) {
    if (-not (Test-Path -LiteralPath $Root)) { return '<missing>' }
    $lines = @()
    foreach ($d in (Get-ChildItem -LiteralPath $Root -Recurse -Force -Directory | Sort-Object FullName)) {
        $lines += ($d.FullName.Substring($Root.Length).TrimStart('\') + '/')
    }
    foreach ($f in (Get-ChildItem -LiteralPath $Root -Recurse -Force -File | Sort-Object FullName)) {
        $hash = (Get-FileHash -LiteralPath $f.FullName -Algorithm SHA256).Hash
        $lines += ($f.FullName.Substring($Root.Length).TrimStart('\') + '=' + $hash)
    }
    return ($lines -join "`n")
}

function New-ProcessList([string]$Name, [string[]]$Names) {
    $path = Join-Path $TmpRoot ('procs-' + $Name + '.txt')
    Write-Text $path (($Names + @('')) -join "`n")
    return $path
}

# ---------- 模拟环境 ----------

$ConfigOriginal = @(
    '# suag configuration'
    'model: bailian/glm-5'
    'hooks:'
    '  command: [gofer, hook, generic, --agent, suag]'
    'memory:'
    '  user_dir: /home/x/.suag/memory'
    'note: SUAG_HOME and SUAG_MODEL are read'
    ''
) -join "`n"

$ConfigExpected = @(
    '# suag configuration'
    'model: bailian/glm-5'
    'hooks:'
    '  command: [gofer, hook, generic, --agent, suga]'
    'memory:'
    '  user_dir: /home/x/.suga/memory'
    'note: SUGA_HOME and SUGA_MODEL are read'
    ''
) -join "`n"

$AgentsOriginal = @(
    'agents:'
    '  suag:'
    '    type: cli-agent'
    '    command: suag'
    '    env:'
    '      SUAG_HOME: /home/x/.suag'
    '      SUAG_MODEL: glm-5'
    '  suag-acp:'
    '    type: acp-agent'
    '    command: suag'
    ''
) -join "`n"

# 步骤 5 只改 agent key、command 与 env 的变量名；env 的值（包括指向 .suag 的路径）原样保留，
# 所以 SUAG_HOME 的值仍是 /home/x/.suag（见 README 的“限制”一节）
$AgentsExpected = @(
    'agents:'
    '  suga:'
    '    type: cli-agent'
    '    command: suga'
    '    env:'
    '      SUGA_HOME: /home/x/.suag'
    '      SUGA_MODEL: glm-5'
    '  suga-acp:'
    '    type: acp-agent'
    '    command: suga'
    ''
) -join "`n"

function New-MockEnv([string]$Name) {
    $root = Join-Path $TmpRoot $Name
    if (Test-Path -LiteralPath $root) { Remove-Item -LiteralPath $root -Recurse -Force }
    $homeDir = Join-Path $root '.suag'
    $target = Join-Path $root '.suga'
    New-Item -ItemType Directory -Path $homeDir -Force | Out-Null
    Write-Text (Join-Path $homeDir 'config.yaml') $ConfigOriginal
    Write-Text (Join-Path $homeDir '.env') (@(
            'SUAG_X=' + $Sentinel
            'OTHER_VALUE=keepme'
            'export SUAG_Y=hello'
            '# note: SUAG_Z is not a variable'
            ''
        ) -join "`n")
    $agents = Join-Path $root 'gofer\agents.yaml'
    Write-Text $agents $AgentsOriginal
    $store = Join-Path $root 'user-env.json'
    Write-Text $store (@(
            '{'
            '  "SUAG_HOME": "' + ($homeDir -replace '\\', '\\') + '",'
            '  "SUAG_MODEL": "glm-5",'
            '  "OTHER": "keep"'
            '}'
        ) -join "`n")
    return [pscustomobject]@{ Root = $root; Home = $homeDir; Target = $target; Agents = $agents; Store = $store }
}

# ---------- 场景 ----------

# 1. 预览模式：零修改，且列出将要改的每一项
function Test-Preview([object]$Mock, [string]$ProcList) {
    $before = Get-Snapshot $Mock.Root
    $r = Invoke-Migrate @('-HomeDir', $Mock.Home, '-ProcessList', $ProcList, '-GoferAgents', $Mock.Agents,
        '-EnvStore', $Mock.Store)
    CheckEq 0 $r.Exit '预览：退出码为 0'
    Check ((Get-Snapshot $Mock.Root) -eq $before) '预览：目录结构与全部文件内容零修改'
    Check ($r.Out -match '模式：预览') '预览：输出标明预览模式'
    CheckContains $r.Out '--agent, suga' '预览：列出 config.yaml 的 agent 名改动'
    CheckContains $r.Out '/home/x/.suga/memory' '预览：列出 .suag 路径改动'
    CheckContains $r.Out '变量名 SUAG_X -> SUGA_X' '预览：只列出 .env 的变量名'
    CheckContains $r.Out 'SUAG_HOME -> SUGA_HOME' '预览：列出用户级环境变量改动'
    CheckContains $r.Out 'suga-acp:' '预览：列出 agents.yaml 的 agent key 改动'
    Check (-not ($r.Out -match [regex]::Escape($Sentinel))) '预览：stdout 不含 .env 的值'
    Check (-not ($r.Err -match [regex]::Escape($Sentinel))) '预览：stderr 不含 .env 的值'
    Check (Test-Path -LiteralPath $Mock.Home) '预览：源目录仍在原处'
    Check (-not (Test-Path -LiteralPath $Mock.Target)) '预览：没有创建目标目录'
}

# 2. 执行：目录改名 + 三个文件替换 + 备份 + 哨兵值不外泄
function Test-Apply([object]$Mock, [string]$ProcList) {
    $r = Invoke-Migrate @('-HomeDir', $Mock.Home, '-ProcessList', $ProcList, '-GoferAgents', $Mock.Agents,
        '-EnvStore', $Mock.Store, '-Apply')
    CheckEq 0 $r.Exit '执行：退出码为 0'
    Check (-not (Test-Path -LiteralPath $Mock.Home)) '执行：源目录已改名'
    Check (Test-Path -LiteralPath (Join-Path $Mock.Target 'config.yaml')) '执行：目标目录里有 config.yaml'
    CheckEq $ConfigExpected ([System.IO.File]::ReadAllText((Join-Path $Mock.Target 'config.yaml'))) '执行：config.yaml 内容完全符合预期'
    CheckEq (@(
            'SUGA_X=' + $Sentinel
            'OTHER_VALUE=keepme'
            'export SUGA_Y=hello'
            '# note: SUAG_Z is not a variable'
            ''
        ) -join "`n") ([System.IO.File]::ReadAllText((Join-Path $Mock.Target '.env'))) '执行：.env 只改变量名，值原样保留'
    CheckEq $AgentsExpected ([System.IO.File]::ReadAllText($Mock.Agents)) '执行：agents.yaml 内容完全符合预期'

    # 备份：命名、位置、不以 .yaml 结尾
    $cfgBak = @(Get-ChildItem -LiteralPath $Mock.Target -Force -Filter 'config.yaml.bak-*')
    CheckEq 1 $cfgBak.Count '执行：config.yaml 有一个备份'
    if ($cfgBak.Count -eq 1) {
        Check ($cfgBak[0].Name -match '\.bak-\d{8}T\d{4}$') '执行：config.yaml 备份名带时间戳'
        Check ($cfgBak[0].Name -notmatch '\.yaml$') '执行：config.yaml 备份名不以 .yaml 结尾'
        CheckEq $ConfigOriginal ([System.IO.File]::ReadAllText($cfgBak[0].FullName)) '执行：config.yaml 备份内容是旧配置'
    }
    $envBak = @(Get-ChildItem -LiteralPath $Mock.Target -Force -Filter '.env.bak-*')
    CheckEq 1 $envBak.Count '执行：.env 有一个备份'
    $agentsBak = @(Get-ChildItem -LiteralPath (Split-Path -Parent $Mock.Agents) -Force -Filter 'agents.yaml.bak-*')
    CheckEq 1 $agentsBak.Count '执行：agents.yaml 有一个备份'
    if ($agentsBak.Count -eq 1) {
        Check ($agentsBak[0].Name -match '\.bak-\d{8}T\d{4}$') '执行：agents.yaml 备份名带时间戳'
        Check ($agentsBak[0].Name -notmatch '\.yaml$') '执行：agents.yaml 备份名不以 .yaml 结尾（gofer 会加载 config.d/*.yaml）'
    }
    $storeBak = @(Get-ChildItem -LiteralPath $Mock.Root -Force -Filter 'user-env.json.bak-*')
    CheckEq 1 $storeBak.Count '执行：环境变量存储文件有一个备份'

    # 用户级环境变量（用 -EnvStore 注入）
    $store = Get-Content -LiteralPath $Mock.Store -Raw | ConvertFrom-Json
    $names = @($store.PSObject.Properties | ForEach-Object { $_.Name })
    Check ($names -contains 'SUGA_HOME' -and $names -contains 'SUGA_MODEL') '执行：EnvStore 里的 SUAG_* 已改名为 SUGA_*'
    Check (-not ($names -contains 'SUAG_HOME') -and -not ($names -contains 'SUAG_MODEL')) '执行：EnvStore 里没有残留的 SUAG_*'
    CheckEq 'glm-5' $store.SUGA_MODEL '执行：EnvStore 的值不变'
    CheckEq 'keep' $store.OTHER '执行：EnvStore 里非 SUAG_ 的条目不变'
    CheckContains $r.Out '重启 gofer server' '执行：结束时提示重启 gofer server'
    CheckContains $r.Out '重开终端' '执行：结束时提示重开终端'
    Check (-not ($r.Out -match [regex]::Escape($Sentinel))) '执行：stdout 不含 .env 的值'
    Check (-not ($r.Err -match [regex]::Escape($Sentinel))) '执行：stderr 不含 .env 的值'
}

# 3. 目标目录已存在且非空：拒绝执行且零修改
function Test-TargetBusy([string]$ProcList) {
    $mock = New-MockEnv 'target-busy'
    New-Item -ItemType Directory -Path $mock.Target -Force | Out-Null
    Write-Text (Join-Path $mock.Target 'keep.txt') 'keep'
    $before = Get-Snapshot $mock.Root
    $r = Invoke-Migrate @('-HomeDir', $mock.Home, '-ProcessList', $ProcList, '-Apply')
    Check ($r.Exit -ne 0) '目标非空：非零退出码'
    Check ((Get-Snapshot $mock.Root) -eq $before) '目标非空：零修改'
    Check (Test-Path -LiteralPath $mock.Home) '目标非空：源目录未动'
    CheckContains ($r.Out + $r.Err) '目标目录已存在且非空' '目标非空：说明了原因'
}

# 4. 注入“有 suag 进程”：拒绝执行且零修改
function Test-ProcessBusy() {
    $mock = New-MockEnv 'process-busy'
    $procs = New-ProcessList 'busy' @('explorer', 'suag', 'other')
    $before = Get-Snapshot $mock.Root
    $r = Invoke-Migrate @('-HomeDir', $mock.Home, '-ProcessList', $procs, '-Apply')
    Check ($r.Exit -ne 0) '有进程在跑：非零退出码'
    Check ((Get-Snapshot $mock.Root) -eq $before) '有进程在跑：零修改'
    CheckContains ($r.Out + $r.Err) '检测到正在运行的进程' '有进程在跑：说明了原因'

    # 反向验证：其它进程名不算命中
    $procs2 = New-ProcessList 'notbusy' @('explorer', 'suage', 'suga-dev')
    $r2 = Invoke-Migrate @('-HomeDir', $mock.Home, '-ProcessList', $procs2)
    CheckEq 0 $r2.Exit '只有别的进程名时：预览正常'

    # suga.exe 也算命中
    $procs3 = New-ProcessList 'exe' @('suga.exe')
    $r3 = Invoke-Migrate @('-HomeDir', $mock.Home, '-ProcessList', $procs3)
    Check ($r3.Exit -ne 0) '进程名单里的 suga.exe 也算命中'
}

# 5. 源目录不存在：提示无需迁移并返回 0
function Test-NoSource([string]$ProcList) {
    $missing = Join-Path $TmpRoot 'not-created\.suag'
    $r = Invoke-Migrate @('-HomeDir', $missing, '-ProcessList', $ProcList)
    CheckEq 0 $r.Exit '源目录不存在：退出码为 0'
    CheckContains $r.Out '无需迁移' '源目录不存在：提示无需迁移'
    Check (-not (Test-Path -LiteralPath $missing)) '源目录不存在：不会创建任何目录'
}

# 6. 源目录解析：-HomeDir > SUAG_HOME > $HOME/.suag
function Test-SourceResolution([string]$ProcList) {
    $mock = New-MockEnv 'resolve-home'
    $other = New-MockEnv 'resolve-env'

    # -HomeDir 优先于 SUAG_HOME
    $env:SUAG_HOME = $other.Home
    $r = Invoke-Migrate @('-HomeDir', $mock.Home, '-ProcessList', $ProcList)
    CheckContains $r.Out ('源目录：' + $mock.Home) '-HomeDir 优先于 SUAG_HOME'

    # 没有 -HomeDir 时用 SUAG_HOME
    $r2 = Invoke-Migrate @('-ProcessList', $ProcList)
    CheckContains $r2.Out ('源目录：' + $other.Home) '没有 -HomeDir 时用 SUAG_HOME'
    CheckContains $r2.Out ('目标目录：' + (Join-Path $other.Root '.suga')) '目标目录缺省为源目录同级的 .suga'

    # -Target 指定目标目录
    $custom = Join-Path $mock.Root 'custom-target'
    $r3 = Invoke-Migrate @('-HomeDir', $mock.Home, '-Target', $custom, '-ProcessList', $ProcList)
    CheckContains $r3.Out ('目标目录：' + $custom) '-Target 指定目标目录'
    Remove-Item Env:SUAG_HOME -ErrorAction SilentlyContinue
}

# 7. 环境变量新名字已被占用：跳过它，别的照常改名
function Test-EnvConflict([string]$ProcList) {
    $mock = New-MockEnv 'env-conflict'
    Write-Text $mock.Store (@(
            '{'
            '  "SUAG_HOME": "old-value",'
            '  "SUGA_HOME": "already-new",'
            '  "SUAG_MODEL": "glm-5"'
            '}'
        ) -join "`n")
    $r = Invoke-Migrate @('-HomeDir', $mock.Home, '-ProcessList', $ProcList, '-EnvStore', $mock.Store, '-Apply')
    CheckEq 0 $r.Exit '环境变量冲突：其余步骤照常完成'
    $store = Get-Content -LiteralPath $mock.Store -Raw | ConvertFrom-Json
    CheckEq 'already-new' $store.SUGA_HOME '环境变量冲突：已存在的 SUGA_HOME 不被覆盖'
    CheckEq 'old-value' $store.SUAG_HOME '环境变量冲突：冲突的 SUAG_HOME 原样保留'
    CheckEq 'glm-5' $store.SUGA_MODEL '环境变量冲突：其它变量照常改名'
    CheckContains ($r.Out + $r.Err) '跳过 SUAG_HOME' '环境变量冲突：提示跳过'
}

# ---------- 主流程 ----------

if (-not (Test-Path -LiteralPath $TmpRoot)) { New-Item -ItemType Directory -Path $TmpRoot -Force | Out-Null }
$emptyProcs = New-ProcessList 'empty' @()

# 只清当前进程的环境变量，不动用户级设置
Get-ChildItem Env: | Where-Object { $_.Name -like 'SUAG_*' -or $_.Name -like 'SUGA_*' } |
    ForEach-Object { Remove-Item -LiteralPath ('Env:' + $_.Name) -ErrorAction SilentlyContinue }

# 记录真实环境，结束时确认没被动过
$userEnvBefore = (@([Environment]::GetEnvironmentVariables('User').Keys) | Sort-Object) -join ','
$realHomeSuag = Test-Path -LiteralPath (Join-Path $HOME '.suag')
$realHomeSuga = Test-Path -LiteralPath (Join-Path $HOME '.suga')

try {
    Write-Host '1. 预览模式'
    $mock = New-MockEnv 'preview'
    Test-Preview $mock $emptyProcs

    Write-Host '2. 执行模式'
    $mock = New-MockEnv 'apply'
    Test-Apply $mock $emptyProcs

    Write-Host '3. 目标目录已存在且非空'
    Test-TargetBusy $emptyProcs

    Write-Host '4. 有 suag 进程在运行'
    Test-ProcessBusy

    Write-Host '5. 源目录不存在'
    Test-NoSource $emptyProcs

    Write-Host '6. 源目录解析顺序'
    Test-SourceResolution $emptyProcs

    Write-Host '7. 环境变量名冲突'
    Test-EnvConflict $emptyProcs

    Write-Host '8. 没有触碰真实环境'
    $userEnvAfter = (@([Environment]::GetEnvironmentVariables('User').Keys) | Sort-Object) -join ','
    CheckEq $userEnvBefore $userEnvAfter '8. 真实用户级环境变量未被修改'
    CheckEq $realHomeSuag (Test-Path -LiteralPath (Join-Path $HOME '.suag')) '8. 真实的 ~/.suag 未被修改'
    CheckEq $realHomeSuga (Test-Path -LiteralPath (Join-Path $HOME '.suga')) '8. 没有创建真实的 ~/.suga'
    Check (-not (Test-Path -LiteralPath (Join-Path $env:USERPROFILE '.suga'))) '8. 测试全部用 -HomeDir/-EnvStore 隔离'
    Check ((Test-Path -LiteralPath $TmpRoot)) '8. 模拟环境都在仓库内的 tmp/migrate-test 下'
}
finally {
    if (Test-Path -LiteralPath $TmpRoot) { Remove-Item -LiteralPath $TmpRoot -Recurse -Force }
}

Write-Host ''
Write-Host ('结果：' + $script:Passed + ' 项通过，' + $script:Failed + ' 项失败')
if ($script:Failed -gt 0) { exit 1 }
exit 0
