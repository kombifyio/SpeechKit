# SpeechKit Server

The SpeechKit Server is the network deployment form of the SpeechKit Framework.
It exposes Dictation, Assist, and Voice Agent over HTTP/WebSocket while using
the same mode contracts as the Windows app and embeddable Go API.

SpeechKit ships as three modules:

| Module | Use it when | Surface |
|---|---|---|
| Local-first Go backend | You embed SpeechKit into a Go product or internal tool | Go API, mode contracts, provider catalog |
| SpeechKit Server | You expose SpeechKit to remote clients | Linux HTTP/WebSocket service |
| Windows Client | You want the desktop app, local tests, or a server-connected workstation | Desktop UI, overlay, settings, global hotkeys |

## Deployment contract

There is one published server image:

| Image | Modes exposed | Build stage | Public contract |
|---|---|---|---|
| `ghcr.io/kombifyio/speechkit-server` | Dictation REST + Assist REST + Voice Agent WS | `--target speechkit-server` | Central Framework server under one URL |

The server is intentionally a thin adapter around the reusable Framework
kernel. The Windows Client remains a UI/client; the Go packages remain the
reusable implementation surface; the server only exposes that surface over
HTTP and WebSocket.

The server:

- Is Linux-only (`//go:build linux`).
- Builds from `deploy/docker/Dockerfile.server`.
- Reads `/etc/speechkit/config.toml` (copy `deploy/config/server.example.toml`).
- Takes secrets from environment variables whose names are referenced from config.
- Exposes the same auth, health, readiness, and API conventions for all modes.

## Quick start

```bash
# 1. Build the central server.
docker build -f deploy/docker/Dockerfile.server \
  --target speechkit-server -t speechkit-server:dev .

# 2. Bring the dev stack up.
export GOOGLE_AI_API_KEY="..."   # optional, enables Gemini Assist + Gemini Live
export HF_TOKEN="..."            # optional, enables Hugging Face STT
export OPENAI_API_KEY="..."      # optional, enables OpenAI STT/LLM/TTS
# Optional only when Google STT is selected for Dictation:
export SPEECHKIT_GOOGLE_STT_API_KEY="..."
export SPEECHKIT_SERVER_TOKEN="replace-with-a-local-dev-token"
export POSTGRES_PASSWORD="replace-with-a-local-dev-password"
docker compose -f deploy/docker/docker-compose.yml up -d

# 3. Verify.
curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/readyz
curl -fsS -H "Authorization: Bearer $SPEECHKIT_SERVER_TOKEN" \
  http://localhost:8080/v1/deployment/status | jq '.auth, .providers'
curl -fsS -H "Authorization: Bearer $SPEECHKIT_SERVER_TOKEN" \
  -X POST http://localhost:8080/v1/assist/self-test
```

The repository dev stack includes Postgres because it exercises the production
store path. A fresh browser-facing local server can use SQLite only; use the
agent/browser reference files at `docs/agent/install-server/docker-compose.example.yml`
and `docs/agent/install-server/config.browser.example.toml`.

Secrets can be injected by any operator-managed secret system. The Framework
has no opinion on the secret manager, only on where values are read
(`os.Getenv(<name from config>)`). The example values above are local
development placeholders only; production Compose runs must provide real
secrets explicitly.

For the common web integration profile — Dictation through Hugging Face,
Assist through Gemini, Voice Agent through Gemini Live, and TTS disabled — set
`HF_TOKEN`, `GOOGLE_AI_API_KEY`, and `[tts].enabled = false`. Do not set a
Google STT key unless Dictation actually selects `stt.google.chirp-3`.
`GOOGLE_AI_API_KEY` is for Gemini Assist/Voice and does not make Google STT a
required provider.

`/readyz` reports mode readiness for load balancers and ignores non-blocking
optional provider probes. `/readyz/strict` keeps the diagnostic all-components
view and may return 503 when an optional provider such as `stt.google` fails.

## Headless deployment contract

Headless installs are configured by `config.toml` plus environment variables.
At process startup, SpeechKit reads TOML and any persisted
`server-settings.json`, then applies deployment env overrides last. This gives
Compose, Kubernetes, Render, Coolify, and other deployers the final say for
runtime credentials without storing secret values in JSON.

