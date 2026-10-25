function Get-RepositoryBuildVersion {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)]
        [string]$RepositoryPath
    )

    $fallbackVersion = 'v0.0.0'
    if (Get-Command 'git' -ErrorAction SilentlyContinue) {
        $previousErrorActionPreference = $ErrorActionPreference
        try {
            $ErrorActionPreference = 'Continue'
            $global:LASTEXITCODE = 0
            $versionOutput = & git -C $RepositoryPath describe --tags --always --dirty 2>$null
            $version = [string]($versionOutput | Select-Object -First 1)
            if ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace($version)) {
                return $version.Trim()
            }
        } catch {
            # 继续使用下方稳定的回退值。
        } finally {
            $ErrorActionPreference = $previousErrorActionPreference
        }
    }

    Write-Warning "无法从 Git 获取构建版本，回退为 $fallbackVersion"
    return $fallbackVersion
}
