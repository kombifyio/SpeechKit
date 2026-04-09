param(
    [switch]$SkipInstaller
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$projectDir = Split-Path -Parent $scriptDir
$frontendDir = Join-Path $projectDir 'frontend/app'
$distDir = Join-Path $projectDir 'dist/windows'
$bundleDir = Join-Path $distDir 'SpeechKit'
$bundleExe = Join-Path $bundleDir 'SpeechKit.exe'
$installerScript = Join-Path $projectDir 'installer/speechkit.nsi'
$installerExe = Join-Path $distDir 'SpeechKit-Setup.exe'
$prepareWhisperRuntimeScript = Join-Path $scriptDir 'prepare-whisper-runtime.ps1'
$prepareWebView2RuntimeScript = Join-Path $scriptDir 'prepare-webview2-runtime.ps1'
$cacheDir = Join-Path $projectDir '.cache'
$goCacheDir = Join-Path $cacheDir 'go-build'
$goTmpDir = Join-Path $cacheDir 'go-tmp'

# MinGW DLLs (libstdc++, libwinpthread) conflict with Node worker forks.
# Keep the original PATH for frontend steps; inject MinGW only for Go steps.
$basePath = $env:PATH
$mingwPath = "C:\msys64\mingw64\bin;$basePath"
$env:CGO_ENABLED = '1'
$env:GOCACHE = $goCacheDir
$env:GOTMPDIR = $goTmpDir

function Get-EnvValue {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name
    )

    $envPath = "Env:\$Name"
    if (Test-Path $envPath) {
        return (Get-Item $envPath).Value
    }
    return ''
}

function Resolve-GoModulePath {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ProjectDir
    )

    $goModPath = Join-Path $ProjectDir 'go.mod'
    if (-not (Test-Path $goModPath)) {
        throw "go.mod not found in $ProjectDir"
    }

    $firstLine = (Get-Content $goModPath -TotalCount 1).Trim()
    if ($firstLine -match '^module\s+(.+)$') {
        $modulePath = $Matches[1].Trim()
    } else {
        throw 'Could not parse module path from go.mod.'
    }

    if ([string]::IsNullOrWhiteSpace($modulePath)) {
        throw 'Could not resolve Go module path from go.mod.'
    }

    return $modulePath
}

if (Test-Path Env:\SPEECHKIT_MANAGED_DOPPLER_PROJECT) {
    $managedDopplerProject = $env:SPEECHKIT_MANAGED_DOPPLER_PROJECT
} else {
    $managedDopplerProject = ''
}
if (Test-Path Env:\SPEECHKIT_MANAGED_DOPPLER_CONFIG) {
    $managedDopplerConfig = $env:SPEECHKIT_MANAGED_DOPPLER_CONFIG
} else {
    $managedDopplerConfig = ''
}
$modulePath = Resolve-GoModulePath -ProjectDir $projectDir
$publicModulePath = 'github.com/kombifyio/SpeechKit'
$isPublicModule = $modulePath -eq $publicModulePath
if (Test-Path Env:\SPEECHKIT_MANAGED_HF_BUILD_ENABLED) {
    $managedHFBuildEnabled = $env:SPEECHKIT_MANAGED_HF_BUILD_ENABLED
} elseif ($isPublicModule) {
    $managedHFBuildEnabled = '0'
} else {
    $managedHFBuildEnabled = '1'
}
if (Test-Path Env:\SPEECHKIT_MANAGED_HF_DEFAULT) {
    $managedHFDefault = $env:SPEECHKIT_MANAGED_HF_DEFAULT
} elseif ($isPublicModule) {
    $managedHFDefault = '0'
} else {
    $managedHFDefault = '1'
}
$goLdflags = @(
    "-H windowsgui"
    "-X $modulePath/internal/config.managedHFBuildEnabled=$managedHFBuildEnabled"
    "-X $modulePath/internal/config.managedHFDefaultOptIn=$managedHFDefault"
    "-X $modulePath/internal/config.managedDopplerDefaultProject=$managedDopplerProject"
    "-X $modulePath/internal/config.managedDopplerDefaultConfig=$managedDopplerConfig"
)

# Read canonical version from root package.json and inject via ldflags.
$rootPackageJson = Join-Path $projectDir 'package.json'
$appVersion = '0.0.0'
if (Test-Path $rootPackageJson) {
    $pkg = Get-Content $rootPackageJson -Raw | ConvertFrom-Json
    if ($pkg.version) {
        $appVersion = $pkg.version
    }
}
$goLdflags += "-X main.AppVersion=$appVersion"
$goLdflags = $goLdflags -join ' '

function Invoke-Step {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Description,
        [Parameter(Mandatory = $true)]
        [string]$FilePath,
        [Parameter()]
        [string[]]$ArgumentList = @()
    )

    Write-Host $Description
    & $FilePath @ArgumentList
    if ($LASTEXITCODE -ne 0) {
        throw "$Description failed with exit code $LASTEXITCODE."
    }
}

function Assert-PathExists {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        [Parameter(Mandatory = $true)]
        [string]$Description
    )

    if (-not (Test-Path $Path)) {
        throw "$Description missing: $Path"
    }
}

