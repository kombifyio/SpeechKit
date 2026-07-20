# verify-companion.ps1 - Companion-Gate ohne Hardware.
#
# Fuehrt vet + test + build fuer den kombify-box-Companion aus. Die Tests
# tragen Build-Tags "windows && cgo" und laufen deshalb NICHT in der Linux-CI —
# dieses Skript ist das verbindliche lokale Gate vor Flash/Demo/Commit
# (siehe kombify-box docs/e2e-runbook.md).
#
#   powershell -ExecutionPolicy Bypass -File examples\kombify-box-satellite\tools\verify-companion.ps1
$ErrorActionPreference = "Stop"

$exedir = Split-Path $PSScriptRoot -Parent                       # examples\kombify-box-satellite
$repo = (Resolve-Path (Join-Path $exedir "..\..")).Path          # SpeechKit-Repo-Root
$exe = Join-Path $exedir "kbx-companion.exe"
$go = "C:\Users\Marcel\go-sdk\go\bin\go.exe"
$mingw = "C:\Users\Marcel\mingw-winlibs\mingw64\bin"

if (-not (Test-Path $go)) {
    $goCmd = Get-Command go -ErrorAction SilentlyContinue
    if (-not $goCmd) { throw "Kein Go-Toolchain gefunden (erwartet: $go)" }
    $go = $goCmd.Source
}

$env:GOWORK = "off"
$env:CGO_ENABLED = "1"
if (Test-Path $mingw) { $env:Path = "$mingw;$env:Path" }

Push-Location $repo
try {
    Write-Host "[gate] go vet ./examples/kombify-box-satellite"
    & $go vet ./examples/kombify-box-satellite
    if ($LASTEXITCODE -ne 0) { throw "go vet failed with exit $LASTEXITCODE" }

    Write-Host "[gate] go test ./examples/kombify-box-satellite"
    & $go test ./examples/kombify-box-satellite
    if ($LASTEXITCODE -ne 0) { throw "go test failed with exit $LASTEXITCODE" }

    Write-Host "[gate] go build -o kbx-companion.exe"
    & $go build -o $exe ./examples/kombify-box-satellite
    if ($LASTEXITCODE -ne 0) { throw "go build failed with exit $LASTEXITCODE" }
} finally {
    Pop-Location
}

$companionHead = (git -C $repo rev-parse HEAD).Trim()
Write-Host "VERIFY-COMPANION PASS"
Write-Host "companion_head=$companionHead"
Write-Host "exe_sha256=$((Get-FileHash $exe -Algorithm SHA256).Hash.ToLowerInvariant())"
