# Erzeugt keywords.txt mit korrekt BPE-tokenisierten Eintraegen passend zum
# GigaSpeech KWS-Modell. Nutzt das SpeechKit-Go-Tool statt Python/sherpa CLI,
# damit der Companion auf Windows ohne zusaetzliches Python laeuft.
$ErrorActionPreference = "Stop"

$model = Join-Path $PSScriptRoot "..\models\sherpa-onnx-kws-zipformer-gigaspeech-3.3M-2024-01-01"
if (-not (Test-Path $model)) { throw "Modell fehlt - erst tools\get-model.ps1 ausfuehren" }

$repo = Resolve-Path (Join-Path $PSScriptRoot "..\..\..")
$go = "C:\Users\Marcel\go-sdk\go\bin\go.exe"
if (-not (Test-Path $go)) {
    $goCmd = Get-Command go -ErrorAction SilentlyContinue
    if ($goCmd) { $go = $goCmd.Source }
}
if (-not (Test-Path $go)) {
    throw "Go toolchain nicht gefunden. Erwartet: C:\Users\Marcel\go-sdk\go\bin\go.exe oder go im PATH."
}

$tokens = Join-Path $model "tokens.txt"
$out = Join-Path $PSScriptRoot "..\keywords.txt"
$jarvisOut = Join-Path $PSScriptRoot "..\keywords.jarvis.txt"

Push-Location $repo
try {
    $jarvisLines = & $go run ./examples/kombify-box-satellite/tools/encode-keywords --tokens $tokens `
        "HEY JARVIS :2.0 @hey_jarvis" `
        "JARVIS :1.6 @jarvis"
    if ($LASTEXITCODE -ne 0) { throw "encode-keywords jarvis failed with exit $LASTEXITCODE" }

    $lines = & $go run ./examples/kombify-box-satellite/tools/encode-keywords --tokens $tokens `
        "KOMBIFY :2.0 @kombify" `
        "HEY KOMBIFY :1.8 @hey_kombify" `
        "HEY JARVIS :2.0 @hey_jarvis" `
        "JARVIS :1.6 @jarvis"
    if ($LASTEXITCODE -ne 0) { throw "encode-keywords failed with exit $LASTEXITCODE" }
} finally {
    Pop-Location
}

$utf8NoBom = [System.Text.UTF8Encoding]::new($false)
[System.IO.File]::WriteAllLines($jarvisOut, [string[]]$jarvisLines, $utf8NoBom)
[System.IO.File]::WriteAllLines($out, [string[]]$lines, $utf8NoBom)
Write-Host "keywords.jarvis.txt geschrieben:"
Get-Content $jarvisOut
Write-Host "keywords.txt geschrieben:"
Get-Content $out
