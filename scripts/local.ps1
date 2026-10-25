[CmdletBinding()]
param([Parameter(Position = 0)][ValidateSet('stop', 'status', 'build')][string]$Action = 'status')
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'build-version.ps1')
$repo = Split-Path -Parent $PSScriptRoot
Push-Location $repo
try {
    switch ($Action) {
        'stop' { docker compose -f docker-compose.dev.yml stop }
        'status' { docker compose -f docker-compose.dev.yml ps -a }
        'build' {
            $version = Get-RepositoryBuildVersion -RepositoryPath $repo
            docker build --build-arg "BUILD_VERSION=$version" -t new-api-dev:manual .
        }
    }
    if ($LASTEXITCODE -ne 0) { throw "本地命令失败：$LASTEXITCODE" }
} finally { Pop-Location }
