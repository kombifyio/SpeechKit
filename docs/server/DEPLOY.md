# SpeechKit Server — Deploy Guide

This document covers deployment paths for the central SpeechKit Server:

1. **Local Docker Compose** (dev + e2e)
2. **Installer setup modes** (onboarding or ready container deploy)
3. **kombify dev deployment** (Coolify Cloud on `kombify-ionos-dev`)
4. **Render Blueprint** (managed cloud, Frankfurt region)
5. **Generic Kubernetes / any OCI runtime** (pointers only; no manifests shipped yet)

Secrets are injected as environment variables by the operator's secret manager.
Doppler, GitHub Actions secrets, Render env vars, Kubernetes Secrets, or a
plain local `.env` file all work because the server only reads values from
`os.Getenv(<name from config>)`.

## Image contract

The SpeechKit Server is distributed as one OCI image:
`ghcr.io/kombifyio/speechkit-server`. Tags follow the repository's semver tags,
for example `ghcr.io/kombifyio/speechkit-server:v0.28.3`. The image:

- listens on port `8080` over plain HTTP (add TLS at the edge)
- exposes `/healthz` and `/readyz`
- reads TOML config from `$SPEECHKIT_CONFIG_PATH` (default `/etc/speechkit/config.toml`)
- reads every secret from environment variables
- writes models + optional SQLite fallback to `/var/lib/speechkit/models`
- exposes Dictation, Assist, and Voice Agent from the same server process

Minimum provider secrets for useful mode coverage:

| Env var | Purpose |
|---|---|
| `SPEECHKIT_SERVER_TOKEN` | required by the shipped Compose stack; bearer token when `[server].auth_mode` is `bearer` or `bearer_or_edge` |
| `POSTGRES_PASSWORD` | required by the shipped Compose stack for the bundled Postgres service |
| `GOOGLE_AI_API_KEY` | Gemini Live Voice Agent + Google STT/TTS |
| `OPENAI_API_KEY` | OpenAI Whisper / GPT / TTS fallback |
| `HF_TOKEN` | HuggingFace STT fallback |
| `EDGE_AUTH_SECRET` | optional Cloudflare-edge-HMAC auth mode |

## 1. Local Docker Compose

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

### End-to-end smoke test

```bash
docker compose -f deploy/docker/docker-compose.test.yml up \
  --exit-code-from test-client --abort-on-container-exit
```

The test-client container curls `/healthz`, `/readyz`, and a minimal
`/api/v1/assist/process` request, and asserts the response contract. CI wires
this into the `server-linux.yml` workflow.

## 2. Installer setup modes

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
curl -fsSL https://speechkit.cc/install-server.sh | sh -s -- --channel preview
```

`--channel preview` is for v0.30 Preview testing only. It uses
`ghcr.io/kombifyio/speechkit-server:v0.30-preview` and never publishes or
updates the release `latest` tag.

## 3. kombify dev deployment

The canonical internal dev server is:

```text
https://speechkit.kombify.dev
```

It runs as a Coolify Cloud Docker Compose service on `kombify-ionos-dev`.
The deploy artifacts live under:

```text
deploy/coolify/kombify-ionos-dev/
```

The important routing rule is auth separation:

- `/healthz`, `/readyz`, and mode APIs under `/v1/dictation`,
  `/v1/assist`, `/v1/voiceagent` and their `/api/v1/*` aliases are routed
  directly to SpeechKit and use SpeechKit Bearer/edge-HMAC auth.
- Browser surfaces are routed through TinyAuth/Pocket ID.

TinyAuth must not wrap the whole host as a single catch-all for API traffic.
Desktop clients need JSON API responses and send `Authorization: Bearer
<SPEECHKIT_SERVER_TOKEN>`.

To update the Coolify service from a local shell with API credentials:

```powershell
$env:COOLIFY_API_BASE = "https://app.coolify.io/api/v1"
$env:COOLIFY_API_TOKEN = "<token>"
$env:COOLIFY_SERVICE_UUID = "<speechkit service uuid>"
powershell -ExecutionPolicy Bypass -File scripts/deploy-coolify-dev.ps1
```

`/readyz` may report degraded when optional providers are intentionally missing;
`/healthz` is the deployment gate.

## 4. Render Blueprint

```bash
render blueprint create deploy/render.yaml

for key in GOOGLE_AI_API_KEY OPENAI_API_KEY HF_TOKEN; do
  value="$(your-secret-manager read "$key")"
  render env set --service-name speechkit-server "$key" "$value"
done

render deploys create --service-name speechkit-server
```

The Blueprint provisions a web service, Postgres, and a persistent disk mounted
at `/var/lib/speechkit/models`. It does not set secret values; operators inject
them after provisioning.

## 5. Generic Kubernetes / OCI

No manifests ship yet. The image contract above is enough for any orchestrator:
one container, port 8080, persistent volume for `/var/lib/speechkit/models`, all
configuration via env vars plus a `config.toml` mount.

Minimum deployment expectations:

- `readinessProbe`: HTTP GET `/readyz`, failure threshold 3
- `livenessProbe`: HTTP GET `/healthz`, failure threshold 3
- `resources`: 500m-2 CPU, 512 MiB-2 GiB RAM depending on provider mix
- TLS termination at ingress / LB; the server itself only speaks HTTP

## Operational notes

- **Graceful shutdown**: SIGTERM triggers a 20 s HTTP drain; WebSocket Voice
  Agent sessions receive `session_end` frames.
- **Voice Agent ticket TTL**: 30 s by default. Tune `[server].ticket_ttl_sec` if
  your load balancer adds latency.
- **Public URL**: set `[server].public_url` when the service is behind a reverse
  proxy or mounted under `/api`; generated Voice Agent `ws_url` values use this
  trusted base instead of forwarded host headers.
- **WebSocket Origins**: browser Voice Agent clients must match
  `[server].cors_allowed_origins`; native clients may omit `Origin`.
- **Edge identity signing**: edge-HMAC auth signs generic edge identity claims:
  `user_id`, `org_id`, `plan`, and `role`. Persona/admin operations require the
  signed role, not an unsigned `X-Edge-Role` value.
- **Persona CRUD is admin-only**: set `Role=admin` on the edge-auth-signed
  identity for the operator account.
- **Provider swap is zero-downtime**: change provider config and restart;
  existing WebSocket sessions end cleanly, new sessions use the new provider.
