#!/bin/bash
# Reproducible Windows bundle build for kombify SpeechKit.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
FRONTEND_DIR="$PROJECT_DIR/frontend/app"
DIST_DIR="$PROJECT_DIR/dist/windows"
BUNDLE_DIR="$DIST_DIR/SpeechKit"
BUNDLE_EXE="$BUNDLE_DIR/SpeechKit.exe"
INSTALLER_SCRIPT="$PROJECT_DIR/installer/speechkit.nsi"
INSTALLER_EXE="$DIST_DIR/SpeechKit-Setup.exe"
PREPARE_WHISPER_RUNTIME_SCRIPT="$SCRIPT_DIR/prepare-whisper-runtime.ps1"

export PATH="/c/msys64/mingw64/bin:$PATH"
export CGO_ENABLED=1

GO_MODULE_PATH="$(go list -m -f '{{.Path}}')"
if [ -z "$GO_MODULE_PATH" ]; then
  echo "Could not resolve Go module path from go.mod" >&2
  exit 1
fi

MANAGED_HF_DEFAULT="${SPEECHKIT_MANAGED_HF_DEFAULT-}"
MANAGED_DOPPLER_PROJECT="${SPEECHKIT_MANAGED_DOPPLER_PROJECT-}"
MANAGED_DOPPLER_CONFIG="${SPEECHKIT_MANAGED_DOPPLER_CONFIG-}"
GO_LDFLAGS="-H windowsgui -X ${GO_MODULE_PATH}/internal/config.managedHFDefaultOptIn=${MANAGED_HF_DEFAULT} -X ${GO_MODULE_PATH}/internal/config.managedDopplerDefaultProject=${MANAGED_DOPPLER_PROJECT} -X ${GO_MODULE_PATH}/internal/config.managedDopplerDefaultConfig=${MANAGED_DOPPLER_CONFIG}"

require_path() {
  local path="$1"
  local description="$2"
  if [ ! -e "$path" ]; then
    echo "$description missing: $path" >&2
    exit 1
  fi
}

require_command() {
  local command_name="$1"
  local description="$2"
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "$description not found: $command_name" >&2
    exit 1
  fi
}

cd "$PROJECT_DIR"

echo "Preparing clean Windows bundle..."
require_path "$FRONTEND_DIR" "Frontend source directory"
require_path "$FRONTEND_DIR/package.json" "Frontend package manifest"
require_path "$FRONTEND_DIR/src" "Frontend source tree"
require_path "$INSTALLER_SCRIPT" "NSIS installer script"
require_path "$PREPARE_WHISPER_RUNTIME_SCRIPT" "Whisper runtime prepare script"
require_command "makensis" "NSIS compiler"
require_command "powershell" "PowerShell"
rm -rf "$BUNDLE_DIR"
mkdir -p "$BUNDLE_DIR"

if [ "${CI:-}" = "true" ] || [ ! -d "$FRONTEND_DIR/node_modules" ]; then
  echo "Installing frontend dependencies..."
  cd "$FRONTEND_DIR"
  npm ci
else
  echo "Using existing frontend dependencies..."
fi

echo "Testing frontend..."
cd "$FRONTEND_DIR"
npm test

echo "Linting frontend..."
npm run lint

echo "Building frontend assets..."
npm run build

cd "$PROJECT_DIR"

echo "Running Go verification..."
go vet ./...
go test ./...

echo "Building SpeechKit.exe..."
go build -ldflags "$GO_LDFLAGS" -o "$BUNDLE_EXE" ./cmd/speechkit/

echo "Writing runtime config..."
cp "$PROJECT_DIR/config.example.toml" "$BUNDLE_DIR/config.toml"

echo "Bundling local whisper runtime..."
powershell -ExecutionPolicy Bypass -File "$PREPARE_WHISPER_RUNTIME_SCRIPT" -BundleDir "$BUNDLE_DIR" -CacheDir "$PROJECT_DIR/.cache"

echo "Building SpeechKit-Setup.exe..."
makensis "$INSTALLER_SCRIPT"

echo ""
echo "Artifacts complete:"
echo "  $BUNDLE_EXE"
echo "  $INSTALLER_EXE"
