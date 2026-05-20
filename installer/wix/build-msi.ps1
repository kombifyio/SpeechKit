<#
.SYNOPSIS
Build SpeechKit MSI from the staged NSIS source directory.

.DESCRIPTION
Assumes scripts/build.ps1 -SkipInstaller has already populated
dist/windows/SpeechKit/. The MSI builds from the same staging directory.

Requires WiX v4 CLI:
    dotnet tool install --global wix --version 4.*

.PARAMETER Version
SpeechKit version string (e.g. "0.34.9"). Defaults to $env:SPEECHKIT_VERSION.

.PARAMETER OutFile
Output path for the .msi file.
Defaults to ../../dist/windows/SpeechKit-x64.msi relative to this script.

.EXAMPLE
    # From repo root:
    .\installer\wix\build-msi.ps1 -Version 0.34.9

.EXAMPLE
    # CI usage (SPEECHKIT_VERSION set in env):
    pushd installer/wix; .\build-msi.ps1; popd
#>

param(
    [string]$Version = $env:SPEECHKIT_VERSION,
    [string]$OutFile = ""
)

$ErrorActionPreference = "Stop"

# Resolve paths relative to this script's directory so the script is safe
# to invoke from any working directory.
$ScriptDir  = $PSScriptRoot
$RepoRoot   = (Resolve-Path "$ScriptDir\..\.." ).Path
$StageDir   = Join-Path $RepoRoot "dist\windows\SpeechKit"

if (-not $Version) {
    Write-Error "Version required. Pass -Version x.y.z or set SPEECHKIT_VERSION."
}

if (-not $OutFile) {
    $OutFile = Join-Path $RepoRoot "dist\windows\SpeechKit-x64.msi"
}

# Validate staging directory is present.
if (-not (Test-Path $StageDir)) {
    Write-Error "Staging directory not found: $StageDir`nRun: .\scripts\build.ps1 -SkipInstaller first."
}

# Validate required binaries exist before heat/wix so failures are diagnostic.
$RequiredFiles = @(
    "SpeechKit.exe",
    "whisper-server.exe",
    "whisper.dll",
    "ggml.dll",
    "models\ggml-small.bin",
    "speechkit-wakeword.exe",
    "wakeword-kws\keywords.txt",
    "wakeword-kws\tokens.txt",
    "wakeword-kws\encoder-epoch-12-avg-2-chunk-16-left-64.onnx",
    "wakeword-kws\decoder-epoch-12-avg-2-chunk-16-left-64.onnx",
    "wakeword-kws\joiner-epoch-12-avg-2-chunk-16-left-64.onnx",
    "onnxruntime.dll",
    "sherpa-onnx-c-api.dll",
    "sherpa-onnx-cxx-api.dll",
    "MicrosoftEdgeWebview2Setup.exe",
    "config.toml"
)
foreach ($f in $RequiredFiles) {
    $p = Join-Path $StageDir $f
    if (-not (Test-Path $p)) {
        Write-Error "Required staging file missing: $p`nEnsure scripts/build.ps1 -SkipInstaller completed successfully."
    }
}

$TemplateConfig = Join-Path $RepoRoot "config.example.toml"
if (-not (Test-Path $TemplateConfig)) {
    Write-Error "Release-safe config template missing: $TemplateConfig"
}
Copy-Item -Path $TemplateConfig -Destination (Join-Path $StageDir "config.toml") -Force
Write-Host "Reset staging config.toml from config.example.toml for MSI packaging."

if (-not (Test-Path (Join-Path $StageDir "llama"))) {
    Write-Error "llama/ subdirectory missing from staging: $StageDir\llama`nEnsure scripts/build.ps1 -SkipInstaller completed successfully."
}

# Propagate version into environment so WiX can read $(env.SPEECHKIT_VERSION).
$env:SPEECHKIT_VERSION = $Version

Write-Host "Building SpeechKit MSI v$Version"
Write-Host "  Stage dir : $StageDir"
Write-Host "  Output    : $OutFile"

# -------------------------------------------------------------------------
# Step 1: Harvest the llama/ subdirectory into a WiX fragment.
#
# -dr LLAMADIR   — sets the root directory reference to LLAMADIR (defined
#                  in SpeechKit.wxs as the llama/ Directory element)
# -cg LlamaComponents — names the ComponentGroup referenced in the Feature
# -gg            — generate GUIDs for each Component
# -sfrag         — suppress the top-level Fragment element (we compose it)
# -srd           — suppress the root directory element
# -ke            — keep empty directories
# -------------------------------------------------------------------------
$LlamaFragmentPath = Join-Path $ScriptDir "llama-fragment.wxs"

Write-Host "`nHarvesting llama/ directory..."
& heat dir "$StageDir\llama" `
    -dr LLAMADIR `
    -cg LlamaComponents `
    -gg `
    -sfrag `
    -srd `
    -ke `
    -out $LlamaFragmentPath

if ($LASTEXITCODE -ne 0) {
    Write-Error "heat failed with exit code $LASTEXITCODE"
}
Write-Host "heat output: $LlamaFragmentPath"

# -------------------------------------------------------------------------
# Step 1b: Migrate the heat-generated fragment from the WiX v3 namespace
# (http://schemas.microsoft.com/wix/2006/wi) to the WiX v4 namespace
# (http://wixtoolset.org/schemas/v4/wxs) that the v4 compiler requires.
#
# `heat.exe` is included in WiX v4 for backward compatibility but it
# still emits v3 schema source. `wix convert` is WiX v4's official v3-
# to-v4 source migrator and is the documented way to bridge that gap
# without rewriting the fragment by hand each build.
# -------------------------------------------------------------------------
Write-Host "`nMigrating fragment to WiX v4 schema..."
& wix convert $LlamaFragmentPath
$convertExit = $LASTEXITCODE
# `wix convert` exit codes (per WiX v4 docs):
#   0 — file already on v4 schema, no changes made
#   2 — file was converted from v3, changes written (this is the
#       common case for a fresh heat output)
#   other — real error (parse failure, write failure, etc.)
# Treating 2 as a failure caused v0.35.9's Build Windows Assets to
# abort even though the conversion had actually succeeded; the v4
# build would have read the converted llama-fragment.wxs without
# issue. Accept both 0 and 2 as success.
if ($convertExit -ne 0 -and $convertExit -ne 2) {
    Write-Error "wix convert failed with exit code $convertExit on $LlamaFragmentPath"
}

# -------------------------------------------------------------------------
# Step 2: Compile and link the MSI.
#
# wix build compiles each .wxs source and links them into a single MSI.
# -arch x64 ensures the package targets 64-bit Windows.
# -------------------------------------------------------------------------
Write-Host "`nBuilding MSI..."
& wix build `
    (Join-Path $ScriptDir "SpeechKit.wxs") `
    $LlamaFragmentPath `
    -arch x64 `
    -out $OutFile

if ($LASTEXITCODE -ne 0) {
    Write-Error "wix build failed with exit code $LASTEXITCODE"
}

$msiInfo = Get-Item $OutFile
Write-Host "`nMSI built successfully."
Write-Host "  Path : $OutFile"
Write-Host ("  Size : {0:N0} bytes ({1:N1} MB)" -f $msiInfo.Length, ($msiInfo.Length / 1MB))
Write-Host ""
Write-Host "Quick verification (admin PowerShell):"
Write-Host "  msiexec /a `"$OutFile`" /qn TARGETDIR=`"%TEMP%\SpeechKit-verify`""
Write-Host "  Get-ChildItem `"%TEMP%\SpeechKit-verify`""
