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
| `SPEECHKIT_SERVER_AUTH_MODE` | optional startup override for `[server].auth_mode` (`none`, `bearer`, `edge_hmac`, `bearer_or_edge`) |
| `SPEECHKIT_SERVER_BEARER_TOKEN_ENV` | optional name of the env var that holds the bearer token; defaults to `SPEECHKIT_SERVER_TOKEN` |
| `SPEECHKIT_SERVER_EDGE_AUTH_SECRET_ENV` | optional name of the env var that holds the edge-HMAC secret; defaults to `EDGE_AUTH_SECRET` |
| `SPEECHKIT_SERVER_BEARER_ROLE` | optional role assigned to bearer-token callers, for example `admin` for single-operator deployments |
| `POSTGRES_PASSWORD` | required by the shipped Compose stack for bundled Postgres |
| `GOOGLE_AI_API_KEY` | Gemini Assist and Gemini Live Voice Agent |
| `SPEECHKIT_GOOGLE_STT_API_KEY` | optional Google STT key; only needed when Dictation selects Google STT |
| `OPENAI_API_KEY` | OpenAI Whisper, GPT, and TTS fallback |
| `HF_TOKEN` | Hugging Face STT fallback |
| `EDGE_AUTH_SECRET` | optional edge-HMAC auth mode |
| `LIVEKIT_URL` | optional Render-hosted LiveKit media target for Voice Agent sessions |
| `LIVEKIT_API_KEY` | optional LiveKit token minting key |
| `LIVEKIT_API_SECRET` | optional LiveKit token minting secret |

## Headless Deployment Contract

Container deployers must treat environment variables as the authoritative
credential source. SpeechKit applies this contract after reading `config.toml`
and persisted `server-settings.json`, so Compose, Kubernetes, Render, or
Coolify-injected secrets override stale local settings without writing secret
values back to disk.

Auth startup rules:

- `SPEECHKIT_SERVER_TOKEN` is the canonical bearer token env var. If it is set,
  it overrides a stored `bearer_token_env` that points at a different env var.
- `SPEECHKIT_SERVER_BEARER_TOKEN_ENV=MY_TOKEN` intentionally selects a custom
  bearer-token env var and takes precedence over `SPEECHKIT_SERVER_TOKEN`.
- `SPEECHKIT_SERVER_AUTH_MODE` can explicitly select `bearer`, `edge_hmac`, or
  `bearer_or_edge`. If omitted and credentials are present while `auth_mode` is
  empty or `none`, SpeechKit infers the matching authenticated mode.
- Authenticated modes fail startup when their required credential env values
  are empty. This is intentional; a public headless deployment must not boot
  into a broken or unauthenticated state.

Deployers can verify the loaded contract through the authenticated status API.
It accepts the service bearer credential, local admin auth, or an edge-HMAC
identity with role `admin`; normal edge user identities are rejected:

```bash
curl -fsS \
  -H "Authorization: Bearer $SPEECHKIT_SERVER_TOKEN" \
  http://localhost:8080/v1/deployment/status | jq '.auth, .providers'
```

The response reports env var names and `*_set` booleans only. It never returns
bearer tokens, provider API keys, or edge-HMAC secrets. The same endpoint is
available at `/api/v1/deployment/status` for clients that use the browser/API
prefix.

## Local Docker Compose

```bash
export SPEECHKIT_SERVER_TOKEN="replace-with-a-local-dev-token"
export POSTGRES_PASSWORD="replace-with-a-local-dev-password"
export GOOGLE_AI_API_KEY="..."  # optional
export HF_TOKEN="..."           # optional Hugging Face Dictation
export OPENAI_API_KEY="..."     # optional
docker compose -f deploy/docker/docker-compose.yml up -d

curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/readyz
curl -fsS http://localhost:8080/readyz/strict || true
curl -fsS -H "Authorization: Bearer $SPEECHKIT_SERVER_TOKEN" \
  http://localhost:8080/v1/deployment/status | jq '.auth'
```

The two placeholder values above are local-development examples only. Do not
reuse them for public, shared, or production deployments.

Use `/readyz` for orchestrator readiness. It only fails for blocking
dependencies of enabled modes. Use `/readyz/strict` for diagnostics when you
want optional provider probes to affect the HTTP status. A failing optional
Google STT probe must not remove a server from rotation when Dictation is using
Hugging Face.

Browser clients should call a trusted BFF or local proxy, not SpeechKit or
provider APIs with provider tokens directly from the browser. Proxy the HTTP
mode endpoints you need (`/v1/dictation/transcribe`, `/v1/assist/process`,
`/v1/voiceagent/sessions`) and bridge the Voice Agent WebSocket with
server-side credentials only.

## Voice Agent LiveKit Opt-In

`media_transport="websocket"` remains the default Voice Agent audio path.
LiveKit is an explicit opt-in media transport and still uses the WebSocket as
the control channel for `start`, transcripts, tool calls, errors, and workflow
steps.

Production clients use this flow:

1. Create a SpeechKit Voice Agent session with
   `POST /v1/voiceagent/sessions`.
2. Join the returned `livekit.url` and `livekit.room` using the returned
   short-lived `livekit.token`.
3. Open the Voice Agent WebSocket returned by the session.
4. Send `{"type":"start","media_transport":"livekit"}` over the WebSocket.
5. Publish microphone audio to LiveKit and subscribe to the agent output
   audio track. Keep using the WebSocket for transcripts and control frames.

