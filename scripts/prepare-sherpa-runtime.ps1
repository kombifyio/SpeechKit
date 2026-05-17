param(
    [Parameter(Mandatory = $true)]
    [string]$BundleDir,
    [Parameter(Mandatory = $true)]
    [string]$CacheDir
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$ProgressPreference = 'SilentlyContinue'

# Bundle the MinGW-compiled sherpa-onnx + onnxruntime native libraries
# next to SpeechKit.exe. The Go binding (github.com/k2-fsa/sherpa-onnx-go,
# pulled by the main go.mod) links against these via cgo with relative
# LDFLAGS that resolve at compile time; at runtime Windows loads them from
# the directory of the executable, which is why we copy them here.
#
# Why MinGW: SpeechKit's Windows build script uses MinGW (msys64 gcc/g++)
# for cgo. Pairing a MinGW-compiled binary with the official Microsoft ORT
# (which is MSVC-built) caused the v0.34.x wake-word native aborts. The
# sherpa-onnx project ships a MinGW build of both their C API and the ORT
# they bundle, so the entire toolchain stays consistent and ABI-stable.
#
# The Go module cache holds the libraries inside the sherpa-onnx-go-windows
# module — there is no separate download to verify. Pinning happens via
# go.sum's hash on that module.

$arch = 'x86_64-pc-windows-gnu'
$moduleVersion = 'v1.13.2' # keep in lockstep with require in main go.mod
$goPath = if ($env:GOPATH) { $env:GOPATH } else { Join-Path $HOME 'go' }
$libDir = Join-Path $goPath "pkg\mod\github.com\k2-fsa\sherpa-onnx-go-windows@$moduleVersion\lib\$arch"

if (-not (Test-Path -LiteralPath $libDir)) {
    Write-Host "sherpa-onnx native lib dir not found at $libDir — running 'go mod download' to fetch it..."
    Push-Location (Split-Path -Parent $PSScriptRoot)
    try {
        & go mod download github.com/k2-fsa/sherpa-onnx-go-windows | Out-Null
    } finally {
        Pop-Location
    }
}
if (-not (Test-Path -LiteralPath $libDir)) {
    throw "sherpa-onnx native lib dir still missing after go mod download: $libDir. Ensure go.mod requires github.com/k2-fsa/sherpa-onnx-go $moduleVersion."
}

$copied = @()
foreach ($dll in Get-ChildItem -Path $libDir -File -Filter '*.dll') {
    $dest = Join-Path $BundleDir $dll.Name
    Copy-Item -LiteralPath $dll.FullName -Destination $dest -Force
    $copied += $dll.Name
}

if ($copied.Count -eq 0) {
    throw "No DLLs found in $libDir — sherpa-onnx-go-windows@$moduleVersion is malformed."
}

# The unused $CacheDir parameter keeps the script signature uniform with
# the whisper / llama / onnxruntime prep steps; future caching policy can
# stash a verified copy under .cache/sherpa-runtime/.
$null = $CacheDir

Write-Host "Bundled sherpa-onnx + ORT MinGW libs ($($copied -join ', '))."
