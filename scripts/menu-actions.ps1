function Get-ProjectMenuActions {
    @(
        @{ Key = '1'; Group = '本地开发'; Label = '启动开发环境（构建后端 + 前端）'; Script = 'dev.ps1'; Arguments = @(); Window = $true; Confirm = ''; Detail = '构建并启动本地后端，运行前端开发服务器。' }
        @{ Key = '2'; Group = '本地开发'; Label = '启动开发环境（沿用镜像）'; Script = 'dev.ps1'; Arguments = @('-NoBuild'); Window = $true; Confirm = ''; Detail = '沿用本地镜像，启动后端和前端开发服务器。' }
        @{ Key = '3'; Group = '本地开发'; Label = '仅启动本地后端（构建）'; Script = 'dev.ps1'; Arguments = @('-BackendOnly'); Window = $false; Confirm = ''; Detail = '构建并启动本地 MySQL、Redis 和 New API 后端。' }
        @{ Key = '4'; Group = '本地开发'; Label = '停止本地后端（保留数据）'; Script = 'local.ps1'; Arguments = @('stop'); Window = $false; Confirm = ''; Detail = '停止本地 Docker 开发环境，保留数据库卷；前端在开发终端中停止。' }
        @{ Key = '5'; Group = '本地开发'; Label = '查看本地容器状态'; Script = 'local.ps1'; Arguments = @('status'); Window = $false; Confirm = ''; Detail = '查询本地 Compose 容器状态。' }
        @{ Key = '6'; Group = '统一发布'; Label = '生成发布计划（只读）'; Script = 'release.ps1'; Arguments = @('plan'); Window = $false; Confirm = ''; Detail = '核对线上基线、影响和验证范围，不构建或改动线上。' }
        @{ Key = '7'; Group = '统一发布'; Label = '执行完整发布'; Script = 'release.ps1'; Arguments = @('run'); Window = $false; Confirm = 'DEPLOY'; Detail = '自动计划、打 tag、构建一次、本地升级验证、上传、切换和验收。' }
        @{ Key = '8'; Group = '统一发布'; Label = '查看线上发布状态'; Script = 'release.ps1'; Arguments = @('status'); Window = $false; Confirm = ''; Detail = '只读查询持久发布阶段。' }
        @{ Key = '9'; Group = '数据维护'; Label = '刷新本地脱敏数据'; Script = 'dev.ps1'; Arguments = @('-RefreshSeed', '-NoBuild', '-BackendOnly'); Window = $false; Confirm = ''; Detail = '沿用镜像，刷新本地模型、定价和脱敏渠道数据，重启本地后端。' }
        @{ Key = '10'; Group = '数据维护'; Label = '重置本地数据库'; Script = 'dev.ps1'; Arguments = @('-ResetData', '-NoBuild', '-BackendOnly'); Window = $false; Confirm = 'RESET'; Detail = '删除本地开发数据库卷并沿用镜像重新初始化；现有本地数据会丢失。' }
        @{ Key = '11'; Group = '统一发布'; Label = '准备发布（不切换）'; Script = 'release.ps1'; Arguments = @('prepare'); Window = $false; Confirm = 'PREPARE'; Detail = '完成重型本地验证及上传加载，线上正常服务；记录发布 ID 用于继续。' }
        @{ Key = '12'; Group = '平滑部署维护'; Label = '检查并收尾旧实例（不强停）'; Script = 'release.ps1'; Arguments = @('cleanup'); Window = $false; Confirm = ''; Detail = '仅在旧连接和异步结算全部排空后停止旧实例；超时保留，可重复执行。' }
        @{ Key = '13'; Group = '平滑部署维护'; Label = '回切上一请求版本'; Script = 'release.ps1'; Arguments = @('rollback'); Window = $false; Confirm = 'ROLLBACK'; Detail = '确认旧版本与当前数据库兼容后回切请求流量；数据库和后台任务版本不会回退。' }
        @{ Key = '14'; Group = '平滑部署维护'; Label = '恢复中断的平滑发布'; Script = 'release.ps1'; Arguments = @('recover'); Window = $false; Confirm = 'RECOVER'; Detail = '恢复到最后已确认的请求实例并收尾候选；后台发布仍运行时发布锁会拒绝重复操作。首次迁移失败需按文档恢复。' }
    )
}

function Invoke-ProjectMenuAction {
    param([Parameter(Mandatory)] [hashtable]$Action)

    Write-Host "`n$($Action.Label)" -ForegroundColor Cyan
    Write-Host $Action.Detail
    Write-Host ('.\scripts\{0} {1}' -f $Action.Script, ($Action.Arguments -join ' ')) -ForegroundColor DarkGray
    if ($Action.Confirm) {
        $answer = Read-Host "输入 $($Action.Confirm) 确认，其他输入取消"
        if ($answer -cne $Action.Confirm) {
            Write-Host '已取消，未执行操作。' -ForegroundColor Yellow
            return
        }
    }

    if ($Action.Script -eq 'release.ps1' -and $Action.Arguments[0] -in @('cleanup', 'rollback', 'recover')) {
        $releaseId = Read-Host '输入已验收发布 ID（release-...）'
        if ($releaseId -notmatch '^release-[A-Za-z0-9_.-]+$') { throw '发布 ID 无效' }
        $Action = $Action.Clone()
        $Action.Arguments = @($Action.Arguments) + @('-Release', $releaseId)
    }
    $scriptPath = Join-Path $PSScriptRoot $Action.Script
    $shellName = if ($PSVersionTable.PSEdition -eq 'Core') { 'pwsh.exe' } else { 'powershell.exe' }
    $shellPath = Join-Path $PSHOME $shellName
    $scriptArguments = @('-NoProfile', '-File', $scriptPath) + $Action.Arguments
    if ($Action.Window) {
        Write-Host '开发服务将在独立终端运行；在该终端按 Ctrl+C 停止前端，关闭终端后返回菜单。'
        # Start-Process joins ArgumentList into a string, so quote the script path.
        $windowArguments = @('-NoProfile', '-File', ('"{0}"' -f $scriptPath)) + $Action.Arguments
        $process = Start-Process -FilePath $shellPath -ArgumentList $windowArguments -WorkingDirectory (Split-Path -Parent $PSScriptRoot) -WindowStyle Normal -Wait -PassThru
        $result = $process.ExitCode
    } else {
        $previousPreference = $ErrorActionPreference
        try {
            $ErrorActionPreference = 'Continue'
            & $shellPath @scriptArguments
            $result = $LASTEXITCODE
        } finally {
            $ErrorActionPreference = $previousPreference
        }
    }
    if ($result -ne 0) {
        throw "子脚本退出码 $result，请查看上方输出。"
    }
    Write-Host '操作已结束。' -ForegroundColor Green
}
