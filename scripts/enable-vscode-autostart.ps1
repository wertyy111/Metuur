[CmdletBinding()]
param(
    [string]$Executable = (Join-Path $env:LOCALAPPDATA "Programs\Metuur\metuur.exe")
)

$ErrorActionPreference = "Stop"
$profilePath = $PROFILE.CurrentUserAllHosts
$startMarker = "# >>> METUUR VS CODE AUTOSTART >>>"
$endMarker = "# <<< METUUR VS CODE AUTOSTART <<<"

$existing = ""
if (Test-Path -LiteralPath $profilePath) {
    $existing = Get-Content -Raw -LiteralPath $profilePath
    $pattern = "(?ms)^" + [regex]::Escape($startMarker) + ".*?^" + [regex]::Escape($endMarker) + "\r?\n?"
    $existing = [regex]::Replace($existing, $pattern, "").TrimEnd()
}

$escapedExecutable = $Executable.Replace("'", "''")
$block = @"
$startMarker
if (`$env:TERM_PROGRAM -eq 'vscode' -and `$env:METUUR_ACTIVE -ne '1' -and `$env:METUUR_DISABLE_AUTOSTART -ne '1') {
    `$metuurExecutable = '$escapedExecutable'
    if (Test-Path -LiteralPath `$metuurExecutable) {
        & `$metuurExecutable
    }
}
$endMarker
"@

$parent = Split-Path -Parent $profilePath
New-Item -ItemType Directory -Force -Path $parent | Out-Null
$content = if ($existing) { "$existing`r`n`r`n$block`r`n" } else { "$block`r`n" }
Set-Content -LiteralPath $profilePath -Value $content -Encoding UTF8

Write-Host "Metuur VS Code autostart enabled in: $profilePath"
Write-Host "Executable: $Executable"
