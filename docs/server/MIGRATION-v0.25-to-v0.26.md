# Migration guide — v0.25 → v0.26

v0.25 was a preparation release: the Framework kernel and Windows reference UI
shipped to OSS, while the server runtime was hardened in the private development
repo. v0.26 adds the central SpeechKit Server as the network deployment shape.

**TL;DR for existing deployments:** desktop and Go SDK users keep compiling and
running as before. You only need new configuration when you want the Windows
Client or another app to call a remote SpeechKit Server.

## Who needs to read which section

- **Windows desktop reference UI only** — skim *Inherited config additions*.
- **Go library consumers** — read *SDK additions*.
- **Network service operators** — read *SpeechKit Server onboarding*.
- **Hybrid desktop/server users** — read *ModeSource onboarding*.

## Inherited config additions

`internal/config/config.go` gained additive server connection and mode source
fields:

```toml
[server_connection]
enabled = false
url = ""
bearer_token_env = "SPEECHKIT_SERVER_TOKEN"
auth_mode = "bearer"
fallback_to_local = true
request_timeout_sec = 30

[model_selection.dictate]
mode_source = "local"

[model_selection.assist]
mode_source = "local"

[model_selection.voice_agent]
mode_source = "local"
```

Both sections are optional. Empty or missing config keeps v0.25 behavior.

## SDK additions

`pkg/speechkit.ModeSetting` gained an optional `ModeSource` field and
`ModeSettings` gained a `ServerConnection` block. Both fields use `omitempty`,
so existing SDK consumers remain source-compatible.

If you expose ModeSettings through your own UI, persist the new fields and route
per `ResolvedModeSource()` on the Go side. The reference Settings panel under
`frontend/app/src/components` shows the canonical shape.

## SpeechKit Server onboarding

The server publishes one image:

| Image | What it serves | Use when |
|---|---|---|
| `ghcr.io/kombifyio/speechkit-server` | Dictation REST + Assist REST + Voice Agent WS | You want the central Framework server behind one URL. |

The server:

- Reads `/etc/speechkit/config.toml` (start from `deploy/config/server.example.toml`).
- Takes secrets from environment variables.
- Speaks the stable v1 HTTP/WebSocket contract.
- Can narrow modes with `[server].modes` or `--modes`, but Voice Agent remains a mode of this same server.

```bash
docker pull ghcr.io/kombifyio/speechkit-server:v0.26.0

cp deploy/config/server.example.toml /etc/speechkit/config.toml
$EDITOR /etc/speechkit/config.toml

export SPEECHKIT_SERVER_TOKEN="..."
export GOOGLE_AI_API_KEY="..."  # optional, enables Voice Agent
export OPENAI_API_KEY="..."     # optional
export HF_TOKEN="..."           # optional

docker run -d --name speechkit-server \
  -p 8080:8080 \
  -v /etc/speechkit:/etc/speechkit:ro \
  -v speechkit-models:/var/lib/speechkit/models \
  -e SPEECHKIT_SERVER_TOKEN \
  -e GOOGLE_AI_API_KEY \
  -e OPENAI_API_KEY \
  -e HF_TOKEN \
  ghcr.io/kombifyio/speechkit-server:v0.26.0

curl -fsS http://localhost:8080/healthz
```

For local development:

```bash
docker compose -f deploy/docker/docker-compose.yml up -d
docker compose -f deploy/docker/docker-compose.test.yml up
```

## ModeSource onboarding

Once you have a SpeechKit Server running, the Windows desktop UI can optionally
route any subset of its three modes through it. From Settings:

1. **Server Connection card** — paste your server URL, set the bearer-token env
   var name, choose whether transient errors should fall back to the local kernel.
2. **Mode Source section** — flip Dictation / Assist / Voice Agent from `local`
   to `server` individually.

Alternatively edit `config.toml` directly:

```toml
[server_connection]
enabled = true
url = "https://speechkit.example.com"
bearer_token_env = "SPEECHKIT_SERVER_TOKEN"
auth_mode = "bearer"
fallback_to_local = true
request_timeout_sec = 30

[model_selection.dictate]
mode_source = "local"

[model_selection.assist]
mode_source = "server"

[model_selection.voice_agent]
mode_source = "server"
```

Settings take effect on next app start. The runtime intentionally does not
migrate in-flight sessions when `[server_connection]` changes.

## Things that did NOT change

- `cmd/speechkit/**`, `internal/hotkey/**`, `internal/output/**`,
  `internal/tray/**`, `internal/winapi/**`, platform-specific audio files, and
  `pkg/speechkit/{dictation,assist,voiceagent}` are unchanged on the public
  wire/interface side.
- `release.yml`, the Windows artifacts, and the Wails-built Windows installer
  pipeline remain separate from the server image workflow.

## If something breaks

Open an issue at <https://github.com/kombifyio/SpeechKit/issues> with the
output of `speechkit --version`, the relevant config section, and the failing
log line.
