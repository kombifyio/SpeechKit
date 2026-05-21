param(
    [switch]$SkipInstaller,
    [switch]$SkipVerification
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
$prepareLlamaRuntimeScript = Join-Path $scriptDir 'prepare-llama-runtime.ps1'
$prepareOnnxRuntimeScript = Join-Path $scriptDir 'prepare-onnx-runtime.ps1'
$prepareSherpaRuntimeScript = Join-Path $scriptDir 'prepare-sherpa-runtime.ps1'
$prepareWakewordModelScript = Join-Path $scriptDir 'prepare-wakeword-model.ps1'
$prepareWebView2RuntimeScript = Join-Path $scriptDir 'prepare-webview2-runtime.ps1'
$cacheDir = Join-Path $projectDir '.cache'
$goCacheDir = Join-Path $cacheDir 'go-build'
$goTmpDir = Join-Path $cacheDir 'go-tmp'
function Resolve-MinGWBinDir {
    $defaultBinDir = 'C:\msys64\mingw64\bin'
    if (Test-Path (Join-Path $defaultBinDir 'gcc.exe')) {
        return $defaultBinDir
    }

    if (-not [string]::IsNullOrWhiteSpace($env:MSYSTEM_PREFIX)) {
        $msystemBinDir = Join-Path $env:MSYSTEM_PREFIX 'bin'
        if (Test-Path (Join-Path $msystemBinDir 'gcc.exe')) {
            return $msystemBinDir
        }
    }

    $gccCommand = Get-Command 'gcc.exe' -ErrorAction SilentlyContinue
    if ($null -ne $gccCommand -and -not [string]::IsNullOrWhiteSpace($gccCommand.Source)) {
        return (Split-Path -Parent $gccCommand.Source)
    }

    return $defaultBinDir
}

function Resolve-SherpaNativeLibDir {
    $arch = 'x86_64-pc-windows-gnu'
    $moduleVersion = 'v1.13.2' # keep in lockstep with scripts/prepare-sherpa-runtime.ps1 and go.mod
    $goPath = (& go env GOPATH).Trim()
    if ([string]::IsNullOrWhiteSpace($goPath)) {
        $goPath = Join-Path $HOME 'go'
    }
    $libDir = Join-Path $goPath "pkg\mod\github.com\k2-fsa\sherpa-onnx-go-windows@$moduleVersion\lib\$arch"

    if (-not (Test-Path -LiteralPath $libDir)) {
        Push-Location $projectDir
        try {
            & go mod download github.com/k2-fsa/sherpa-onnx-go-windows | Out-Null
        }
        finally {
            Pop-Location
        }
    }
    if (-not (Test-Path -LiteralPath $libDir)) {
        throw "sherpa-onnx native lib dir missing after go mod download: $libDir"
    }
    return $libDir
}

$mingwBinDir = Resolve-MinGWBinDir
$sherpaNativeLibDir = Resolve-SherpaNativeLibDir
$mingwGcc = Join-Path $mingwBinDir 'gcc.exe'
$mingwGxx = Join-Path $mingwBinDir 'g++.exe'

# MinGW DLLs (libstdc++, libwinpthread) conflict with Node worker forks.
# Keep the original PATH for frontend steps; inject MinGW only for Go steps.
$basePath = $env:PATH
$goNativePath = "$mingwBinDir;$sherpaNativeLibDir;$basePath"
$env:CGO_ENABLED = '1'
$env:CC = $mingwGcc
$env:CXX = $mingwGxx
$env:GOCACHE = $goCacheDir
$env:GOTMPDIR = $goTmpDir
$env:GOWORK = 'off'

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

function Import-DotEnvFiles {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ProjectDir
    )

    $allowedNames = @(
        'SPEECHKIT_MANAGED_DOPPLER_PROJECT',
        'SPEECHKIT_MANAGED_DOPPLER_CONFIG',
        'SPEECHKIT_MANAGED_HF_BUILD_ENABLED',
        'SPEECHKIT_MANAGED_HF_DEFAULT',
        'SPEECHKIT_WINDOWS_SIGNING_PUBLISHER',
        'SPEECHKIT_WINDOWS_SIGNING_THUMBPRINT'
    )

    foreach ($fileName in @('.env', '.env.local')) {
        $filePath = Join-Path $ProjectDir $fileName
        if (-not (Test-Path $filePath)) {
            continue
        }

        Write-Host "Loading environment defaults from $fileName..."
        foreach ($rawLine in Get-Content $filePath) {
            $line = $rawLine.Trim()
            if ([string]::IsNullOrWhiteSpace($line) -or $line.StartsWith('#')) {
                continue
            }
            if ($line.StartsWith('export ')) {
                $line = $line.Substring(7).Trim()
            }

            $separatorIndex = $line.IndexOf('=')
            if ($separatorIndex -lt 1) {
                continue
            }

            $name = $line.Substring(0, $separatorIndex).Trim()
            if ([string]::IsNullOrWhiteSpace($name) -or (Test-Path "Env:\$name")) {
                continue
            }
            if ($name -notin $allowedNames) {
                continue
            }

            $value = $line.Substring($separatorIndex + 1)
            if ($value.Length -ge 2) {
                $quote = $value[0]
                if (($quote -eq '"' -or $quote -eq "'") -and $value[$value.Length - 1] -eq $quote) {
                    $value = $value.Substring(1, $value.Length - 2)
                }
            }

            Set-Item -Path "Env:\$name" -Value $value
        }
    }
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

function New-GoStringLdflag {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,
        [Parameter()]
        [string]$Value = ''
    )

    $assignment = "$Name=$Value"
    $escaped = $assignment.Replace('\', '\\').Replace('"', '\"')
    return "-X `"$escaped`""
}

Import-DotEnvFiles -ProjectDir $projectDir

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
$windowsSigningPublisher = Get-EnvValue -Name 'SPEECHKIT_WINDOWS_SIGNING_PUBLISHER'
$windowsSigningThumbprint = Get-EnvValue -Name 'SPEECHKIT_WINDOWS_SIGNING_THUMBPRINT'
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
    (New-GoStringLdflag -Name "$modulePath/internal/config.managedHFBuildEnabled" -Value $managedHFBuildEnabled)
    (New-GoStringLdflag -Name "$modulePath/internal/config.managedHFDefaultOptIn" -Value $managedHFDefault)
    (New-GoStringLdflag -Name "$modulePath/internal/config.managedDopplerDefaultProject" -Value $managedDopplerProject)
    (New-GoStringLdflag -Name "$modulePath/internal/config.managedDopplerDefaultConfig" -Value $managedDopplerConfig)
)
if (-not [string]::IsNullOrWhiteSpace($windowsSigningPublisher)) {
    $goLdflags += (New-GoStringLdflag -Name "$modulePath/cmd/speechkit.installerSignatureDefaultPublisher" -Value $windowsSigningPublisher)
}
if (-not [string]::IsNullOrWhiteSpace($windowsSigningThumbprint)) {
    $goLdflags += (New-GoStringLdflag -Name "$modulePath/cmd/speechkit.installerSignatureDefaultThumbprint" -Value $windowsSigningThumbprint)
}

# Read canonical version from root package.json and inject via ldflags.
$rootPackageJson = Join-Path $projectDir 'package.json'
$appVersion = '0.0.0'
if (Test-Path $rootPackageJson) {
    $pkg = Get-Content $rootPackageJson -Raw | ConvertFrom-Json
    if ($pkg.version) {
        $appVersion = $pkg.version
    }
}
$goLdflags += (New-GoStringLdflag -Name 'main.AppVersion' -Value $appVersion)
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

function Assert-Sha256Hash {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        [Parameter(Mandatory = $true)]
        [string]$Expected,
        [Parameter(Mandatory = $true)]
        [string]$Description
    )

    Assert-PathExists -Path $Path -Description $Description
    $actual = (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $Expected.ToLowerInvariant()) {
        throw "$Description SHA256 mismatch at ${Path}: expected $Expected got $actual."
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

function Find-PowerShellExecutable {
    foreach ($candidate in @('pwsh', 'powershell.exe')) {
        $command = Get-Command $candidate -ErrorAction SilentlyContinue
        if ($null -ne $command -and -not [string]::IsNullOrWhiteSpace($command.Source)) {
            return $command.Source
        }
    }

    throw 'No PowerShell executable found. Expected pwsh or powershell.exe.'
}

function Normalize-GeneratedTextAssets {
    param(
        [Parameter(Mandatory = $true)]
        [string]$AssetDir
    )

    if (-not (Test-Path $AssetDir)) {
        return
    }

    $utf8NoBom = [System.Text.UTF8Encoding]::new($false)
    $textExtensions = @('.css', '.html', '.js', '.json', '.map', '.svg', '.txt', '.xml')
    Get-ChildItem -Path $AssetDir -File -Recurse |
        Where-Object { $textExtensions -contains $_.Extension.ToLowerInvariant() } |
        ForEach-Object {
            $content = [System.IO.File]::ReadAllText($_.FullName)
            $content = $content.Replace("`r`n", "`n").Replace("`r", "`n")
            $lines = $content.Split([string[]]@("`n"), [System.StringSplitOptions]::None)
            $normalized = (($lines | ForEach-Object { $_.TrimEnd() }) -join "`n")
            [System.IO.File]::WriteAllText($_.FullName, $normalized, $utf8NoBom)
        }
}

