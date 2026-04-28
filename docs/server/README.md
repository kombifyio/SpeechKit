# SpeechKit Server-Target

The Server-Target is one of three first-class deployment forms for the SpeechKit
Framework, alongside the **Device-Target** (e.g. the Windows reference UI under
`cmd/speechkit`) and the **Local-Target** (direct library/CLI use). All three
share the same Framework kernel under `internal/{ai,assist,dictation,models,
router,shortcuts,stt,tts,vad,voiceagent}`; the Server-Target is purely an HTTP
/ WebSocket adapter that exposes the Framework to remote callers.

> **SpeechKit is the Framework, not the client.** The Windows desktop app is a
> reference UI that demonstrates how to integrate SpeechKit into a device-local
> application. Server-Target exists so teams can embed the same Framework into
> their own products over a network without shipping the Framework inside
> their own binary.

## Deployment at a glance

The Server-Target ships in **two flavours** built from one source tree:

| Binary / image | Modes exposed | Docker target | When to choose |
|---|---|---|---|
| `speechkit-server` (`ghcr.io/kombifyio/speechkit-server`) | Dictation REST + Assist REST + Voice Agent WS | `--target speechkit-server` | Single-pod deployments. One URL, all three modes. Default. |
| `speechkit-voice` (`ghcr.io/kombifyio/speechkit-voice`) | Voice Agent WS only | `--target speechkit-voice` | Run voice on its own pod when scaling stateful WebSocket traffic separately from stateless REST, or when voice needs different node sizing (more memory, optional GPU) than REST. |

Both binaries:

- Are Linux-only (`//go:build linux`).
- Build from `deploy/docker/Dockerfile.server` via the `--target` flag.
- Read `/etc/speechkit/config.toml` (copy `deploy/config/server.example.toml`).
- Take secrets from environment variables — names referenced from config; no
  secret values are ever read from the config file.
- Share the same kernel. The Voice Server is **not** a different product —
  it's a deploy-time decision about which modes the binary serves.

The split is invisible to the desktop: the device-target's
`[server_connection]` + per-mode `mode_source` config (see Phase 4 below)
lets Dictation/Assist hit one URL and Voice Agent hit another.

## Quick start (local dev)

```bash
# 1. Build the full server (all three modes)
docker build -f deploy/docker/Dockerfile.server \
  --target speechkit-server -t speechkit-server:dev .

# Optional: build the voice-only image too
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

# 4. Optional: also bring up a voice-only instance on :8090 for split-
#    deployment parity testing.
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

The mode set the binary serves is decided at startup from
`[server].modes` in `config.toml` or the `--modes=` CLI flag. The two
published images set sensible defaults so most operators never touch
the flag:

- `speechkit-server`: defaults to all three modes.
- `speechkit-voice`: defaults to `voiceagent` only.

## Backend vs. Voice Server — when to split

You don't have to. The full `speechkit-server` image handles all three
modes from one container, and that's the simplest deploy. Pick the
two-pod split when at least one of these is true:

- **Different scaling axes.** Dictation + Assist are stateless REST
  calls; horizontally scale-out is trivial and instances are
  interchangeable. Voice Agent holds long-lived WebSocket sessions
  with state (Persona/Role/Sequence, Gemini Live connection, idle
  watchdog); scaling it is more about pinning sessions to instances
  than spinning up new ones. The two scaling characteristics fight
  each other if you put them on the same pod.
- **Different node sizing.** REST traffic is happy on lean nodes;
  Voice Agent benefits from more memory (concurrent sessions),
  optionally a GPU (the cascaded provider's local LLM), and tighter
  network latency to the upstream voice provider. Putting voice on
  its own pod lets you size each tier honestly.
- **Different blast radius.** A bad Gemini Live deploy that breaks
  Voice Agent shouldn't take Dictation/Assist down. Two pods give
  you that isolation for free.
- **Different release cadence.** Voice Agent providers churn faster
  than STT/Assist (Gemini 3.1 preview, Moshi/Kyutai, the cascaded
  provider). Splitting lets you upgrade the voice tier without
  re-rolling the REST tier.

The device-target is fully prepared for the split: each mode has its
own `mode_source` switch (`local` | `server`) and the
`[server_connection]` URL is shared but you can run two SpeechKit
servers on different hostnames and have the desktop app point
Dictation/Assist at one and Voice Agent at the other. The split is
invisible to end users — they just talk and type.

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

The Server-Target contains **no business logic**. Every substantive operation
delegates to the Framework packages:

| Server handler        | Delegates to                         |
|-----------------------|--------------------------------------|
| Dictation transcribe  | `internal/router` + `internal/stt/*` |
| Assist process        | `internal/assist.Pipeline`           |
| Voice Agent session   | `internal/voiceagent.Session` + `internal/voiceagent.GeminiLive` |
| Persona compose       | reads TOML seeds + store-backed overrides, composes `voiceagent.LiveConfig` |

Adding a fourth deployment target (for instance an Android binding) follows the
same pattern: a thin adapter directory that talks to the same Framework kernel.

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

## What M1 delivers

- Binary boots, parses CLI flags, loads `config.toml`, sets up structured logs.
- HTTP listener with graceful shutdown on SIGINT/SIGTERM.
- `/healthz` (always 200 while the process is up) and `/readyz` (component
  health snapshot; 503 until every registered component is OK).
- Middleware chain: recover → logging → CORS → auth.
- Reference Dockerfile and docker-compose for local development.
- CI workflow that builds, vets, tests, and smoke-tests the image.

Subsequent milestones (M2–M6) flesh out the modes and the deploy pipeline.
