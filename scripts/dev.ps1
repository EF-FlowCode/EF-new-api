[CmdletBinding()]
param(
    [switch]$NoBuild,
    [switch]$ResetData,
    [switch]$RefreshSeed,
    [switch]$BackendOnly
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$RepoRoot = Split-Path -Parent $PSScriptRoot
$ComposeFile = Join-Path $RepoRoot 'docker-compose.dev.yml'
$WebDir = Join-Path $RepoRoot 'web'
$SeedDir = Join-Path $RepoRoot '.ai-governance\workspace\snapshots'
$BackendUrl = 'http://127.0.0.1:3000'
$FrontendUrl = 'http://127.0.0.1:3001'
$MySqlContainer = 'new-api-dev-mysql'
$MySqlDatabase = 'new_api'
$MySqlPassword = '123456'
$AdminUsername = 'root'
$AdminPassword = if ($env:NEW_API_LOCAL_ADMIN_PASSWORD) {
    $env:NEW_API_LOCAL_ADMIN_PASSWORD
} else {
    '123456789'
}

. (Join-Path $PSScriptRoot 'build-version.ps1')

function Invoke-Checked {
    param(
        [Parameter(Mandatory)] [string]$FilePath,
        [Parameter(ValueFromRemainingArguments)] [string[]]$Arguments
    )

    $global:LASTEXITCODE = 0
    & $FilePath @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "命令执行失败 ($LASTEXITCODE): $FilePath $($Arguments -join ' ')"
    }
}

function Invoke-DevCompose {
    $composeArguments = @('compose', '-f', $ComposeFile) + @($args)
    Invoke-Checked -FilePath 'docker' -Arguments $composeArguments
}

function Invoke-DevComposeBuild {
    $buildVersion = Get-RepositoryBuildVersion -RepositoryPath $RepoRoot
    $previousBuildVersion = [Environment]::GetEnvironmentVariable(
        'BUILD_VERSION',
        [EnvironmentVariableTarget]::Process
    )
    try {
        [Environment]::SetEnvironmentVariable(
            'BUILD_VERSION',
            $buildVersion,
            [EnvironmentVariableTarget]::Process
        )
        Write-Host "本地镜像版本：$buildVersion" -ForegroundColor Cyan
        Invoke-DevCompose @args
    } finally {
        [Environment]::SetEnvironmentVariable(
            'BUILD_VERSION',
            $previousBuildVersion,
            [EnvironmentVariableTarget]::Process
        )
    }
}

function Wait-Backend {
    $deadline = (Get-Date).AddMinutes(5)
    do {
        try {
            $status = Invoke-RestMethod -Uri "$BackendUrl/api/status" -TimeoutSec 5
            if ($status.success) {
                return
            }
        } catch {
            Start-Sleep -Seconds 2
        }
    } while ((Get-Date) -lt $deadline)

    Invoke-DevCompose logs --tail 100 new-api
    throw '等待本地 New API 后端启动超时'
}

function Get-LatestSeedFile {
    param(
        [Parameter(Mandatory)] [string]$Pattern
    )

    if (-not (Test-Path -LiteralPath $SeedDir)) {
        return $null
    }

    $file = Get-ChildItem -LiteralPath $SeedDir -Filter $Pattern -File |
        Sort-Object Name -Descending |
        Select-Object -First 1
    if ($file) {
        return $file.FullName
    }
    return $null
}

function Get-SanitizedSeedFiles {
    return @{
        Core = Get-LatestSeedFile -Pattern 'new-api-dev-sanitized-*.sql'
        Pricing = Get-LatestSeedFile -Pattern 'remote-pricing-options-*.sql'
        Channels = Get-LatestSeedFile -Pattern 'remote-sanitized-channels-*.sql'
    }
}

function Import-MySqlFile {
    param(
        [Parameter(Mandatory)] [string]$Path,
        [Parameter(Mandatory)] [string]$Label
    )

    $tempPath = "/tmp/$([IO.Path]::GetFileName($Path))"
    Write-Host "正在导入${Label}：$Path" -ForegroundColor Cyan
    Invoke-Checked -FilePath 'docker' -Arguments @('cp', $Path, "${MySqlContainer}:$tempPath")
    try {
        $importCommand = "mysql -uroot -p$MySqlPassword --default-character-set=utf8mb4 $MySqlDatabase < '$tempPath'"
        Invoke-Checked -FilePath 'docker' -Arguments @('exec', $MySqlContainer, 'sh', '-c', $importCommand)
    } finally {
        & docker exec $MySqlContainer rm -f $tempPath *> $null
    }
}

