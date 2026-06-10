# Technical Getting Started

This guide is for engineers setting up SpeechKit directly.

## Install the SpeechKit Server

Stable:

```sh
curl -fsSL https://speechkit.cc/install-server.sh | sh
```

The installer writes:

- `docker-compose.yml`
- `config.toml`
- `.env`
- a persistent `data/` directory

Default install directory: `/opt/speechkit`.

The default Docker Compose port binding is `127.0.0.1:8080:8080`. For public
servers, put SpeechKit behind a TLS reverse proxy and run the installer with
`--public-bind` only when the host should listen on all interfaces.

## Verify

```sh
cd /opt/speechkit
docker compose ps
curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/readyz
```

Authenticated API calls use the generated `SPEECHKIT_SERVER_TOKEN` from
`/opt/speechkit/.env`.

## Use the API

OpenAPI:

```text
https://speechkit.cc/api/openapi.v1.yaml
```

Voice Agent AsyncAPI:

```text
https://speechkit.cc/api/asyncapi.v1.yaml
```

Core endpoints:

- `POST /v1/dictation/transcribe`
- `POST /v1/assist/process`
- `POST /v1/voiceagent/sessions`
- `GET /v1/customization/templates`
- `GET /v1/customization/templates/{templateId}/pack`
- `GET /v1/catalog/profiles`
- `GET /v1/config`
- `POST /v1/tts/synthesize`

## Use Go

```sh
go get github.com/kombifyio/SpeechKit
```

Use `pkg/speechkit/client` when you want to call a running SpeechKit Server.

Use the v0.44 SDK packages when you embed SpeechKit directly into another Go
host. Import the smallest public component that matches the job instead of
loading the whole framework:

```sh
go run ./examples/embed-companion
go run ./examples/embed-tts
go run ./examples/embed-event-bus
```

Important packages:

- `pkg/speechkit/wakeword` and `pkg/speechkit/wakeword/sherpa` for wake-word contracts.
- `pkg/speechkit/companion` for hands-free target routing with `TargetAssist`, `TargetVoiceAgent`, or `TargetDictationUIAssisted`.
- `pkg/speechkit/tts` for Provider, Router, Service, and provider-kind routing.
- `pkg/speechkit/assist` for one-shot Assist services, multi-turn skill context, codeword routing, and optional Genkit adapters.
- `pkg/speechkit/dictation` for strict STT-only embedded dictation.
- `pkg/speechkit/customize` for Words, Replacements, Lexicons, Rulesets, and
  portable Customization Packs. Native Templates are curated pack sources
  selected through `active_template_ids`.
- `pkg/speechkit/agentkit` and `pkg/speechkit/voiceagent/live` for embedded realtime Voice Agent hosts.

Hands-Free is an activation and voice-output layer, not a fourth mode. Voice
Companions are usually `TargetAssist`; continuous dialogue companions use
`TargetVoiceAgent`; Dictation uses `TargetDictationUIAssisted` because text
still needs a visible target or explicit commit surface.

## Use MCP

```json
{
  "mcpServers": {
    "speechkit": {
      "command": "speechkit-mcp",
      "args": ["--mode=docs,test"]
    }
  }
}
```