Write-Host 'Preparing clean Windows bundle...'
Assert-PathExists -Path $mingwGcc -Description 'MinGW gcc compiler'
Assert-PathExists -Path $mingwGxx -Description 'MinGW g++ compiler'
Assert-PathExists -Path $frontendDir -Description 'Frontend source directory'
Assert-PathExists -Path (Join-Path $frontendDir 'package.json') -Description 'Frontend package manifest'
Assert-PathExists -Path (Join-Path $frontendDir 'src') -Description 'Frontend source tree'
Assert-PathExists -Path $prepareWhisperRuntimeScript -Description 'Whisper runtime prepare script'
Assert-PathExists -Path $prepareLlamaRuntimeScript -Description 'llama.cpp runtime prepare script'
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
$powershellExe = Find-PowerShellExecutable
# Wipe only build artifacts. Dev bundle rebuilds preserve local operator state:
#   - data/        -> install.toml (setup_done), feedback SQLite store
#   - config.toml  -> Settings UI edits and local dev server targets.
#
# Installer/release builds must not preserve config.toml because the installer
# packages the staged file as the first-run default for other users.
$preserveBundleEntries = if ($SkipInstaller) { @('data', 'config.toml') } else { @('data') }
if (Test-Path $bundleDir) {
    Get-ChildItem -Path $bundleDir -Force | Where-Object { $preserveBundleEntries -notcontains $_.Name } | Remove-Item -Recurse -Force
}
New-Item -ItemType Directory -Path $bundleDir -Force | Out-Null

