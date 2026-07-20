#!/bin/sh
# SpeechKit server entrypoint.
#
# Before starting the server it syncs the wake-word model bytes from Cloudflare
# R2 into the models volume, so the server can serve them at
# /v1/wakeword/files/<name>. This is gated on R2 credentials being present: a
# self-hosted OSS server without them simply skips the sync, and its clients use
# the kombify-hosted origin the catalog URLs already name. A failed sync is
# non-fatal — the /files route just 404s until a later start succeeds.
set -eu

WW_DIR="${SPEECHKIT_MODEL_DIR:-/var/lib/speechkit/models}/wakeword"
R2_WAKEWORD_BUCKET="${R2_WAKEWORD_BUCKET:-${R2_BUCKET_NAME:-kombify}}"
R2_WAKEWORD_PREFIX="${R2_WAKEWORD_PREFIX:-wakeword}"

if [ -n "${R2_ACCESS_KEY_ID:-}" ] && [ -n "${R2_SECRET_ACCESS_KEY:-}" ] && [ -n "${R2_ENDPOINT:-}" ]; then
  echo "[entrypoint] syncing wake-word models: r2:${R2_WAKEWORD_BUCKET}/${R2_WAKEWORD_PREFIX}/ -> ${WW_DIR}"
  mkdir -p "$WW_DIR"
  RCLONE_CONFIG_R2_TYPE=s3 \
  RCLONE_CONFIG_R2_PROVIDER=Cloudflare \
  RCLONE_CONFIG_R2_ACCESS_KEY_ID="$R2_ACCESS_KEY_ID" \
  RCLONE_CONFIG_R2_SECRET_ACCESS_KEY="$R2_SECRET_ACCESS_KEY" \
  RCLONE_CONFIG_R2_ENDPOINT="$R2_ENDPOINT" \
    rclone copy "r2:${R2_WAKEWORD_BUCKET}/${R2_WAKEWORD_PREFIX}/" "$WW_DIR" \
      --transfers 8 --s3-no-check-bucket --no-traverse \
    && echo "[entrypoint] wake-word model sync complete ($(ls -1 "$WW_DIR" 2>/dev/null | wc -l) files)" \
    || echo "[entrypoint] WARN: wake-word model sync failed; /v1/wakeword/files will 404 until a later start succeeds"
else
  echo "[entrypoint] R2 credentials not set; skipping wake-word model sync (clients use the catalog origin)"
fi

exec /app/speechkit-server "$@"
