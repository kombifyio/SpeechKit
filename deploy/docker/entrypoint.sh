#!/bin/sh
# SpeechKit server entrypoint.
#
# The server serves no model or other file bytes; wake-word models are
# downloaded by clients from the Cloudflare R2 origin the catalog names
# (D-2026-10-25-03).
set -eu

exec /app/speechkit-server "$@"
