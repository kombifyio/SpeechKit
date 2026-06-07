param(
    [Parameter(Mandatory = $true)]
    [string]$SourceDir,
    [string]$DestinationDir = 'release/latest/windows',
    [string]$Version = '',
    [string]$SourceRepo = 'kombifyio/SpeechKit',
    [string]$SourceReleaseUrl = '',
    [string]$CommitSha = '',
    [switch]$RequireInstaller,
    [switch]$MetadataOnly,
    [int64]$MaxInstallerBytes = 300000000
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$projectDir = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$sourceRoot = [System.IO.Path]::GetFullPath((Join-Path $projectDir $SourceDir))
if ([System.IO.Path]::IsPathRooted($SourceDir)) {
    $sourceRoot = [System.IO.Path]::GetFullPath($SourceDir)
}
$destinationRoot = [System.IO.Path]::GetFullPath((Join-Path $projectDir $DestinationDir))
if ([System.IO.Path]::IsPathRooted($DestinationDir)) {
    $destinationRoot = [System.IO.Path]::GetFullPath($DestinationDir)
}

if (-not (Test-Path -LiteralPath $sourceRoot)) {
    throw "SourceDir not found: $sourceRoot"
}

$installer = Get-ChildItem -LiteralPath $sourceRoot -Filter 'SpeechKit-Setup.exe' -File -ErrorAction SilentlyContinue | Select-Object -First 1
if ($RequireInstaller -and $null -eq $installer) {
    throw "SpeechKit-Setup.exe is required but was not found in $sourceRoot"
}

if ($null -ne $installer -and $installer.Length -gt $MaxInstallerBytes) {
    throw "SpeechKit-Setup.exe is $($installer.Length) bytes, above the repository mirror limit of $MaxInstallerBytes bytes. Keep it as a release asset only or raise the limit deliberately."
}

New-Item -ItemType Directory -Path $destinationRoot -Force | Out-Null

$assetNames = @(
    'SpeechKit-Setup.exe',
    'SpeechKit-x64.msi',
    'UNSIGNED-WINDOWS-RELEASE.txt'
)

foreach ($assetName in $assetNames) {
    $target = Join-Path $destinationRoot $assetName
    if (Test-Path -LiteralPath $target) {
        Remove-Item -LiteralPath $target -Force
    }

    $source = Join-Path $sourceRoot $assetName
    if (Test-Path -LiteralPath $source) {
        $sourceItem = Get-Item -LiteralPath $source
        if (($sourceItem.Extension -in @('.exe', '.msi')) -and $sourceItem.Length -gt $MaxInstallerBytes) {
            throw "$assetName is $($sourceItem.Length) bytes, above the repository mirror limit of $MaxInstallerBytes bytes. Keep it as a release asset only or raise the limit deliberately."
        }
        if (-not $MetadataOnly) {
            Copy-Item -LiteralPath $source -Destination $target -Force
        }
    }
}

$mirroredFiles = foreach ($assetName in $assetNames) {
    $path = if ($MetadataOnly) { Join-Path $sourceRoot $assetName } else { Join-Path $destinationRoot $assetName }
    if (-not (Test-Path -LiteralPath $path)) {
        continue
    }
    $item = Get-Item -LiteralPath $path
    $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $path).Hash.ToLowerInvariant()
    $downloadUrl = ''
    if (-not [string]::IsNullOrWhiteSpace($Version)) {
        $downloadUrl = "https://github.com/$SourceRepo/releases/download/$Version/$assetName"
    }
    [ordered]@{
        name = $assetName
        size_bytes = $item.Length
        sha256 = $hash
        download_url = $downloadUrl
    }
}

if ($RequireInstaller -and -not ($mirroredFiles | Where-Object { $_.name -eq 'SpeechKit-Setup.exe' })) {
    throw 'Installer mirror did not produce SpeechKit-Setup.exe.'
}

$checksumLines = foreach ($file in $mirroredFiles) {
    "$($file.sha256) *$($file.name)"
}
Set-Content -LiteralPath (Join-Path $destinationRoot 'SHA256SUMS.txt') -Value $checksumLines -Encoding ascii

if ([string]::IsNullOrWhiteSpace($SourceReleaseUrl) -and -not [string]::IsNullOrWhiteSpace($Version)) {
    $SourceReleaseUrl = "https://github.com/$SourceRepo/releases/tag/$Version"
}

$manifest = [ordered]@{
    schema = 'speechkit.latest-installer@2'
    version = $Version
    source_repo = $SourceRepo
    source_release_url = $SourceReleaseUrl
    source_commit = $CommitSha
    mirrored_at_utc = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
    max_installer_bytes = $MaxInstallerBytes
    files = @($mirroredFiles)
}
$manifest | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path $destinationRoot 'INSTALLER-MANIFEST.json') -Encoding utf8

$readme = @"
# Latest Windows Installer

This directory intentionally mirrors metadata for the current installable
Windows release. Installer binaries are kept as GitHub Release assets, not as
tracked source files.

Use `INSTALLER-MANIFEST.json` to find the canonical `download_url`, size, and
SHA-256 for `SpeechKit-Setup.exe` and `SpeechKit-x64.msi`.

The canonical release assets are still the GitHub Release files for `$Version`
in `$SourceRepo`. Verify local files with `SHA256SUMS.txt` or inspect
`INSTALLER-MANIFEST.json` for source and hash metadata.

Do not commit generated installers under `release/latest/windows` or `dist/`;
both are local/generated artifact locations.
"@
Set-Content -LiteralPath (Join-Path $destinationRoot 'README.md') -Value $readme -Encoding utf8

Write-Host "Mirrored installer assets to $destinationRoot"
foreach ($file in $mirroredFiles) {
    Write-Host "  $($file.name) $($file.size_bytes) bytes $($file.sha256)"
}
