# SpeechKit Server — Deploy Guide

This document covers deployment paths for the central SpeechKit Server:

1. **Local Docker Compose** (dev + e2e)
2. **Installer setup modes** (onboarding or ready container deploy)
3. **Managed dev/staging deployment** (reference workflow shape)
4. **Render Blueprint** (managed cloud, Frankfurt region)
5. **Generic Kubernetes / any OCI runtime** (pointers only; no manifests shipped yet)

Secrets are injected as environment variables by the operator's secret manager.
Doppler, GitHub Actions secrets, Render env vars, Kubernetes Secrets, or a
plain local `.env` file all work because the server only reads values from
`os.Getenv(<name from config>)`.

## Image contract

The SpeechKit Server is distributed as one OCI image:
`ghcr.io/kombifyio/speechkit-server`. Tags follow the repository's semver tags,
for example `ghcr.io/kombifyio/speechkit-server:v0.28.0`. The image:

- listens on port `8080` over plain HTTP (add TLS at the edge)
- exposes `/healthz` and `/readyz`
- reads TOML config from `$SPEECHKIT_CONFIG_PATH` (default `/etc/speechkit/config.toml`)
- reads every secret from environment variables
- writes models + optional SQLite fallback to `/var/lib/speechkit/models`
- exposes Dictation, Assist, and Voice Agent from the same server process

Minimum provider secrets for useful mode coverage:

| Env var | Purpose |
|---|---|
| `SPEECHKIT_SERVER_TOKEN` | optional bearer token when `[server].auth_mode` is `bearer` or `bearer_or_edge` |
| `GOOGLE_AI_API_KEY` | Gemini Live Voice Agent + Google STT/TTS |
| `OPENAI_API_KEY` | OpenAI Whisper / GPT / TTS fallback |
| `HF_TOKEN` | HuggingFace STT fallback |
| `EDGE_AUTH_SECRET` | optional Cloudflare-edge-HMAC auth mode |

## 1. Local Docker Compose

```bash
export GOOGLE_AI_API_KEY="..."  # optional
export OPENAI_API_KEY="..."     # optional
docker compose -f deploy/docker/docker-compose.yml up -d

curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/readyz
```

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

## 3. Managed dev/staging deployment

A managed dev or staging environment should use the same image contract as
production, with a shorter release cadence:

- Build a `git-{sha}` image from `deploy/docker/Dockerfile.server` target `speechkit-server`.
- Push the image to your private container registry.
- Render a runtime `config.toml` and compose or orchestrator payload.
- Inject provider secrets from your chosen secret manager.
- Deploy to the target host.
- Gate the rollout on `GET /healthz`.

The canonical kombify dev deployment is `https://speechkit.kombify.dev` and is
managed by `.github/workflows/auto-deploy-dev.yml`.

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
- **Persona CRUD is admin-only**: set `Role=admin` on the edge-auth-signed
  identity for the operator account.
- **Provider swap is zero-downtime**: change provider config and restart;
  existing WebSocket sessions end cleanly, new sessions use the new provider.