# --- Frontend (clean PATH - no MinGW DLLs) ---
$env:PATH = $basePath
Push-Location $frontendDir
try {
    if ($env:CI -eq 'true' -or -not (Test-Path (Join-Path $frontendDir 'node_modules'))) {
        Invoke-Step -Description 'Installing frontend dependencies...' -FilePath 'npm.cmd' -ArgumentList @('ci')
    } else {
        Write-Host 'Using existing frontend dependencies...'
    }

    if ($SkipVerification) {
        Write-Host 'Skipping frontend tests and lint (SkipVerification specified).'
    } else {
        Invoke-Step -Description 'Testing frontend...' -FilePath 'npm.cmd' -ArgumentList @('test')
        Invoke-Step -Description 'Linting frontend...' -FilePath 'npm.cmd' -ArgumentList @('run', 'lint')
    }

    Invoke-Step -Description 'Building frontend assets...' -FilePath 'npm.cmd' -ArgumentList @('run', 'build')
    Normalize-GeneratedTextAssets -AssetDir (Join-Path $projectDir 'internal/frontendassets/dist')
}
finally {
    Pop-Location
}

# --- Go (MinGW on PATH for CGo) ---
$env:PATH = $goNativePath
Push-Location $projectDir
try {
    # Build-time defaults are resolved into ldflags above. The test suite
    # should stay hermetic and must not inherit local provider or Doppler
    # credentials from the caller environment.
    foreach ($name in @(
        'DOPPLER_PROJECT',
        'DOPPLER_CONFIG',
        'DOPPLER_PATH',
        'HF_TOKEN',
        'OPENAI_API_KEY',
        'GOOGLE_AI_API_KEY',
        'GROQ_API_KEY',
        'OPENROUTER_API_KEY',
        'VPS_API_KEY',
        'SPEECHKIT_ENABLE_MANAGED_HF',
        'SPEECHKIT_MANAGED_DOPPLER_PROJECT',
        'SPEECHKIT_MANAGED_DOPPLER_CONFIG',
        'SPEECHKIT_MANAGED_HF_BUILD_ENABLED',
        'SPEECHKIT_MANAGED_HF_DEFAULT',
        'SPEECHKIT_WINDOWS_SIGNING_PUBLISHER',
        'SPEECHKIT_WINDOWS_SIGNING_THUMBPRINT'
    )) {
        Remove-Item -Path "Env:\$name" -ErrorAction SilentlyContinue
    }

    if ($SkipVerification) {
        Write-Host 'Skipping Go vet, tests, and race tests (SkipVerification specified).'
    } else {
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
    }
    Invoke-Step -Description 'Building SpeechKit.exe...' -FilePath 'go' -ArgumentList @('build', '-ldflags', $goLdflags, '-o', $bundleExe, './cmd/speechkit/')

    # Wake-word sidecar. Built from cmd/speechkit-wakeword and dropped next
    # to SpeechKit.exe; the desktop adapter spawns it on demand. Same
    # MinGW + cgo setup as the host binary so the sherpa-onnx runtime libs
    # already in the bundle resolve at runtime. Uses a smaller ldflags set
    # because the sidecar only needs main.AppVersion injected - no
    # installer-signature placeholders.
    $sidecarExe = Join-Path $bundleDir 'speechkit-wakeword.exe'
    $sidecarLdflags = (New-GoStringLdflag -Name 'main.AppVersion' -Value $appVersion)
    Invoke-Step -Description 'Building speechkit-wakeword.exe (sidecar)...' -FilePath 'go' -ArgumentList @('build', '-ldflags', $sidecarLdflags, '-o', $sidecarExe, './cmd/speechkit-wakeword/')

    $openWakewordSidecarExe = Join-Path $bundleDir 'speechkit-openwakeword.exe'
    Invoke-Step -Description 'Building speechkit-openwakeword.exe (sidecar)...' -FilePath 'go' -ArgumentList @('build', '-ldflags', $sidecarLdflags, '-o', $openWakewordSidecarExe, './cmd/speechkit-openwakeword/')
}
finally {
    Pop-Location
    $env:PATH = $basePath
}

