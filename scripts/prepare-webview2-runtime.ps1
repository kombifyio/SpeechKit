param(
    [Parameter(Mandatory = $true)]
    [string]$BundleDir,
    [Parameter(Mandatory = $true)]
    [string]$CacheDir
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$ProgressPreference = 'SilentlyContinue'

$bootstrapperUrl = 'https://go.microsoft.com/fwlink/p/?LinkId=2124703'
$cachePath = Join-Path $CacheDir 'MicrosoftEdgeWebview2Setup.exe'
$bundlePath = Join-Path $BundleDir 'MicrosoftEdgeWebview2Setup.exe'

function Assert-MicrosoftSignature {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    $signature = Get-AuthenticodeSignature -FilePath $Path
    if ($signature.Status -ne 'Valid') {
        throw "Invalid signature for WebView2 bootstrapper: $($signature.Status)"
    }

    $subject = $signature.SignerCertificate.Subject
    if ($subject -notlike '*Microsoft*') {
        throw "Unexpected signer for WebView2 bootstrapper: $subject"
    }
}

if (-not (Test-Path -LiteralPath $CacheDir)) {
    New-Item -ItemType Directory -Path $CacheDir | Out-Null
}

$needsDownload = $true
if (Test-Path -LiteralPath $cachePath) {
    try {
        Assert-MicrosoftSignature -Path $cachePath
        $needsDownload = $false
    } catch {
        Remove-Item -LiteralPath $cachePath -Force
    }
}

if ($needsDownload) {
    $tempPath = "$cachePath.download"
    if (Test-Path -LiteralPath $tempPath) {
        Remove-Item -LiteralPath $tempPath -Force
    }

    Write-Host 'Downloading WebView2 bootstrapper...'
    Invoke-WebRequest -Uri $bootstrapperUrl -OutFile $tempPath
    Assert-MicrosoftSignature -Path $tempPath
    Move-Item -LiteralPath $tempPath -Destination $cachePath -Force
}

Copy-Item -LiteralPath $cachePath -Destination $bundlePath -Force
Write-Host 'Bundled WebView2 bootstrapper prepared.'

