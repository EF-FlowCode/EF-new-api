[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
. (Join-Path $PSScriptRoot 'scripts\menu-actions.ps1')

$actions = @(Get-ProjectMenuActions)
while ($true) {
    Write-Host ''
    Write-Host 'New API 项目菜单' -ForegroundColor Cyan
    Write-Host "项目：$PSScriptRoot" -ForegroundColor DarkGray
    $lastGroup = ''
    foreach ($action in $actions) {
        if ($action.Group -ne $lastGroup) {
            Write-Host "`n$($action.Group)" -ForegroundColor Yellow
            $lastGroup = $action.Group
        }
        Write-Host ('  {0,2}. {1}' -f $action.Key, $action.Label)
    }
    Write-Host "`n   0. 退出"
    $choice = Read-Host '请选择编号'
    if ($null -eq $choice -or $choice.Trim() -eq '0') { break }
    $selected = $actions | Where-Object { $_.Key -eq $choice.Trim() } | Select-Object -First 1
    if ($null -eq $selected) {
        Write-Host '编号无效，请重新选择。' -ForegroundColor Yellow
        continue
    }
    try {
        Invoke-ProjectMenuAction -Action $selected
    } catch {
        Write-Host "操作失败：$($_.Exception.Message)" -ForegroundColor Red
    }
    $null = Read-Host '按回车返回菜单'
}