| Env var | Effect |
|---|---|
| `SPEECHKIT_SERVER_TOKEN` | Canonical bearer token value for server auth. When set, it overrides a stale persisted `bearer_token_env` that points somewhere else. |
| `SPEECHKIT_SERVER_BEARER_TOKEN_ENV` | Optional custom env var name for the bearer token, for example `DEPLOYMENT_SERVER_TOKEN`. |
| `SPEECHKIT_SERVER_AUTH_MODE` | Optional startup override: `none`, `bearer`, `edge_hmac`, or `bearer_or_edge`. |
| `SPEECHKIT_SERVER_EDGE_AUTH_SECRET_ENV` | Optional custom env var name for the edge-HMAC shared secret. |
| `SPEECHKIT_SERVER_BEARER_ROLE` | Optional role for bearer-token callers, for example `admin` in a single-operator deployment. |
| `SPEECHKIT_PUBLIC_URL` | Canonical public HTTP base used to generate browser-reachable Voice Agent `ws_url` values. |
| `SPEECHKIT_SERVER_PUBLIC_URL` | Compatibility alias for `SPEECHKIT_PUBLIC_URL`; ignored when the canonical variable is also set. |

Authenticated public modes fail startup if the required env credential values
are empty. `auth_mode = "none"` remains limited to local loopback/dev use.

Deployers can confirm what the running process loaded through:

```bash
curl -fsS \
  -H "Authorization: Bearer $SPEECHKIT_SERVER_TOKEN" \
  http://localhost:8080/v1/deployment/status
```

This endpoint is auth-protected and redacted. It accepts the service bearer
credential, local admin auth, or an edge-HMAC identity with role `admin`; normal
edge user identities are rejected. It returns env var names and booleans such
as `auth.bearer_token_set` and `providers.google.api_key.configured`, but never
token or provider key values. The compatibility path
`/api/v1/deployment/status` returns the same payload.

## Installer setup modes

`scripts/install-server.sh` writes a Docker Compose stack plus `.env` into the
target directory. It always installs the same central server. The choice is
only whether setup is operator-driven or immediately ready:

```bash
# First-run setup through /setup.
scripts/install-server.sh \
  --onboarding \
  --public-url https://speechkit.example.com

# Ready-to-run container deploy with self-hosted defaults.
scripts/install-server.sh \
  --ready \
  --public-url https://speechkit.example.com
```

Use `--onboarding` when an operator should configure providers through `/setup`.
Use `--ready` for a container deploy that starts with self-hosted defaults:
whisper.cpp for STT, llama.cpp for the agent LLM, and env-based secrets. Both
paths write the same Compose and environment contract.

The public website also ships a no-clone installer for agents and browser-fetch
workflows:

```bash
curl -fsSL https://speechkit.cc/install-server.sh | sh
```

The installer pulls `ghcr.io/kombifyio/speechkit-server:latest`, which tracks
the most recent stable release.

Automation path: for a fresh local server that an agent should call
immediately, set `SPEECHKIT_SERVER_TOKEN` in the environment before first
start. Otherwise finish the first-run setup through `/v1/server/settings`
before creating Dictation, Assist, or Voice Agent sessions.

## API contract

The complete v1 HTTP and WebSocket surface is documented in
[`docs/server/openapi.v1.yaml`](./openapi.v1.yaml) (OpenAPI 3.1).
Render it with any OpenAPI viewer or generate a typed client with
`openapi-generator`. The canonical browser-facing API prefix is `/api/v1`; the
original `/v1` paths remain available for compatibility.

## Modes

| Mode | Transport | Entry path | Status |
|---|---|---|---|
| Dictation | HTTP POST | `/api/v1/dictation/transcribe` | ships |
| Assist | HTTP POST | `/api/v1/assist/process` | ships |
| Voice Agent | HTTP + WS | `/api/v1/voiceagent/sessions` + `/ws` | ships |
| Voice Agent LiveKit token | HTTP GET | `/api/v1/voiceagent/sessions/{id}/livekit-token` | optional when `[server.livekit]` is enabled |
| Personas API | HTTP CRUD | `/api/v1/personas`, `/api/v1/roles`, `/api/v1/sequences` | ships |
| Health | HTTP GET | `/healthz`, `/readyz` | ships |
| Deployment status | HTTP GET/HEAD | `/api/v1/deployment/status` | auth-protected, redacted |
| Test UI | HTTP GET | `/` | browser smoke-test surface |

The mode set is decided at startup from `[server].modes` in `config.toml` or
the `--modes=` CLI flag. Empty means all three modes. Voice Agent remains a
mode of this server, not a separate image or deployment tier.

Use `voiceagent` as the canonical URL/query/config spelling for server mode
filters and one-shot evidence. The catalog keeps accepting compatibility
aliases such as `voice_agent`, but docs, tests, and functional results use
`voiceagent`.

## Provider support

