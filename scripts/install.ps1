[CmdletBinding()]
param(
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA "Programs\Metuur"),
    [switch]$NoPath,
    [switch]$InstallVSCodeBridge
)

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot
$binary = Join-Path $InstallDir "metuur.exe"

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "Go is not installed. Install Go 1.25+ from https://go.dev/dl/"
}

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
Push-Location $projectRoot
try {
    go build -buildvcs=false -trimpath -ldflags="-s -w" -o $binary ./cmd/metuur
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed with exit code $LASTEXITCODE"
    }
}
finally {
    Pop-Location
}

& $binary config init

if ($InstallVSCodeBridge) {
    try {
        & (Join-Path $PSScriptRoot "build-install-vscode-extension.ps1")
    }
    catch {
        Write-Warning "Metuur was installed, but the optional VS Code active-file bridge could not be installed: $($_.Exception.Message)"
    }
}

if (-not $NoPath) {
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $entries = @($userPath -split ";" | Where-Object { $_ })
    if ($entries -notcontains $InstallDir) {
        $newPath = (@($entries) + $InstallDir) -join ";"
        [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
        $env:Path = "$env:Path;$InstallDir"
        Write-Host "Added $InstallDir to the user PATH."
    }
}

Write-Host "Metuur installed: $binary"
Write-Host "Open a new VS Code terminal and run: metuur"