function Import-SanitizedSeed {
    param([Parameter(Mandatory)] [hashtable]$Files)

    Import-MySqlFile -Path $Files.Core -Label '模型、供应商和能力数据'
    Import-MySqlFile -Path $Files.Pricing -Label '定价与分组配置'
    Import-MySqlFile -Path $Files.Channels -Label '脱敏渠道结构'

    Write-Host '正在重启后端以刷新模型、渠道和定价缓存...' -ForegroundColor Cyan
    Invoke-DevCompose restart new-api
    Wait-Backend
}

function Get-SetupState {
    return Invoke-RestMethod -Uri "$BackendUrl/api/setup" -TimeoutSec 10
}

function Ensure-LocalAdmin {
    $setup = Get-SetupState
    if ($setup.data.status) {
        return $false
    }

    $body = @{
        username = $AdminUsername
        password = $AdminPassword
        confirmPassword = $AdminPassword
        SelfUseModeEnabled = $false
        DemoSiteEnabled = $false
    } | ConvertTo-Json
    $result = Invoke-RestMethod -Uri "$BackendUrl/api/setup" -Method Post -ContentType 'application/json' -Body $body -TimeoutSec 30
    if (-not $result.success) {
        throw "创建本地管理员失败：$($result.message)"
    }
    return $true
}

if (-not (Test-Path -LiteralPath $ComposeFile)) {
    throw "找不到 Docker Compose 配置：$ComposeFile"
}

$global:LASTEXITCODE = 0
& docker info *> $null
if ($LASTEXITCODE -ne 0) {
    throw 'Docker Desktop 未运行或当前 Docker 上下文不可用。'
}

if ($ResetData) {
    Write-Host '正在删除本地开发数据卷...' -ForegroundColor Yellow
    Invoke-DevCompose down -v --remove-orphans
}

$upArguments = @('up', '-d')
if ($NoBuild) {
    $upArguments += '--no-build'
} else {
    $upArguments += '--build'
}

Write-Host '正在启动本地 MySQL、Redis 和 New API 后端...' -ForegroundColor Cyan
if ($NoBuild) {
    Invoke-DevCompose @upArguments
} else {
    Invoke-DevComposeBuild @upArguments
}
Wait-Backend

$setupState = Get-SetupState
$seedFiles = Get-SanitizedSeedFiles
$seedAvailable = $seedFiles.Core -and $seedFiles.Pricing -and $seedFiles.Channels
if ($RefreshSeed -and -not $seedAvailable) {
    throw "刷新失败：$SeedDir 中缺少模型、定价或脱敏渠道安全快照"
}
$shouldImportSeed = $RefreshSeed -or (($ResetData -or (-not $setupState.data.status)) -and $seedAvailable)
if ($shouldImportSeed) {
    Import-SanitizedSeed -Files $seedFiles
} elseif (($ResetData -or (-not $setupState.data.status)) -and -not $seedAvailable) {
    Write-Warning "未找到完整安全快照，将初始化空白本地数据库：$SeedDir"
}
$adminCreated = Ensure-LocalAdmin

Write-Host "后端已就绪：$BackendUrl" -ForegroundColor Green
if ($adminCreated) {
    Write-Host "本地管理员：$AdminUsername / $AdminPassword"
} else {
    Write-Host '本地管理员：沿用现有数据库账号'
}
Write-Host "MySQL：127.0.0.1:3307 / root / $MySqlPassword / $MySqlDatabase"
Write-Host '停止后端：docker compose -f docker-compose.dev.yml down'

if ($BackendOnly) {
    return
}

if (-not (Get-Command 'bun' -ErrorAction SilentlyContinue)) {
    throw '未找到 bun，请安装 Bun 或使用 -BackendOnly 仅启动 Docker 后端。'
}

if (-not (Test-Path -LiteralPath (Join-Path $WebDir 'node_modules'))) {
    Write-Host '首次启动，正在安装前端依赖...' -ForegroundColor Cyan
    Push-Location $WebDir
    try {
        Invoke-Checked -FilePath 'bun' -Arguments @('install')
    } finally {
        Pop-Location
    }
}

Write-Host "正在启动前端：$FrontendUrl" -ForegroundColor Cyan
Write-Host '按 Ctrl+C 停止前端开发服务器；Docker 后端会继续运行。'
Push-Location $WebDir
try {
    Invoke-Checked -FilePath 'bun' -Arguments @('run', 'dev', '--port', '3001')
} finally {
    Pop-Location
}
