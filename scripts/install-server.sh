#!/usr/bin/env sh
set -eu

INSTALL_DIR="/opt/speechkit"
PUBLIC_URL="${SPEECHKIT_PUBLIC_URL:-http://localhost:8080}"
PUBLIC_HOST="${SPEECHKIT_PUBLIC_HOST:-}"
SERVER_IMAGE="${SPEECHKIT_SERVER_IMAGE:-ghcr.io/kombifyio/speechkit-server:latest}"
ONBOARDING_UI="true"
START_STACK="true"
STRICT_LOCAL_ONLY="false"

# Every env var that would let the server reach a hosted provider. Mirrors
# config.CloudCredentialEnvNames (the default names); a Go test runs this
# script with each of those names set and expects --strict-local-only to
# refuse. The install-E2E workflows clear exactly these: the Linux gate via
# --print-cloud-credential-env, the Windows gate by reading this line, so keep
# it a single double-quoted, space-separated line.
CLOUD_CREDENTIAL_ENV="GOOGLE_AI_API_KEY SPEECHKIT_GOOGLE_STT_API_KEY GOOGLE_CLOUD_STT_API_KEY GOOGLE_STT_API_KEY SPEECHKIT_GOOGLE_STT_CREDENTIALS_JSON GOOGLE_APPLICATION_CREDENTIALS OPENAI_API_KEY GROQ_API_KEY DEEPGRAM_API_KEY ASSEMBLYAI_API_KEY HF_TOKEN OPENROUTER_API_KEY CLOUDFLARE_AI_GATEWAY_AUTH_TOKEN CLOUDFLARE_API_TOKEN AZURE_AI_API_KEY XAI_API_KEY"

usage() {
  cat <<'EOF'
Usage: scripts/install-server.sh [options]

Options:
  --image IMAGE       SpeechKit server image. Default: ghcr.io/kombifyio/speechkit-server:latest
  --dir PATH          Install directory. Default: /opt/speechkit
  --public-url URL    Public URL. Default: http://localhost:8080
  --public-host HOST  Traefik host rule. Derived from --public-url by default.
  --onboarding        Enable /setup onboarding UI and settings writes. Default.
  --ready             Disable onboarding UI for ready-to-run container deploys.
  --no-ui             Alias for --ready.
  --no-up             Write compose/.env only; do not start containers.
  --strict-local-only Refuse to run when any cloud-provider env key is set.
                      Used by install-e2e-linux.yml to enforce the local-
                      only guarantee at install time.
  --print-cloud-credential-env
                      Print the cloud-provider env names, one per line, and
                      exit. The install-E2E gates clear exactly these.
  -h, --help          Show this help.

Setup modes:
  --onboarding  Starts the central server with the setup UI enabled.
  --ready       Starts the same central server immediately with self-hosted
                defaults and env-based secrets.

Defaults:
  Server: ghcr.io/kombifyio/speechkit-server:latest
  STT:    Phonon-2.5, bundled in the server image, on a processor with AVX2
          (x86-64-v3) or ARM dot product; otherwise the whisper.cpp sidecar
          (profile whisper). SPEECHKIT_INSTALL_STT=phonon|whisper overrides
          the detection.
  LLM:    Gemma 4 E4B IT Q4_K_M via llama.cpp
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --dir)
      INSTALL_DIR="${2:?missing value for --dir}"
      shift 2
      ;;
    --public-url)
      PUBLIC_URL="${2:?missing value for --public-url}"
      shift 2
      ;;
    --public-host)
      PUBLIC_HOST="${2:?missing value for --public-host}"
      shift 2
      ;;
    --image)
      SERVER_IMAGE="${2:?missing value for --image}"
      shift 2
      ;;
    --onboarding)
      ONBOARDING_UI="true"
      shift
      ;;
    --ready|--no-ui)
      ONBOARDING_UI="false"
      shift
      ;;
    --no-up)
      START_STACK="false"
      shift
      ;;
    --strict-local-only)
      STRICT_LOCAL_ONLY="true"
      shift
      ;;
    --print-cloud-credential-env)
      for key in $CLOUD_CREDENTIAL_ENV; do
        echo "$key"
      done
      exit 0
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