| Mode | Self-hosted/local | Gemini | OpenAI | Groq |
|---|---|---|---|---|
| Dictation | `whisper.cpp` sidecar or local desktop STT | Google STT when selected | Whisper API | Whisper-compatible Groq STT |
| Assist | local LLM sidecar / Ollama-style endpoint | Gemini text models | Chat Completions / Responses models | Groq chat models |
| Voice Agent | cascaded self-hosted provider for server smoke/dev; no fully local native realtime audio provider in v1 | Gemini Live default | OpenAI Realtime when `[voice_agent].provider = "openai"` | not a native realtime Voice Agent provider |

Missing-key behavior is explicit: HTTP session creation for Voice Agent still
returns a session and ticket while the provider is degraded, so clients can
surface a precise provider error on WebSocket start. `/readyz` reports the
selected provider as degraded, and the WebSocket emits a
`provider_connect_failed` style error when the required key, such as
`GOOGLE_AI_API_KEY` for Gemini Live or `OPENAI_API_KEY` for OpenAI Realtime, is
absent.

## Voice Agent workflows

Voice Agent behavior is authored through the shared behavior catalog:

- `personas` define stable identities, voices, locales, default roles, and
  optional `default_sequence` values.
- `roles` define durable behavior prompts, thinking, VAD, and tool policy.
- `sequences` define ordered workflow steps with instructions, exit criteria,
  optional required tools, and `max_turns`.

The built-in desktop personas (`brainstorming_companion`, `humor_companion`,
`support_companion`) are seeded into the same catalog as personas, roles, and
default sequences. Local installs use `[voice_agent].agent_profile_id` plus the
optional `[voice_agent].agent_sequence_id`; server clients use `persona_id` and
`sequence_id` in the WebSocket `start` frame. If no explicit sequence is sent,
the selected persona's `default_sequence` is used.

At WebSocket startup, the first client text frame must be `start`. It may carry
`persona_id`, `role_id`, `sequence_id`, and `media_transport`. The
`media_transport` default is `websocket`, preserving the original binary PCM
audio frames. `media_transport: "livekit"` keeps the WebSocket as the control
channel and moves microphone/model audio through LiveKit tracks. In v1 that
LiveKit audio path is limited to native realtime PCM providers, Gemini and
OpenAI; cascaded providers stay on WebSocket audio until explicit transcoding
is added. When a sequence is active, the server resolves step 0, connects the
provider with the composed prompt, and emits `sequence_step` with
`status="entered"`.

| `media_transport` | Control frames | Client audio | Model audio | Provider support |
|---|---|---|---|---|
| `websocket` (default) | JSON over SpeechKit WS | binary PCM frames on SpeechKit WS | binary PCM frames on SpeechKit WS | Gemini, OpenAI, cascaded providers |
| `livekit` | JSON over SpeechKit WS | LiveKit track | LiveKit track | Gemini and OpenAI native realtime providers |

Clients can advance the workflow by sending:

```json
{"type":"advance_step","reason":"host"}
```

The server emits `sequence_step` frames for `completed`, `entered`, and
`sequence_completed`. If a step defines `max_turns`, the server advances after
that many completed user turns. Provider tool calls are emitted as `tool_call`;
clients answer with `tool_response`. Provider implementations can update live
instructions natively; otherwise the adapter injects a host-instruction update
as text.

## LiveKit media tokens

SpeechKit can mint LiveKit join tokens and optionally use LiveKit as the Voice
Agent media transport. Configure `[server.livekit]` with `url`, `api_key_env`,
`api_secret_env`, `token_ttl_sec`, and `room_prefix`. When enabled,
`POST /v1/voiceagent/sessions` includes a `livekit` object with `url`, `room`,
`token`, `token_expires_at`, and `participant_identity`. Clients connect media
directly to the Render-hosted LiveKit target using that token while keeping
SpeechKit as the authenticated policy boundary; no host migration is part of
this path.

The WebSocket remains required for all Voice Agent sessions. Send
`{"type":"start","media_transport":"livekit"}` after the upgrade to opt into
LiveKit media. If omitted, audio continues over WebSocket binary frames.
Under LiveKit media, WebSocket binary audio is rejected for the session.

`GET /v1/voiceagent/sessions/{id}/livekit-token` refreshes the token for the
session owner or an admin. The endpoint never returns LiveKit API secrets.

## Authentication

Built-in auth is configured via `[server].auth_mode`:

- `bearer_or_edge` (Kombify production default) — accepts either a static
  service bearer token or trusted Gateway edge auth.
- `bearer` — single static token from
  `$SPEECHKIT_SERVER_TOKEN`.
- `edge_hmac` — trusts HMAC-signed headers from an authenticated edge.
- `none` — local development only. Do not expose a `none` deployment publicly.

