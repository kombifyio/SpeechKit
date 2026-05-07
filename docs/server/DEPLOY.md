# SpeechKit Server Deploy Guide

This document covers the public deployment paths for the central SpeechKit
Server:

1. Local Docker Compose
2. Installer setup modes
3. Generic Kubernetes or any OCI runtime

Secrets are injected as environment variables by the operator's secret manager
or a local `.env` file. SpeechKit only reads values through
`os.Getenv(<name from config>)`.

## Image Contract

The SpeechKit Server is distributed as one OCI image:
`ghcr.io/kombifyio/speechkit-server`. Tags follow the repository's semver tags,
for example `ghcr.io/kombifyio/speechkit-server:v0.30.0`. The image:

- listens on port `8080` over plain HTTP
- expects TLS at the edge or reverse proxy
- exposes `/healthz` and `/readyz`
- reads TOML config from `$SPEECHKIT_CONFIG_PATH`
- reads every secret from environment variables
- writes models plus optional SQLite fallback data to `/var/lib/speechkit`
- exposes Dictation, Assist, and Voice Agent from the same process

Minimum provider secrets for useful mode coverage:

| Env var | Purpose |
|---|---|
| `SPEECHKIT_SERVER_TOKEN` | bearer token when `[server].auth_mode` is `bearer` or `bearer_or_edge` |
| `POSTGRES_PASSWORD` | required by the shipped Compose stack for bundled Postgres |
| `GOOGLE_AI_API_KEY` | Gemini Live Voice Agent and Google STT/TTS |
| `OPENAI_API_KEY` | OpenAI Whisper, GPT, and TTS fallback |
| `HF_TOKEN` | Hugging Face STT fallback |
| `EDGE_AUTH_SECRET` | optional edge-HMAC auth mode |

## Local Docker Compose

```bash
export SPEECHKIT_SERVER_TOKEN="replace-with-a-local-dev-token"
export POSTGRES_PASSWORD="replace-with-a-local-dev-password"
export GOOGLE_AI_API_KEY="..."  # optional
export OPENAI_API_KEY="..."     # optional
docker compose -f deploy/docker/docker-compose.yml up -d

curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/readyz
```

The two placeholder values above are local-development examples only. Do not
reuse them for public, shared, or production deployments.

### End-To-End Smoke Test

```bash
docker compose -f deploy/docker/docker-compose.test.yml up \
  --exit-code-from test-client --abort-on-container-exit
```

The test-client container curls `/healthz`, `/readyz`, and a minimal
`/api/v1/assist/process` request, then asserts the response contract.

## Installer Setup Modes

For host-based Docker Compose installs, use the installer script:

```bash
# Operator-driven setup through /setup.
scripts/install-server.sh \
  --onboarding \
  --public-url https://speechkit.example.com

# Ready-to-run server deployment with self-hosted defaults.
scripts/install-server.sh \
  --ready \
  --public-url https://speechkit.example.com
```

Both commands write the same central server stack. `--onboarding` keeps `/setup`
enabled and allows settings writes. `--ready` disables onboarding UI/settings
writes for preconfigured deployments with self-hosted defaults and env-based
secrets.

For agent-driven server setup without cloning the repository, use the public
website installer:

```bash
curl -fsSL https://speechkit.cc/install-server.sh | sh
```

The installer pulls `ghcr.io/kombifyio/speechkit-server:latest`, which tracks
the most recent stable release.

## Generic Kubernetes Or OCI

No Kubernetes manifests ship yet. The image contract above is enough for any
orchestrator: one container, port 8080, persistent volume for
`/var/lib/speechkit`, all configuration via env vars plus a mounted
`config.toml`.

Minimum deployment expectations:

- `readinessProbe`: HTTP GET `/readyz`, failure threshold 3
- `livenessProbe`: HTTP GET `/healthz`, failure threshold 3
- `resources`: 500m-2 CPU, 512 MiB-2 GiB RAM depending on provider mix
- TLS termination at ingress or load balancer; the server itself only speaks HTTP

## Operational Notes

- **Graceful shutdown**: SIGTERM triggers a 20 s HTTP drain; WebSocket Voice
  Agent sessions receive `session_end` frames.
- **Voice Agent ticket TTL**: 30 s by default. Tune
  `[server].ticket_ttl_sec` if your load balancer adds latency.
- **Public URL**: set `[server].public_url` when the service is behind a
  reverse proxy or mounted under `/api`; generated Voice Agent `ws_url` values
  use this trusted base instead of forwarded host headers.
- **WebSocket Origins**: browser Voice Agent clients must match
  `[server].cors_allowed_origins`; native clients may omit `Origin`.
- **Edge identity signing**: edge-HMAC auth signs `user_id`, `org_id`, `plan`,
  and `role`. Persona/admin operations require the signed role.
- **Persona CRUD is admin-only**: set `Role=admin` on the edge-auth-signed
  identity for the operator account.
- **Provider swap is restart-based**: change provider config and restart;
  existing WebSocket sessions end cleanly, new sessions use the new provider.
