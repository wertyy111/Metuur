[CmdletBinding()]
param(
    [string]$OutputPath,
    [string]$CodeCommand,
    [string]$ExtensionsDir,
    [switch]$NoInstall
)

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot
$extensionRoot = Join-Path $projectRoot "integrations\vscode"
$packagePath = Join-Path $extensionRoot "package.json"
$entryPoint = Join-Path $extensionRoot "extension.js"
$licensePath = Join-Path $projectRoot "LICENSE"

foreach ($requiredFile in @($packagePath, $entryPoint, $licensePath)) {
    if (-not (Test-Path -LiteralPath $requiredFile -PathType Leaf)) {
        throw "Required extension file is missing: $requiredFile"
    }
}

$package = Get-Content -Raw -LiteralPath $packagePath | ConvertFrom-Json
if (-not $package.name -or -not $package.version -or -not $package.publisher) {
    throw "package.json must define name, version, and publisher"
}

if (-not $OutputPath) {
    $distDirectory = Join-Path $projectRoot "dist"
    $OutputPath = Join-Path $distDirectory ("{0}-{1}.vsix" -f $package.name, $package.version)
}
$OutputPath = [System.IO.Path]::GetFullPath($OutputPath)
$outputDirectory = Split-Path -Parent $OutputPath
New-Item -ItemType Directory -Force -Path $outputDirectory | Out-Null

$stage = Join-Path ([System.IO.Path]::GetTempPath()) ("metuur-vsix-" + [guid]::NewGuid().ToString("N"))
$stageExtension = Join-Path $stage "extension"
New-Item -ItemType Directory -Force -Path $stageExtension | Out-Null

try {
    Copy-Item -LiteralPath $packagePath -Destination (Join-Path $stageExtension "package.json")
    Copy-Item -LiteralPath $entryPoint -Destination (Join-Path $stageExtension "extension.js")
    Copy-Item -LiteralPath $licensePath -Destination (Join-Path $stageExtension "LICENSE.txt")

    $xmlEscape = {
        param([string]$Value)
        [System.Security.SecurityElement]::Escape($Value)
    }
    $identityId = & $xmlEscape $package.name
    $identityVersion = & $xmlEscape $package.version
    $identityPublisher = & $xmlEscape $package.publisher
    $displayName = & $xmlEscape $package.displayName
    $description = & $xmlEscape $package.description
    $engine = & $xmlEscape $package.engines.vscode

    $manifest = @"
<?xml version="1.0" encoding="utf-8"?>
<PackageManifest Version="2.0.0" xmlns="http://schemas.microsoft.com/developer/vsx-schema/2011">
  <Metadata>
    <Identity Language="en-US" Id="$identityId" Version="$identityVersion" Publisher="$identityPublisher" />
    <DisplayName>$displayName</DisplayName>
    <Description xml:space="preserve">$description</Description>
    <Categories>Other</Categories>
    <Properties>
      <Property Id="Microsoft.VisualStudio.Code.Engine" Value="$engine" />
      <Property Id="Microsoft.VisualStudio.Code.ExtensionKind" Value="workspace" />
    </Properties>
  </Metadata>
  <Installation>
    <InstallationTarget Id="Microsoft.VisualStudio.Code" />
  </Installation>
  <Dependencies />
  <Assets>
    <Asset Type="Microsoft.VisualStudio.Code.Manifest" Path="extension/package.json" Addressable="true" />
  </Assets>
</PackageManifest>
"@

    $contentTypes = @'
<?xml version="1.0" encoding="utf-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="json" ContentType="application/json" />
  <Default Extension="js" ContentType="application/javascript" />
  <Default Extension="txt" ContentType="text/plain" />
  <Default Extension="vsixmanifest" ContentType="text/xml" />
</Types>
'@

    Set-Content -LiteralPath (Join-Path $stage "extension.vsixmanifest") -Value $manifest -Encoding UTF8
    Set-Content -LiteralPath (Join-Path $stage "[Content_Types].xml") -Value $contentTypes -Encoding UTF8

    if (Test-Path -LiteralPath $OutputPath) {
        Remove-Item -LiteralPath $OutputPath -Force
    }
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    [System.IO.Compression.ZipFile]::CreateFromDirectory(
        $stage,
        $OutputPath,
        [System.IO.Compression.CompressionLevel]::Optimal,
        $false
    )
}
finally {
    if (Test-Path -LiteralPath $stage) {
        Remove-Item -LiteralPath $stage -Recurse -Force
    }
}

Write-Host "VSIX: $OutputPath"

if ($NoInstall) {
    return
}

if (-not $CodeCommand) {
    $command = Get-Command code.cmd, code -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($command) {
        $CodeCommand = $command.Source
    }
}
if (-not $CodeCommand) {
    foreach ($candidate in @(
        (Join-Path $env:LOCALAPPDATA "Programs\Microsoft VS Code\bin\code.cmd"),
        "C:\Microsoft VS Code\bin\code.cmd",
        "C:\Program Files\Microsoft VS Code\bin\code.cmd"
    )) {
        if (Test-Path -LiteralPath $candidate -PathType Leaf) {
            $CodeCommand = $candidate
            break
        }
    }
}
if (-not $CodeCommand -or -not (Test-Path -LiteralPath $CodeCommand -PathType Leaf)) {
    throw "VS Code CLI was not found. Pass -CodeCommand with the path to code.cmd."
}

$installArgs = @("--install-extension", $OutputPath, "--force")
if ($ExtensionsDir) {
    $ExtensionsDir = [System.IO.Path]::GetFullPath($ExtensionsDir)
    New-Item -ItemType Directory -Force -Path $ExtensionsDir | Out-Null
    $installArgs = @("--extensions-dir", $ExtensionsDir) + $installArgs
}
& $CodeCommand @installArgs
if ($LASTEXITCODE -ne 0) {
    throw "VS Code extension installation failed with exit code $LASTEXITCODE"
}
Write-Host "Metuur VS Code Bridge installed. Reload VS Code to activate it."
if ($ExtensionsDir) {
    Write-Host "Launch VS Code with: code --extensions-dir `"$ExtensionsDir`""
}
