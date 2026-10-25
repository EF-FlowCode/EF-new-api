[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$repoRoot = Split-Path -Parent $PSScriptRoot
$testRoot = Join-Path ([IO.Path]::GetTempPath()) ('new-api-menu-test-' + [guid]::NewGuid().ToString('N'))
$fixtureRoot = Join-Path $testRoot 'project with spaces'
$fixtureScripts = Join-Path $fixtureRoot 'scripts'
$tracePath = Join-Path $testRoot 'trace.jsonl'
$shellName = if ($PSVersionTable.PSEdition -eq 'Core') { 'pwsh.exe' } else { 'powershell.exe' }
$shellPath = Join-Path $PSHOME $shellName
$previousTrace = $env:NEW_API_MENU_TEST_TRACE
$previousExit = $env:NEW_API_MENU_TEST_EXIT

function Assert-MenuTest {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

function Invoke-MenuTest {
    param([string[]]$InputLines, [int]$ChildExit = 0)
    if (Test-Path -LiteralPath $tracePath) { Remove-Item -LiteralPath $tracePath }
    $env:NEW_API_MENU_TEST_EXIT = [string]$ChildExit
    $output = $InputLines | & $shellPath -NoProfile -File (Join-Path $fixtureRoot 'menu.ps1') 2>&1 | Out-String
    Assert-MenuTest ($LASTEXITCODE -eq 0) 'Menu process exited with failure'
    $records = @()
    if (Test-Path -LiteralPath $tracePath) {
        $records = @(Get-Content -LiteralPath $tracePath -Encoding UTF8 | ForEach-Object { $_ | ConvertFrom-Json })
    }
    return @{ Output = $output; Records = $records }
}

try {
    foreach ($file in (@(Get-Item (Join-Path $repoRoot 'menu.ps1')) + @(Get-ChildItem $PSScriptRoot -Filter '*.ps1'))) {
        $parseTokens = $null
        $parseErrors = $null
        $null = [System.Management.Automation.Language.Parser]::ParseFile($file.FullName, [ref]$parseTokens, [ref]$parseErrors)
        Assert-MenuTest ($parseErrors.Count -eq 0) "Syntax error: $($file.Name)"
    }

    # Exercise the relocated scripts from another directory without invoking Docker.
    & {
        $observed = @{}
        function docker {
            $observed.Directory = (Get-Location).Path
            $observed.Arguments = $args -join ' '
            $global:LASTEXITCODE = 0
        }
        Push-Location ([IO.Path]::GetTempPath())
        try {
            & (Join-Path $PSScriptRoot 'local.ps1') status
            Assert-MenuTest ($observed.Directory -eq $repoRoot) 'Deploy did not resolve the repository root'
            Assert-MenuTest ($observed.Arguments -eq 'compose -f docker-compose.dev.yml ps -a') 'Wrong local status command'
            function docker { $global:LASTEXITCODE = 91 }
            $failure = ''
            try { & (Join-Path $PSScriptRoot 'dev.ps1') -BackendOnly } catch { $failure = $_.Exception.Message }
            Assert-MenuTest ($failure -like 'Docker Desktop*') 'Dev script failed before reaching the mocked Docker check'
        } finally {
            Pop-Location
        }
    }

    $null = New-Item -ItemType Directory -Path $fixtureScripts -Force
    Copy-Item -LiteralPath (Join-Path $repoRoot 'menu.ps1') -Destination $fixtureRoot
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'menu-actions.ps1') -Destination $fixtureScripts
    $fixture = @'
[pscustomobject]@{ Script = (Split-Path -Leaf $PSCommandPath); Arguments = @($args) } |
    ConvertTo-Json -Compress | Add-Content -LiteralPath $env:NEW_API_MENU_TEST_TRACE -Encoding UTF8
exit ([int]$env:NEW_API_MENU_TEST_EXIT)
'@
    foreach ($name in @('dev.ps1', 'local.ps1', 'release.ps1')) {
        Set-Content -LiteralPath (Join-Path $fixtureScripts $name) -Value $fixture -Encoding UTF8
    }
    $env:NEW_API_MENU_TEST_TRACE = $tracePath

    $cases = @(
        @{ Key = '3'; Script = 'dev.ps1'; Args = '-BackendOnly'; Confirm = '' }
        @{ Key = '4'; Script = 'local.ps1'; Args = 'stop'; Confirm = '' }
        @{ Key = '5'; Script = 'local.ps1'; Args = 'status'; Confirm = '' }
        @{ Key = '6'; Script = 'release.ps1'; Args = 'plan'; Confirm = '' }
        @{ Key = '7'; Script = 'release.ps1'; Args = 'run'; Confirm = 'DEPLOY' }
        @{ Key = '8'; Script = 'release.ps1'; Args = 'status'; Confirm = '' }
        @{ Key = '9'; Script = 'dev.ps1'; Args = '-RefreshSeed -NoBuild -BackendOnly'; Confirm = '' }
        @{ Key = '10'; Script = 'dev.ps1'; Args = '-ResetData -NoBuild -BackendOnly'; Confirm = 'RESET' }
        @{ Key = '11'; Script = 'release.ps1'; Args = 'prepare'; Confirm = 'PREPARE' }
        @{ Key = '12'; Script = 'release.ps1'; Args = 'cleanup -Release release-test'; Confirm = '' }
        @{ Key = '13'; Script = 'release.ps1'; Args = 'rollback -Release release-test'; Confirm = 'ROLLBACK' }
        @{ Key = '14'; Script = 'release.ps1'; Args = 'recover -Release release-test'; Confirm = 'RECOVER' }
    )
    foreach ($case in $cases) {
        $inputLines = @($case.Key)
        if ($case.Confirm) { $inputLines += $case.Confirm }
        if ($case.Key -in @('12', '13', '14')) { $inputLines += 'release-test' }
        $result = Invoke-MenuTest -InputLines ($inputLines + @('', '0'))
        Assert-MenuTest ($result.Records.Count -eq 1) "Expected one child for action $($case.Key)"
        Assert-MenuTest ($result.Records[0].Script -eq $case.Script) "Wrong script for action $($case.Key)"
        Assert-MenuTest (($result.Records[0].Arguments -join ' ') -eq $case.Args) "Wrong arguments for action $($case.Key)"
    }
    foreach ($key in @('7', '10', '11', '13', '14')) {
        $result = Invoke-MenuTest -InputLines @($key, 'NO', '', '0')
        Assert-MenuTest ($result.Records.Count -eq 0) "Cancelled action $key launched a process"
    }
    $result = Invoke-MenuTest -InputLines @('invalid', '0')
    Assert-MenuTest ($result.Records.Count -eq 0) 'Invalid action launched a process'
    $result = Invoke-MenuTest -InputLines @('5', '', '5', '', '0') -ChildExit 37
    Assert-MenuTest ($result.Records.Count -eq 2) 'Child failure prevented returning to the menu'
    Assert-MenuTest ($result.Output -match '37') 'Child exit code was not displayed'

    . (Join-Path $fixtureScripts 'menu-actions.ps1')
    function Start-Process {
        param($FilePath, $ArgumentList, $WorkingDirectory, $WindowStyle, [switch]$Wait, [switch]$PassThru)
        $script:windowCall = $PSBoundParameters
        return [pscustomobject]@{ ExitCode = 0 }
    }
    $actions = @(Get-ProjectMenuActions)
    foreach ($key in @('1', '2')) {
        $action = $actions | Where-Object { $_.Key -eq $key }
        Invoke-ProjectMenuAction -Action $action
        Assert-MenuTest ($script:windowCall.ArgumentList[2] -eq ('"{0}"' -f (Join-Path $fixtureScripts 'dev.ps1'))) 'Window script path was not quoted'
        Assert-MenuTest ($script:windowCall.WorkingDirectory -eq $fixtureRoot) 'Wrong window working directory'
        Assert-MenuTest ($script:windowCall.WindowStyle -eq 'Normal' -and $script:windowCall.Wait) 'Development terminal must be visible and waited for'
        if ($key -eq '2') {
            Assert-MenuTest ($script:windowCall.ArgumentList[3] -eq '-NoBuild') 'NoBuild argument missing'
        }
    }
    Write-Host 'PASS: syntax, relocated scripts, all 14 actions, confirmations, invalid input, child failure recovery and paths with spaces'
} finally {
    $env:NEW_API_MENU_TEST_TRACE = $previousTrace
    $env:NEW_API_MENU_TEST_EXIT = $previousExit
    if (Test-Path -LiteralPath $testRoot) {
        $resolved = (Resolve-Path -LiteralPath $testRoot).Path
        $expectedPrefix = Join-Path ([IO.Path]::GetTempPath()) 'new-api-menu-test-'
        if (-not $resolved.StartsWith($expectedPrefix, [StringComparison]::OrdinalIgnoreCase)) {
            throw 'Unexpected test cleanup path'
        }
        Remove-Item -LiteralPath $resolved -Recurse -Force
    }
}
