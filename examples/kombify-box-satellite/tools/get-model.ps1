# Laedt das sherpa-onnx KWS-Modell (gigaspeech zipformer, englisch, Apache-2.0)
# in .\models\ herunter - einmalig noetig.
$ErrorActionPreference = "Stop"
$name = "sherpa-onnx-kws-zipformer-gigaspeech-3.3M-2024-01-01"
$url  = "https://github.com/k2-fsa/sherpa-onnx/releases/download/kws-models/$name.tar.bz2"
$dest = Join-Path $PSScriptRoot "..\models"
New-Item -ItemType Directory -Force -Path $dest | Out-Null
$tar = Join-Path $dest "$name.tar.bz2"
if (-not (Test-Path (Join-Path $dest $name))) {
    Write-Host "Lade $url ..."
    Invoke-WebRequest -Uri $url -OutFile $tar
    tar -xjf $tar -C $dest
    Remove-Item $tar
}
Write-Host "Modell bereit: $dest\$name"
