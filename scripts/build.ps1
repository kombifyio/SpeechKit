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
$cacheDir = Join-Path $projectDir '.cache'
$goCacheDir = Join-Path $cacheDir 'go-build'
$goTmpDir = Join-Path $cacheDir 'go-tmp'

$env:PATH = "C:\msys64\mingw64\bin;$env:PATH"
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

function Parse-BoolFlag {
    param(
        [string]$Value
    )

    $normalized = ''
    if ($null -ne $Value) {
        $normalized = $Value.Trim().ToLowerInvariant()
    }

    switch ($normalized) {
        '1' { return $true }
        'true' { return $true }
        'yes' { return $true }
        'on' { return $true }
        default { return $false }
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

if (Test-Path Env:\SPEECHKIT_MANAGED_HF_DEFAULT) {
    $managedHFDefault = $env:SPEECHKIT_MANAGED_HF_DEFAULT
} else {
    $managedHFDefault = ''
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
$goLdflags = @(
    "-H windowsgui"
    "-X $modulePath/internal/config.managedHFDefaultOptIn=$managedHFDefault"
    "-X $modulePath/internal/config.managedDopplerDefaultProject=$managedDopplerProject"
    "-X $modulePath/internal/config.managedDopplerDefaultConfig=$managedDopplerConfig"
) -join ' '

function Invoke-Step {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Description,
        [Parameter(Mandatory = $true)]
        [scriptblock]$Action
    )

    Write-Host $Description
    & $Action
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

function Find-DopplerExecutable {
    $command = Get-Command 'doppler' -ErrorAction SilentlyContinue
    if ($null -ne $command -and -not [string]::IsNullOrWhiteSpace($command.Source)) {
        return $command.Source
    }

    $candidates = @(
        (Join-Path $env:LOCALAPPDATA 'Microsoft\WinGet\Links\doppler.exe'),
        (Join-Path $env:LOCALAPPDATA 'Programs\Doppler\doppler.exe'),
        (Join-Path $env:ProgramFiles 'Doppler\doppler.exe'),
        (Join-Path ${env:ProgramFiles(x86)} 'Doppler\doppler.exe')
    )
    foreach ($candidate in $candidates) {
        if (-not [string]::IsNullOrWhiteSpace($candidate) -and (Test-Path $candidate)) {
            return $candidate
        }
    }

    return ''
}

function Resolve-DopplerSecret {
    param(
        [Parameter(Mandatory = $true)]
        [string]$SecretName,
        [Parameter(Mandatory = $true)]
        [string]$Project,
        [Parameter(Mandatory = $true)]
        [string]$Config
    )

    $dopplerPath = Find-DopplerExecutable
    if ([string]::IsNullOrWhiteSpace($dopplerPath)) {
        return ''
    }

    $resolved = & $dopplerPath secrets get $SecretName --plain --project $Project --config $Config --no-read-env 2>$null
    if ($LASTEXITCODE -ne 0) {
        return ''
    }
    return ($resolved | Out-String).Trim()
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

function Resolve-DevBuildHuggingFaceToken {
    $directToken = Get-EnvValue -Name 'SPEECHKIT_DEV_HF_TOKEN'
    if (-not [string]::IsNullOrWhiteSpace($directToken)) {
        return [pscustomobject]@{
            Token   = $directToken.Trim()
            Source  = 'SPEECHKIT_DEV_HF_TOKEN'
            EnvName = 'HF_TOKEN'
        }
    }

    $tokenEnvName = (Get-EnvValue -Name 'SPEECHKIT_DEV_HF_TOKEN_ENV').Trim()
    if ([string]::IsNullOrWhiteSpace($tokenEnvName)) {
        $tokenEnvName = 'HF_TOKEN'
    }

    $envToken = Get-EnvValue -Name $tokenEnvName
    if (-not [string]::IsNullOrWhiteSpace($envToken)) {
        return [pscustomobject]@{
            Token   = $envToken.Trim()
            Source  = "env:$tokenEnvName"
            EnvName = $tokenEnvName
        }
    }

    $dopplerProject = (Get-EnvValue -Name 'DOPPLER_PROJECT').Trim()
    $dopplerConfig = (Get-EnvValue -Name 'DOPPLER_CONFIG').Trim()
    if (-not [string]::IsNullOrWhiteSpace($dopplerProject) -and -not [string]::IsNullOrWhiteSpace($dopplerConfig)) {
        $dopplerToken = Resolve-DopplerSecret -SecretName $tokenEnvName -Project $dopplerProject -Config $dopplerConfig
        if (-not [string]::IsNullOrWhiteSpace($dopplerToken)) {
            return [pscustomobject]@{
                Token   = $dopplerToken
                Source  = "doppler:$dopplerProject/$dopplerConfig"
                EnvName = $tokenEnvName
            }
        }
    }

    return [pscustomobject]@{
        Token   = ''
        Source  = 'none'
        EnvName = $tokenEnvName
    }
}

function Set-PendingInstallHuggingFaceTokenBootstrap {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Token
    )

    $trimmed = $Token.Trim()
    if ([string]::IsNullOrWhiteSpace($trimmed)) {
        throw 'PendingHFInstallToken cannot be empty.'
    }

    $registryPath = 'HKCU:\Software\kombify\SpeechKit'
    if (-not (Test-Path $registryPath)) {
        New-Item -Path $registryPath -Force | Out-Null
    }
    New-ItemProperty -Path $registryPath -Name 'PendingHFInstallToken' -Value $trimmed -PropertyType String -Force | Out-Null
}

$seedDevBuildHFToken = Parse-BoolFlag -Value (Get-EnvValue -Name 'SPEECHKIT_DEV_SEED_HF_TOKEN')

Write-Host 'Preparing clean Windows bundle...'
Assert-PathExists -Path $frontendDir -Description 'Frontend source directory'
Assert-PathExists -Path (Join-Path $frontendDir 'package.json') -Description 'Frontend package manifest'
Assert-PathExists -Path (Join-Path $frontendDir 'src') -Description 'Frontend source tree'
Assert-PathExists -Path $installerScript -Description 'NSIS installer script'
Assert-PathExists -Path $prepareWhisperRuntimeScript -Description 'Whisper runtime prepare script'
$nsisExe = Find-NSISExecutable
if ([string]::IsNullOrWhiteSpace($nsisExe)) {
    throw 'NSIS makensis.exe not found. Install NSIS or add makensis to PATH.'
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

Push-Location $frontendDir
try {
    if ($env:CI -eq 'true' -or -not (Test-Path (Join-Path $frontendDir 'node_modules'))) {
        Invoke-Step 'Installing frontend dependencies...' { npm ci }
    } else {
        Write-Host 'Using existing frontend dependencies...'
    }

    Invoke-Step 'Testing frontend...' { npm test }
    Invoke-Step 'Linting frontend...' { npm run lint }

    Invoke-Step 'Building frontend assets...' { npm run build }
}
finally {
    Pop-Location
}

Push-Location $projectDir
try {
    Invoke-Step 'Running Go vet...' { go vet ./... }
    Invoke-Step 'Running Go tests...' { go test ./... }

    Invoke-Step 'Building SpeechKit.exe...' { go build -ldflags $goLdflags -o $bundleExe ./cmd/speechkit/ }
}
finally {
    Pop-Location
}

Write-Host 'Writing runtime config...'
$bundleConfig = Join-Path $bundleDir 'config.toml'
Copy-Item -Path (Join-Path $projectDir 'config.example.toml') -Destination $bundleConfig -Force
Invoke-Step 'Bundling local whisper runtime...' {
    & powershell -ExecutionPolicy Bypass -File $prepareWhisperRuntimeScript -BundleDir $bundleDir -CacheDir $cacheDir
}

Invoke-Step 'Building SpeechKit-Setup.exe...' { & $nsisExe $installerScript }

if ($seedDevBuildHFToken) {
    Write-Host 'Seeding local Hugging Face install token bootstrap...'
    $resolvedDevToken = Resolve-DevBuildHuggingFaceToken
    if ([string]::IsNullOrWhiteSpace($resolvedDevToken.Token)) {
        throw "SPEECHKIT_DEV_SEED_HF_TOKEN is enabled, but no token was found. Set SPEECHKIT_DEV_HF_TOKEN, $($resolvedDevToken.EnvName), or explicit DOPPLER_PROJECT/DOPPLER_CONFIG."
    }
    Set-PendingInstallHuggingFaceTokenBootstrap -Token $resolvedDevToken.Token
    Write-Host "Seeded PendingHFInstallToken from $($resolvedDevToken.Source)."
}

Write-Host ''
Write-Host 'Artifacts complete:'
Write-Host "  $bundleExe"
Write-Host "  $installerExe"
