# SpeechKit Server-Target — Deploy Guide

This document covers the deployment paths for the Server-Target:

1. **Local Docker Compose** (dev + e2e)
2. **Managed dev/staging deployment** (reference workflow shape)
3. **Render Blueprint** (managed cloud, Frankfurt region)
4. **Generic Kubernetes / any OCI runtime** (pointers only; no manifests shipped yet)

Secrets are injected as environment variables by the operator's secret
manager. Doppler, GitHub Actions secrets, Render env vars, Kubernetes
Secrets, or a plain local `.env` file all work because the server only
reads values from `os.Getenv(<name from config>)`.

## Image contract

The Server-Target is distributed as OCI images built from the same Dockerfile.
Tags follow the repository's semver tags, for example
`ghcr.io/kombifyio/speechkit-server:v0.27.0`. Every image:

- listens on port `8080` over plain HTTP (add TLS at the edge)
- exposes `/healthz` (liveness) and `/readyz` (readiness + component map)
- reads its TOML config from `$SPEECHKIT_CONFIG_PATH` (default
  `/etc/speechkit/config.toml`)
- reads every secret from environment variables; variable *names* are
  configured in `config.toml`, *values* come from env
- writes models + an optional SQLite fallback to `/var/lib/speechkit/models`
  (mount a 10+ GB volume)

Minimum provider secret for a useful deployment:

| Env var | Purpose |
|---|---|
| `SPEECHKIT_SERVER_TOKEN` | bearer token for `/v1/*` (required) |
| `GOOGLE_AI_API_KEY` | Gemini Live Voice Agent + Google STT/TTS |
| `OPENAI_API_KEY` | OpenAI Whisper / GPT / TTS fallback |
| `HF_TOKEN` | HuggingFace STT fallback |
| `EDGE_AUTH_SECRET` | optional, enables the Cloudflare-edge-HMAC auth mode |

## 1. Local Docker Compose

Reference stack for day-to-day development:

```bash
export SPEECHKIT_SERVER_TOKEN="dev-bearer-token"
export GOOGLE_AI_API_KEY="..."  # optional
export OPENAI_API_KEY="..."     # optional
docker compose -f deploy/docker/docker-compose.yml up -d

curl -fsS http://localhost:8080/healthz   # → {"status":"ok"}
curl -fsS http://localhost:8080/readyz    # → component health per mode
```

### End-to-end smoke test

```bash
docker compose -f deploy/docker/docker-compose.test.yml up \
  --exit-code-from test-client --abort-on-container-exit
```

The test-client container curls `/healthz`, `/readyz`, and a minimal
`/v1/assist/process` request, and asserts the response contract — it
exits non-zero when anything is off. CI wires this into the
`server-linux.yml` workflow.

### whisper.cpp sidecar (optional, for cascaded provider)

The server expects whisper.cpp on `http://127.0.0.1:8180` when
`[voice_agent] provider = "cascaded"` is active. For local dev you can
add a sidecar:

```yaml
# deploy/docker/docker-compose.override.yml
services:
  whisper:
    image: ghcr.io/ggerganov/whisper.cpp:main-server
    command: ["-m", "/models/ggml-base.en.bin", "-p", "8180", "-t", "4"]
    volumes:
      - ./whisper-models:/models
    network_mode: "service:speechkit-server"   # share network so 127.0.0.1:8180 works
```

Production deployments run whisper.cpp in-process via the existing
Framework subprocess manager (`internal/stt/local.go`); the sidecar is
for Docker-only dev where spawning native processes is awkward.

## 2. Managed dev/staging deployment

A managed dev or staging environment should use the same image contract as
production, with a shorter release cadence:

- Build a `git-{sha}` image from `deploy/docker/Dockerfile.server`.
- Push the image to your private container registry.
- Render a runtime `config.toml` and compose or orchestrator payload.
- Inject provider secrets from your chosen secret manager.
- Deploy to the target host.
- Gate the rollout on `GET /healthz`.

