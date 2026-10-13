<#
.SYNOPSIS
  把旧名字 suag 的本地配置迁移为新名字 suga（默认只预览，加 -Apply 才执行）。

.DESCRIPTION
  程序本体不做任何兼容或自动迁移；本脚本供用户在自己的机器上手工执行一次。

  源目录：-HomeDir > $env:SUAG_HOME > $HOME\.suag
  目标目录：-Target > 源目录同级的 .suga

  步骤：
    1. 源目录整体改名为目标目录
    2. config.yaml：agent 名（如 `--agent, suag`）、指向 .suag 的路径、SUAG_ 变量名
    3. .env：只改以 SUAG_ 开头的变量名，值原样保留（任何输出都不含 .env 的值）
    4. 用户级环境变量 SUAG_* 改名为 SUGA_*（值不变，不打印值）
    5. 可选 -GoferAgents <agents.yaml>：agent key、command、env 里的名字

  被改动的文件先备份为 <文件名>.bak-<yyyyMMddTHHmm>；备份名不以 .yaml 结尾，
  避免被 gofer 当成 config.d/*.yaml 加载。

  前提检查（不满足就停止并说明）：有 suag / suga 进程在跑；目标目录存在且非空；
  源目录不存在时提示无需迁移并返回 0。任一步失败立即停止，并在末尾列出已完成的
  步骤和全部备份位置，便于手工回退。

.PARAMETER Apply
  真正执行修改；不传时只列出将要改的每一项与原因，不做任何修改。

.PARAMETER HomeDir
  显式的源目录，优先于 $env:SUAG_HOME。

.PARAMETER Target
  目标目录，缺省为源目录同级的 .suga。

.PARAMETER ProcessList
  测试用：文本文件，每行一个进程名，用它的内容代替真实的进程检查。

.PARAMETER EnvStore
  测试用：JSON 文件（一个 name -> value 对象），代替 Windows 用户级环境变量。

.PARAMETER GoferAgents
  gofer 的 agents.yaml 路径（可选）。
#>
[CmdletBinding()]
param(
    [switch]$Apply,
    [string]$HomeDir,
    [string]$Target,
    [string]$ProcessList,
    [string]$EnvStore,
    [string]$GoferAgents
)

$ErrorActionPreference = 'Stop'

# 本次运行统一使用的时间戳，例如 .env.bak-20261013T2200
$script:Stamp = Get-Date -Format 'yyyyMMddTHHmm'
$script:DoneSteps = @()   # 已完成的步骤（失败时输出，便于回退）
$script:Backups = @()     # 全部备份文件位置
$script:RenameInfo = $null

# ---------- 输出 ----------

function Say([string]$Message) { Write-Host $Message }

function SayErr([string]$Message) { [Console]::Error.WriteLine($Message) }

# 执行模式下真正做了某项；预览模式下列出
function Show-Step([string]$Text) {
    if ($Apply) { Say ('  [已执行] ' + $Text) } else { Say ('  [将执行] ' + $Text) }
}

function Show-RevertSummary {
    Say ''
    Say '已完成的步骤：'
    if ($script:DoneSteps.Count -eq 0) { Say '  （无）' } else { foreach ($s in $script:DoneSteps) { Say ('  - ' + $s) } }
    Say '备份位置：'
    if ($script:Backups.Count -eq 0) { Say '  （无）' } else { foreach ($b in $script:Backups) { Say ('  - ' + $b) } }
    Say '手工回退方法：'
    if ($script:RenameInfo) { Say ('  - 把目录 ' + $script:RenameInfo.To + ' 改回 ' + $script:RenameInfo.From) }
    foreach ($b in $script:Backups) {
        $orig = $b -replace '\.bak-\d{8}T\d{4}$', ''
        Say ('  - 用 ' + $b + ' 覆盖 ' + $orig)
    }
    Say '  - 用户级环境变量：把 SUGA_* 改回 SUAG_*（脚本不打印值，请按需手工恢复）'
}

# 前提检查失败或某一步出错：说明原因、列出已完成的内容后退出 1
function Stop-Migration([string]$Message) {
    SayErr ('错误：' + $Message)
    Show-RevertSummary
    exit 1
}

# ---------- 文件读写（UTF-8 无 BOM，保留原有换行） ----------

function Read-FileText([string]$Path) {
    return [System.IO.File]::ReadAllText($Path)
}

function Write-FileText([string]$Path, [string]$Text) {
    $utf8 = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText($Path, $Text, $utf8)
}

# 按行切开并保留行尾符，逐行替换后用原样的换行拼回去
function Split-KeepEol([string]$Text) {
    return , ([regex]::Split($Text, "(\r\n|\n|\r)"))
}

function Backup-File([string]$Path) {
    $bak = $Path + '.bak-' + $script:Stamp
    Copy-Item -LiteralPath $Path -Destination $bak -Force
    if ($script:Backups -notcontains $bak) { $script:Backups += $bak }
    Say ('  已备份：' + $bak)
}

# ---------- 逐行替换计划 ----------

# 各规则：agent 名（suag / suag-acp）、指向 .suag 的路径、SUAG_ 变量名
function Get-ConfigRules {
    return @(
        [pscustomobject]@{ Name = 'agent 名'; Pattern = '(?<pre>--agent[,\s=]{1,3})suag(?![A-Za-z0-9_])'; Replacement = '${pre}suga' },
        [pscustomobject]@{ Name = '指向 .suag 的路径'; Pattern = '\.suag(?![A-Za-z0-9_])'; Replacement = '.suga' },
        [pscustomobject]@{ Name = 'SUAG_ 变量名'; Pattern = '(?<![A-Za-z0-9_])SUAG_'; Replacement = 'SUGA_' }
    )
}

# gofer agents.yaml：agent key、command: suag、env 里的 SUAG_
function Get-GoferAgentsRules {
    return @(
        [pscustomobject]@{ Name = 'agent key'; Pattern = '^(?<ind>[ \t]*)suag(?<suf>-acp)?(?<rest>\s*:)'; Replacement = '${ind}suga${suf}${rest}' },
        [pscustomobject]@{ Name = 'command: suag'; Pattern = '(?<pre>command\s*:\s*)suag(?![A-Za-z0-9_])'; Replacement = '${pre}suga' },
        [pscustomobject]@{ Name = 'SUAG_ 变量名'; Pattern = '(?<![A-Za-z0-9_])SUAG_'; Replacement = 'SUGA_' }
    )
}

# .env 只改赋值语句左侧以 SUAG_ 开头的变量名，右侧的值一个字都不动
function Get-EnvRules {
    return @(
        [pscustomobject]@{
            Name        = 'SUAG_ 变量名'
            Pattern     = '^(?<pre>\s*(?:export\s+)?)SUAG_(?<suffix>[A-Za-z0-9_]*)(?<rest>\s*=.*)$'
            Replacement = '${pre}SUGA_${suffix}${rest}'
        }
    )
}

# 计算某个文件的替换计划：Text 为替换后的全文，Changes 为逐行的改动说明
function Get-FilePlan([string]$Path, [object[]]$Rules) {
    $parts = Split-KeepEol (Read-FileText $Path)
    $changes = New-Object System.Collections.Generic.List[object]
    for ($i = 0; $i -lt $parts.Count; $i += 2) {
        $line = $parts[$i]
        $new = $line
        $why = New-Object System.Collections.Generic.List[string]
        foreach ($rule in $Rules) {
            $cand = [regex]::Replace($new, $rule.Pattern, $rule.Replacement)
            if ($cand -ne $new) { $why.Add($rule.Name) }
            $new = $cand
        }
        if ($new -ne $line) {
            $changes.Add([pscustomobject]@{
                    Line   = [int](($i / 2) + 1)
                    Old    = $line
                    New    = $new
                    Reason = ($why -join '、')
                })
            $parts[$i] = $new
        }
    }
    return [pscustomobject]@{ Text = ($parts -join ''); Changes = $changes }
}

# 打印一条改动。NamesOnly 用于 .env：只打印变量名，绝不打印值
function Format-Change($Change, [switch]$NamesOnly) {
    if ($NamesOnly) {
        $m = [regex]::Match($Change.Old, '^\s*(?:export\s+)?(SUAG_[A-Za-z0-9_]*)')
        $name = $m.Groups[1].Value
        $newName = 'SUGA_' + $name.Substring(5)
        return ('变量名 ' + $name + ' -> ' + $newName + '（值原样保留，不打印）')
    }
    return ($Change.Old.Trim() + '  ->  ' + $Change.New.Trim() + '（' + $Change.Reason + '）')
}

# 预览：列出改动；执行：先备份再写回
function Invoke-FileMigration([string]$Path, [string]$DisplayName, [object[]]$Rules, [switch]$NamesOnly) {
    $plan = Get-FilePlan -Path $Path -Rules $Rules
    if ($plan.Changes.Count -eq 0) {
        Say ('  ' + $DisplayName + '：无需修改')
        return
    }
    $verb = ''
    if ($Apply) {
        Backup-File $Path
        Write-FileText -Path $Path -Text $plan.Text
        $verb = '[已执行]'
    } else {
        $verb = '[将执行]'
    }
    foreach ($c in $plan.Changes) {
        Say ('  ' + $verb + ' ' + $DisplayName + ':' + $c.Line + ' ' + (Format-Change $c -NamesOnly:$NamesOnly))
    }
}

# ---------- 步骤 1 之前的检查 ----------

# 正在运行的进程名：-ProcessList 给的文件以每行一个进程名代替真实检查；
# 只有 suag / suga 才算命中，别的进程名不影响
function Get-RunningNames {
    if ($ProcessList) {
        if (-not (Test-Path -LiteralPath $ProcessList -PathType Leaf)) {
            Stop-Migration ('进程名单文件不存在：' + $ProcessList)
        }
        return @(Get-Content -LiteralPath $ProcessList |
                ForEach-Object { ($_.Trim() -replace '(?i)\.exe$', '').ToLower() } |
                Where-Object { $_ -eq 'suag' -or $_ -eq 'suga' } |
                Select-Object -Unique)
    }
    $procs = Get-Process -Name suag, suga -ErrorAction SilentlyContinue
    if ($procs) { return @($procs | Select-Object -ExpandProperty Name -Unique) }
    return @()
}

function Test-EmptyDir([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path -PathType Container)) { return $false }
    $items = @(Get-ChildItem -LiteralPath $Path -Force -ErrorAction SilentlyContinue)
    return ($items.Count -eq 0)
}

# ---------- 步骤 4：用户级环境变量 ----------

function Read-EnvStoreMap([string]$Path) {
    $map = New-Object System.Collections.Specialized.OrderedDictionary
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        Stop-Migration ('环境变量存储文件不存在：' + $Path)
    }
    $raw = Read-FileText $Path
    if ($raw.Trim()) {
        $obj = $raw | ConvertFrom-Json
        foreach ($p in $obj.PSObject.Properties) { $map[$p.Name] = [string]$p.Value }
    }
    return $map
}

# 名字 -> 新名字；不是 SUAG_ 开头的原样返回
function Get-RenamedEnvName([string]$Name) {
    if ($Name -like 'SUAG_*') { return ('SUGA_' + $Name.Substring(5)) }
    return $Name
}

function Invoke-UserEnvStep {
    if ($EnvStore) {
        # 测试用存储：整个 JSON 文件一次重写
        $map = Read-EnvStoreMap $EnvStore
        $names = @($map.Keys)
    } else {
        $map = [Environment]::GetEnvironmentVariables('User')
        $names = @($map.Keys)
    }
    $targets = @($names | Where-Object { $_ -like 'SUAG_*' })
    if ($targets.Count -eq 0) {
        Say '  4. 用户级环境变量：没有 SUAG_* 变量，无需修改'
        return
    }
    # 新名字已被占用时不动它，交给用户手工处理
    $todo = @()
    foreach ($name in $targets) {
        $newName = Get-RenamedEnvName $name
        if ($names -contains $newName) {
            SayErr ('  跳过 ' + $name + '：' + $newName + ' 已存在，请手工确认后处理')
            continue
        }
        $todo += $name
    }
    if ($todo.Count -eq 0) { return }
    foreach ($name in $todo) {
        Show-Step ('4. 用户级环境变量：' + $name + ' -> ' + (Get-RenamedEnvName $name) + '（值不变，不打印）')
    }
    if (-not $Apply) { return }
    if ($EnvStore) {
        try {
            Backup-File $EnvStore
            $newMap = New-Object System.Collections.Specialized.OrderedDictionary
            # 只改名真正要处理的键；跳过的键保持原名
            foreach ($k in $names) {
                if ($todo -contains $k) { $newMap[(Get-RenamedEnvName $k)] = $map[$k] }
                else { $newMap[$k] = $map[$k] }
            }
            Write-FileText -Path $EnvStore -Text ($newMap | ConvertTo-Json -Depth 4)
        } catch {
            Stop-Migration ('环境变量存储文件改名失败：' + $_.Exception.Message)
        }
        return
    }
    foreach ($name in $todo) {
        try {
            # 值只在内存里搬运，不打印
            $value = [Environment]::GetEnvironmentVariable($name, 'User')
            [Environment]::SetEnvironmentVariable((Get-RenamedEnvName $name), $value, 'User')
            [Environment]::SetEnvironmentVariable($name, $null, 'User')
        } catch {
            Stop-Migration ('用户级环境变量改名失败 ' + $name + '：' + $_.Exception.Message)
        }
    }
}

# ---------- 参数解析 ----------

$src = $null
if ($HomeDir) { $src = $HomeDir }
elseif ($env:SUAG_HOME) { $src = $env:SUAG_HOME }
else { $src = Join-Path $HOME '.suag' }
$src = [System.IO.Path]::GetFullPath($src.TrimEnd('\', '/'))

if ($Target) {
    $dst = [System.IO.Path]::GetFullPath($Target.TrimEnd('\', '/'))
} else {
    $dst = Join-Path ([System.IO.Path]::GetDirectoryName($src)) '.suga'
}

Say 'suag -> suga 本地配置迁移'
Say ('  源目录：' + $src)
Say ('  目标目录：' + $dst)
if ($Apply) { Say '  模式：执行（会修改文件与环境变量）' } else { Say '  模式：预览（不做任何修改，加 -Apply 执行）' }

# ---------- 前提检查 ----------

if (-not (Test-Path -LiteralPath $src)) {
    Say '源目录不存在，无需迁移。'
    exit 0
}
if (-not (Test-Path -LiteralPath $src -PathType Container)) {
    Stop-Migration ('源目录不是目录：' + $src)
}

$running = @(Get-RunningNames)
if ($running.Count -gt 0) {
    Stop-Migration ('检测到正在运行的进程：' + ($running -join ', ') +
        '（gofer 拉起的任务、chat、acp、trace web 都算）。请先结束它们再重试。')
}

if (Test-Path -LiteralPath $dst) {
    if (-not (Test-Path -LiteralPath $dst -PathType Container)) {
        Stop-Migration ('目标路径已存在而且不是目录：' + $dst)
    }
    if (-not (Test-EmptyDir $dst)) {
        Stop-Migration ('目标目录已存在且非空：' + $dst + '。请先备份并移走它，或改用 -Target。')
    }
    Say ('  目标目录已存在但为空，执行时会先删除它：' + $dst)
}

# ---------- 步骤 1：目录改名 ----------

if ($Apply) {
    try {
        if (Test-Path -LiteralPath $dst) { Remove-Item -LiteralPath $dst -Force }
        Move-Item -LiteralPath $src -Destination $dst -ErrorAction Stop
    } catch {
        Stop-Migration ('目录改名失败：' + $_.Exception.Message)
    }
    $script:RenameInfo = [pscustomobject]@{ From = $src; To = $dst }
    $script:DoneSteps += ('1. 目录改名：' + $src + ' -> ' + $dst)
}
Show-Step ('1. 目录改名：' + $src + ' -> ' + $dst)

# 后续步骤都在目标目录里操作（预览模式读源目录）
if ($Apply) { $base = $dst } else { $base = $src }

# ---------- 步骤 2：config.yaml ----------

$configPath = Join-Path $base 'config.yaml'
if (Test-Path -LiteralPath $configPath -PathType Leaf) {
    Invoke-FileMigration -Path $configPath -DisplayName 'config.yaml' -Rules (Get-ConfigRules)
    if ($Apply) { $script:DoneSteps += '2. config.yaml：agent 名、.suag 路径、SUAG_ 变量名' }
} else {
    Say '  config.yaml：不存在，跳过'
}

# ---------- 步骤 3：.env（只改变量名，不打印值） ----------

$envPath = Join-Path $base '.env'
if (Test-Path -LiteralPath $envPath -PathType Leaf) {
    Invoke-FileMigration -Path $envPath -DisplayName '.env' -Rules (Get-EnvRules) -NamesOnly
    if ($Apply) { $script:DoneSteps += '3. .env：SUAG_ 变量名改名为 SUGA_' }
} else {
    Say '  .env：不存在，跳过'
}

# ---------- 步骤 4：用户级环境变量 ----------

Invoke-UserEnvStep
if ($Apply) { $script:DoneSteps += '4. 用户级环境变量：SUAG_* -> SUGA_*' }

# ---------- 步骤 5：gofer agents.yaml（可选） ----------

if ($GoferAgents) {
    $agentsPath = [System.IO.Path]::GetFullPath($GoferAgents)
    if (-not (Test-Path -LiteralPath $agentsPath -PathType Leaf)) {
        Stop-Migration ('agents.yaml 不存在：' + $agentsPath)
    }
    Invoke-FileMigration -Path $agentsPath -DisplayName (Split-Path -Leaf $agentsPath) -Rules (Get-GoferAgentsRules)
    if ($Apply) { $script:DoneSteps += ('5. gofer agents：' + $agentsPath) }
} else {
    Say '  gofer agents.yaml：未指定 -GoferAgents，跳过'
}

# ---------- 结束提示 ----------

Say ''
if (-not $Apply) {
    Say '预览结束：没有做任何修改。确认无误后加 -Apply 执行。'
    exit 0
}

Say '迁移完成。接下来需要：'
Say '  1. 重开终端，让新的环境变量生效。'
Say '  2. 改了用户级环境变量的话，重启 gofer server（只 reload 不够，server 进程的环境不会更新）。'
if ($GoferAgents) { Say '  3. 用了 -GoferAgents：在 gofer 侧执行配置重载。' }
Say '  本脚本不改 gofer 数据库、不改工作区项目目录、不删除旧二进制。'
exit 0
