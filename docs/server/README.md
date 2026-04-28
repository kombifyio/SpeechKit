# SpeechKit Server-Target

The Server-Target is the network deployment form of the SpeechKit Framework. It
exposes Dictation, Assist, and Voice Agent over HTTP/WebSocket while using the
same mode contracts as the Windows app and embeddable Go API.

The Windows app, the Go API, and the Server-Target are three ways to host the
same framework core:

| Target | Use it when | Surface |
|---|---|---|
| Device-Target | You want the Windows reference app | Desktop UI, overlay, settings, global hotkeys |
| Local-Target | You embed SpeechKit into a Go product | Go API, mode contracts, provider catalog |
| Server-Target | You expose SpeechKit to remote clients | Containerized HTTP/WebSocket service |

## Deployment profiles

The Server-Target publishes two deployment profiles:

| Binary / image | Modes exposed | Docker target | When to choose |
|---|---|---|---|
| `speechkit-server` (`ghcr.io/kombifyio/speechkit-server`) | Dictation REST + Assist REST + Voice Agent WS | `--target speechkit-server` | Single-pod deployments. One URL, all three modes. Default. |
| `speechkit-voice` (`ghcr.io/kombifyio/speechkit-voice`) | Voice Agent WS | `--target speechkit-voice` | Realtime voice traffic on its own service, with the same Server-Target contract. |

Both binaries:

- Are Linux-only (`//go:build linux`).
- Build from `deploy/docker/Dockerfile.server` via the `--target` flag.
- Read `/etc/speechkit/config.toml` (copy `deploy/config/server.example.toml`).
- Take secrets from environment variables whose names are referenced from
  config.
- Share the same Server-Target settings, auth model, health checks, and API
  conventions.

The desktop can route each mode to a server through `[server_connection]` and
per-mode `mode_source` settings.

## Quick start (local dev)

```bash
# 1. Build the full server (all three modes)
docker build -f deploy/docker/Dockerfile.server \
  --target speechkit-server -t speechkit-server:dev .

# Optional: build the voice profile too
docker build -f deploy/docker/Dockerfile.server \
  --target speechkit-voice  -t speechkit-voice:dev  .

# 2. Bring the dev stack up
export SPEECHKIT_SERVER_TOKEN="dev-bearer-token-change-me"
export GOOGLE_AI_API_KEY="..."   # optional, enables Voice Agent + Google STT/TTS
export OPENAI_API_KEY="..."      # optional
export HF_TOKEN="..."            # optional, enables HF STT
docker compose -f deploy/docker/docker-compose.yml up -d

# 3. Verify
curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/readyz

# 4. Optional: also bring up the voice profile on :8090 for deployment testing.
docker compose -f deploy/docker/docker-compose.voice.yml up -d
```

Secrets can equivalently be injected via `doppler run -- docker compose up`;
the Framework has no opinion on the secret manager, only on where the values
are read (`os.Getenv(<name from config>)`).

## API contract

The complete v1 HTTP and WebSocket surface is documented in
[`docs/server/openapi.v1.yaml`](./openapi.v1.yaml) (OpenAPI 3.1).
Render it with any OpenAPI viewer (Swagger UI, Stoplight, Redoc) or
generate a typed client with `openapi-generator`. The contract is
considered stable across v0.26.x patch releases — fields are additive
across minor bumps; renames or removals require a major version bump.

## Modes

| Mode         | Transport     | Entry path                              | Status |
|--------------|---------------|-----------------------------------------|--------|
| Dictation    | HTTP POST     | `/v1/dictation/transcribe`              | ✅ ships in v0.26 |
| Assist       | HTTP POST     | `/v1/assist/process`                    | ✅ ships in v0.26 |
| Voice Agent  | HTTP + WS     | `/v1/voiceagent/sessions` + `/ws`       | ✅ ships in v0.26 |
| Personas API | HTTP CRUD     | `/v1/personas`, `/v1/roles`, `/v1/sequences` | ✅ ships in v0.26 |
| Health       | HTTP GET      | `/healthz`, `/readyz`                   | ✅ |

