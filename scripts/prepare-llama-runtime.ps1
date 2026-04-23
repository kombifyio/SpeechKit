param(
    [Parameter(Mandatory = $true)]
    [string]$BundleDir,
    [Parameter(Mandatory = $true)]
    [string]$CacheDir
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$ProgressPreference = 'SilentlyContinue'

$llamaReleaseVersion = 'b8882'
$llamaAssetName = 'llama-b8882-bin-win-cpu-x64.zip'
$llamaAssetSha256 = 'af85f68c143164cc13dce68ce40466e12a2f1e5b545ca9316a654414e4b01bef'
$llamaAssetUrl = "https://github.com/ggml-org/llama.cpp/releases/download/$llamaReleaseVersion/$llamaAssetName"

$runtimeCacheDir = Join-Path $CacheDir 'llama-runtime'
$runtimeZipPath = Join-Path $runtimeCacheDir $llamaAssetName
$runtimeExtractDir = Join-Path $runtimeCacheDir "runtime-$llamaReleaseVersion"

function Get-Sha256 {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Assert-Sha256 {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        [Parameter(Mandatory = $true)]
        [string]$Expected,
        [Parameter(Mandatory = $true)]
        [string]$Description
    )

    $actual = Get-Sha256 -Path $Path
    if ($actual -ne $Expected.ToLowerInvariant()) {
        throw "$Description hash mismatch. Expected $Expected, got $actual."
    }
}

function Download-VerifiedFile {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Url,
        [Parameter(Mandatory = $true)]
        [string]$Destination,
        [Parameter(Mandatory = $true)]
        [string]$ExpectedSha256,
        [Parameter(Mandatory = $true)]
        [string]$Description
    )

    if (Test-Path -LiteralPath $Destination) {
        try {
            Assert-Sha256 -Path $Destination -Expected $ExpectedSha256 -Description $Description
            Write-Host "Using cached $Description..."
            return
        } catch {
            Remove-Item -LiteralPath $Destination -Force
        }
    }

    $tempPath = "$Destination.download"
    if (Test-Path -LiteralPath $tempPath) {
        Remove-Item -LiteralPath $tempPath -Force
    }

    Write-Host "Downloading $Description..."
    Invoke-WebRequest -Uri $Url -OutFile $tempPath
    Assert-Sha256 -Path $tempPath -Expected $ExpectedSha256 -Description $Description
    Move-Item -LiteralPath $tempPath -Destination $Destination -Force
}

function Copy-RuntimeFiles {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ExtractDir,
        [Parameter(Mandatory = $true)]
        [string]$BundleDir
    )

    $serverExe = Get-ChildItem -Path $ExtractDir -Recurse -File -Filter 'llama-server.exe' | Select-Object -First 1
    if ($null -eq $serverExe) {
        throw 'llama-server.exe not found in extracted llama.cpp runtime.'
    }

    $runtimeDir = $serverExe.DirectoryName
    $bundleRuntimeDir = Join-Path $BundleDir 'llama'
    if (-not (Test-Path -LiteralPath $bundleRuntimeDir)) {
        New-Item -ItemType Directory -Path $bundleRuntimeDir | Out-Null
    }
    Copy-Item -LiteralPath $serverExe.FullName -Destination (Join-Path $bundleRuntimeDir 'llama-server.exe') -Force

    $dlls = Get-ChildItem -Path $runtimeDir -File -Filter '*.dll'
    foreach ($dll in $dlls) {
        Copy-Item -LiteralPath $dll.FullName -Destination (Join-Path $bundleRuntimeDir $dll.Name) -Force
    }
}

if (-not (Test-Path -LiteralPath $runtimeCacheDir)) {
    New-Item -ItemType Directory -Path $runtimeCacheDir | Out-Null
}
Download-VerifiedFile -Url $llamaAssetUrl -Destination $runtimeZipPath -ExpectedSha256 $llamaAssetSha256 -Description 'llama.cpp Windows CPU runtime'

if (Test-Path -LiteralPath $runtimeExtractDir) {
    Remove-Item -LiteralPath $runtimeExtractDir -Recurse -Force
}
Expand-Archive -LiteralPath $runtimeZipPath -DestinationPath $runtimeExtractDir -Force
Copy-RuntimeFiles -ExtractDir $runtimeExtractDir -BundleDir $BundleDir

Write-Host 'Bundled local llama.cpp runtime prepared without model weights.'
