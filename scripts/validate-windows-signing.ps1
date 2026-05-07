param(
    [string]$DistDir,
    [string]$BundleExePath,
    [string]$InstallerExePath,
    [string]$ExpectedPublisher = '',
    [switch]$RequireInstaller,
    [switch]$RequireTimestamp,
    [switch]$UseSignTool
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$projectDir = Split-Path -Parent $scriptDir

if ([string]::IsNullOrWhiteSpace($DistDir)) {
    $DistDir = Join-Path $projectDir 'dist/windows'
}

if ([string]::IsNullOrWhiteSpace($BundleExePath)) {
    $bundleExe = Join-Path $DistDir 'SpeechKit\SpeechKit.exe'
} else {
    $bundleExe = $BundleExePath
}

if ([string]::IsNullOrWhiteSpace($InstallerExePath)) {
    $installerExe = Join-Path $DistDir 'SpeechKit-Setup.exe'
} else {
    $installerExe = $InstallerExePath
}

function Get-SignToolPath {
    $command = Get-Command 'signtool.exe' -ErrorAction SilentlyContinue
    if ($null -ne $command -and -not [string]::IsNullOrWhiteSpace($command.Source)) {
        return $command.Source
    }

    $candidates = @(
        'C:\Program Files (x86)\Windows Kits\10\bin\x64\signtool.exe',
        'C:\Program Files (x86)\Windows Kits\10\bin\10.0.26100.0\x64\signtool.exe',
        'C:\Program Files (x86)\Windows Kits\10\App Certification Kit\signtool.exe'
    )

    foreach ($candidate in $candidates) {
        if (Test-Path $candidate) {
            return $candidate
        }
    }

    return ''
}

function Test-HasTimestamp {
    param(
        [Parameter(Mandatory = $true)]
        [System.Management.Automation.Signature]$Signature
    )

    if ($null -eq $Signature.TimeStamperCertificate) {
        return $false
    }

    return -not [string]::IsNullOrWhiteSpace($Signature.TimeStamperCertificate.Subject)
}

function Test-FileSignature {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        [Parameter(Mandatory = $true)]
        [string]$Role,
        [string]$ExpectedPublisher,
        [switch]$RequireTimestamp,
        [switch]$UseSignTool,
        [string]$SignToolPath
    )

    if (-not (Test-Path $Path)) {
        throw "$Role missing: $Path"
    }

    $signature = Get-AuthenticodeSignature -FilePath $Path
    $status = [string]$signature.Status
    $subject = ''
    if ($null -ne $signature.SignerCertificate) {
        $subject = [string]$signature.SignerCertificate.Subject
    }

    if ($status -ne 'Valid') {
        throw "$Role is not validly Authenticode-signed. Status: $status. File: $Path"
    }

    if ([string]::IsNullOrWhiteSpace($subject)) {
        throw "$Role has no signer subject. File: $Path"
    }

    if (-not [string]::IsNullOrWhiteSpace($ExpectedPublisher) -and $subject -notlike "*$ExpectedPublisher*") {
        throw "$Role signer does not match expected publisher '$ExpectedPublisher'. Actual subject: $subject"
    }

    if ($RequireTimestamp -and -not (Test-HasTimestamp -Signature $signature)) {
        throw "$Role is signed but has no trusted timestamp. File: $Path"
    }

    if ($UseSignTool) {
        if ([string]::IsNullOrWhiteSpace($SignToolPath)) {
            throw 'UseSignTool was requested, but signtool.exe was not found.'
        }

        & $SignToolPath verify /pa /all /v $Path | Out-Host
        if ($LASTEXITCODE -ne 0) {
            throw "SignTool verification failed for $Role. File: $Path"
        }
    }

    [pscustomobject]@{
        Role = $Role
        Path = $Path
        SignerSubject = $subject
        Timestamped = (Test-HasTimestamp -Signature $signature)
        Status = $status
    }
}

$signToolPath = ''
if ($UseSignTool) {
    $signToolPath = Get-SignToolPath
}

$results = @()
$results += Test-FileSignature -Path $bundleExe -Role 'SpeechKit.exe' -ExpectedPublisher $ExpectedPublisher -RequireTimestamp:$RequireTimestamp -UseSignTool:$UseSignTool -SignToolPath $signToolPath

if ($RequireInstaller -or (Test-Path $installerExe)) {
    $results += Test-FileSignature -Path $installerExe -Role 'SpeechKit-Setup.exe' -ExpectedPublisher $ExpectedPublisher -RequireTimestamp:$RequireTimestamp -UseSignTool:$UseSignTool -SignToolPath $signToolPath
}

Write-Host ''
Write-Host 'Windows signing validation passed.'
$results | Format-Table Role, Status, Timestamped, SignerSubject -AutoSize | Out-Host
Write-Host ''
Write-Host 'Important:'
Write-Host '  - A valid Authenticode signature is what removes the UAC publisher being shown as unknown.'
Write-Host '  - Microsoft SmartScreen is separate and can still warn until the file or certificate builds reputation.'