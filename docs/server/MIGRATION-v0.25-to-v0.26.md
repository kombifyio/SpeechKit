# Migration guide — v0.25 → v0.26

v0.25 was a preparation release: the Framework kernel + Windows
reference UI shipped to OSS, but the Server-Target was held back so
it could be hardened in the private development repo. v0.26 is the
first release that includes the Server-Target on the public OSS
side.

This guide walks you through the upgrade. **TL;DR for existing
deployments:** nothing breaks; you just inherit a new optional
deployment surface and a new optional config section. Read on if
you actually want to use them.

## Who needs to read which section

- **You only use the Windows desktop reference UI** —
  Skim *Inherited config additions* and stop. Your install upgrades
  cleanly with no behaviour change.

- **You embed `pkg/speechkit/{dictation,assist,voiceagent}` as a
  Go library** — Read *SDK additions*; nothing on the existing
  surface changed, but `ModeSetting` and `ModeSettings` grew new
  fields you may want to consume.

- **You want to run SpeechKit as a network service** — Read
  *Server-Target onboarding*. This is the new deployment shape.

- **You want to delegate one or more modes from your desktop to a
  remote SpeechKit server** — Read *ModeSource onboarding*. This is
  the hybrid configuration ModeSource ships in 0.26.

## Inherited config additions

`internal/config/config.go` gained two additive sections:

```toml
[server_connection]
enabled = false
url = ""
bearer_token_env = "SPEECHKIT_SERVER_TOKEN"
fallback_to_local = true
request_timeout_sec = 30

[model_selection.dictate]
mode_source = "local"   # new — "local" or "server"

[model_selection.assist]
mode_source = "local"

[model_selection.voice_agent]
mode_source = "local"
```

Both sections are optional. Empty / missing is treated as the
defaults shown above, so a `config.toml` written by v0.25 keeps
behaving identically on v0.26.

## SDK additions

`pkg/speechkit.ModeSetting` gained an optional `ModeSource` field
and `ModeSettings` gained a `ServerConnection` block. Both fields
use `omitempty`, so old SDK consumers see no breakage.

If you fan ModeSettings out to a UI, the new fields are live —
serve them, persist them, and route per `ResolvedModeSource()` on
the Go side. The reference Settings panel under
`frontend/app/src/components/{server-connection-card,
mode-source-section,mode-source-toggle}.tsx` shows the canonical
shape; copy or wrap as needed.

## Server-Target onboarding

v0.26 ships **two images** from the same Go source tree. Pick the
shape that matches your deployment plan:

| Image | What it serves | Use when |
|---|---|---|
| `ghcr.io/kombifyio/speechkit-server` | All three modes — Dictation REST + Assist REST + Voice Agent WS | You want one URL, simple ops, single-pod deploy. Default. |
| `ghcr.io/kombifyio/speechkit-voice` | Voice Agent WS only | You're scaling voice independently from REST traffic, or running voice on beefier nodes (more memory, optional GPU) than REST needs. |

Both images:

- Read `/etc/speechkit/config.toml` (start from
  `deploy/config/server.example.toml`).
- Take secrets from environment variables — `SPEECHKIT_SERVER_TOKEN`
  is the only required one; provider keys are per-mode optional.