function Find-NSISExecutable {
    $command = Get-Command 'makensis' -ErrorAction SilentlyContinue
    if ($null -ne $command -and -not [string]::IsNullOrWhiteSpace($command.Source)) {
        return $command.Source
    }

    $candidates = @(
        'C:\Program Files (x86)\NSIS\makensis.exe',
        'C:\Program Files\NSIS\makensis.exe'
    )
    foreach ($candidate in $candidates) {
        if (Test-Path $candidate) {
            return $candidate
        }
    }

    return ''
}

Write-Host 'Preparing clean Windows bundle...'
Assert-PathExists -Path $frontendDir -Description 'Frontend source directory'
Assert-PathExists -Path (Join-Path $frontendDir 'package.json') -Description 'Frontend package manifest'
Assert-PathExists -Path (Join-Path $frontendDir 'src') -Description 'Frontend source tree'
Assert-PathExists -Path $prepareWhisperRuntimeScript -Description 'Whisper runtime prepare script'
Assert-PathExists -Path $prepareWebView2RuntimeScript -Description 'WebView2 runtime prepare script'
if (-not $SkipInstaller) {
    Assert-PathExists -Path $installerScript -Description 'NSIS installer script'
    $nsisExe = Find-NSISExecutable
    if ([string]::IsNullOrWhiteSpace($nsisExe)) {
        throw 'NSIS makensis.exe not found. Install NSIS or add makensis to PATH.'
    }
} else {
    $nsisExe = ''
}
if (-not (Test-Path $cacheDir)) {
    New-Item -ItemType Directory -Path $cacheDir | Out-Null
}
if (-not (Test-Path $goCacheDir)) {
    New-Item -ItemType Directory -Path $goCacheDir | Out-Null
}
if (-not (Test-Path $goTmpDir)) {
    New-Item -ItemType Directory -Path $goTmpDir | Out-Null
}
if (Test-Path $bundleDir) {
    Remove-Item -Recurse -Force $bundleDir
}
New-Item -ItemType Directory -Path $bundleDir | Out-Null

# --- Frontend (clean PATH — no MinGW DLLs) ---
$env:PATH = $basePath
Push-Location $frontendDir
try {
    if ($env:CI -eq 'true' -or -not (Test-Path (Join-Path $frontendDir 'node_modules'))) {
        Invoke-Step -Description 'Installing frontend dependencies...' -FilePath 'npm.cmd' -ArgumentList @('ci')
    } else {
        Write-Host 'Using existing frontend dependencies...'
    }

    Invoke-Step -Description 'Testing frontend...' -FilePath 'npm.cmd' -ArgumentList @('test')
    Invoke-Step -Description 'Linting frontend...' -FilePath 'npm.cmd' -ArgumentList @('run', 'lint')

    Invoke-Step -Description 'Building frontend assets...' -FilePath 'npm.cmd' -ArgumentList @('run', 'build')
}
finally {
    Pop-Location
}

# --- Go (MinGW on PATH for CGo) ---
$env:PATH = $mingwPath
Push-Location $projectDir
try {
    Invoke-Step -Description 'Running Go vet...' -FilePath 'go' -ArgumentList @('vet', './...')
    Invoke-Step -Description 'Running Go tests...' -FilePath 'go' -ArgumentList @('test', './...')
    Invoke-Step -Description 'Running Go race tests...' -FilePath 'go' -ArgumentList @(
        'test',
        '-race',
        './pkg/speechkit/...',
        './internal/router/...',
        './internal/voiceagent/...',
        './internal/assist/...',
        './internal/store/...',
        './internal/secrets/...',
        './internal/ai/...',
        './internal/config/...',
        './internal/stt/...',
        './internal/tts/...',
        './internal/shortcuts/...',
        './internal/features/...',
        './internal/models/...',
        './internal/textactions/...'
    )
    Invoke-Step -Description 'Building SpeechKit.exe...' -FilePath 'go' -ArgumentList @('build', '-ldflags', $goLdflags, '-o', $bundleExe, './cmd/speechkit/')
}
finally {
    Pop-Location
    $env:PATH = $basePath
}

Write-Host 'Writing runtime config...'
$bundleConfig = Join-Path $bundleDir 'config.toml'
Copy-Item -Path (Join-Path $projectDir 'config.example.toml') -Destination $bundleConfig -Force
Invoke-Step -Description 'Bundling local whisper runtime...' -FilePath 'pwsh' -ArgumentList @('-ExecutionPolicy', 'Bypass', '-File', $prepareWhisperRuntimeScript, '-BundleDir', $bundleDir, '-CacheDir', $cacheDir)
Invoke-Step -Description 'Bundling WebView2 bootstrapper...' -FilePath 'pwsh' -ArgumentList @('-ExecutionPolicy', 'Bypass', '-File', $prepareWebView2RuntimeScript, '-BundleDir', $bundleDir, '-CacheDir', $cacheDir)

if ($SkipInstaller) {
    Write-Host 'Skipping installer build (SkipInstaller specified).'
} else {
    Write-Host "Building installer for version $appVersion..."
    Invoke-Step -Description 'Building SpeechKit-Setup.exe...' -FilePath $nsisExe -ArgumentList @("/DVERSION=$appVersion", $installerScript)
}

Write-Host ''
Write-Host 'Artifacts complete:'
Write-Host "  $bundleExe"
if (-not $SkipInstaller) {
    Write-Host "  $installerExe"
}