Write-Host 'Writing runtime config...'
$bundleConfig = Join-Path $bundleDir 'config.toml'
# Dev builds seed the config from the template only on first install. Installer
# builds always reset to the template so local/private server targets can never
# leak into config.default.toml or a fresh user install.
if (-not $SkipInstaller) {
    Copy-Item -Path (Join-Path $projectDir 'config.example.toml') -Destination $bundleConfig -Force
    Write-Host '  -> installed release-safe config.toml from config.example.toml.'
} elseif (-not (Test-Path -LiteralPath $bundleConfig)) {
    Copy-Item -Path (Join-Path $projectDir 'config.example.toml') -Destination $bundleConfig -Force
    Write-Host '  -> installed config.toml from config.example.toml (first run).'
} else {
    Write-Host "  -> preserving existing config.toml (delete $bundleConfig before rebuilding to reset)."
}
Invoke-Step -Description 'Bundling local whisper runtime...' -FilePath $powershellExe -ArgumentList @('-ExecutionPolicy', 'Bypass', '-File', $prepareWhisperRuntimeScript, '-BundleDir', $bundleDir, '-CacheDir', $cacheDir)
# Build-time gate: the NSIS/MSI installer scripts assume the bundled starter
# model is present in <bundleDir>/models/ggml-small.bin. If prepare-whisper-runtime
# silently skipped the copy (older script, partial cache, modified hash table) the
# packaged installer would ship without the model and Setup-Wizard's
# /app/complete-setup would 409 on every first-launch.
# Keep this assertion in lockstep with $starterModelSha256 in
# scripts/prepare-whisper-runtime.ps1.
$starterModelStagedPath = Join-Path $bundleDir 'models\ggml-small.bin'
$starterModelExpectedSha256 = '1be3a9b2063867b937e64e2ec7483364a79917e157fa98c5d94b5c1fffea987b'
Assert-Sha256Hash -Path $starterModelStagedPath -Expected $starterModelExpectedSha256 -Description 'Bundled starter Whisper model'
Write-Host "  -> verified staged starter model at $starterModelStagedPath"
Invoke-Step -Description 'Bundling local llama.cpp runtime...' -FilePath $powershellExe -ArgumentList @('-ExecutionPolicy', 'Bypass', '-File', $prepareLlamaRuntimeScript, '-BundleDir', $bundleDir, '-CacheDir', $cacheDir)
Invoke-Step -Description 'Bundling ONNX Runtime (VAD)...' -FilePath $powershellExe -ArgumentList @('-ExecutionPolicy', 'Bypass', '-File', $prepareOnnxRuntimeScript, '-BundleDir', $bundleDir, '-CacheDir', $cacheDir)
Invoke-Step -Description 'Bundling sherpa-onnx runtime (wake-word)...' -FilePath $powershellExe -ArgumentList @('-ExecutionPolicy', 'Bypass', '-File', $prepareSherpaRuntimeScript, '-BundleDir', $bundleDir, '-CacheDir', $cacheDir)
Invoke-Step -Description 'Bundling wake-word KWS model...' -FilePath $powershellExe -ArgumentList @('-ExecutionPolicy', 'Bypass', '-File', $prepareWakewordModelScript, '-BundleDir', $bundleDir, '-CacheDir', $cacheDir)
Invoke-Step -Description 'Bundling WebView2 bootstrapper...' -FilePath $powershellExe -ArgumentList @('-ExecutionPolicy', 'Bypass', '-File', $prepareWebView2RuntimeScript, '-BundleDir', $bundleDir, '-CacheDir', $cacheDir)

