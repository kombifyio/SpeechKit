# SpeechKit Server

The SpeechKit Server is the network deployment form of the SpeechKit Framework.
It exposes Dictation, Assist, and Voice Agent over HTTP/WebSocket while using
the same mode contracts as the Windows app and embeddable Go API.

SpeechKit ships as three products:

| Product | Use it when | Surface |
|---|---|---|
| Local Windows Client | You want the desktop app | Desktop UI, overlay, settings, global hotkeys |
| Go Voice Framework | You embed SpeechKit into a Go product | Go API, mode contracts, provider catalog |
| SpeechKit Server | You expose SpeechKit to remote clients | Containerized HTTP/WebSocket service |

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
export GOOGLE_AI_API_KEY="..."   # optional, enables Voice Agent + Google STT/TTS
export OPENAI_API_KEY="..."      # optional
export HF_TOKEN="..."            # optional, enables HF STT
export SPEECHKIT_SERVER_TOKEN="dev-local-token"
docker compose -f deploy/docker/docker-compose.yml up -d

# 3. Verify.
curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/readyz
```

Secrets can equivalently be injected via `doppler run -- docker compose up`;
the Framework has no opinion on the secret manager, only on where values are
read (`os.Getenv(<name from config>)`).

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
| Personas API | HTTP CRUD | `/api/v1/personas`, `/api/v1/roles`, `/api/v1/sequences` | ships |
| Health | HTTP GET | `/healthz`, `/readyz` | ships |
| Test UI | HTTP GET | `/` | browser smoke-test surface |

The mode set is decided at startup from `[server].modes` in `config.toml` or
the `--modes=` CLI flag. Empty means all three modes. Voice Agent remains a
mode of this server, not a separate image or deployment tier.

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
`persona_id`, `role_id`, and `sequence_id`. When a sequence is active, the
server resolves step 0, connects the provider with the composed prompt, and
emits `sequence_step` with `status="entered"`.

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

## Authentication

Built-in auth is configured via `[server].auth_mode`:

- `bearer` (production default) — single static token from
  `$SPEECHKIT_SERVER_TOKEN`.
- `edge_hmac` — trusts HMAC-signed headers from an authenticated edge.
- `bearer_or_edge` — accepts either bearer or edge auth.
- `none` — local development only. Do not expose a `none` deployment publicly.

`/healthz`, `/readyz`, `/`, and `/setup` are always public so probes, browser
smoke tests, and first-run onboarding can load without credentials. When auth is
enabled, only `/api/v1/*` and compatibility `/v1/*` calls require credentials.
The setup page can generate a server API token during onboarding. The generated
value is shown once, loaded into the running server process, and omitted from
`server-settings.json`; persist it in the deployment environment as
`SPEECHKIT_SERVER_TOKEN` so it survives restarts. Clients then send:

```http
Authorization: Bearer <token>
```

If setup auth is switched to self-managed, SpeechKit does not generate a token
or change the current server auth mode; the deployment owner must provide
external auth, a bearer env var, or an explicit local-only `auth_mode = "none"`.

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
