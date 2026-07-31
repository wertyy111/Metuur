[CmdletBinding(SupportsShouldProcess)]
param(
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA "Programs\Metuur"),
    [switch]$KeepConfig
)

$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
$entries = @($userPath -split ";" | Where-Object { $_ -and $_ -ne $InstallDir })
[Environment]::SetEnvironmentVariable("Path", ($entries -join ";"), "User")

if ($PSCmdlet.ShouldProcess($InstallDir, "Remove Metuur installation")) {
    Remove-Item -LiteralPath $InstallDir -Recurse -Force -ErrorAction SilentlyContinue
}

if (-not $KeepConfig) {
    $configDir = Join-Path $env:LOCALAPPDATA "Metuur"
    if ($PSCmdlet.ShouldProcess($configDir, "Remove Metuur configuration and history")) {
        Remove-Item -LiteralPath $configDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}

Write-Host "Metuur removed. Restart the terminal to refresh PATH."
