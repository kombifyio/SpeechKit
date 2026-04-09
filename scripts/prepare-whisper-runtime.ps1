param(
    [Parameter(Mandatory = $true)]
    [string]$BundleDir,
    [Parameter(Mandatory = $true)]
    [string]$CacheDir
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$ProgressPreference = 'SilentlyContinue'

$whisperReleaseVersion = 'v1.8.4'
$whisperAssetName = 'whisper-bin-x64.zip'
$whisperAssetSha256 = '74f973345cb52ef5ba3ec9e7e7af8e48cc8c71722d1528603b80588a11f82e3e'
$whisperAssetUrl = "https://github.com/ggml-org/whisper.cpp/releases/download/$whisperReleaseVersion/$whisperAssetName"

$modelName = 'ggml-small.bin'
$modelSha256 = '1be3a9b2063867b937e64e2ec7483364a79917e157fa98c5d94b5c1fffea987b'
$modelUrl = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/$modelName"

$runtimeCacheDir = Join-Path $CacheDir 'whisper-runtime'
$runtimeZipPath = Join-Path $runtimeCacheDir $whisperAssetName
$runtimeExtractDir = Join-Path $runtimeCacheDir "runtime-$whisperReleaseVersion"
$modelCachePath = Join-Path $runtimeCacheDir $modelName
$bundleModelsDir = Join-Path $BundleDir 'models'

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

    $serverExe = Get-ChildItem -Path $ExtractDir -Recurse -File -Filter 'whisper-server.exe' | Select-Object -First 1
    if ($null -eq $serverExe) {
        throw 'whisper-server.exe not found in extracted whisper runtime.'
    }

    $runtimeDir = $serverExe.DirectoryName
    Copy-Item -LiteralPath $serverExe.FullName -Destination (Join-Path $BundleDir 'whisper-server.exe') -Force

    $dlls = Get-ChildItem -Path $runtimeDir -File -Filter '*.dll'
    foreach ($dll in $dlls) {
        Copy-Item -LiteralPath $dll.FullName -Destination (Join-Path $BundleDir $dll.Name) -Force
    }
}

function Resolve-VCRuntimeDllPath {
    param(
        [Parameter(Mandatory = $true)]
        [string]$DllName
    )

    $directCandidates = @(
        (Join-Path $env:SystemRoot "System32\$DllName"),
        (Join-Path $env:SystemRoot "SysWOW64\$DllName")
    )

    foreach ($candidate in $directCandidates) {
        if (Test-Path -LiteralPath $candidate) {
            return $candidate
        }
    }

    $vsRoot = Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio'
    if (Test-Path -LiteralPath $vsRoot) {
        $match = Get-ChildItem -Path $vsRoot -Recurse -File -Filter $DllName -ErrorAction SilentlyContinue |
            Sort-Object LastWriteTime -Descending |
            Select-Object -First 1
        if ($null -ne $match) {
            return $match.FullName
        }
    }

    return ''
}

function Copy-VCRuntimeDependencies {
    param(
        [Parameter(Mandatory = $true)]
        [string]$BundleDir
    )

    $requiredDlls = @(
        'msvcp140.dll',
        'vcruntime140.dll',
        'vcruntime140_1.dll',
        'vcomp140.dll'
    )

    $missing = @()
    foreach ($dllName in $requiredDlls) {
        $sourcePath = Resolve-VCRuntimeDllPath -DllName $dllName
        if ([string]::IsNullOrWhiteSpace($sourcePath)) {
            $missing += $dllName
            continue
        }
        Copy-Item -LiteralPath $sourcePath -Destination (Join-Path $BundleDir $dllName) -Force
    }

    if ($missing.Count -gt 0) {
        throw "Missing VC++ runtime DLLs required by whisper runtime: $($missing -join ', '). Install Visual C++ Redistributable on the build host."
    }
}

if (-not (Test-Path -LiteralPath $runtimeCacheDir)) {
    New-Item -ItemType Directory -Path $runtimeCacheDir | Out-Null
}
if (-not (Test-Path -LiteralPath $bundleModelsDir)) {
    New-Item -ItemType Directory -Path $bundleModelsDir | Out-Null
}

Download-VerifiedFile -Url $whisperAssetUrl -Destination $runtimeZipPath -ExpectedSha256 $whisperAssetSha256 -Description 'whisper.cpp Windows runtime'
Download-VerifiedFile -Url $modelUrl -Destination $modelCachePath -ExpectedSha256 $modelSha256 -Description 'ggml-small model'

if (Test-Path -LiteralPath $runtimeExtractDir) {
    Remove-Item -LiteralPath $runtimeExtractDir -Recurse -Force
}
Expand-Archive -LiteralPath $runtimeZipPath -DestinationPath $runtimeExtractDir -Force
Copy-RuntimeFiles -ExtractDir $runtimeExtractDir -BundleDir $BundleDir
Copy-VCRuntimeDependencies -BundleDir $BundleDir
Copy-Item -LiteralPath $modelCachePath -Destination (Join-Path $bundleModelsDir $modelName) -Force

Write-Host 'Bundled local whisper runtime prepared.'
