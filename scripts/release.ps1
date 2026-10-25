[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidateSet('plan', 'verify', 'prepare', 'run', 'resume', 'status', 'recover', 'cleanup', 'rollback', 'retire-rollback')]
    [string]$Action = 'plan',
    [string]$Config = '',
    [string]$Release = '',
    [string]$Baseline = '',
    [switch]$Maintenance,
    [switch]$WorkerHandoff
)
$ErrorActionPreference = 'Stop'
$python = if ($env:NEW_API_PYTHON) { $env:NEW_API_PYTHON } else { 'E:\path\python\python.exe' }
$arguments = @('-X', 'utf8', (Join-Path $PSScriptRoot 'release.py'), $Action)
if ($Config) { $arguments += @('--config', $Config) }
if ($Release) { $arguments += @('--release', $Release) }
if ($Baseline) { $arguments += @('--baseline', $Baseline) }
if ($Maintenance) { $arguments += '--maintenance' }
if ($WorkerHandoff) { $arguments += '--worker-handoff' }
& $python @arguments
exit $LASTEXITCODE