The mode set the binary serves is decided at startup from `[server].modes` in
`config.toml` or the `--modes=` CLI flag. The published images set defaults for
their deployment profile:

- `speechkit-server`: defaults to all three modes.
- `speechkit-voice`: defaults to `voiceagent` only.

## Voice profile

The `speechkit-voice` profile is useful when realtime Voice Agent sessions get
their own infrastructure. Typical reasons:

- **Different scaling axes.** Dictation + Assist are stateless REST
  calls; horizontally scale-out is trivial and instances are
  interchangeable. Voice Agent holds long-lived WebSocket sessions
  with state (Persona/Role/Sequence, Gemini Live connection, idle
  watchdog); scaling it focuses on session placement and connection
  stability.
- **Different node sizing.** REST traffic is happy on lean nodes;
  Voice Agent benefits from more memory (concurrent sessions),
  optionally a GPU (the cascaded provider's local LLM), and tighter
  network latency to the upstream voice provider. Putting voice on
  its own pod lets you size each tier honestly.
- **Different blast radius.** Isolating realtime voice keeps
  Dictation and Assist on their own rollout path during voice-provider
  changes.
- **Different release cadence.** Voice Agent providers churn faster
  than STT/Assist (Gemini 3.1 preview, Moshi/Kyutai, the cascaded
  provider). Splitting lets you upgrade the voice tier without
  re-rolling the REST tier.

The Device-Target can route modes independently through `mode_source`
(`local` | `server`) and server connection settings, so Dictation, Assist, and
Voice Agent can use the deployment profile that fits the workload.

## Authentication

Three auth modes are supported via `[server].auth_mode`:

- `bearer` (default) — single static token from `$SPEECHKIT_SERVER_TOKEN`.
  Appropriate for internal service-to-service calls on a trusted network.
- `edge_hmac` — trusts HMAC-signed headers from an authenticated edge (e.g. a
  Cloudflare Worker that has already validated a user JWT). Expected headers:
  `X-Edge-Auth-Hmac`, `X-Edge-User-Id`, `X-Edge-Org-Id`, `X-Edge-Plan`,
  optionally `X-Edge-Role`.
- `bearer_or_edge` — accepts either, useful when one deployment serves both
  internal and browser-originated traffic.

`/healthz` and `/readyz` are always public so external probes work without
credentials.

## Relation to the Framework kernel

Every substantive operation delegates to the shared Framework packages:

| Server handler        | Delegates to                         |
|-----------------------|--------------------------------------|
| Dictation transcribe  | `internal/router` + `internal/stt/*` |
| Assist process        | `internal/assist.Pipeline`           |
| Voice Agent session   | `internal/voiceagent.Session` + `internal/voiceagent.GeminiLive` |
| Persona compose       | reads TOML seeds + store-backed overrides, composes `voiceagent.LiveConfig` |

Adding another deployment target follows the same pattern: a thin adapter around
the same Framework kernel.

## Directory layout

```
cmd/speechkit-server/          # Linux entry point (//go:build linux)
internal/server/core/          # Bootstrap, lifecycle, health
internal/server/middleware/    # Auth, logging, rate-limit, CORS, recover
internal/server/{mode-pkg}/    # Dictation / Assist / VoiceAgent / Persona handlers (M2+)
deploy/docker/                 # Dockerfile, docker-compose
deploy/config/                 # Reference config.toml
docs/server/                   # This document + API reference
```

## Current server surface

- CLI flags and TOML configuration.
- HTTP listener with graceful shutdown on SIGINT/SIGTERM.
- Liveness and readiness endpoints.
- Middleware chain for recover, logging, CORS, and auth.
- Dictation REST, Assist REST, Voice Agent WebSocket, personas, roles,
  sequences, and health endpoints.
- Reference Dockerfile and docker-compose files for local development.
- CI workflow that builds, vets, tests, and smoke-tests the server image.
