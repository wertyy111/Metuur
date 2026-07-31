[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$profilePath = $PROFILE.CurrentUserAllHosts
if (-not (Test-Path -LiteralPath $profilePath)) {
    Write-Host "Metuur VS Code autostart is already disabled."
    return
}

$startMarker = "# >>> METUUR VS CODE AUTOSTART >>>"
$endMarker = "# <<< METUUR VS CODE AUTOSTART <<<"
$existing = Get-Content -Raw -LiteralPath $profilePath
$pattern = "(?ms)^" + [regex]::Escape($startMarker) + ".*?^" + [regex]::Escape($endMarker) + "\r?\n?"
$updated = [regex]::Replace($existing, $pattern, "").TrimEnd()

if ($updated) {
    Set-Content -LiteralPath $profilePath -Value "$updated`r`n" -Encoding UTF8
} else {
    Remove-Item -LiteralPath $profilePath -Force
}

Write-Host "Metuur VS Code autostart disabled."