The v1 bridge supports native realtime PCM providers only: Gemini and OpenAI.
Cascaded providers continue to reject LiveKit media until explicit transcoding
is added. Render-hosted LiveKit remains the target; no host migration is part
of this deployment path.

## Automated Render Production Rollout

Production deployments should use an image-backed Render web service. The
recommended rollout deploys immutable release images from
`ghcr.io/kombifyio/speechkit-server:<tag>` rather than arbitrary branch heads.

The release path is automated:

1. The private release workflow exports the release to `kombifyio/SpeechKit`
  and pushes the semver tag.
2. The public server image workflow publishes
  `ghcr.io/kombifyio/speechkit-server:<tag>` and `:latest`.
3. The private `Deploy Render Server` job waits for the public image workflow,
  updates the Render service image to that immutable tag, triggers the Render
  deploy API, and waits for the deploy to become `live`.
4. The job verifies `/healthz`, `/readyz`, and `/readyz/strict`. If
  `SPEECHKIT_SERVER_TOKEN` is available in Doppler, it is exported as
  `SPEECHKIT_SMOKE_TOKEN` and the job also runs `cmd/sk-e2e` across Dictation,
  Assist, and Voice Agent.

Required private-repo GitHub settings:

| Name | Type | Purpose |
|---|---|---|
| `DOPPLER_TOKEN` | secret | Read-only service token for `kombify-io/prd_speechkit`; used to load deploy/test credentials at runtime |
| `RENDER_SPEECHKIT_SERVICE_ID` | variable | Render web service id |
| `RENDER_GHCR_REGISTRY_CREDENTIAL_ID` | variable | Optional Render registry credential id for private GHCR images |
| `SPEECHKIT_PROD_ORIGIN` | variable | Public SpeechKit origin, for example `https://speechkit.example.com` |

The Doppler config `kombify-io/prd_speechkit` must contain `RENDER_API_KEY`
and `SPEECHKIT_SERVER_TOKEN`. Legacy GitHub secrets named `RENDER_API_KEY`,
`SPEECHKIT_SMOKE_TOKEN`, or `SPEECHKIT_SERVER_TOKEN` are accepted only as
fallback values while bootstrapping.

Manual redeploys use the same path:

```bash
gh workflow run deploy-render-server.yml \
  --repo <private-release-repo> \
  --ref main \
  -f tag=v0.31.1 \
  -f strict_ready=true \
  -f run_e2e=true
```

### End-To-End Smoke Test

```bash
docker compose -f deploy/docker/docker-compose.test.yml up \
  --exit-code-from test-client --abort-on-container-exit
```

The test-client container curls `/healthz`, `/readyz`, the authenticated
deployment status API, and the Assist self-test, then asserts the response
contracts and verifies that the bearer token is not leaked in status JSON.

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
public only until bootstrap completes and allows the first settings write.
After a token or completed onboarding state exists, `/setup` requires an admin
identity. `--ready` disables onboarding UI/settings writes for preconfigured
deployments with self-hosted defaults and env-based secrets.

For agent-driven server setup without cloning the repository, use the public
website installer:

```bash
curl -fsSL https://speechkit.cc/install-server.sh | sh
```

The installer pulls `ghcr.io/kombifyio/speechkit-server:latest`, which tracks
the most recent stable release.

After the container is healthy, agents should complete first-run setup through
the setup API and enable the standalone admin login:

```bash
ADMIN_PASSWORD="$(openssl rand -base64 32)"
curl -fsS -X PATCH http://localhost:8080/v1/server/settings \
  -H 'Content-Type: application/json' \
  -d "{
    \"onboarding_complete\": true,
    \"admin_auth\": {
      \"enabled\": true,
      \"username\": \"admin\",
      \"password\": \"${ADMIN_PASSWORD}\"
    },
    \"server_auth\": {
      \"mode\": \"managed_bearer\",
      \"bearer_token_env\": \"SPEECHKIT_SERVER_TOKEN\",
      \"generate_token\": true
    }
  }"
```

The generated server bearer token is returned once by the API and must be
stored in the deployment secret environment. The admin password is never stored
as plaintext; the server persists only a bcrypt hash. To rely on an external
identity-aware edge instead of the browser admin login, send
`"admin_auth": {"enabled": false}` during setup.

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
- **Voice Agent ticket TTL**: 90 s by default. Tickets are single-use and are
  not refreshed in v1; clients must create a new session after expiry. Tune
  `[server].ticket_ttl_sec` if your load balancer adds latency.
- **Public URL**: set `[server].public_url` when the service is behind a
  reverse proxy or mounted under `/api`; generated Voice Agent `ws_url` values
  use this trusted base instead of forwarded host headers. In container/browser
  deployments, prefer `SPEECHKIT_PUBLIC_URL`; `SPEECHKIT_SERVER_PUBLIC_URL` is
  accepted as an alias.
- **WebSocket Origins**: browser Voice Agent clients must match
  `[server].cors_allowed_origins`; native clients may omit `Origin`.
- **Edge identity signing**: edge-HMAC auth signs `user_id`, `org_id`, `plan`,
  and `role`. Persona/admin operations require the signed role.
- **Admin UI and writes are admin-only**: set `Role=admin` on the
  edge-auth-signed identity for the operator account. Single-operator bearer
  deployments can set `[server].bearer_role = "admin"` explicitly.
- **Provider swap is restart-based**: change provider config and restart;
  existing WebSocket sessions end cleanly, new sessions use the new provider.