- Speak the same v1 contract; clients can't tell which one they're
  talking to (and shouldn't need to).

See the "Backend vs. Voice Server" decision guide in
`docs/server/README.md` if you're unsure.

### Single-pod deploy (full server)

```bash
# 1. Pull:
docker pull ghcr.io/kombifyio/speechkit-server:v0.26.0

# 2. Copy the reference config:
cp deploy/config/server.example.toml /etc/speechkit/config.toml
$EDITOR /etc/speechkit/config.toml

# 3. Export bearer token + provider keys:
export SPEECHKIT_SERVER_TOKEN="…"          # required
export GOOGLE_AI_API_KEY="…"               # optional, enables Voice Agent
export OPENAI_API_KEY="…"                  # optional
export HF_TOKEN="…"                        # optional, enables HF STT

# 4. Run:
docker run -d --name speechkit-server \
  -p 8080:8080 \
  -v /etc/speechkit:/etc/speechkit:ro \
  -v speechkit-models:/var/lib/speechkit/models \
  -e SPEECHKIT_SERVER_TOKEN \
  -e GOOGLE_AI_API_KEY \
  -e OPENAI_API_KEY \
  -e HF_TOKEN \
  ghcr.io/kombifyio/speechkit-server:v0.26.0

# 5. Verify:
curl -fsS http://localhost:8080/healthz
curl -fsS -H "Authorization: Bearer $SPEECHKIT_SERVER_TOKEN" \
  http://localhost:8080/v1/personas
```

### Split deploy (REST tier + Voice tier)

```bash
# Backend tier: speechkit-server with REST modes only.
docker run -d --name speechkit-backend \
  -p 8080:8080 \
  -v /etc/speechkit:/etc/speechkit:ro \
  -e SPEECHKIT_SERVER_TOKEN \
  -e OPENAI_API_KEY \
  -e HF_TOKEN \
  ghcr.io/kombifyio/speechkit-server:v0.26.0 \
  --modes=dictation,assist

# Voice tier: speechkit-voice — voice WS only by default.
docker run -d --name speechkit-voice \
  -p 8090:8080 \
  -v /etc/speechkit:/etc/speechkit:ro \
  -e SPEECHKIT_SERVER_TOKEN \
  -e GOOGLE_AI_API_KEY \
  ghcr.io/kombifyio/speechkit-voice:v0.26.0

# Verify both:
curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8090/healthz
```

The desktop client points at both URLs via per-mode `mode_source`:
Dictation/Assist → `http://backend.example.com:8080`, Voice Agent →
`http://voice.example.com:8090`. The end user sees one app.

`docs/server/openapi.v1.yaml` is the canonical API reference. Load
it in Swagger UI, Stoplight, or Redoc — or feed it to
`openapi-generator` to scaffold a typed client in the language of
your choice. The contract is stable across v0.26.x patch releases
(additive changes only).

For local development:

```bash
# Dev stack (server + Postgres):
docker compose -f deploy/docker/docker-compose.yml up -d

# Test stack (server + whisper.cpp sidecar + e2e client):
docker compose -f deploy/docker/docker-compose.test.yml up
```

The two scripts/test-e2e-local.{sh,ps1} are the canonical "run a
smoke against this" entry points — they spin up the dev stack, wait
for `/healthz`, run `cmd/sk-e2e` against it, and tear down.

## ModeSource onboarding

Once you have a Server-Target running, the Windows desktop UI can
optionally route any subset of its three modes through it. From
the Settings dialog → General tab:

1. **Server Connection card** — paste your server URL, set the
   bearer-token env var name, choose whether transient errors
   should fall back to the local kernel.
2. **Mode Source section** — flip Dictation / Assist / Voice Agent
   from "Local" to "Server" individually.

Alternatively edit `config.toml` directly:

```toml
[server_connection]
enabled = true
url = "https://speechkit.example.com"
bearer_token_env = "SPEECHKIT_SERVER_TOKEN"
fallback_to_local = true
request_timeout_sec = 30

[model_selection.dictate]
primary_profile_id = "stt.local.whispercpp"
mode_source = "local"             # keep dictation snappy locally

[model_selection.assist]
primary_profile_id = "assist.builtin.gemma4-e4b"
mode_source = "server"            # route Assist to the server

[model_selection.voice_agent]
primary_profile_id = "realtime.google.gemini-native-audio"
mode_source = "server"            # route Voice Agent to the server
```

Settings take effect on next app start. The runtime intentionally
doesn't migrate in-flight sessions when `[server_connection]`
changes — that keeps the invariant trivial.

## Things that did NOT change

- `cmd/speechkit/**`, `internal/hotkey/**`, `internal/output/**`,
  `internal/tray/**`, `internal/winapi/**`, the platform-specific
  files under `internal/audio/*_windows_cgo.go`, and the existing
  shape of `pkg/speechkit/{dictation,assist,voiceagent}` are all
  untouched on the wire / interface side. v0.25 callers compile
  unchanged.
- `release.yml`, the existing `dist-windows/**` artifacts, and the
  Wails-built Windows installer pipeline are untouched. v0.26 ships
  alongside the new server pipeline (`release-server-docker.yml`),
  not in place of it.

## If something breaks

- Open an issue at <https://github.com/kombifyio/SpeechKit/issues>
  with the output of `speechkit --version`, the relevant config
  section, and the failing log line.
- The release workflow auto-publishes both the Windows build
  (`release.yml`) and the Linux server image
  (`release-server-docker.yml`); double-check both completed by
  visiting the release page on GitHub.
