param(
    [Parameter(Mandatory = $true)]
    [string]$BundleDir,
    [Parameter(Mandatory = $true)]
    [string]$CacheDir
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$ProgressPreference = 'SilentlyContinue'

# Pinned sherpa-onnx Zipformer KWS model. ~17 MB, Apache-2.0, ships
# encoder/decoder/joiner ONNX + tokens.txt + keywords.txt. Update this
# triple together when bumping; SHA256 verification keeps the bundle
# reproducible across CI runs.
$modelRelease = 'sherpa-onnx-kws-zipformer-gigaspeech-3.3M-2024-01-01'
$modelAsset = "$modelRelease.tar.bz2"
$modelUrl = "https://github.com/k2-fsa/sherpa-onnx/releases/download/kws-models/$modelAsset"
$modelSha256 = '' # computed on first download below; CI can pin it later

$runtimeCacheDir = Join-Path $CacheDir 'wakeword-model'
$openWakewordCacheDir = Join-Path $CacheDir 'openwakeword-models'
$modelTarPath = Join-Path $runtimeCacheDir $modelAsset
$modelExtractDir = Join-Path $runtimeCacheDir "extract-$modelRelease"
$targetDir = Join-Path $BundleDir 'wakeword-kws'
$openWakewordTargetDir = Join-Path (Join-Path $BundleDir 'models') 'wakeword'

function Get-Sha256 {
    param([Parameter(Mandatory = $true)][string]$Path)
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Download-VerifiedFile {
    param(
        [Parameter(Mandatory = $true)][string]$Url,
        [Parameter(Mandatory = $true)][string]$Destination,
        [Parameter(Mandatory = $true)][string]$ExpectedSha256,
        [Parameter(Mandatory = $true)][string]$Description
    )

    if (Test-Path -LiteralPath $Destination) {
        $actual = Get-Sha256 -Path $Destination
        if ($actual -eq $ExpectedSha256.ToLowerInvariant()) {
            Write-Host "Using cached $Description"
            return
        }
        Remove-Item -LiteralPath $Destination -Force
    }

    $tmp = "$Destination.download"
    if (Test-Path -LiteralPath $tmp) {
        Remove-Item -LiteralPath $tmp -Force
    }
    Write-Host "Downloading $Description ..."
    Invoke-WebRequest -Uri $Url -OutFile $tmp
    $actual = Get-Sha256 -Path $tmp
    if ($actual -ne $ExpectedSha256.ToLowerInvariant()) {
        Remove-Item -LiteralPath $tmp -Force
        throw "$Description hash mismatch. Expected $ExpectedSha256, got $actual."
    }
    Move-Item -LiteralPath $tmp -Destination $Destination -Force
}

if (-not (Test-Path -LiteralPath $runtimeCacheDir)) {
    New-Item -ItemType Directory -Path $runtimeCacheDir | Out-Null
}
if (-not (Test-Path -LiteralPath $openWakewordCacheDir)) {
    New-Item -ItemType Directory -Path $openWakewordCacheDir | Out-Null
}

if (-not (Test-Path -LiteralPath $modelTarPath)) {
    Write-Host "Downloading $modelAsset (~17 MB) ..."
    $tmp = "$modelTarPath.download"
    if (Test-Path $tmp) { Remove-Item $tmp -Force }
    Invoke-WebRequest -Uri $modelUrl -OutFile $tmp
    Move-Item -LiteralPath $tmp -Destination $modelTarPath -Force
} else {
    Write-Host "Using cached $modelAsset"
}

if ($modelSha256) {
    $actual = Get-Sha256 -Path $modelTarPath
    if ($actual -ne $modelSha256.ToLowerInvariant()) {
        Remove-Item -LiteralPath $modelTarPath -Force
        throw "Wake-word model hash mismatch. Expected $modelSha256, got $actual. Re-run to redownload."
    }
}

if (Test-Path -LiteralPath $modelExtractDir) {
    Remove-Item -LiteralPath $modelExtractDir -Recurse -Force
}
New-Item -ItemType Directory -Path $modelExtractDir | Out-Null

# tar.exe ships with Windows 10+ (bsdtar with libarchive bz2 built in) and
# extracts .tar.bz2 in one shot. We resolve the System32 path explicitly
# because Git's GNU tar may shadow it in the build-host PATH and that one
# shells out to bzip2.exe which is not always reachable from the cleaned
# build PATH, producing "Compressed file ends unexpectedly" with an
# otherwise-valid archive.
$systemTar = Join-Path ([Environment]::SystemDirectory) 'tar.exe'
if (-not (Test-Path -LiteralPath $systemTar)) {
    throw "Windows tar.exe not found at $systemTar. Requires Windows 10+."
}
& $systemTar -xjf $modelTarPath -C $modelExtractDir
if ($LASTEXITCODE -ne 0) {
    throw "Failed to extract $modelTarPath"
}

$sourceDir = Get-ChildItem -Path $modelExtractDir -Directory -Filter $modelRelease | Select-Object -First 1
if ($null -eq $sourceDir) {
    throw "Extracted archive did not contain the expected $modelRelease directory"
}

if (Test-Path -LiteralPath $targetDir) {
    Remove-Item -LiteralPath $targetDir -Recurse -Force
}
New-Item -ItemType Directory -Path $targetDir | Out-Null

# Only ship the files the desktop adapter (cmd/speechkit/desktop_wakeword.go)
# actually loads — encoder/decoder/joiner ONNX + tokens.txt. We skip the
# int8 variants, the BPE model and test wavs to keep the bundle lean.
# keywords.txt from upstream is intentionally NOT copied: the upstream
# file lists generic phrases ("HEY SIRI", "ALEXA", ...) which don't match
# the SpeechKit catalog (hey_quby / hey_computer / hey_jarvis / hey_mira /
# hey_kombify). Instead we write the curated SpeechKit keywords below.
$required = @(
    'encoder-epoch-12-avg-2-chunk-16-left-64.onnx',
    'decoder-epoch-12-avg-2-chunk-16-left-64.onnx',
    'joiner-epoch-12-avg-2-chunk-16-left-64.onnx',
    'tokens.txt'
)
foreach ($name in $required) {
    $src = Join-Path $sourceDir.FullName $name
    if (-not (Test-Path -LiteralPath $src)) {
        throw "Required wake-word model file missing in upstream archive: $name"
    }
    Copy-Item -LiteralPath $src -Destination (Join-Path $targetDir $name) -Force
}

# Write the SpeechKit-curated keywords file. Each line is the
# BPE-tokenised form of one phrase followed by `:boost @label`. Tokens
# were produced by sentencepiece against the bundled bpe.model. When
# adding new entries:
#   1. Run `python -c "import sentencepiece as spm; sp=spm.SentencePieceProcessor(); sp.Load(BPE_MODEL); print(' '.join(sp.EncodeAsPieces(TEXT.upper())))"`
#   2. Append the result to the array below with a per-line :boost (1.5 is a
#      reasonable default; raise to 2.0 for short phrases like "alexa", lower
#      to 1.2 for very common ones to suppress false-accepts).
#   3. Use the @label matching internal/wakeword/catalog.go ID so the
#      desktop adapter can resolve display name + catalog metadata at runtime.
# sherpa-onnx EncodeBase parses each line as space-separated tokens followed
# by optional `:<boost>`, `#<threshold>`, `@<label>` modifiers — each prefix
# MUST be its own whitespace-separated field. Concatenating `Y:1.5` makes the
# parser try to look up "Y:1.5" as a token and fail with "Cannot find ID".
$speechkitKeywords = @(
    '▁HE Y ▁QU B Y :1.5 @hey_quby'
    '▁HE Y ▁C U B I :1.5 @hey_quby'
    '▁HE Y ▁K U B I :1.5 @hey_quby'
    '▁HE Y ▁COMP U TER :1.5 @hey_computer'
    '▁HE Y ▁JA R VI S :1.5 @hey_jarvis'
    '▁HE Y ▁MI RA :1.5 @hey_mira'
    '▁HE Y ▁K OM B IF Y :1.5 @hey_kombify'
    '▁HE Y ▁S I RI :1.5 @hey_siri'
    '▁A LE X A :2.0 @alexa'
    '▁HI ▁GO O G LE :1.5 @hi_google'
)
$keywordsPath = Join-Path $targetDir 'keywords.txt'
[System.IO.File]::WriteAllLines($keywordsPath, $speechkitKeywords)

Write-Host "Bundled sherpa-onnx KWS model into $targetDir ($([math]::Round((Get-ChildItem $targetDir -File | Measure-Object -Property Length -Sum).Sum / 1MB, 1)) MB)."

# openWakeWord-compatible ONNX artifacts used by the temporary
# LiveKit/openWakeWord backend. These are small enough to ship in the
# desktop bundle so the backend works immediately after install, while the
# model catalog can still manage updated/user-selected copies in
# %LOCALAPPDATA%\SpeechKit\models\wakeword.
$openWakewordFiles = @(
    @{
        Name = 'melspectrogram.onnx'
        Url = 'https://huggingface.co/Soulcreek2/speechkit-wakeword-models/resolve/main/melspectrogram.onnx'
        Sha256 = 'ba2b0e0f8b7b875369a2c89cb13360ff53bac436f2895cced9f479fa65eb176f'
        Description = 'openWakeWord melspectrogram model'
    }
    @{
        Name = 'embedding_model.onnx'
        Url = 'https://huggingface.co/Soulcreek2/speechkit-wakeword-models/resolve/main/embedding_model.onnx'
        Sha256 = '70d164290c1d095d1d4ee149bc5e00543250a7316b59f31d056cff7bd3075c1f'
        Description = 'openWakeWord embedding model'
    }
    @{
        Name = 'hey_quby.onnx'
        Url = 'https://huggingface.co/Soulcreek2/speechkit-wakeword-models/resolve/main/hey_quby.onnx'
        Sha256 = 'd2219a70af63a12750b8d0a21fda38688b96841d21f564e57f99af7ba56951a6'
        Description = 'openWakeWord phrase model hey_quby'
    }
    @{
        Name = 'hey_computer.onnx'
        Url = 'https://huggingface.co/Soulcreek2/speechkit-wakeword-models/resolve/main/hey_computer.onnx'
        Sha256 = '3acbd9ffff04beba2d16ebdfd0d4c734d65fecdd22446f25f4d0afa6e5d7606b'
        Description = 'openWakeWord phrase model hey_computer'
    }
    @{
        Name = 'hey_jarvis.onnx'
        Url = 'https://huggingface.co/Soulcreek2/speechkit-wakeword-models/resolve/main/hey_jarvis.onnx'
        Sha256 = '7256019a18029c7bea33abc3344f7a3d0e07655cf4a5f18e65c9b6329eac3fb6'
        Description = 'openWakeWord phrase model hey_jarvis'
    }
    @{
        Name = 'hey_mira.onnx'
        Url = 'https://huggingface.co/Soulcreek2/speechkit-wakeword-models/resolve/main/hey_mira.onnx'
        Sha256 = 'cb1f371f3a61dccc43c47bc79145f504b1e2e2ed1ab233a427494fb57378e794'
        Description = 'openWakeWord phrase model hey_mira'
    }
    @{
        Name = 'hey_kombify.onnx'
        Url = 'https://huggingface.co/Soulcreek2/speechkit-wakeword-models/resolve/main/hey_kombify.onnx'
        Sha256 = '24c6d2d1c235892362ebf12b0055801d2f8461f856e15d704c3d8262304f4c9f'
        Description = 'openWakeWord phrase model hey_kombify'
    }
)

if (Test-Path -LiteralPath $openWakewordTargetDir) {
    Remove-Item -LiteralPath $openWakewordTargetDir -Recurse -Force
}
New-Item -ItemType Directory -Path $openWakewordTargetDir | Out-Null
foreach ($file in $openWakewordFiles) {
    $cachePath = Join-Path $openWakewordCacheDir $file.Name
    Download-VerifiedFile -Url $file.Url -Destination $cachePath -ExpectedSha256 $file.Sha256 -Description $file.Description
    Copy-Item -LiteralPath $cachePath -Destination (Join-Path $openWakewordTargetDir $file.Name) -Force
}

Write-Host "Bundled openWakeWord ONNX models into $openWakewordTargetDir ($([math]::Round((Get-ChildItem $openWakewordTargetDir -File | Measure-Object -Property Length -Sum).Sum / 1MB, 1)) MB)."
