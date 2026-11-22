#!/usr/bin/env sh
set -eu

INSTALL_DIR="/opt/speechkit"
PUBLIC_URL="${SPEECHKIT_PUBLIC_URL:-http://localhost:8080}"
CHANNEL="stable"
SERVER_IMAGE="${SPEECHKIT_SERVER_IMAGE:-}"
START_STACK="true"
BIND_ADDR="${SPEECHKIT_BIND_ADDR:-127.0.0.1}"
PUBLIC_BIND="false"

usage() {
  cat <<'EOF'
Usage: install-server.sh [options]

Options:
  --channel stable|preview  Install stable latest or v0.30 Preview. Default: stable.
  --dir PATH                Install directory. Default: /opt/speechkit.
  --public-url URL          Public URL used by generated WebSocket URLs.
  --image IMAGE             Override the server image.
  --public-bind             Expose port 8080 on all interfaces. Use behind TLS.
  --no-up                   Write files only; do not start Docker Compose.
  -h, --help                Show help.

Examples:
  curl -fsSL https://speechkit.cc/install-server.sh | sh
  curl -fsSL https://speechkit.cc/install-server.sh | sh -s -- --channel preview
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --channel)
      CHANNEL="${2:?missing value for --channel}"
      shift 2
      ;;
    --dir)
      INSTALL_DIR="${2:?missing value for --dir}"
      shift 2
      ;;
    --public-url)
      PUBLIC_URL="${2:?missing value for --public-url}"
      shift 2
      ;;
    --image)
      SERVER_IMAGE="${2:?missing value for --image}"
      shift 2
      ;;
    --public-bind)
      BIND_ADDR="0.0.0.0"
      PUBLIC_BIND="true"
      shift
      ;;
    --no-up)
      START_STACK="false"
      shift
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

case "$CHANNEL" in
  stable)
    DEFAULT_IMAGE="ghcr.io/kombifyio/speechkit-server:latest"
    ;;
  preview)
    DEFAULT_IMAGE="ghcr.io/kombifyio/speechkit-server:v0.30-preview"
    ;;
  *)
    echo "--channel must be stable or preview" >&2
    exit 2
    ;;
esac

if [ -z "$SERVER_IMAGE" ]; then
  SERVER_IMAGE="$DEFAULT_IMAGE"
fi

generate_token() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
    return
  fi
  if [ -r /dev/urandom ] && command -v od >/dev/null 2>&1; then
    od -An -N32 -tx1 /dev/urandom | tr -d ' \n'
    return
  fi
  printf 'speechkit-%s-%s' "$(date +%s)" "$$"
}

mkdir -p "$INSTALL_DIR/data"

TOKEN="${SPEECHKIT_SERVER_TOKEN:-}"
if [ -z "$TOKEN" ]; then
  TOKEN="$(generate_token)"
fi

umask 077
cat >"$INSTALL_DIR/.env" <<EOF
SPEECHKIT_SERVER_IMAGE=$SERVER_IMAGE
SPEECHKIT_SERVER_TOKEN=$TOKEN
SPEECHKIT_BIND_ADDR=$BIND_ADDR
HF_TOKEN=${HF_TOKEN:-}
OPENAI_API_KEY=${OPENAI_API_KEY:-}
GROQ_API_KEY=${GROQ_API_KEY:-}
# GOOGLE_AI_API_KEY=   # opt-in BYOK Gemini Live / Google STT+TTS; also enable [providers.google] in config.toml
EDGE_AUTH_SECRET=${EDGE_AUTH_SECRET:-}
EOF

cat >"$INSTALL_DIR/config.toml" <<EOF
[general]
language = "en"

[server]
listen_addr = ":8080"
public_url = "$PUBLIC_URL"
modes = ["dictation", "assist", "voiceagent"]
auth_mode = "bearer"
bearer_token_env = "SPEECHKIT_SERVER_TOKEN"
bearer_role = "admin"
edge_auth_secret_env = "EDGE_AUTH_SECRET"
cors_allowed_origins = []
rate_limit_rps = 10.0
rate_limit_burst = 20
max_upload_mb = 25
max_voiceagent_sessions = 100
max_sessions_per_user = 3
ticket_ttl_sec = 90
voiceagent_idle_timeout_sec = 900
whisper_binary = "/usr/local/bin/whisper-server"
whisper_port = 8180
model_dir = "/var/lib/speechkit/models"
log_format = "json"
log_level = "info"

[server.features]
catalog = true
storage_reads = true
vocabulary = true
tts_direct = true

[store]
backend = "sqlite"
sqlite_path = "/var/lib/speechkit/models/speechkit.db"
save_audio = true
audio_retention_days = 7
max_audio_storage_mb = 500

[huggingface]
enabled = true
model = "openai/whisper-large-v3"
token_env = "HF_TOKEN"

[providers.openai]
enabled = true
api_key_env = "OPENAI_API_KEY"
stt_model = "gpt-transcribe"
utility_model = "gpt-6-luna"
assist_model = "gpt-6.1-sol"
agent_model = "gpt-6.1-sol"
tts_model = "gpt-4o-mini-tts"
tts_voice = "nova"

# Opt-in BYOK only: Google Cloud STT/TTS and Gemini Live with your own Google
# credentials. Never a default. Uncomment, set GOOGLE_AI_API_KEY in .env, and
# set [voice_agent] provider = "gemini" to use it.
# [providers.google]
# enabled = true
# api_key_env = "GOOGLE_AI_API_KEY"
# stt_model = "latest_long"

[tts]
enabled = true
strategy = "cloud-first"
format = "mp3"
speed = 1.0

[voice_agent]
enabled = true
provider = "openai"
model = "gpt-realtime-2.1"
fallback_model = ""
voice = "alloy"
automatic_activity_detection = true
enable_input_transcript = true
enable_output_transcript = true
EOF

cat >"$INSTALL_DIR/docker-compose.yml" <<'EOF'
services:
  speechkit-server:
    image: ${SPEECHKIT_SERVER_IMAGE}
    env_file:
      - .env
    volumes:
      - ./config.toml:/etc/speechkit/config.toml:ro
      - ./data:/var/lib/speechkit/models
    ports:
      - "${SPEECHKIT_BIND_ADDR:-127.0.0.1}:8080:8080"
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "curl", "-fsS", "http://localhost:8080/healthz"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 30s
EOF

chmod 600 "$INSTALL_DIR/.env"

echo "SpeechKit Server files written to $INSTALL_DIR"
echo "Channel: $CHANNEL"
echo "Image: $SERVER_IMAGE"
echo "Public URL: $PUBLIC_URL"
echo "Bind address: $BIND_ADDR"
echo "Bearer token stored in $INSTALL_DIR/.env"

if [ "$PUBLIC_BIND" = "true" ]; then
  echo "Warning: --public-bind exposes SpeechKit on all interfaces. Use a TLS reverse proxy and keep bearer auth enabled." >&2
fi

if [ "$START_STACK" = "true" ]; then
  cd "$INSTALL_DIR"
  docker compose pull
  docker compose up -d
  echo "SpeechKit Server is starting at $PUBLIC_URL"
else
  echo "Skipped startup. Run: cd $INSTALL_DIR && docker compose up -d"
fi