Dev compose stacks commonly run the SpeechKit server behind a small edge
proxy. Use release tags for public/stable environments and `git-{sha}` images
only for private staging or dogfood systems.

Required runtime key:

| Env var | Purpose |
|---|---|
| `SPEECHKIT_SERVER_TOKEN` | bearer token for `/v1/*` |

Recommended provider keys:

| Env var | Purpose |
|---|---|
| `GOOGLE_AI_API_KEY` | Gemini Live Voice Agent + Google STT/TTS |
| `OPENAI_API_KEY` | OpenAI Whisper / GPT / TTS fallback |
| `GROQ_API_KEY` | Groq LLM/STT fallback |
| `HF_TOKEN` | HuggingFace STT fallback |

`/readyz` may report degraded when optional providers are intentionally
missing; `/healthz` is the deployment gate.

## 3. Render Blueprint

```bash
# One-time blueprint apply:
render blueprint create deploy/render.yaml

# Inject secrets from your secret manager (repeat for every key in deploy/render.yaml):
for key in SPEECHKIT_SERVER_TOKEN GOOGLE_AI_API_KEY OPENAI_API_KEY HF_TOKEN; do
  value="$(your-secret-manager read "$key")"
  render env set --service-name speechkit-server "$key" "$value"
done

# Trigger a deploy:
render deploys create --service-name speechkit-server
```

The Blueprint provisions:

- a Standard web service (2 CPU, 4 GB RAM) running the Dockerfile
- a Basic 1GB Postgres 17 database in Frankfurt
- a 10 GB persistent disk mounted at `/var/lib/speechkit/models`

Blueprint does NOT set secret values — `sync: false` forces operators to
inject them post-provision, keeping the chosen secret manager as the source
of truth.

### Deploy verification

```bash
url=$(render services show --name speechkit-server --format json | jq -r .serviceDetails.url)
curl -fsS "$url/healthz"
curl -fsS "$url/readyz"
```

### Rollback

```bash
render deploys rollback --service-name speechkit-server --to <previous-deploy-id>
```

Roll back only when `/readyz` reports persistent unhealth or the
downstream client (kombify-AI) reports a regression — transient
degraded states for missing provider keys are expected during initial
setup and don't warrant rollback.

## 4. Generic Kubernetes / OCI

No manifests ship yet; add one in `deploy/kubernetes/` when the first
K8s-hosted deployment lands. The image contract above is enough for any
orchestrator: one container, port 8080, persistent volume for
`/var/lib/speechkit/models`, all configuration via env vars +
`config.toml` mount.

Minimum deployment expectations:
- `readinessProbe`: HTTP GET `/readyz`, failure threshold 3
- `livenessProbe`: HTTP GET `/healthz`, failure threshold 3
- `resources`: 500m–2 CPU, 512 MiB–2 GiB RAM depending on provider mix
  (Gemini-only deployments need less than Cascaded-with-local-whisper)
- TLS termination at the ingress / LB; the server itself only speaks
  HTTP

## Operational notes

- **Graceful shutdown**: SIGTERM triggers a 20 s HTTP drain; WebSocket
  Voice Agent sessions receive `session_end` frames. Container
  runtimes should therefore use a grace period ≥ 25 s.
- **Voice Agent ticket TTL**: 30 s by default. A deploy that takes >30 s
  between POST `/v1/voiceagent/sessions` and the WebSocket upgrade will
  fail — tune `[server].ticket_ttl_sec` if your LB adds latency.
- **Persona CRUD is admin-only**: set `Role=admin` on the
  edge-auth-signed identity for the operator account. Without it,
  reads work but writes return 403.
- **Provider swap is zero-downtime**: change `[voice_agent] provider`
  and restart; existing WebSocket sessions end with `session_end` but
  new sessions use the new provider.

## Next deploy milestones

- **M9b**: a Moshi sidecar (GPU-required) for the `moshi` provider —
  `deploy/docker/docker-compose.gpu.yml` to be added.
- **M9c**: a performance-comparison harness (`cmd/speechkit-server/testdata/wsperf`)
  will live alongside the test-client in this directory.
