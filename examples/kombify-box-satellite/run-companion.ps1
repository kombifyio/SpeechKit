# Run the kombify box companion against the flashed box.
#
# For the full voice loop, set:
#   $env:KOMBIFY_GATEWAY_BASE_URL = "https://<gateway>/v1"
#   $env:KOMBIFY_GATEWAY_TOKEN    = "<token>"
# For the SpeechKit Voice Agent mode, point at the real speechkit-server:
#   $env:SPEECHKIT_SERVER_URL   = "https://speechkit.kombify.io"
#   $env:SPEECHKIT_SERVER_TOKEN = "<server bearer token, if required>"
#
# Without a real gateway you can still verify mic + wakeword: watch the
# "[mic] peak RMS" lines and "[wake]" when you say "hey jarvis".
#
# Run from the SpeechKit repo root:
#   powershell -ExecutionPolicy Bypass -File examples\kombify-box-satellite\run-companion.ps1

$ErrorActionPreference = "Stop"
# repo-relativ statt absolut: ueberlebt Workspace-Umzuege (2026-07:
# C:\Github\SpeechKit -> C:\Github\kombify-workspace\kombify-SpeechKit)
$repo = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$exedir = $PSScriptRoot
$exe = Join-Path $exedir "kbx-companion.exe"
$config = Join-Path $repo "examples\kombify-box-satellite\config.toml"
$go = "C:\Users\Marcel\go-sdk\go\bin\go.exe"
$mingw = "C:\Users\Marcel\mingw-winlibs\mingw64\bin"

if (-not (Test-Path $go)) {
    $goCmd = Get-Command go -ErrorAction SilentlyContinue
    if ($goCmd) { $go = $goCmd.Source }
}

function Test-CompanionBuildStale {
    if (-not (Test-Path $exe)) { return $true }
    $exeTime = (Get-Item $exe).LastWriteTimeUtc
    $newerSource = Get-ChildItem $exedir -Filter *.go -File |
        Where-Object { $_.LastWriteTimeUtc -gt $exeTime } |
        Select-Object -First 1
    return $null -ne $newerSource
}

if (Test-CompanionBuildStale) {
    if (-not (Test-Path $go)) {
        throw "Missing $exe and no Go toolchain found. Expected $go"
    }
    Write-Host "[build] refreshing kbx-companion.exe"
    Push-Location $repo
    try {
        $env:CGO_ENABLED = "1"
        if (Test-Path $mingw) {
            $env:Path = "$mingw;$env:Path"
        }
        & $go build -o $exe ./examples/kombify-box-satellite
        if ($LASTEXITCODE -ne 0) { throw "go build failed with exit $LASTEXITCODE" }
    } finally {
        Pop-Location
    }
}

# The matched sherpa DLLs must sit next to the exe so they win over older
# copies elsewhere on PATH.
$mod = "C:\Users\Marcel\go\pkg\mod\github.com\k2-fsa\sherpa-onnx-go-windows@v1.13.4\lib\x86_64-pc-windows-gnu"
try {
    Copy-Item "$mod\onnxruntime.dll","$mod\sherpa-onnx-c-api.dll","$mod\sherpa-onnx-cxx-api.dll" $exedir -Force -ErrorAction Stop
} catch {
    Write-Warning "Could not refresh sherpa DLLs, using existing copies: $($_.Exception.Message)"
}

$configText = Get-Content $config -Raw
$env:SPEECHKIT_DISABLE_PORTABLE = "1"

if (-not $env:KOMBIFY_GATEWAY_TOKEN -and $configText -match "KOMBIFY_GATEWAY_TOKEN|REPLACE-WITH-KOMBIFY-GATEWAY") {
    Write-Warning "KOMBIFY_GATEWAY_TOKEN is not set. Local startup can run, but real STT/LLM/TTS needs the kombify gateway token."
}

if (-not $env:SPEECHKIT_SERVER_URL) {
    $env:SPEECHKIT_SERVER_URL = "https://speechkit.kombify.io"
}

if (-not $env:SPEECHKIT_SERVER_TOKEN -and -not $env:SPEECHKIT_TOKEN) {
    Write-Warning "SPEECHKIT_SERVER_TOKEN/SPEECHKIT_TOKEN is not set. This is OK only if the SpeechKit Server allows public/no-auth session creation."
}

$configChanged = $false

if ($env:KOMBIFY_COMPANION_MODE) {
    $mode = $env:KOMBIFY_COMPANION_MODE.Trim()
    $configText = $configText -replace 'target\s*=\s*"(wake_only|wake-only|assist|voice_agent|voice-agent|agent|live)"', "target = `"$mode`""
    $configChanged = $true
}

if ($env:KOMBIFY_GATEWAY_BASE_URL) {
    $base = $env:KOMBIFY_GATEWAY_BASE_URL.TrimEnd("/")
    $configText = $configText.Replace("https://REPLACE-WITH-KOMBIFY-GATEWAY/v1", $base)
    $configChanged = $true
} elseif ($configText -match "REPLACE-WITH-KOMBIFY-GATEWAY") {
    Write-Warning "KOMBIFY_GATEWAY_BASE_URL is not set; config.toml still contains the placeholder gateway URL."
}

if ($env:SPEECHKIT_SERVER_URL) {
    $server = $env:SPEECHKIT_SERVER_URL.TrimEnd("/")
    $configText = $configText.Replace('base_url  = "https://speechkit.kombify.io"', "base_url  = `"$server`"")
    $configChanged = $true
}

if ($configChanged) {
    $tmpConfig = Join-Path $env:TEMP "kbx-companion.config.toml"
    $configText | Set-Content -Path $tmpConfig -Encoding UTF8
    $config = $tmpConfig
}

# Der CDC-Status-Link zur Box lebt jetzt im Companion selbst (boxlink.go,
# gefuettert vom companion.Options.OnStage-Hook). Dieses Skript staged nur
# noch Env/DLLs und startet die exe. Port-Override weiterhin ueber
# KOMBIFY_BOX_STATUS_PORT oder [box].status_port in config.toml.

try {
    Push-Location $repo
    & $exe $config
    exit $LASTEXITCODE
} finally {
    Pop-Location -ErrorAction SilentlyContinue
}