derive_host() {
  value=$1
  value=${value#http://}
  value=${value#https://}
  value=${value%%/*}
  value=${value%%:*}
  printf '%s' "$value"
}

if [ -z "$PUBLIC_HOST" ]; then
  PUBLIC_HOST=$(derive_host "$PUBLIC_URL")
fi
if [ -z "$PUBLIC_HOST" ]; then
  echo "Could not derive public host from --public-url. Pass --public-host." >&2
  exit 2
fi

if [ "$STRICT_LOCAL_ONLY" = "true" ]; then
  local_only_failed=0
  for key in $CLOUD_CREDENTIAL_ENV; do
    val=$(printenv "$key" 2>/dev/null || true)
    if [ -n "$val" ]; then
      echo "strict-local-only: $key is set in env; install-server.sh refuses to write a non-local config" >&2
      local_only_failed=1
    fi
  done
  if [ "$local_only_failed" = "1" ]; then
    exit 2
  fi
  echo "strict-local-only: no cloud-provider keys detected — proceeding with local-only defaults"
fi

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
COMPOSE_SRC="$REPO_DIR/deploy/docker/docker-compose.yml"
COMPOSE_DST="$INSTALL_DIR/docker-compose.yml"
ENV_DST="$INSTALL_DIR/.env"

if [ ! -f "$COMPOSE_SRC" ]; then
  echo "Compose source not found: $COMPOSE_SRC" >&2
  exit 2
fi

mkdir -p "$INSTALL_DIR"
cp "$COMPOSE_SRC" "$COMPOSE_DST"

ensure_env_file() {
  if [ ! -f "$ENV_DST" ]; then
    umask 077
    : >"$ENV_DST"
  fi
  chmod 600 "$ENV_DST"
}

upsert_env() {
  key=$1
  value=$2
  tmp="$ENV_DST.tmp"
  awk -v key="$key" -v value="$value" '
    BEGIN { seen = 0 }
    index($0, key "=") == 1 {
      print key "=" value
      seen = 1
      next
    }
    { print }
    END {
      if (!seen) print key "=" value
    }
  ' "$ENV_DST" >"$tmp"
  chmod 600 "$tmp"
  mv "$tmp" "$ENV_DST"
}

# Phonon-2.5, the speech-to-text model baked into the server image, needs an
# x86-64-v3 processor (AVX2, FMA, BMI1, BMI2, F16C, MOVBE, LZCNT) or ARM with
# the dot-product extension; the same rule as phonon.CPUSupported in the server.
# SPEECHKIT_INSTALL_STT=phonon or =whisper overrides the detection (an install
# prepared on one host for another).
bundled_stt_supported() {
  case "${SPEECHKIT_INSTALL_STT:-auto}" in
    phonon) return 0 ;;
    whisper) return 1 ;;
  esac
  flags=$(grep -m1 -E '^(flags|Features)[[:space:]]*:' /proc/cpuinfo 2>/dev/null || true)
  case "$(uname -m)" in
    x86_64|amd64)
      for flag in avx2 fma bmi1 bmi2 f16c movbe abm; do
        case " $flags " in
          *" $flag "*) ;;
          *) return 1 ;;
        esac
      done
      return 0
      ;;
    aarch64|arm64)
      case " $flags " in
        *" asimddp "*) return 0 ;;
      esac
      ;;
  esac
  return 1
}

ensure_blank_env() {
  key=$1
  if ! grep -q "^$key=" "$ENV_DST"; then
    printf '%s=\n' "$key" >>"$ENV_DST"
  fi
}

ensure_env_file
upsert_env "SPEECHKIT_SERVER_IMAGE" "$SERVER_IMAGE"
upsert_env "SPEECHKIT_PUBLIC_URL" "$PUBLIC_URL"
upsert_env "SPEECHKIT_PUBLIC_HOST" "$PUBLIC_HOST"
upsert_env "SPEECHKIT_SELFHOSTED_DEFAULTS" "true"
# Activate the llama sidecar declared in docker-compose.yml so the
# self-hosted LLM default has something at the other end of
# SPEECHKIT_SELFHOSTED_LLM_BASE_URL. Speech-to-text needs no sidecar when the
# processor runs the Phonon-2.5 model baked into the server image; otherwise
# the whisper.cpp sidecar (profile whisper) serves it.
if bundled_stt_supported; then
  LOCAL_STT="phonon"
  upsert_env "COMPOSE_PROFILES" "local"
  ensure_blank_env "SPEECHKIT_SELFHOSTED_STT_URL"
else
  LOCAL_STT="whisper"
  upsert_env "COMPOSE_PROFILES" "local,whisper"
  upsert_env "SPEECHKIT_SELFHOSTED_STT_URL" "http://speechkit-whisper:8080"
fi
upsert_env "SPEECHKIT_SELFHOSTED_WHISPER_MODEL" "large-v3-turbo"
upsert_env "SPEECHKIT_SELFHOSTED_STT_MODEL" "whisper-1"
upsert_env "SPEECHKIT_SELFHOSTED_LLM_REPO" "bartowski/google_gemma-4-E4B-it-GGUF:Q4_K_M"
upsert_env "SPEECHKIT_SELFHOSTED_LLM_MODEL" "bartowski/google_gemma-4-E4B-it-GGUF:Q4_K_M"
upsert_env "SPEECHKIT_SERVER_ONBOARDING_UI" "$ONBOARDING_UI"
upsert_env "SPEECHKIT_SERVER_SETTINGS_WRITE" "$ONBOARDING_UI"

ensure_blank_env "SPEECHKIT_SERVER_TOKEN"
ensure_blank_env "EDGE_AUTH_SECRET"
for key in $CLOUD_CREDENTIAL_ENV; do
  ensure_blank_env "$key"
done

echo "Wrote $COMPOSE_DST"
echo "Wrote $ENV_DST"
echo "Public URL: $PUBLIC_URL"
echo "Local speech-to-text: $LOCAL_STT"
echo "Setup mode: $(if [ "$ONBOARDING_UI" = "true" ]; then echo onboarding; else echo ready; fi)"

if [ "$START_STACK" = "true" ]; then
  cd "$INSTALL_DIR"
  docker compose pull
  docker compose up -d
  echo "SpeechKit server stack is starting at $PUBLIC_URL"
  if [ "$LOCAL_STT" = "phonon" ]; then
    echo "First boot downloads Gemma 4 for the LLM; speech-to-text uses the bundled Phonon-2.5. Readiness can take several minutes."
  else
    echo "This processor cannot run the bundled Phonon-2.5 (no AVX2); speech-to-text uses the whisper.cpp sidecar. First boot downloads Whisper Large v3 Turbo and Gemma 4; readiness can take several minutes."
  fi
else
  echo "Skipped docker compose up. Run: cd $INSTALL_DIR && docker compose up -d"
fi