# Build-time gate: every artifact the NSIS+WiX installer scripts reference
# must exist in $bundleDir BEFORE we pack. The wake-word sidecar binary and
# its KWS model bundle had been silently absent from both installers, which
# caused "wake-word sidecar asset missing" warnings on every fresh install
# even though the Go build had produced the sidecar.
$requiredBundleArtifacts = @(
    'speechkit-wakeword.exe',
    'speechkit-openwakeword.exe',
    'wakeword-kws\keywords.txt',
    'wakeword-kws\tokens.txt',
    'wakeword-kws\encoder-epoch-12-avg-2-chunk-16-left-64.onnx',
    'wakeword-kws\decoder-epoch-12-avg-2-chunk-16-left-64.onnx',
    'wakeword-kws\joiner-epoch-12-avg-2-chunk-16-left-64.onnx',
    'models\wakeword\melspectrogram.onnx',
    'models\wakeword\embedding_model.onnx',
    'models\wakeword\hey_quby.onnx',
    'models\wakeword\hey_computer.onnx',
    'models\wakeword\hey_jarvis.onnx',
    'models\wakeword\hey_mira.onnx',
    'models\wakeword\hey_kombify.onnx',
    'onnxruntime.dll',
    'sherpa-onnx-c-api.dll',
    'sherpa-onnx-cxx-api.dll',
    'whisper-server.exe',
    'whisper.dll',
    'ggml.dll'
)
foreach ($relPath in $requiredBundleArtifacts) {
    Assert-PathExists -Path (Join-Path $bundleDir $relPath) -Description "Bundle artifact $relPath"
}
Write-Host "  -> verified $($requiredBundleArtifacts.Count) required bundle artifacts present"

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