`/healthz`, `/readyz`, and `/` are always public so probes and browser smoke
tests can load without credentials. `/setup` is public only during the
first-run bootstrap window. Once a server token or completed onboarding state
exists, `/setup` is an admin UI and requires an authenticated identity with
`role = "admin"` from either `bearer_role = "admin"` or a trusted edge HMAC
header `X-Edge-Role: admin`. Browser requests without valid admin credentials
receive an HTML sign-in-required page; API requests still receive the JSON
`unauthenticated` envelope. Voice Agent WebSocket upgrades at
`/v1/voiceagent/sessions/{id}/ws` and
`/api/v1/voiceagent/sessions/{id}/ws` bypass bearer/edge auth because the
handler validates the short-lived session ticket itself. Browser clients should
use only the `?ticket=` query parameter returned by
`POST /v1/voiceagent/sessions`; the bearer token is for HTTP requests and does
not belong in the browser WebSocket handshake. When auth is enabled, all other
`/api/v1/*` and compatibility `/v1/*` calls require credentials.
The setup page can generate a server API token during onboarding. The generated
value is shown once, loaded into the running server process, and omitted from
`server-settings.json`; persist it in the deployment environment as
`SPEECHKIT_SERVER_TOKEN` so it survives restarts. Clients then send:

```http
Authorization: Bearer <token>
```

Desktop clients should use `[server_connection].auth_mode = "bearer"` against
the operator-managed SpeechKit origin, with the token resolved from
`SPEECHKIT_SERVER_TOKEN`. If a separate edge or custom server is used later,
that edge should translate its own auth into SpeechKit's trusted `X-Edge-*`
HMAC identity headers before forwarding to the origin.

For `edge_hmac`, the edge signs the exact string
`user_id + "\n" + org_id + "\n" + plan + "\n" + role` with the shared secret
from `EDGE_AUTH_SECRET`. `role` may be empty, but the trailing newline remains
part of the signature base.

## Public URL and WebSocket origins

Set `[server].public_url` when the server is behind a reverse proxy or mounted
under a prefix such as `/api`. Voice Agent session creation uses it to generate
the returned `ws_url`; otherwise SpeechKit derives the URL from the sanitized
request host and ignores `X-Forwarded-Host`.

Container/browser deployments should set `SPEECHKIT_PUBLIC_URL` to the
browser-reachable HTTP base, for example `http://localhost:8080` in Docker
Desktop. `SPEECHKIT_SERVER_PUBLIC_URL` is accepted as an alias for compatibility
with older automation, but `SPEECHKIT_PUBLIC_URL` is canonical. If neither is
set and the request reaches the server through a Docker-internal host such as
`speechkit-server:8080`, the returned `ws_url` may be unusable from the
browser; either set the public URL or proxy the WebSocket through your backend.

Voice Agent tickets default to 90 seconds. They are single-use and are not
refreshed in v1; if a microphone permission dialog or user delay lets a ticket
expire, create a new session and use the new `ws_url`.

Browser WebSocket clients must send an `Origin` that exactly matches
`[server].cors_allowed_origins`, or the server rejects the upgrade with `403`.
Native clients that send no `Origin` are allowed. Use `["*"]` only for
explicit local no-auth OSS/dev mode; authenticated server modes reject wildcard
CORS during startup.

If setup auth is switched to self-managed, SpeechKit does not generate a token
or change the current server auth mode; the deployment owner must provide
external auth, a bearer env var, or an explicit local-only `auth_mode = "none"`.
After bootstrap, settings writes through `/v1/server/settings` require an
admin identity; read-only settings snapshots remain redacted unless the request
is authenticated.

## Relation to the Framework kernel

Every substantive operation delegates to the shared Framework packages:

| Server handler | Delegates to |
|---|---|
| Dictation transcribe | `internal/router` + `internal/stt/*` |
| Assist process | `internal/assist.Pipeline` |
| Voice Agent session | `internal/voiceagent.Session` + `internal/voiceagent.GeminiLive` |
| Persona compose | TOML seeds + store-backed overrides + `voiceagent.LiveConfig` |

This keeps the architecture DRY: public Go packages define the reusable
framework surface, internal packages implement the mode runtime once, the
server adapts it to HTTP/WebSocket, and the Windows Client adapts it to desktop
UI workflows.

## Directory layout

```text
cmd/speechkit-server/          # Linux entry point (//go:build linux)
internal/server/core/          # Bootstrap, lifecycle, health
internal/server/middleware/    # Auth, logging, rate-limit, CORS, recover
internal/server/{mode-pkg}/    # Dictation / Assist / VoiceAgent / Persona handlers
deploy/docker/                 # Dockerfile, docker-compose
deploy/config/                 # Reference config.toml
docs/server/                   # This document + API reference
```
