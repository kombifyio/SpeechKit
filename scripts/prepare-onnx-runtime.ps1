param(
    [Parameter(Mandatory = $true)]
    [string]$BundleDir,
    [Parameter(Mandatory = $true)]
    [string]$CacheDir
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$ProgressPreference = 'SilentlyContinue'

# The bundled DLL must match the C-API version baked into
# github.com/yalue/onnxruntime_go (see go.mod). v1.27.0 of that binding
# calls api_base->GetApi(24), which Microsoft ORT 1.24.x is the first
# release line to expose. Older DLLs (e.g. the 1.17.x copy that Windows
# ships in System32) return NULL from GetApi(24) and the wrapper aborts
# with "Platform-specific initialization failed: Error setting ORT API
# base: 2" — wake-word and VAD then fail.
#
# When bumping the Go binding past v1.27.0, update both $ortReleaseVersion
# AND $ortAssetSha256 in lock-step. The mapping table lives in
# https://github.com/yalue/onnxruntime_go/blob/main/onnxruntime_c_api.h
# (`#define ORT_API_VERSION N`) and the Microsoft ORT release that ships
# that N (`include/onnxruntime/core/session/onnxruntime_c_api.h` at the
# matching tag).
$ortReleaseVersion = 'v1.24.4'
$ortAssetName = "onnxruntime-win-x64-$($ortReleaseVersion.TrimStart('v')).zip"
$ortAssetSha256 = 'd2319fddfb6ea4db99ccc4b60c85c517bcd855721f5daa6a06d40d7cb2ee2357'
$ortAssetUrl = "https://github.com/microsoft/onnxruntime/releases/download/$ortReleaseVersion/$ortAssetName"

$runtimeCacheDir = Join-Path $CacheDir 'onnx-runtime'
$runtimeZipPath = Join-Path $runtimeCacheDir $ortAssetName
$runtimeExtractDir = Join-Path $runtimeCacheDir "runtime-$ortReleaseVersion"

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

function Copy-RuntimeDll {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ExtractDir,
        [Parameter(Mandatory = $true)]
        [string]$BundleDir
    )

    $dll = Get-ChildItem -Path $ExtractDir -Recurse -File -Filter 'onnxruntime.dll' | Select-Object -First 1
    if ($null -eq $dll) {
        throw 'onnxruntime.dll not found in extracted ONNX Runtime archive.'
    }

    Copy-Item -LiteralPath $dll.FullName -Destination (Join-Path $BundleDir 'onnxruntime.dll') -Force
}

if (-not (Test-Path -LiteralPath $runtimeCacheDir)) {
    New-Item -ItemType Directory -Path $runtimeCacheDir | Out-Null
}
Download-VerifiedFile -Url $ortAssetUrl -Destination $runtimeZipPath -ExpectedSha256 $ortAssetSha256 -Description "ONNX Runtime $ortReleaseVersion Windows x64"

if (Test-Path -LiteralPath $runtimeExtractDir) {
    Remove-Item -LiteralPath $runtimeExtractDir -Recurse -Force
}
Expand-Archive -LiteralPath $runtimeZipPath -DestinationPath $runtimeExtractDir -Force
Copy-RuntimeDll -ExtractDir $runtimeExtractDir -BundleDir $BundleDir

Write-Host "Bundled ONNX Runtime $ortReleaseVersion (api 24, matches yalue/onnxruntime_go v1.27.0)."
